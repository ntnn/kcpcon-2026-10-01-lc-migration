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

// printViewerCommands writes a kubeconfig for the parent workspace to outDir, stalk has no workspace flag,
// and prints the commands to watch the migration from other terminals.
func printViewerCommands(ctx context.Context, w io.Writer, ws workspaceFlags, outDir string) error {
	cluster, err := ws.cluster(ctx)
	if err != nil {
		return err
	}
	parentServer, _, err := ws.servers()
	if err != nil {
		return err
	}

	parentKubeconfig := filepath.Join(outDir, kubeconfigName(ws.parent))
	if err := writeKubeconfig(ws.kubeconfig, parentKubeconfig, parentServer); err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, `# Terminal 1: keys of logical cluster %[1]s in the etcd of every shard
bin/etcdview %[2]s -cluster %[1]s

# Terminal 2: Workspace and LogicalClusterMigration objects
hack/tools/stalk --kubeconfig %[3]s workspaces,logicalclustermigrations
`,
		cluster,
		etcdEndpoints,
		parentKubeconfig,
	)
	if err != nil {
		return fmt.Errorf("printing commands: %w", err)
	}
	return nil
}

// printInspectCommands prints the commands to inspect the workspace and the migration.
func printInspectCommands(w io.Writer, ws workspaceFlags, migration string) error {
	kubeconfig, err := filepath.Abs(ws.kubeconfig)
	if err != nil {
		return fmt.Errorf("resolving kubeconfig path: %w", err)
	}
	_, err = fmt.Fprintf(w, `# Inspect the workspace and the migration
export KUBECONFIG=%[1]s
hack/tools/kcpctl -W :%[2]s get workspace %[3]s -o yaml
hack/tools/kcpctl -W :%[2]s get logicalclustermigration %[4]s -o yaml
`,
		kubeconfig,
		ws.parent,
		ws.workspace,
		migration,
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
