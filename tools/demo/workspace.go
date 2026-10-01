package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// workspaceFlags selects the migrated workspace and its parent.
type workspaceFlags struct {
	kubeconfig string
	parent     string
	workspace  string
}

func (w *workspaceFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&w.kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "kcp admin kubeconfig")
	fs.StringVar(&w.parent, "parent", "root", "workspace holding the migrated workspace and the migration")
	fs.StringVar(&w.workspace, "workspace", "tenant", "name of the migrated workspace")
}

func (w *workspaceFlags) validate() error {
	if w.kubeconfig == "" {
		return errors.New("-kubeconfig or $KUBECONFIG is required")
	}
	if w.parent == "" || w.workspace == "" {
		return errors.New("-parent and -workspace are required")
	}
	return nil
}

// servers returns the URLs of the parent and the migrated workspace.
func (w *workspaceFlags) servers() (string, string, error) {
	parent, err := w.server(w.parent)
	if err != nil {
		return "", "", err
	}
	return parent, parent + ":" + w.workspace, nil
}

// server returns the URL of the workspace at path.
func (w *workspaceFlags) server(path string) (string, error) {
	cfg, err := restConfig(w.kubeconfig, "")
	if err != nil {
		return "", err
	}
	u, err := url.Parse(cfg.Host)
	if err != nil {
		return "", fmt.Errorf("parsing server %q: %w", cfg.Host, err)
	}
	return u.Scheme + "://" + u.Host + "/clusters/" + path, nil
}

// cluster returns the logical cluster of the migrated workspace.
func (w *workspaceFlags) cluster(ctx context.Context) (string, error) {
	parentServer, _, err := w.servers()
	if err != nil {
		return "", err
	}
	cfg, err := restConfig(w.kubeconfig, parentServer)
	if err != nil {
		return "", err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("creating client: %w", err)
	}
	ws, err := client.Resource(workspaceGVR).Get(ctx, w.workspace, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting workspace %s:%s: %w", w.parent, w.workspace, err)
	}
	cluster, _, err := unstructured.NestedString(ws.Object, "spec", "cluster")
	if err != nil || cluster == "" {
		return "", fmt.Errorf("workspace %s:%s has no spec.cluster", w.parent, w.workspace)
	}
	return cluster, nil
}
