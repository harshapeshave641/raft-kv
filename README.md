# RaftKV — Distributed Replicated Key-Value Store

RaftKV is a strongly consistent, fault-tolerant, and durable distributed key-value store built in Go using the **Raft Consensus Algorithm**. It is designed for systems where data integrity and high availability are paramount.

## 🏗️ Architecture

The system is built on a modular, decoupled architecture that separates pure logic from side effects, making it highly testable and resilient.

### **1. RaftCore (The Brain)**
*   **Location:** `internal/raft/core.go`
*   **Role:** A "pure" state machine implementation of the Raft protocol.
*   **Details:** It handles leader election, log replication, and commitment logic. It has zero I/O and zero network dependencies; it simply consumes events and returns a list of "Actions" to be performed.

### **2. RaftNode (The Orchestrator)**
*   **Location:** `internal/raft/node.go`
*   **Role:** The bridge between the pure `RaftCore` and the real world.
*   **Details:** Manages concurrency (mutexes), executes disk I/O (WAL/StateStore), handles network RPCs via the Transport layer, and applies committed entries to the State Machine.

### **3. Persistence Layer**
*   **WAL (Write-Ahead Log):** Ensures every log entry is durable on disk before it is acknowledged or replicated.
*   **StateStore:** Persists volatile metadata like `currentTerm` and `votedFor` to survive crashes.

### **4. Networking & API**
*   **HTTP Transport:** Handles inter-node communication (JSON-over-HTTP).
*   **Client API:** Provides a RESTful interface for external clients to interact with the KV store.

---

## 🚀 Getting Started

### **Prerequisites**
*   Go 1.22 or higher

### **Build**
```bash
go build -o raftkv.exe ./cmd/raftkv
```

### **Run a 3-Node Cluster (Local Test)**

Open three terminal windows and run the following commands to start a local cluster:

**Node 1:**
```bash
./raftkv.exe --id node1 --port 3001 --data data1 --peers "node2=localhost:3002,node3=localhost:3003"
```

**Node 2:**
```bash
./raftkv.exe --id node2 --port 3002 --data data2 --peers "node1=localhost:3001,node3=localhost:3003"
```

**Node 3:**
```bash
./raftkv.exe --id node3 --port 3003 --data data3 --peers "node1=localhost:3001,node2=localhost:3002"
```

---

## 📡 Client API Reference

All write operations must go through the **Leader**. Reads are also restricted to the Leader to ensure **Linearizability**.

### **1. Set a Key**
```bash
curl -X PUT http://localhost:3001/v1/keys/my-key -H "Content-Type: application/json" -d '{"value": "hello-world"}'
```
*Returns `202 Accepted` once the command is proposed.*

### **2. Get a Key**
```bash
curl http://localhost:3001/v1/keys/my-key
```
*If the node is not the leader, it returns a `307 Temporary Redirect` with the Leader ID.*

### **3. Delete a Key**
```bash
curl -X DELETE http://localhost:3001/v1/keys/my-key
```

### **4. List All Keys**
```bash
curl http://localhost:3001/v1/keys
```

---

## 🧪 Testing and Fault Tolerance

### **Automatic Recovery**
Upon startup, the server automatically:
1.  Loads the **WAL** from disk.
2.  **Replays** all log entries into the in-memory State Machine.
3.  Resumes the Raft protocol from the last known state.

### **Failover Test**
1.  Identify the current Leader from the logs (e.g., `node1`).
2.  Kill the leader process.
3.  Observe the logs of other nodes as they detect the failure and elect a **new leader** within ~300ms.
4.  Restart the old leader and observe it rejoining the cluster and catching up.

### **Unit Tests**
Run the comprehensive test suite for the core consensus logic:
```bash
go test ./internal/raft/...
```

---

## 🛠️ Tech Stack
*   **Language:** Go (Standard Library)
*   **Consensus:** Raft (Strongly Consistent)
*   **Transport:** JSON over HTTP/1.1
*   **Storage:** Append-only WAL (File-based)
