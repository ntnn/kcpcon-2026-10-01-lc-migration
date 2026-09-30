package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// dialTimeout bounds the initial connection to each etcd.
const dialTimeout = 5 * time.Second

// endpoint is a shard name and the URL of its etcd.
type endpoint struct {
	shard string
	url   string
}

// endpointsFlag collects repeated -etcd shard=url flags in order.
type endpointsFlag []endpoint

func (f *endpointsFlag) String() string {
	parts := make([]string, 0, len(*f))
	for _, ep := range *f {
		parts = append(parts, ep.shard+"="+ep.url)
	}
	return strings.Join(parts, ",")
}

func (f *endpointsFlag) Set(value string) error {
	shard, url, ok := strings.Cut(value, "=")
	if !ok || shard == "" || url == "" {
		return fmt.Errorf("expected shard=url, got %q", value)
	}
	for _, ep := range *f {
		if ep.shard == shard {
			return fmt.Errorf("shard %q given twice", shard)
		}
	}
	*f = append(*f, endpoint{
		shard: shard,
		url:   url,
	})
	return nil
}

func main() {
	var endpoints endpointsFlag
	flag.Var(&endpoints, "etcd", "shard=url of a shard etcd, repeatable, columns follow flag order")
	cluster := flag.String("cluster", "", "logical cluster to show")
	prefix := flag.String("prefix", "/registry/", "etcd storage prefix of the shards")
	flag.Parse()

	if err := run(endpoints, *cluster, *prefix); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(endpoints []endpoint, cluster, prefix string) error {
	if len(endpoints) == 0 {
		return errors.New("at least one -etcd is required")
	}
	if cluster == "" {
		return errors.New("-cluster is required")
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	msgs := make(chan tea.Msg)
	var wg sync.WaitGroup
	var clients []*clientv3.Client
	defer func() {
		for _, client := range clients {
			_ = client.Close()
		}
	}()

	for _, ep := range endpoints {
		client, err := clientv3.New(clientv3.Config{
			Endpoints:   []string{ep.url},
			DialTimeout: dialTimeout,
		})
		if err != nil {
			return fmt.Errorf("connecting to etcd of shard %s: %w", ep.shard, err)
		}
		clients = append(clients, client)

		w := watcher{
			shard:   ep.shard,
			prefix:  prefix,
			cluster: cluster,
			kv:      client,
			watch:   client,
			out:     msgs,
		}
		wg.Go(func() {
			w.run(ctx)
		})
	}

	_, err := tea.NewProgram(newModel(cluster, endpoints, msgs)).Run()
	// Watchers stop before their clients are closed by the deferred calls.
	cancel()
	wg.Wait()
	if err != nil {
		return fmt.Errorf("running ui: %w", err)
	}
	return nil
}
