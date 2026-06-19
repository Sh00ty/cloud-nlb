# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Cloud NLB is a cloud-native L3/L4 load balancer consisting of three main components:
- **Control Plane (CPL)**: gRPC API service with reconciliation logic for TargetGroup placement
- **Healthcheck (HC)**: Stateless cluster for endpoint health monitoring with gossip/consistent hashing
- **NLB Agent**: Runs alongside data-plane nodes, managing desired vs actual state reconciliation

## Build Commands

### Prerequisites
- Go workspace with modules: control-plane, healthcheck, nlb-agent, tools
- Docker/colima for container builds
- Kubernetes cluster for deployment

### Building Components
```bash
# Build all components
make build

# Build individual components
make build-hc      # Build healthcheck service
make build-cp      # Build control-plane (API + reconciler)
make build-agent   # Build NLB agent
make build-tools   # Build test tools (server/client)
```

### Container Builds
Components use Docker with Go caching:
- Control Plane: `control-plane/docker/Dockerfile.api` and `Dockerfile.reconciler`
- NLB Agent: `nlb-agent/docker/Dockerfile.agent`
- Built via: `colima nerdctl -- --namespace k8s.io build -t <image>:dev . -f <dockerfile>`

### Deployment
```bash
# Deploy full stack
make deploy

# Deploy individual components
make deploy-hc     # Deploy healthcheck with PostgreSQL/Redpanda
make deploy-cp     # Deploy control-plane API and reconciler
make deploy-agent  # Deploy NLB agent as StatefulSet
make deploy-tools  # Deploy test server/client
make deploy-obs    # Deploy observability stack (Prometheus/Grafana)
```

### Development Status
```bash
make status        # Check all pods and services across namespaces
make clean         # Delete all deployed components
```

## Architecture

### Data Flow
1. **TargetGroup Upsert** → Control Plane stores in etcd → Reconciler distributes to agents
2. **Endpoints Health** → Healthcheck cluster → PostgreSQL → CDC (Redpanda) → Kafka → Agents
3. **Agent Reconciliation** → Apply desired state to VPP/IPVS backends with health-aware logic

### Versioning Model
- `SpecVersion`: Monotonically increasing per-TargetGroup spec changes (etcd CAS-increment)
- `EndpointsVersion`: Monotonically increasing per-TargetGroup endpoint sequence changes (etcd CAS-increment)
- `PlacementVersion`: Per-data-plane node placement generation, incremented on any TG movement
- `generation`: Agent's health status generation for triggering reconciliation

### Data Structures
**TargetGroup**: Contains spec, endpoints snapshot, and changelog with version tracking
- `Spec`: Protocol (TCP/UDP), port, virtual IP
- `EndpointsChangelog`: Ordered list of ADD/REMOVE endpoint events
- `EndpointsSnapshot`: Complete endpoint set (partial implementation)

**Placement**: Per-node TG assignment with version
- `Version`: Placement generation number, increments on any change
- `TargetGroups`: Set of TG IDs assigned to this data-plane node

**DataPlaneState**: Node status for reconciliation decisions
- Status: alive | dead | drained | unknown
- Dead status processed with delay to prevent flapping

### Communication Patterns
- **Control Plane ↔ Agent**: gRPC long-poll (`StreamDataPlaneAssignments`) with diff-based updates
- **Healthcheck → Agent**: At-least-once delivery via Kafka (Debezium CDC from PostgreSQL)
- **Service Discovery**: gossip/SWIM protocol via hashicorp/memberlist for hc-worker StatefulSet集群
  - Used for fault detection and membership events (NodeLeave, NodeJoin, NodeUpdate)
  - Events trigger vshard rebalancing via consistent hashing (dxhash)
  - hc-worker pods use StatefulSet with stable network identities

### Key Components Interaction

#### Control Plane Runtime (`control-plane/internal/api/apiruntime/`)
- In-memory caches for dataPlaneCache and targetGroupCache
- Notifier system for per-node/per-TG subscriptions with long-poll deadlines
- Diff algorithm: placement changes + version-aware endpoint updates

#### Reconciler Logic (`control-plane/internal/reconciler/`)
- Leader election via etcd (`/cloud-nlb/reconciler/all-targets`)
- Events: TargetGroupCreated, DataPlaneAlive/Dead, RunReconcile
- Two-step placement algorithm with movement minimization

#### TargetGroup Placement Algorithm
Two-step reconciliation process that balances TGs across alive data-plane nodes:

**Step 1: calculateDesiredLazy** - Replace TGs from dead nodes and handle under-replication
- Identify TGs needing replacement from dead node placements
- Calculate under-replicated TGs based on `targetGroupsReplicationFactor`
- Randomly distribute missing TG replicas to alive nodes (load-aware version increment)

**Step 2: fixDataplanePlacements** - Load balancing with two-pointer technique
- Calculate target load per node: `(total TGs × replicationFactor) / aliveNodes`
- Sort nodes by load (fewest TGs first), use deterministic tie-breaking by nodeID
- Two-pointer algorithm moves TGs from over-loaded (right) to under-loaded (left) nodes
- Only moves TGs that don't already exist on destination (avoids duplicates)
- Each successful move increments placement versions on both source and destination

**Key Algorithm Properties:**
- Maintains replication factor for each TG across alive nodes
- Balances load approximately evenly across data-plane nodes
- Minimizes number of TG movements during reconciliation
- Respects current placement version numbers for version tracking
- Handles dead node detection with delayed processing to prevent flapping

#### Healthcheck Sharding (`healthcheck/internal/sharder/`)
- Two-level consistent hashing using **dxhash algorithm**: endpoint → vshard → HC node
- vshardCh: mapping endpoint → Vshard via `xxhash(targetKey)` + `dxhash.GetWithOffset(key, 0)`
- nodeVShardsSharder: mapping Vshard → HC nodes using `dxhash.GetWithOffset(vshard, i)` for replication
- Memberlist events trigger vshard ownership recalculation
- vshard replication factor determines endpoint distribution

#### Agent Reconciliation (`nlb-agent/internal/reconciler/`)
Worker pool architecture with sharding, health-aware endpoint management, and error handling.

**Worker Pool and Task Distribution:**
- Worker count: `concurrency` parameter (configurable)
- Sharding: `xxhash(Sum64(tgID)) % uint64(len(workers))` for consistent routing
- Per-worker queue size: 1 (bounded to prevent overload)
- Max attempts: configurable via `maxReconcileAttempts` (default 3), excessive tasks dropped
- Pending tracking: mutex-protected map prevents duplicate enqueue per TG
- Periodic full reconcile: `forceReconcileInterval` ticker triggers all TG reconciliations

**Reconciliation Process (`reconcileTargetGroup`):**
1. **Spec reconciliation** (`reconcileSpec`):
   - Compare `desiredSpec.Version` vs `actualSpec.Version`
   - Apply to VPP backend via `vpp.ApplySpec()`
   - Persist actual state with `stor.SetActualSpec()`

2. **Endpoints reconciliation** (`reconcileEndpoints`):
   - Triggered when `EndpointsVersion` changes OR `generation` (health status) changes
   - Health status cache: `endpointStatusCache.SetTgEndpointsVerState()`
   - Diff calculation with health filtering (see below)

**Endpoint Diff with Health Filtering (`getEndpointsDiff`):**
- Builds hash maps for desired and actual endpoints (key = ip+port)
- **For desired endpoints:** Check health via `endpointsStatusManager.GetEndpointsStatus()`
  - Skip unhealthy endpoints entirely (not added)
  - If endpoint exists but weight changed: remove old, add new
- **For actual endpoints:** Remove if not in desired OR if unhealthy
- Weight updates implemented as delete+add (VPP limitation)

**Cleanup on TG Removal (`removeTG`):**
- Remove all endpoints from VPP backend
- Remove spec from VPP
- Delete actual state from persistent storage

**Storage Interface (`Storage`):**
- `GetDesiredSpec/SetDesiredSpec`: desired state from Control Plane
- `GetDesiredEndpoints/SetDesiredEndpoints`: desired endpoints with changelog application
- `GetActualSpec/SetActualSpec`: actual state applied to VPP
- `GetActualEndpoints/SetActualEndpoints`: actual endpoints in VPP
- `DeleteActual`: cleanup on TG removal
- Changelog application: `constructEndpoints()` merges stored + snapshot + changelog events

### etcd Keyspace Layout
Base: `/cloud-nlb-registry`
- Target Groups: `spec/timestamp/`, `spec/desired/`, `spec/current/`
- Endpoints: `endpoints/timestamp/`, `endpoints/changelog/`, `endpoints/compacted/`
- Data Planes: `placements/<nodeId>`, `statuses/<nodeId>`
- Leadership: `/cloud-nlb/reconciler/all-targets`

### Backend Interfaces
- VPP Manager: `ApplySpec`, `AddEndpoints`, `RemoveEndpoints`, `RemoveSpec`
- Available backends: ipvsvpp (IPVS), stubvpp (mock), testgovpp (Go implementation)

## Development Notes

### Messaging Protocols
- Control Plane: `control-plane/api/proto/cplpbv1/control-plane.proto`
- Healthcheck: `healthcheck/api/proto/hcpbv1/lbhealth.proto`

### Environment Setup
Components use `.env` files and `github.com/vrischmann/envconfig` for configuration.
Key configs: etcd endpoints, PostgreSQL connection, Kafka brokers, gossip settings.

### Observability
- Prometheus metrics exposed on `:9090` across components
- Zerolog structured logging with levels Info/Warn/Error
- Grafana dashboards in `obs/grafana/dashboards/`

## Testing and Chaos Engineering

### Test Tools (`tools/`)

**Test Server (`tools/cmd/testserver/`):**
Multi-purpose server for load balancer validation with health check and traffic endpoints.
- Ports: 8080 (probes), 8081 (metrics), 8090 (health checks), 10000 (traffic)
- Endpoints:
  - `/health/`: Health check receiver with per-user-agent and global interval metrics
  - `/ping`: Simple ping response with X-Served-By header
  - `/`: Full JSON response with hostname, pod_ip, request counter, timestamp
- Metrics: `testserver_hc_requests_total`, `testserver_hc_coverage_interval_seconds`, `testserver_traffic_requests_total`

**Test Client (`tools/cmd/testclient/`):**
Configurable load generator for NLB testing.
- Flags: `-url`, `-c` (concurrency), `-qps`, `-timeout`, `-duration`, `-jitter`, `-keepalive`
- Pacing: Per-worker ticker for QPS control, jitter prevents burst alignment
- Metrics: `testclient_requests_total{method,code,served_by}`, `testclient_request_duration_seconds`, `testclient_request_errors_total{kind}`
- Error classification: timeout, refused, dns, eof, other

**Deployment:**
- Server: `tools/k8s/test-srv-deployment.yaml` (StatefulSet with anti-affinity)
- Client: `tools/k8s/test-clnt-deployment.yaml`

### Chaos Testing (`chaos/`)

Workflow definition: `chaos/chaos-mesh-workflow.yaml` - multi-phase resilience validation.

**Phase 1: Single Component Failures (baseline)**
- `single-hc-kill`: Kill 75% of hc-worker pods
- `single-hc-failure-long`: Pod failure (4m) on 75% of hc-worker
- `single-test-server-kill`: Container kill on test-server
- `single-agent-failure`: Pod failure on 50% of nlb-agent
- `single-reconciler-kill`: Control plane reconciler disruption
- `single-agent-cpu-stress`: 60% CPU load on 2 workers
- `single-hc-mem-stress`: 256MB memory stress on hc-worker
- `single-infra-latency`: 200ms delay to etcd/infra (jitter 50ms)
- `single-infra-partition`: Network partition to etcd (30s)
- `server-flap-test`: Rapid test-server container kills (flapping simulation)

**Phase 2: Paired Failures (cascading)**
- `pair-hc-plus-servers`: HC workers AND test servers failing
- `pair-agent-plus-servers`: NLB agents AND test servers
- `pair-agent-plus-reconciler`: Agents + Control Plane reconciler
- `pair-hc-gossip-degraded`: HC pod failures + packet loss (30%)
- `pair-reconciler-plus-infra`: Reconciler + infrastructure latency (300ms)
- `pair-cdc-lag-plus-servers`: CDC lag (agent latency 500ms) + server failures

**Phase 3: Triple Failures (major degradation)**
- `triple-hc-servers-agents`: All three critical components
- `triple-agent-reconciler-infra`: Data path + control plane + infrastructure
- `triple-hc-gossip-servers-cdc`: Gossip loss (35%) + HC failures + CDC lag

**Phase 4: Total Chaos (system-wide)**
- `chaos-hc-kill`: 75% hc-worker failures
- `chaos-hc-gossip-loss`: 40% packet loss on gossip
- `chaos-server-1-kill`: 50% test-server failures
- `chaos-agent-failure`: 40% nlb-agent failures
- `chaos-reconciler-kill`: Control plane disruption
- `chaos-infra-latency`: 400ms+ latency to infrastructure
- `chaos-dns-error`: DNS failures on service discovery

**Advanced Scenarios (disabled by default):**
- `pair-hc-plus-infra-partition`: Complete infrastructure isolation
- `triple-full-data-path-break`: Agent partition + server kills
- `triple-control-plane-meltdown`: Complete control plane failure

**Cooldown Periods:**
- Short: 45s between individual tests
- Long: 2m between phases (system stabilization)

### Test GoVPP Backend (`nlb-agent/internal/vpp/testgovpp/`)

Pure Go userspace load balancer implementation for testing without VPP/IPVS.

**Architecture:**
- **ApplySpec**: Creates TCP listener or UDP packet conn per TargetGroup
- **startTCP**: Per-TG TCP listener with Accept() loop
- **startUDP**: Packet-based UDP handling with `udpSession` state

**TCP Load Balancing (`handleTCPConn`):**
- 5-tuple hash: proto + srcIP + srcPort + dstIP + dstPort
- Weighted Rendezvous hashing: picks backend with highest score
- Hash computation: `maphash.Hash` with seed, score = hash / weight
- Zero-downtime: backend selection atomic with connection proxy

**UDP Load Balancing (`udpLoop`):**
- Packet reading loop + session management
- Session key: 5-tuple (like TCP)
- `getOrCreateUDPSess`: Creates backend UDP socket, starts relay goroutine
- `udpBackendToClientLoop`: Reads backend responses, writes to client
- `udpReaper`: TTL-based session cleanup (default 60s)

**Weighted Rendezvous Hashing (`pickHRWFromKey`):**
- For each endpoint: compute hash of (5-tuple + endpoint identity)
- Score = hash / weight (higher weight = lower divisor = better chance)
- Guarantees same 5-tuple maps to same backend (session affinity)
- Handles weighted distribution proportional to endpoint weights

**Why Rendezvous:**
- Minimal state for consistent hashing
- Fast recomputation on endpoint changes
- Works for both TCP (per-connection) and UDP (per-session)

### Healthcheck Deployment Architecture
- hc-worker runs as **StatefulSet** with 4 replicas by default
- Each node uses `HC_WORKER_ID` from pod metadata for identity
- Gossip protocol uses stable DNS names within StatefulSet
- StatefulSet ensures predictable pod names and network stability for gossip

### Multi-module Workspace
Uses Go workspace with modules:
- `./control-plane` - gRPC API + etcd integration
- `./healthcheck` - Health checking with PostgreSQL + Kafka
- `./nlb-agent` - Data plane agent reconciliation
- `./tools` - Testing utilities

## Key Implementation Details

### Long-poll Protocol
Client sends `DataPlaneAssignmentRequest` with:
- `node_id`, `placement_version`, `target_groups_status`
- Server responds with diff or `NO_CHANGES` after timeout
- Current limitation: server closes stream after first response, client must reconnect

### Healthcheck Status Flow
1. Executor detects status change → Sender writes to PostgreSQL
2. Debezium CDC captures change → Redpanda topic
3. Agent consumes with `group.id = nodeID` (at-least-once)
4. Status service updates with idempotent operations

### Reconciliation Reliability
- Retry with backoff on etcd transaction conflicts
- Delayed node death processing (`nodeDeathEventDelay`) to prevent flapping
- Version-based conflict resolution ensures monotonic progress

### Healthcheck Membership and Fault Detection
- SWIM protocol implementation via hashicorp/memberlist
- Events processed: NodeJoin → MarkHealthy, NodeLeave/NodeUpdate → MarkUnhealthy
- Suspect state used for early vshard prefetch before transition to Dead
- Gossip intervals configurable via GOSSIP_PROBE_INTERVAL/PROBE_TIMEOUT