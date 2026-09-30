package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
)

func migrate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	var ws workspaceFlags
	ws.register(fs)
	name := fs.String("name", "", "name of the LogicalClusterMigration, defaults to the workspace name")
	destination := fs.String("destination", "shard-1", "destination shard")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}
	if err := ws.validate(); err != nil {
		return err
	}
	if *destination == "" {
		return errors.New("-destination is required")
	}
	if *name == "" {
		*name = ws.workspace
	}

	cluster, err := ws.cluster(ctx)
	if err != nil {
		return err
	}
	server, _, err := ws.servers()
	if err != nil {
		return err
	}
	client, err := dynamicClient(ws.kubeconfig, server)
	if err != nil {
		return err
	}
	migrations := client.Resource(migrationGVR)

	start := time.Now()
	if err := createAndWait(ctx, migrations, migrationObject(*name, cluster, *destination), ""); err != nil {
		return err
	}
	return followMigration(ctx, migrations, *name, start)
}

// followMigration prints progress of the migration name until it completes or fails.
func followMigration(ctx context.Context, migrations dynamic.ResourceInterface, name string, start time.Time) error {
	selector := fields.OneTermEqualSelector("metadata.name", name).String()
	lw := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = selector
			return migrations.List(ctx, options) //nolint:wrapcheck // surfaced by UntilWithSync
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = selector
			return migrations.Watch(ctx, options) //nolint:wrapcheck // surfaced by UntilWithSync
		},
	}

	p := progress{
		start: start,
		now:   time.Now,
	}
	if _, err := watchtools.UntilWithSync(ctx, lw, &unstructured.Unstructured{}, nil, p.update); err != nil {
		return fmt.Errorf("following migration %s: %w", name, err)
	}
	return nil
}

// progressInterval is the minimum time between progress lines within a phase.
const progressInterval = 2 * time.Second

// progress prints changes of a migration status.
type progress struct {
	start   time.Time
	now     func() time.Time
	phase   string
	entries int64
	printed time.Time
}

// update prints phase and copy progress changes.
// Returns true once the migration reached a terminal phase.
func (p *progress) update(event watch.Event) (bool, error) {
	obj, ok := event.Object.(*unstructured.Unstructured)
	if !ok {
		return false, nil
	}
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	entries, _, _ := unstructured.NestedInt64(obj.Object, "status", "entriesCopied")

	now := p.now()
	terminal := phase == "Completed" || phase == "Failed"
	changed := phase != p.phase || (entries != p.entries && now.Sub(p.printed) >= progressInterval)
	if changed || (terminal && entries != p.entries) {
		fmt.Printf("%8s  %-20s  %d entries copied\n", now.Sub(p.start).Round(100*time.Millisecond), phase, entries)
		p.phase = phase
		p.entries = entries
		p.printed = now
	}

	return terminal, nil
}
