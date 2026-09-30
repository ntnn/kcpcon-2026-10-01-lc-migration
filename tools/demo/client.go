package main

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// clientQPS and clientBurst lift the client-go defaults of 5 and 10, seeding is request bound.
	clientQPS   = 100
	clientBurst = 200
)

// restConfig loads kubeconfig and points it at server.
// kubeconfig: path, empty uses $KUBECONFIG
// server: workspace URL, e.g. https://127.0.0.1:6443/clusters/<logical cluster>, empty keeps the kubeconfig server
func restConfig(kubeconfig, server string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfig
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	// Set after loading, a server override in clientcmd drops the CA.
	if server != "" {
		cfg.Host = server
	}
	cfg.QPS = clientQPS
	cfg.Burst = clientBurst
	return cfg, nil
}
