package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// errWatchClosed is returned when etcd ends a watch without an error.
var errWatchClosed = errors.New("watch closed")

// listPageSize is the number of keys per list request, values of kcp objects are up to 1 MiB.
const listPageSize = 100

// retryInterval is the wait before listing again after a failed list or watch.
const retryInterval = 2 * time.Second

// listMsg carries the full state of a shard.
type listMsg struct {
	shard string
	items []item
}

// eventsMsg carries changes on a shard.
type eventsMsg struct {
	shard string
	items []item
}

// statusMsg reports the connection state of a shard.
type statusMsg struct {
	shard  string
	status string
	err    error
}

// watcher streams the keys of one logical cluster in one shard etcd.
type watcher struct {
	shard   string
	prefix  string
	cluster string
	kv      clientv3.KV
	watch   clientv3.Watcher
	out     chan<- tea.Msg
}

// run lists and watches until ctx is done, starting over on errors.
func (w watcher) run(ctx context.Context) {
	for ctx.Err() == nil {
		rev, err := w.list(ctx)
		if err == nil {
			w.send(ctx, statusMsg{
				shard:  w.shard,
				status: fmt.Sprintf("watching from revision %d", rev+1),
			})
			err = w.watchFrom(ctx, rev+1)
		}
		if ctx.Err() != nil {
			return
		}
		w.send(ctx, statusMsg{
			shard: w.shard,
			err:   err,
		})

		select {
		case <-ctx.Done():
			return
		case <-time.After(retryInterval):
		}
	}
}

// list sends all keys of the logical cluster and returns the revision they were read at.
func (w watcher) list(ctx context.Context) (int64, error) {
	var items []item
	var rev int64
	key := w.prefix
	end := clientv3.GetPrefixRangeEnd(w.prefix)
	for {
		opts := []clientv3.OpOption{
			clientv3.WithRange(end),
			clientv3.WithLimit(listPageSize),
		}
		// Later pages read at the revision of the first page for a consistent list.
		if rev != 0 {
			opts = append(opts, clientv3.WithRev(rev))
		}
		resp, err := w.kv.Get(ctx, key, opts...)
		if err != nil {
			return 0, fmt.Errorf("listing %s: %w", w.prefix, err)
		}
		if rev == 0 {
			rev = resp.Header.Revision
		}

		for _, kv := range resp.Kvs {
			it, ok := w.item(string(kv.Key))
			if !ok {
				continue
			}
			it.rev = kv.ModRevision
			it.lease = kv.Lease
			it.uid = objectUID(kv.Value)
			items = append(items, it)
		}

		if !resp.More || len(resp.Kvs) == 0 {
			break
		}
		key = string(resp.Kvs[len(resp.Kvs)-1].Key) + "\x00"
	}

	w.send(ctx, listMsg{
		shard: w.shard,
		items: items,
	})
	return rev, nil
}

// watchFrom sends changes starting at rev until ctx is done or the watch fails.
func (w watcher) watchFrom(ctx context.Context, rev int64) error {
	ch := w.watch.Watch(ctx, w.prefix, clientv3.WithPrefix(), clientv3.WithRev(rev))
	for resp := range ch {
		if err := resp.Err(); err != nil {
			return fmt.Errorf("watching %s: %w", w.prefix, err)
		}

		var items []item
		for _, ev := range resp.Events {
			it, ok := w.item(string(ev.Kv.Key))
			if !ok {
				continue
			}
			it.rev = ev.Kv.ModRevision
			if ev.Type == clientv3.EventTypeDelete {
				it.deleted = true
			} else {
				it.lease = ev.Kv.Lease
				it.uid = objectUID(ev.Kv.Value)
			}
			items = append(items, it)
		}
		if len(items) == 0 {
			continue
		}

		w.send(ctx, eventsMsg{
			shard: w.shard,
			items: items,
		})
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("watching %s: %w", w.prefix, err)
	}
	return fmt.Errorf("watching %s: %w", w.prefix, errWatchClosed)
}

// item parses key and reports whether it belongs to the watched logical cluster.
func (w watcher) item(key string) (item, bool) {
	parts, ok := SplitKey(w.prefix, key, w.cluster)
	if !ok || parts.Cluster != w.cluster {
		return item{}, false
	}
	return item{
		id:    strings.TrimPrefix(key, w.prefix),
		parts: parts,
	}, true
}

func (w watcher) send(ctx context.Context, msg tea.Msg) {
	select {
	case <-ctx.Done():
	case w.out <- msg:
	}
}

// waitForMsg returns a command delivering the next watcher message.
func waitForMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}
