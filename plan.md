# RaftKV Development Plan

## Phase 0 — Basic Server ✅
- Go module setup
- Basic HTTP server with /ping and /status

## Phase 1 — Key-Value Store ✅
- In-memory KV store with sync.RWMutex
- Command abstraction (Set, Delete)
- /get, /put, and /delete endpoints
- Unit tests

## Phase 2 & 3 — Write-Ahead Log (WAL) & Persistence ✅
- Write-ahead log — every state change written to disk before responding
- fsync guarantees to survive power loss
- Incremental `LogEntry` structure with monotonically increasing index
- Crash recovery — node restores state via deterministic replay from log

## Phase 4 — Leader Election ✅ (Core Logic Done)
- Node struct (RaftNode orchestrator) (In Progress)
- CLI flags (--id, --port, --peers)
- Node states (follower, candidate, leader) ✅
- Election timeout logic ✅
- RequestVote RPC (Core Logic ✅, Networking Pending)
- Voting and term tracking ✅

## Phase 5 — Log Replication & Heartbeats ✅ (Core Logic Done)
- Leader heartbeat loop
- AppendEntries RPC (Core Logic ✅, Networking Pending)
- Stable leader under normal conditions
- Replicate to followers
- Consistency checks ✅

## Phase 6 — Commit & Apply ✅ (Core Logic Done)
- Apply committed entries to state machine (Core Logic ✅, KV integration Pending)
- Snapshot for durability
- Batched AppendEntries — leader batches multiple log entries per RPC
- Pipelined RPCs — leader doesn't wait for one AppendEntries response before sending the next

### Log & Persistence (Future)
- **Log compaction via snapshots** — when log grows beyond threshold, take a snapshot of state machine, truncate log behind it
- **Snapshot transfer** — leader sends snapshot to severely lagging followers via InstallSnapshot RPC

### Cluster Membership
- **Static cluster config** — fixed set of nodes defined at startup
- **Joint consensus for membership changes** — add or remove nodes safely without split brain
- Config changes go through the log like any other command

### Client HTTP API
```text
GET    /v1/keys/:key           → get value
PUT    /v1/keys/:key           → set value
DELETE /v1/keys/:key           → delete key
GET    /v1/keys                → list all keys

GET    /v1/cluster/status      → leader, term, cluster health
GET    /v1/cluster/nodes       → all nodes and their state
POST   /v1/cluster/transfer    → trigger leadership transfer

GET    /v1/metrics             → internal metrics (Prometheus format)
```

### Leader Redirect
- If client hits a follower with a write → follower returns `302` with leader address
- Client SDK handles redirect automatically
- Reads can optionally be served from followers (with staleness warning)

### Observability
- Structured JSON logging at every state transition
- Prometheus metrics endpoint
- Per node: term, state, commitIndex, lastApplied, log length, snapshot index
- Per RPC: latency, success/failure rate
- Election events: who started, who won, how long it took

### Chaos Testing
- Kill any single node → cluster continues
- Kill leader → new leader elected within 500ms
- Restart killed node → rejoins and catches up
- Network partition simulation → no split brain
- Full cluster restart → all committed data recovered

---

## What Is Explicitly Out of Scope

- TLS / mTLS between nodes (noted as future work)
- Authentication on client API (noted as future work)
- Multi-key transactions
- TTL on keys
- Watch / subscribe on key changes
- Range queries
- Multi-datacenter replication

---

## Project Structure (Go)

```text
raftkv/
├── cmd/
│   └── raftkv/
│       └── main.go              ← entry point, CLI, config
│
├── internal/
│   ├── raft/
│   │   ├── node.go              ← orchestrates all raft components
│   │   ├── log.go               ← log management, persistence
│   │   ├── types.go             ← all shared types and interfaces
│   │   └── election.go          ← randomized timeout management
│   │
│   ├── rpc/
│   │   ├── server.go            ← receives RPCs from peer nodes
│   │   └── client.go            ← sends RPCs to peer nodes
│   │
│   ├── store/
│   │   ├── kv.go                ← key-value state machine
│   │   ├── command.go           ← command abstraction
│   │   └── snapshot.go          ← snapshot serialization
│   │
│   ├── persistence/
│   │   ├── wal.go               ← write-ahead log, fsync
│   │   └── snapshot.go          ← snapshot storage and loading
│   │
│   ├── membership/
│   │   └── config.go            ← static + dynamic cluster membership
│   │
│   ├── api/
│   │   └── http.go              ← HTTP server & endpoints
│   │
│   └── metrics/
│       └── metrics.go           ← prometheus metrics
│
├── tests/
│   ├── integration/
│   │   ├── election_test.go     ← 3 node cluster election tests
│   │   ├── replication_test.go  ← write and read consistency tests
│   │   └── recovery_test.go     ← crash and restart tests
│   └── chaos/
│       ├── partition_test.go    ← network partition simulation
│       └── killanything_test.go ← random node kills
│
├── docker/
│   ├── Dockerfile
│   └── docker-compose.yml       ← 3 or 5 node cluster locally
│
├── bench/
│   └── benchmark.go             ← writes/sec, read latency, p99
│
├── go.mod
└── README.md
```

---

## Build Phases

| Phase | What | Done When | Status |
|---|---|---|---|
| **0** | Basic Server Setup | HTTP server runs, module setup | ✅ Done |
| **1** | Key-Value Store | In-memory KV store with sync.RWMutex | ✅ Done |
| **2** | Write-Ahead Log (WAL) & Persistence | Log persists to disk with fsync | ✅ Done |
| **3** | Pure Raft State Machine (`RaftCore`) | All election, replication & commit logic fully tested without I/O | ✅ Done |
| **4** | Orchestrator (`RaftNode`) & Side Effects | Node executes disk writes (WAL) and handles concurrent timers | ⏳ In Progress |
| **5** | RPC layer & Node to Node communication | Nodes exchange RequestVote & AppendEntries over network | Pending |
| **6** | Commit + state machine application | KV reads return correct values after writes | Pending |
| **7** | Crash recovery — full | Kill any node, restart, rejoins correctly | Pending |
| **8** | Pre-vote + pipelining + batching | Production performance characteristics | Pending |
| **9** | Snapshots + InstallSnapshot RPC | Log compaction works, lagging nodes catch up | Pending |
| **10** | Membership changes via joint consensus | Add/remove nodes without downtime | Pending |
| **11** | Observability — logs + metrics | Prometheus endpoint, structured logs | Pending |
| **12** | Chaos testing suite | Cluster survives everything you throw at it | Pending |

---

## Definition of Done

A Docker Compose file that spins up a 5-node RaftKV cluster. You can:

```bash
# Start cluster
docker-compose up

# Write a key
curl -X PUT http://localhost:3001/v1/keys/hello -d '{"value":"world"}'

# Read from any node
curl http://localhost:3002/v1/keys/hello

# Kill the leader
docker-compose stop raftkv-1

# Cluster elects new leader within 500ms
# Read still works
curl http://localhost:3003/v1/keys/hello

# Restart the node
docker-compose start raftkv-1

# Node rejoins, catches up, cluster is healthy
curl http://localhost:3001/v1/cluster/status
```

Benchmarked. Chaos tested. Documented. Published on GitHub.
