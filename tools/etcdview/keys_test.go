package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitKey(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		key      string
		lc       string
		expected KeyParts
		ok       bool
	}{
		"built-in namespaced": {
			key: "/registry/core/configmaps/lc1/default/cm-1",
			lc:  "lc1",
			expected: KeyParts{
				Group:    "core",
				Resource: "configmaps",
				Cluster:  "lc1",
				Rest:     "default/cm-1",
			},
			ok: true,
		},
		"built-in cluster scoped": {
			key: "/registry/rbac.authorization.k8s.io/clusterrolebindings/lc1/workspace-admin",
			lc:  "lc1",
			expected: KeyParts{
				Group:    "rbac.authorization.k8s.io",
				Resource: "clusterrolebindings",
				Cluster:  "lc1",
				Rest:     "workspace-admin",
			},
			ok: true,
		},
		"custom resource": {
			key: "/registry/core.kcp.io/logicalclusters/customresources/lc1/cluster",
			lc:  "lc1",
			expected: KeyParts{
				Group:    "core.kcp.io",
				Resource: "logicalclusters",
				Segment:  "customresources",
				Cluster:  "lc1",
				Rest:     "cluster",
			},
			ok: true,
		},
		"identity cluster scoped": {
			key: "/registry/wildwest.dev/cowboys/abc123/lc1/billy",
			lc:  "lc1",
			expected: KeyParts{
				Group:    "wildwest.dev",
				Resource: "cowboys",
				Segment:  "abc123",
				Cluster:  "lc1",
				Rest:     "billy",
			},
			ok: true,
		},
		"identity namespaced": {
			key: "/registry/wildwest.dev/cowboys/abc123/lc1/default/billy",
			lc:  "lc1",
			expected: KeyParts{
				Group:    "wildwest.dev",
				Resource: "cowboys",
				Segment:  "abc123",
				Cluster:  "lc1",
				Rest:     "default/billy",
			},
			ok: true,
		},
		"other cluster namespaced parses as identity": {
			key: "/registry/core/configmaps/lc2/default/cm-1",
			lc:  "lc1",
			expected: KeyParts{
				Group:    "core",
				Resource: "configmaps",
				Segment:  "lc2",
				Cluster:  "default",
				Rest:     "cm-1",
			},
			ok: true,
		},
		"too short":                       {key: "/registry/core/configmaps", lc: "lc1"},
		"customresources without cluster": {key: "/registry/g/r/customresources", lc: "lc1"},
		"other prefix":                    {key: "/other/core/configmaps/lc1/default/cm-1", lc: "lc1"},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			parts, ok := SplitKey("/registry/", cas.key, cas.lc)
			require.Equal(t, cas.ok, ok)
			assert.Equal(t, cas.expected, parts)
		})
	}
}
