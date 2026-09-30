package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
)

// etcdEndpoints are the host ports mapped to the etcd NodePorts in demo/kind.yaml.
const etcdEndpoints = "-etcd root=http://127.0.0.1:23790 -etcd shard-0=http://127.0.0.1:23791 -etcd shard-1=http://127.0.0.1:23792"

func commands(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("commands", flag.ContinueOnError)
	var ws workspaceFlags
	ws.register(fs)
	outDir := fs.String("out", ".kube", "directory for the generated kubeconfigs")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}
	if err := ws.validate(); err != nil {
		return err
	}

	return printViewerCommands(ctx, os.Stdout, ws, *outDir)
}

// printViewerCommands writes kubeconfigs for the parent and the migrated workspace to outDir
// and prints the commands to watch the migration from other terminals.
func printViewerCommands(ctx context.Context, w io.Writer, ws workspaceFlags, outDir string) error {
	cluster, err := ws.cluster(ctx)
	if err != nil {
		return err
	}
	parentServer, workspaceServer, err := ws.servers()
	if err != nil {
		return err
	}

	parentKubeconfig := filepath.Join(outDir, kubeconfigName(ws.parent))
	if err := writeKubeconfig(ws.kubeconfig, parentKubeconfig, parentServer); err != nil {
		return err
	}
	workspaceKubeconfig := filepath.Join(outDir, kubeconfigName(ws.parent+":"+ws.workspace))
	if err := writeKubeconfig(ws.kubeconfig, workspaceKubeconfig, workspaceServer); err != nil {
		return err
	}

	return printCommands(w, commandValues{
		cluster:             cluster,
		parentKubeconfig:    parentKubeconfig,
		workspaceKubeconfig: workspaceKubeconfig,
	})
}

// commandValues are substituted into the printed commands.
type commandValues struct {
	cluster             string
	parentKubeconfig    string
	workspaceKubeconfig string
}

func printCommands(w io.Writer, v commandValues) error {
	_, err := fmt.Fprintf(w, `# Terminal 1: keys of logical cluster %[1]s in the etcd of every shard
bin/etcdview %[2]s -cluster %[1]s

# Terminal 2: Workspace and LogicalClusterMigration objects
hack/tools/stalk --kubeconfig %[3]s workspaces,logicalclustermigrations

# Terminal 3: a client of the workspace, one request per second
while sleep 1; do kubectl --kubeconfig %[4]s get configmap kube-root-ca.crt -o name; done
`,
		v.cluster,
		etcdEndpoints,
		v.parentKubeconfig,
		v.workspaceKubeconfig,
	)
	if err != nil {
		return fmt.Errorf("printing commands: %w", err)
	}
	return nil
}

// writeKubeconfig copies kubeconfig to path with the current context pointing at server.
func writeKubeconfig(kubeconfig, path, server string) error {
	cfg, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %w", err)
	}
	current, ok := cfg.Contexts[cfg.CurrentContext]
	if !ok {
		return fmt.Errorf("kubeconfig %s has no current context", kubeconfig)
	}
	cluster, ok := cfg.Clusters[current.Cluster]
	if !ok {
		return fmt.Errorf("kubeconfig %s has no cluster %q", kubeconfig, current.Cluster)
	}
	cluster.Server = server

	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		return fmt.Errorf("writing kubeconfig %s: %w", path, err)
	}
	return nil
}

// kubeconfigName turns a workspace path into a file name.
func kubeconfigName(path string) string {
	return strings.ReplaceAll(path, ":", "-") + ".kubeconfig"
}
