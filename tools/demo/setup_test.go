package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCutLast(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in     string
		before string
		after  string
		ok     bool
	}{
		"two segments":   {"root:demo", "root", "demo", true},
		"three segments": {"root:org:demo", "root:org", "demo", true},
		"no separator":   {"root", "", "", false},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			before, after, ok := cutLast(cas.in, ":")
			assert.Equal(t, cas.ok, ok)
			assert.Equal(t, cas.before, before)
			assert.Equal(t, cas.after, after)
		})
	}
}
