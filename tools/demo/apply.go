package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
)

const (
	// readyInterval and readyTimeout bound waiting for workspaces and bindings.
	readyInterval = time.Second
	readyTimeout  = 2 * time.Minute
)

// createAndWait creates obj, an existing object is kept, and waits for status.phase to equal phase.
// phase: empty skips waiting
func createAndWait(ctx context.Context, resource dynamic.ResourceInterface, obj *unstructured.Unstructured, phase string) error {
	what := obj.GetKind() + " " + obj.GetName()
	if _, err := resource.Create(ctx, obj, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating %s: %w", what, err)
	}
	if phase == "" {
		return nil
	}

	var last error
	err := wait.PollUntilContextTimeout(ctx, readyInterval, readyTimeout, true, func(ctx context.Context) (bool, error) {
		current, err := resource.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			last = err
			return false, nil
		}
		got, _, _ := unstructured.NestedString(current.Object, "status", "phase")
		last = fmt.Errorf("phase is %q", got)
		return got == phase, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for %s to be %s: %w", what, phase, errors.Join(err, last))
	}
	return nil
}

func dynamicClient(kubeconfig, server string) (*dynamic.DynamicClient, error) {
	cfg, err := restConfig(kubeconfig, server)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}
	return client, nil
}
