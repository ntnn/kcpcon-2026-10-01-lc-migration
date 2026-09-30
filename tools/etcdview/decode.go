package main

import (
	"bytes"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	// protobufPrefix marks values written by the Kubernetes protobuf serializer.
	protobufPrefix = []byte("k8s\x00")
	// encryptedPrefix marks values written by an encryption at rest provider.
	encryptedPrefix = []byte("k8s:enc:")
)

// uidEncrypted is shown instead of a UID for values encrypted at rest.
const uidEncrypted = "encrypted"

// objectUID returns metadata.uid of a stored object.
// Built-in types are protobuf, custom resources JSON.
// Returns an empty string if the value cannot be decoded.
func objectUID(value []byte) string {
	if bytes.HasPrefix(value, encryptedPrefix) {
		return uidEncrypted
	}

	var meta metav1.PartialObjectMetadata
	if bytes.HasPrefix(value, protobufPrefix) {
		var unknown runtime.Unknown
		if err := unknown.Unmarshal(value[len(protobufPrefix):]); err != nil {
			return ""
		}
		// Every built-in type carries ObjectMeta as field 1, like PartialObjectMetadata.
		if err := meta.Unmarshal(unknown.Raw); err != nil {
			return ""
		}
		return string(meta.UID)
	}

	if err := json.Unmarshal(value, &meta); err != nil {
		return ""
	}
	return string(meta.UID)
}
