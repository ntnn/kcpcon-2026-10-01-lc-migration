package main

import (
	"sort"
	"time"
)

// entry is the state of one key on one shard.
type entry struct {
	// rev is the etcd ModRevision, surfaced as resourceVersion by the shard.
	rev int64
	// lease is the etcd lease ID, 0 without lease.
	lease   int64
	uid     string
	deleted bool
	changed time.Time
}

// row is one key across all shards.
type row struct {
	parts  KeyParts
	shards map[string]entry
}

// present returns the shards currently holding the key.
func (r *row) present(order []string) []string {
	var shards []string
	for _, shard := range order {
		if e, ok := r.shards[shard]; ok && !e.deleted {
			shards = append(shards, shard)
		}
	}
	return shards
}

// item is a key/value observed on a shard.
type item struct {
	// id is the key below the storage prefix, identical across shards.
	id      string
	parts   KeyParts
	rev     int64
	lease   int64
	uid     string
	deleted bool
}

// state holds all rows of the watched logical cluster.
type state struct {
	shards []string
	rows   map[string]*row
	// listed records shards whose initial list was applied, later changes are highlighted.
	listed map[string]bool
}

func newState(shards []string) *state {
	return &state{
		shards: shards,
		rows:   map[string]*row{},
		listed: map[string]bool{},
	}
}

// applyList replaces the entries of shard with items.
func (s *state) applyList(shard string, items []item, now time.Time) {
	highlight := s.listed[shard]
	s.listed[shard] = true

	seen := map[string]bool{}
	for _, it := range items {
		seen[it.id] = true
		s.set(shard, it, now, highlight)
	}

	for id, r := range s.rows {
		e, ok := r.shards[shard]
		if !ok || e.deleted || seen[id] {
			continue
		}
		s.set(shard, item{id: id, parts: r.parts, deleted: true}, now, highlight)
	}
}

// applyEvents applies watch events of shard.
func (s *state) applyEvents(shard string, items []item, now time.Time) {
	for _, it := range items {
		s.set(shard, it, now, true)
	}
}

func (s *state) set(shard string, it item, now time.Time, highlight bool) {
	r, ok := s.rows[it.id]
	if !ok {
		if it.deleted {
			return
		}
		r = &row{
			parts:  it.parts,
			shards: map[string]entry{},
		}
		s.rows[it.id] = r
	}

	prev, existed := r.shards[shard]
	if it.deleted && (!existed || prev.deleted) {
		return
	}

	e := entry{
		rev:     it.rev,
		lease:   it.lease,
		uid:     it.uid,
		deleted: it.deleted,
		changed: prev.changed,
	}
	if it.deleted {
		e = prev
		e.deleted = true
	}
	if highlight && (!existed || prev.rev != it.rev || prev.deleted != it.deleted) {
		e.changed = now
	}
	r.shards[shard] = e
}

// prune drops deleted entries and empty rows last changed before cutoff.
func (s *state) prune(cutoff time.Time) {
	for id, r := range s.rows {
		for shard, e := range r.shards {
			if e.deleted && e.changed.Before(cutoff) {
				delete(r.shards, shard)
			}
		}
		if len(r.shards) == 0 {
			delete(s.rows, id)
		}
	}
}

// sortedIDs returns the row ids in display order.
func (s *state) sortedIDs() []string {
	ids := make([]string, 0, len(s.rows))
	for id := range s.rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// resourceCount is the number of keys of one resource per shard.
type resourceCount struct {
	resource string
	counts   map[string]int
}

// countByResource returns the present keys per group/resource and shard, sorted by resource.
func (s *state) countByResource() []resourceCount {
	byResource := map[string]map[string]int{}
	for _, r := range s.rows {
		resource := r.parts.Group + "/" + r.parts.Resource
		counts, ok := byResource[resource]
		if !ok {
			counts = map[string]int{}
			byResource[resource] = counts
		}
		for _, shard := range r.present(s.shards) {
			counts[shard]++
		}
	}

	ret := make([]resourceCount, 0, len(byResource))
	for resource, counts := range byResource {
		ret = append(ret, resourceCount{
			resource: resource,
			counts:   counts,
		})
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].resource < ret[j].resource
	})
	return ret
}
