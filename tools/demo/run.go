package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// sampleName is the seeded configmap compared before and after the migration.
const sampleName = "bulk-00000"

// errQuit is returned when the presenter quits.
var errQuit = errors.New("quit")

var (
	styleStep   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	stylePrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleDone   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
)

func runDemo(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var ws workspaceFlags
	ws.register(fs)
	var opts seedOptions
	opts.register(fs)
	shard := fs.String("shard", "shard-0", "origin shard of the migrated workspace")
	destination := fs.String("destination", "shard-1", "destination shard")
	outDir := fs.String("out", ".kube", "directory for the generated kubeconfigs")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}
	if err := ws.validate(); err != nil {
		return err
	}
	if err := opts.validate(); err != nil {
		return err
	}

	d, err := newDemo(ws, *shard, *destination)
	if err != nil {
		return err
	}
	st := stepper{
		in:  bufio.NewReader(os.Stdin),
		out: os.Stdout,
	}

	err = d.run(ctx, st, opts, *outDir)
	if errors.Is(err, errQuit) {
		return nil
	}
	return err
}

// demo holds the clients and names of one demo run.
type demo struct {
	ws          workspaceFlags
	shard       string
	destination string

	parentServer    string
	workspaceServer string
}

func newDemo(ws workspaceFlags, shard, destination string) (*demo, error) {
	parentServer, workspaceServer, err := ws.servers()
	if err != nil {
		return nil, err
	}
	return &demo{
		ws:              ws,
		shard:           shard,
		destination:     destination,
		parentServer:    parentServer,
		workspaceServer: workspaceServer,
	}, nil
}

func (d *demo) run(ctx context.Context, st stepper, opts seedOptions, outDir string) error {
	parentClient, err := dynamicClient(d.ws.kubeconfig, d.parentServer)
	if err != nil {
		return err
	}

	binding := migrationBindingObject()
	if err := st.step(fmt.Sprintf("Bind the migration API in %s", d.ws.parent), binding); err != nil {
		return err
	}
	if err := createAndWait(ctx, parentClient.Resource(apiBindingGVR), binding, "Bound"); err != nil {
		return err
	}
	if err := st.done("APIBinding %s is Bound", migrationBindingName); err != nil {
		return err
	}

	tenant := workspaceObject(d.ws.workspace, d.shard)
	if err := st.step(fmt.Sprintf("Create workspace %s:%s on shard %s", d.ws.parent, d.ws.workspace, d.shard), tenant); err != nil {
		return err
	}
	if err := createAndWait(ctx, parentClient.Resource(workspaceGVR), tenant, "Ready"); err != nil {
		return err
	}
	cluster, err := d.ws.cluster(ctx)
	if err != nil {
		return err
	}
	if err := st.done("workspace %s:%s is Ready, logical cluster %s", d.ws.parent, d.ws.workspace, cluster); err != nil {
		return err
	}

	sample := seedConfigMap(sampleName, fmt.Sprintf("<%d bytes of random data>", opts.size))
	title := fmt.Sprintf("Fill %s:%s with %d configmaps and %d secrets like this", d.ws.parent, d.ws.workspace, opts.configmaps, opts.secrets)
	if err := st.step(title, sample); err != nil {
		return err
	}
	if err := seedWorkspace(ctx, d.ws.kubeconfig, d.workspaceServer, opts); err != nil {
		return err
	}

	if err := st.step("Run these in other terminals", nil); err != nil {
		return err
	}
	if err := printViewerCommands(ctx, st.out, d.ws, outDir); err != nil {
		return err
	}
	if err := st.wait("continue"); err != nil {
		return err
	}

	before, err := d.snapshot(ctx)
	if err != nil {
		return err
	}

	migration := migrationObject(d.ws.workspace, cluster, d.destination)
	if err := st.step(fmt.Sprintf("Migrate %s:%s to shard %s", d.ws.parent, d.ws.workspace, d.destination), migration); err != nil {
		return err
	}
	start := time.Now()
	migrations := parentClient.Resource(migrationGVR)
	if err := createAndWait(ctx, migrations, migration, ""); err != nil {
		return err
	}
	if err := followMigration(ctx, migrations, migration.GetName(), start); err != nil {
		return err
	}

	if err := st.step("Compare before and after", nil); err != nil {
		return err
	}
	after, err := d.snapshot(ctx)
	if err != nil {
		return err
	}
	if err := printComparison(st.out, before, after); err != nil {
		return err
	}
	if err := st.print("\n"); err != nil {
		return err
	}
	return printInspectCommands(st.out, d.ws, migration.GetName())
}

// snapshot is the state compared before and after the migration.
type snapshot struct {
	shard string
	uid   string
	rev   string
}

func (d *demo) snapshot(ctx context.Context) (snapshot, error) {
	parentClient, err := dynamicClient(d.ws.kubeconfig, d.parentServer)
	if err != nil {
		return snapshot{}, err
	}
	ws, err := parentClient.Resource(workspaceGVR).Get(ctx, d.ws.workspace, metav1.GetOptions{})
	if err != nil {
		return snapshot{}, fmt.Errorf("getting workspace %s: %w", d.ws.workspace, err)
	}

	cfg, err := restConfig(d.ws.kubeconfig, d.workspaceServer)
	if err != nil {
		return snapshot{}, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return snapshot{}, fmt.Errorf("creating client: %w", err)
	}
	cm, err := client.CoreV1().ConfigMaps(seedNamespace).Get(ctx, sampleName, metav1.GetOptions{})
	if err != nil {
		return snapshot{}, fmt.Errorf("getting configmap %s: %w", sampleName, err)
	}

	return snapshot{
		shard: ws.GetAnnotations()["core.kcp.io/shard"],
		uid:   string(cm.UID),
		rev:   cm.ResourceVersion,
	}, nil
}

func printComparison(w io.Writer, before, after snapshot) error {
	_, err := fmt.Fprintf(w, "%-40s %-38s %s\n%-40s %-38s %s\n%-40s %-38s %s\n%-40s %-38s %s\n",
		"", "before", "after",
		"workspace annotation core.kcp.io/shard", before.shard, after.shard,
		"configmap "+sampleName+" uid", before.uid, after.uid,
		"configmap "+sampleName+" resourceVersion", before.rev, after.rev,
	)
	if err != nil {
		return fmt.Errorf("printing comparison: %w", err)
	}
	return nil
}

// stepper shows each step and waits for the presenter.
type stepper struct {
	in  *bufio.Reader
	out io.Writer
}

// step prints title and obj as YAML, then waits for enter.
// obj: nil prints only the title
func (st stepper) step(title string, obj any) error {
	if err := st.print("\n" + styleStep.Render("== "+title) + "\n"); err != nil {
		return err
	}
	if obj == nil {
		return nil
	}

	b, err := yaml.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", title, err)
	}
	if err := st.print(string(b)); err != nil {
		return err
	}
	return st.wait("apply")
}

// wait blocks until enter, q quits.
func (st stepper) wait(action string) error {
	if err := st.print(stylePrompt.Render(fmt.Sprintf("[enter] %s  [q] quit ", action))); err != nil {
		return err
	}
	line, err := st.in.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	if strings.TrimSpace(line) == "q" {
		return errQuit
	}
	return nil
}

func (st stepper) done(format string, args ...any) error {
	return st.print(styleDone.Render(fmt.Sprintf(format, args...)) + "\n")
}

func (st stepper) print(s string) error {
	if _, err := io.WriteString(st.out, s); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}
