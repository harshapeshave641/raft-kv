# RaftKV: Distributed Replicated Coordination Engine

RaftKV is a strongly consistent, fault-tolerant, and durable distributed key-value store built in Go. It leverages the Raft Consensus Algorithm to ensure data integrity across a cluster of nodes, providing a reliable foundation for distributed coordination, configuration management, and real-time state synchronization.

---

## 1. System Architecture

The system is designed with a strict separation of concerns, decoupling pure consensus logic from side-effect-heavy operations like disk I/O and networking.

### Core Components

#### RaftCore (Pure Consensus Engine)
The RaftCore is a deterministic, "pure" implementation of the Raft protocol. It does not perform any I/O or networking. Instead, it consumes events (ticks, RPC requests, responses) and returns a list of abstract "Actions" (e.g., PersistState, SendRPC, ApplyCommand). This design ensures the core logic is highly testable and independent of the execution environment.

#### RaftNode (The Orchestrator)
The RaftNode serves as the execution layer for the RaftCore. It manages:
- **Concurrency Control:** Using mutexes to protect the volatile state of the node.
- **Physical Persistence:** Executing disk writes for the Write-Ahead Log (WAL) and StateStore.
- **Event Loops:** Managing election timers and heartbeat tickers.
- **RPC Coordination:** Interfacing with the Transport layer to send and receive cluster messages.

#### State Machine (Multi-Tenant Storage)
The storage engine is a thread-safe, namespace-aware key-value store. It is integrated directly with the Watch API, ensuring that every state transition (SET, DELETE, WIPE) is immediately broadcast to relevant subscribers.

---

## 2. Advanced Features and Capabilities

### Multi-Tenancy and Isolation
RaftKV implements logical partitioning through **Namespaces**.
- **Data Isolation:** Every key belongs to a specific namespace, preventing collisions between different applications sharing the same cluster.
- **Namespace Management:** Supports atomic operations at the namespace level, including full namespace wipes.
- **Defaulting:** Operations that omit a namespace are automatically routed to the "default" partition for backward compatibility.

### Real-Time Watch API (SSE)
A reactive event system based on Server-Sent Events (SSE).
- **Fan-Out Architecture:** Uses a centralized `WatcherRegistry` to manage thousands of concurrent subscribers.
- **Non-Blocking Delivery:** Implements a buffered channel-per-subscriber model to ensure that slow-consuming clients do not block the progress of the Raft State Machine.
- **Granular Filters:** Clients can subscribe to specific keys or monitor entire namespaces for broad visibility.

### Backpressure and Load Protection
To ensure system stability under extreme load, RaftKV utilizes **Semaphores** as a backpressure mechanism:
- **Proposal Semaphore:** Limits the number of concurrent Raft proposals. This prevents the node from overwhelming the disk subsystem and network buffers during write bursts.
- **Watch Semaphore:** Limits the number of active SSE connections. This protects the server from memory exhaustion and socket starvation caused by excessive streaming clients.

### Zero-Config TOFU mTLS Security
RaftKV provides industry-standard security with zero manual configuration.
- **Identity Generation:** Every node generates a unique Ed25519 self-signed certificate upon initialization.
- **Trust-On-First-Use (TOFU):** Nodes automatically "pin" the fingerprints of their peers during the initial handshake.
- **mTLS Enforcement:** All inter-node communication is encrypted and mutually authenticated. If a peer's certificate changes, the connection is rejected to prevent Man-in-the-Middle attacks.

---

## 3. System Flows

### The Write Path (Commit Cycle)
1. **Request Ingress:** A client sends a PUT request to the API.
2. **Backpressure Check:** The API acquires a slot in the `proposeSem`.
3. **Proposal:** The command is proposed to the `RaftNode`.
4. **Durability:** The leader writes the entry to its local WAL.
5. **Replication:** The leader replicates the log entry to followers via HTTPS.
6. **Quorum:** Once a majority acknowledges the entry, the leader marks it as committed.
7. **Application:** The `RaftNode` applies the command to the State Machine.
8. **Notification:** The State Machine triggers the `WatcherRegistry` to push the event to active SSE clients.
9. **Response:** The API returns success to the client.

### The Watch Path
1. **Subscription:** A client opens an SSE connection to `/v1/watch`.
2. **Resource Guard:** The server acquires a slot in the `watchSem`.
3. **Registry:** The client is added to the `WatcherRegistry` with a dedicated event channel.
4. **Streaming:** As the State Machine processes commands, events are pushed into the client's channel and flushed over the HTTPS connection.
5. **Cleanup:** If the client disconnects, the channel is closed, the registry is pruned, and the semaphore slot is released.

---

## 4. API Reference

All requests must use **HTTPS**. Use the `-k` flag with curl for self-signed certificates.

### Key-Value Operations
- `PUT /v1/n/{ns}/keys/{key}`: Set a value. Requires JSON body `{"value": "..."}`.
- `GET /v1/n/{ns}/keys/{key}`: Retrieve a value.
- `DELETE /v1/n/{ns}/keys/{key}`: Remove a key.
- `GET /v1/n/{ns}/keys`: List all keys in a namespace.

### Streaming Operations
- `GET /v1/n/{ns}/watch`: Stream all events for a namespace.
- `GET /v1/n/{ns}/watch/{key}`: Stream events for a specific key.

---

## 5. Operations and Maintenance

### Building the Project
```bash
go build -o raftkv-binary cmd/raftkv/main.go
```

### Running a Cluster
Use the provided automation script to start a local 3-node cluster:
```bash
python3 scripts/cluster.py --nodes 3 --data new
```

### Configuration and Tuning
System parameters are centralized in `internal/config/constants.go`. Key parameters include:
- `SnapshotThreshold`: Frequency of log compaction (default: 200 entries).
- `HeartbeatInterval`: Leader heartbeat frequency (default: 50ms).
- `MaxConcurrentWatchers`: Maximum SSE streaming clients (default: 50).
- `MaxProposeConcurrency`: Maximum simultaneous write proposals (default: 5).

---

## 6. Observability and Debugging (In Progress)

Debugging distributed consensus systems like Raft is notoriously difficult due to the complex interleaving of network events, timeouts, and state transitions. To address this, development is underway on an observability suite centered around **Neo4j**.

- **Event Ingestion:** Every significant Raft event (e.g., Term changes, Vote requests, Log appends, State transitions) is ingested into a Neo4j graph database.
- **Visualization:** By representing the cluster state as a graph, the relationship between different nodes, the flow of log entries, and the lineage of leader elections can be visualized.
- **Advanced Debugging:** This graph-based approach allows for complex queries to identify split-brain scenarios, trace the cause of election storms, and verify safety properties in real-time.
