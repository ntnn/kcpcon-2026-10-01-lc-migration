package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestObjectUID(t *testing.T) {
	t.Parallel()

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cm",
			UID:  "uid-proto",
		},
		Data: map[string]string{"k": "v"},
	}
	raw, err := cm.Marshal()
	require.NoError(t, err)
	unknown := runtime.Unknown{
		TypeMeta: runtime.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		Raw: raw,
	}
	wrapped, err := unknown.Marshal()
	require.NoError(t, err)

	cases := map[string]struct {
		value    []byte
		expected string
	}{
		"protobuf":  {append(append([]byte{}, protobufPrefix...), wrapped...), "uid-proto"},
		"json":      {[]byte(`{"apiVersion":"wildwest.dev/v1","kind":"Cowboy","metadata":{"name":"billy","uid":"uid-json"}}`), "uid-json"},
		"encrypted": {[]byte("k8s:enc:aesgcm:v1:key1:garbage"), uidEncrypted},
		"garbage":   {[]byte("not an object"), ""},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, cas.expected, objectUID(cas.value))
		})
	}
}
