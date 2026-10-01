---
theme: default
title: Logical Cluster Migrations - What, Why and How
author: Nelo-T. Wallus
colorSchema: dark
aspectRatio: 16/9
transition: none
mdc: true
layout: cover
---

# Logical Cluster Migrations

What, Why and How

Nelo-T. Wallus, SAP SE

kcpCon 2026

---
layout: section
---

... and whats missing

---
layout: section
---

# Nomenclature

---

# Nomenclature

- LogicalCluster
  - "Namespace" for kube objects
  - Assigned to a shard
- Workspace
  - Frontmatter for a LogicalCluster
  - Object in the parent LogicalCluster

---
layout: section
---

# What

---

# What

- Move workspaces from one shard to another

---
layout: section
---

# Why

---

# Use cases

- Single shard to multi shard
- Decommissioning a shard, hardware or region changes
- Isolating a busy tenant

---

# Problems

- Shards grow over time
  - Workspaces grow in size
  - Additional workspaces get scheduled
  - Traffic may vary wildly depending on the tenant
- Shards are constrained by the etcd backing them
- Shards are constrained by hardware

---

# Scale the shards

- More resources for shards
- More resources for etcd

---

# Scale the shards

- More resources for shards
- More resources for etcd

## Contra

- Still bounded by etcd limits

---

# Add more shards

- Add additional shards for more capacity

---

# Add more shards

- Add additional shards for more capacity

## Contra

- Busy tenants still disrupt each other
- Still bounded by etcd limits

---

# Copy through the API

- List available APIs
  - for each API enumerate every object
  - get and apply
- Backup/Restore tools like Velero

---

# Copy through the API

- List available APIs
  - for each API enumerate every object
  - get and apply
- Backup/Restore tools like Velero

## Contra

- Restores into a new logical cluster
  - references from other workspaces break
  - Existing consumers of APIExports are lost
- New objects on the target
  - new UID, creationTimestamp, managedFields
  - status and ownerReference UIDs lost
  - admission runs again

---

# Copy through the API

- List available APIs
  - for each API enumerate every object
  - get and apply
- Backup/restore tools like Velero

## Pro

- Simple to implement
- Uses existing tooling

---

# Copy raw etcd data

- copy every object byte-by-byte from origin to destination shard's etcd

---

# Copy raw etcd data

- copy every object byte-by-byte from origin to destination shard's etcd

## Contra

- etcd operations bypassing the API server

---

# Copy raw etcd data

- copy every object byte-by-byte from origin to destination shard's etcd

## Contra

- etcd operations bypassing the API server

## Pro

- preserves identity
- preserves UIDs, ownerReferences, timestamps, ...
- keeps APIExport/-Bindings working

---
layout: section
---

# How

---

# Goals

- Primitive to build upon
- Logical cluster keeps its identity
- Objects retain their UID
- Unrelated workspaces are not interrupted

---

# Non-Goals

- Automatic Rebalancing
- Destination selection

---

# API

- Feature gate `LogicalClusterMigration`
- API `migration.kcp.io/v1alpha1`:

```yaml
apiVersion: migration.kcp.io/v1alpha1
kind: LogicalClusterMigration
metadata:
  name: tenant
spec:
  logicalCluster: 2idl9ngkjhlfjddm
  destinationShard: shard-1
```

---

# Coordination

- Shards update the `LogicalClusterMigration` through the front-proxy
- React to changes received via the cache-server
- Data flows directly from origin to destination through the VW

```mermaid
flowchart LR
  admin[Admin] -->|create| lcm[LogicalClusterMigration]
  lcm -->|replicated| cache[(Cache server)]
  cache -->|informer| origin[Origin shard]
  cache -->|informer| dest[Destination shard]
  origin -->|status via front-proxy| lcm
  dest -->|status via front-proxy| lcm
  dest -->|LogicalClusterDump| origin
```

---
layout: center
---

# Demo: Setup

---

# Phases

```mermaid
flowchart LR
  P[Preparing<br/>origin] --> M[Migrating<br/>destination]
  M --> OC[OriginCleanup<br/>origin]
  OC --> DF[DestinationFinalize<br/>destination]
  DF --> C[Completed]
```

---

# Preparing on Origin

```mermaid
flowchart LR
  P[Preparing<br/>origin] --> M[Migrating<br/>destination]
  M --> OC[OriginCleanup<br/>origin]
  OC --> DF[DestinationFinalize<br/>destination]
  DF --> C[Completed]
  classDef current fill:#4b5563,stroke:#e5e7eb,stroke-width:2px,color:#fff
  class P current
```

- Hides the logical cluster and blocks requests to it
- Cancel its open connections
- Purge it from the shard's informers
- Marks the LC as in-migration with the `internal.kcp.io/migrating` annotation

---

# Migrating on Destination

```mermaid
flowchart LR
  P[Preparing<br/>origin] --> M[Migrating<br/>destination]
  M --> OC[OriginCleanup<br/>origin]
  OC --> DF[DestinationFinalize<br/>destination]
  DF --> C[Completed]
  classDef current fill:#4b5563,stroke:#e5e7eb,stroke-width:2px,color:#fff
  class M current
```

- Hides the logical cluster and blocks requests to it
- Migrates data
  - Requests `LogicalClusterDump` from Origin VW
  - Paginated
- Record maintained in `LogicalClusterMigration`

---

# OriginCleanup on Origin

```mermaid
flowchart LR
  P[Preparing<br/>origin] --> M[Migrating<br/>destination]
  M --> OC[OriginCleanup<br/>origin]
  OC --> DF[DestinationFinalize<br/>destination]
  DF --> C[Completed]
  classDef current fill:#4b5563,stroke:#e5e7eb,stroke-width:2px,color:#fff
  class OC current
```

- `etcd Delete` per group/resource prefix of the logical cluster

---

# DestinationFinalize on Destination

```mermaid
flowchart LR
  P[Preparing<br/>origin] --> M[Migrating<br/>destination]
  M --> OC[OriginCleanup<br/>origin]
  OC --> DF[DestinationFinalize<br/>destination]
  DF --> C[Completed]
  classDef current fill:#4b5563,stroke:#e5e7eb,stroke-width:2px,color:#fff
  class DF current
```

- Unhides the logical cluster
- Recreate bound CRDs
- Allow requests again
- Relist all informers

---

# Completed

```mermaid
flowchart LR
  P[Preparing<br/>origin] --> M[Migrating<br/>destination]
  M --> OC[OriginCleanup<br/>origin]
  OC --> DF[DestinationFinalize<br/>destination]
  DF --> C[Completed]
  classDef current fill:#4b5563,stroke:#e5e7eb,stroke-width:2px,color:#fff
  class C current
```

---
layout: center
---

# Demo: Migration

---

# Requests during a migration

| Request | Response |
|---|---|
| get, list, create, ... | `504 Timeout` |
| watch with resourceVersion | `410 Gone` |
| watch without resourceVersion | `503`, `Retry-After: 1` |

---

# What moved

- Every etcd key of the logical cluster
- UIDs and creationTimestamps unchanged
- resourceVersions changed
- Workspace annotation `core.kcp.io/shard` updated

<!--
Step 7 prints the comparison of bulk-00000 before and after.
Then the kcpctl commands to show the Workspace and the LogicalClusterMigration.
-->

---

# Drawbacks

- Workspace unavailable for the whole copy
- ~0.5 GiB took 2 to 3 minutes in the demo
- clients get disrupted and must relist

---
layout: section
---

# Pitfalls

<!-- 3 min -->

---

# Operations

- Feature gate on all shards
- No cancel, manual intervention required on errors ([#4405](https://github.com/kcp-dev/kcp/issues/4405))
- Migrating back to a former origin stalls ([#4409](https://github.com/kcp-dev/kcp/issues/4409))
- Entire keyspace is scanned ([#4399](https://github.com/kcp-dev/kcp/issues/4399))
- Large workspaces are expensive ([#4399](https://github.com/kcp-dev/kcp/issues/4399))
  - the dump reads 1000 values per etcd request
- Large objects are expensive

<!--
Migrating back: cancelled per-cluster context stays in the context manager after OriginCleanup.
Dump: pkg/server/migrationdump scans the whole keyspace with values, #4399.
-->

---

# Data

- Encryption at rest ([#4408](https://github.com/kcp-dev/kcp/issues/4408))
  - encryption keys must match
  - for aesgcm also the used storage prefix must match
  - destination does not become ready after restart
- etcd leases are dropped ([#4407](https://github.com/kcp-dev/kcp/issues/4407))
  - Events never expire on the destination
- Same kcp version on all shards
  - objects keep the origin's storage version

<!--
aesgcm with /registry vs /shard-2: "cipher: message authentication failed".
KMS v2 from code reading only.
-->

---
layout: section
---

# Improvements

<!-- 2 min -->

---

# Improvements

- Online migration: copy, replay changes, short cutover ([#4410](https://github.com/kcp-dev/kcp/issues/4410))
- Verify copied data before `OriginCleanup` ([#4411](https://github.com/kcp-dev/kcp/issues/4411))
- Dump only the logical cluster's key ranges ([#4399](https://github.com/kcp-dev/kcp/issues/4399))
- Keep etcd leases ([#4407](https://github.com/kcp-dev/kcp/issues/4407))
- Not relisting the whole shard per migration

Epic: [kcp-dev/kcp#3498](https://github.com/kcp-dev/kcp/issues/3498)

---

# Contributing

- Test migrations in your environment
- Pick a ticket and make a PR

---
layout: center
---

# Questions
