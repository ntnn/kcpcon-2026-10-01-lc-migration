package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

func TestProgress_update(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		phase    string
		expected bool
	}{
		"no phase yet": {"", false},
		"migrating":    {"Migrating", false},
		"completed":    {"Completed", true},
		"failed":       {"Failed", true},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			obj := &unstructured.Unstructured{Object: map[string]any{}}
			require.NoError(t, unstructured.SetNestedField(obj.Object, cas.phase, "status", "phase"))

			p := progress{start: time.Now(), now: time.Now}
			done, err := p.update(watch.Event{Type: watch.Modified, Object: obj})
			require.NoError(t, err)
			assert.Equal(t, cas.expected, done)
		})
	}
}
