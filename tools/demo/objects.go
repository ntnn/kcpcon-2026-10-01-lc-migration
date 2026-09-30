package main

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	workspaceGVR = schema.GroupVersionResource{
		Group:    "tenancy.kcp.io",
		Version:  "v1alpha1",
		Resource: "workspaces",
	}
	apiBindingGVR = schema.GroupVersionResource{
		Group:    "apis.kcp.io",
		Version:  "v1alpha2",
		Resource: "apibindings",
	}
	migrationGVR = schema.GroupVersionResource{
		Group:    "migration.kcp.io",
		Version:  "v1alpha1",
		Resource: "logicalclustermigrations",
	}
)

// migrationBindingName is the name of the APIBinding for migration.kcp.io.
const migrationBindingName = "migration"

// workspaceObject returns a Workspace pinned to shard.
func workspaceObject(name, shard string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": workspaceGVR.GroupVersion().String(),
			"kind":       "Workspace",
			"metadata": map[string]any{
				"name": name,
			},
			"spec": map[string]any{
				"location": map[string]any{
					"selector": map[string]any{
						"matchLabels": map[string]any{
							"name": shard,
						},
					},
				},
			},
		},
	}
}

// migrationBindingObject returns an APIBinding for migration.kcp.io exported from root.
func migrationBindingObject() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiBindingGVR.GroupVersion().String(),
			"kind":       "APIBinding",
			"metadata": map[string]any{
				"name": migrationBindingName,
			},
			"spec": map[string]any{
				"reference": map[string]any{
					"export": map[string]any{
						"path": "root",
						"name": "migration.kcp.io",
					},
				},
			},
		},
	}
}

// migrationObject returns a LogicalClusterMigration of cluster to destination.
func migrationObject(name, cluster, destination string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": migrationGVR.GroupVersion().String(),
			"kind":       "LogicalClusterMigration",
			"metadata": map[string]any{
				"name": name,
			},
			"spec": map[string]any{
				"logicalCluster":   cluster,
				"destinationShard": destination,
			},
		},
	}
}
