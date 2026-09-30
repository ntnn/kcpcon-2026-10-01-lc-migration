package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestState(t *testing.T) {
	t.Parallel()

	start := time.Unix(1000, 0)
	later := start.Add(time.Minute)
	cm := item{
		id: "core/configmaps/lc1/default/cm-1",
		parts: KeyParts{
			Group:    "core",
			Resource: "configmaps",
			Cluster:  "lc1",
			Rest:     "default/cm-1",
		},
		rev: 10,
		uid: "uid-1",
	}

	s := newState([]string{"shard-0", "shard-1"})
	s.applyList("shard-0", []item{cm}, start)
	s.applyList("shard-1", nil, start)

	r := s.rows[cm.id]
	require.NotNil(t, r)
	assert.True(t, r.shards["shard-0"].changed.IsZero(), "initial list must not highlight")

	copied := cm
	copied.rev = 3
	s.applyEvents("shard-1", []item{copied}, later)
	assert.Equal(t, []string{"shard-0", "shard-1"}, r.present(s.shards))
	assert.Equal(t, later, r.shards["shard-1"].changed)

	deleted := cm
	deleted.deleted = true
	s.applyEvents("shard-0", []item{deleted}, later)
	assert.Equal(t, []string{"shard-1"}, r.present(s.shards))
	assert.Equal(t, "uid-1", r.shards["shard-0"].uid, "deleted entry keeps its last state")

	s.prune(later.Add(time.Second))
	_, ok := r.shards["shard-0"]
	assert.False(t, ok, "deleted entry is pruned after cutoff")
	assert.Equal(t, 1, s.countByResource()[0].counts["shard-1"])

	s.applyList("shard-1", nil, later)
	assert.True(t, r.shards["shard-1"].deleted, "relist marks missing keys deleted")
	s.prune(later.Add(time.Second))
	assert.Empty(t, s.rows)
}
