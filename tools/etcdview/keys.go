/*
Copyright 2026 The kcp Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Adapted from github.com/kcp-dev/kcp pkg/etcd/keys.go.

package main

import (
	"strings"
)

// KeyParts holds the parsed components of an etcd key produced by SplitKey.
type KeyParts struct {
	Group    string
	Resource string
	// Segment is "customresources", an identity hash, or "" for built-in resources.
	Segment string
	Cluster string
	// Rest is everything after the cluster segment: [<namespace>/]<name>.
	Rest string
}

// SplitKey parses an etcd key into its structural components.
// lc resolves the ambiguous 5 segment layout.
// Returns false only if the key could not be parsed, not if it belongs to another logical cluster.
func SplitKey(prefix, key, lc string) (KeyParts, bool) {
	if !strings.HasPrefix(key, prefix) {
		return KeyParts{}, false
	}
	rest := strings.TrimPrefix(key, prefix)
	rest = strings.TrimPrefix(rest, "/")

	parts := strings.SplitN(rest, "/", 6)

	if len(parts) < 3 {
		return KeyParts{}, false
	}
	ret := KeyParts{
		Group:    parts[0],
		Resource: parts[1],
	}

	if parts[2] == "customresources" {
		// group/resource/"customresources"/cluster/[namespace/]name
		if len(parts) < 4 {
			return KeyParts{}, false
		}
		ret.Segment = parts[2]
		ret.Cluster = parts[3]
		ret.Rest = strings.Join(parts[4:], "/")
		return ret, true
	}

	if len(parts) == 6 {
		// group/resource/identity/cluster/namespace/name
		ret.Segment = parts[2]
		ret.Cluster = parts[3]
		ret.Rest = strings.Join(parts[4:], "/")
		return ret, true
	}

	if len(parts) == 5 {
		if parts[2] == lc {
			// group/resource/cluster/namespace/name
			ret.Cluster = parts[2]
			ret.Rest = strings.Join(parts[3:], "/")
			return ret, true
		}
		// group/resource/identity/cluster/name
		ret.Segment = parts[2]
		ret.Cluster = parts[3]
		ret.Rest = strings.Join(parts[4:], "/")
		return ret, true
	}

	// group/resource/cluster/[namespace/]name
	ret.Cluster = parts[2]
	ret.Rest = strings.Join(parts[3:], "/")
	return ret, true
}
