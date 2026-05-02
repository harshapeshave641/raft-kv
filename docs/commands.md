# RaftKV Development Commands

Reference guide for starting, testing, and monitoring the Raft cluster and observability layer.

##  Infrastructure (Neo4j)

### Start Neo4j
```bash
docker-compose up -d neo4j
```

### Reset / Wipe Database (Fresh Start)
```bash
docker-compose down -v
docker-compose up -d neo4j
```

### Check Database Health
```bash
curl -u neo4j:password -H "Content-Type: application/json" \
  -d '{"statements":[{"statement":"MATCH (n) RETURN count(n)"}]}' \
  http://localhost:7474/db/neo4j/tx/commit
```

---

##  Running the Cluster

### Automated Startup (Recommended)
Starts 3 nodes and Neo4j automatically.
```bash
./scripts/start_cluster.sh
```

### Manual Node Startup
If you need to run nodes in separate terminals:
```bash
# Node 1
go run cmd/raftkv/main.go --id node1 --port 3001 --data data/node1 --peers node2=localhost:3002,node3=localhost:3003

# Node 2
go run cmd/raftkv/main.go --id node2 --port 3002 --data data/node2 --peers node1=localhost:3001,node3=localhost:3003

# Node 3
go run cmd/raftkv/main.go --id node3 --port 3003 --data data/node3 --peers node1=localhost:3001,node2=localhost:3002
```

---

##  Observability Dashboard (UI)

### Start Web Server
```bash
python3 -m http.server 8080 --directory ui
```
Then visit: [http://localhost:8080](http://localhost:8080)

### Run Invariant Checker (Python)
Automated audit of the Raft logs stored in Neo4j.
```bash
python3 scripts/check_raft_invariants.py
```

---

##  Client API Testing

### Write Data (PUT)
```bash
curl -X PUT http://localhost:3001/v1/keys/mykey -d '{"value": "myvalue"}'
```

### Read Data (GET)
```bash
curl http://localhost:3001/v1/keys/mykey
```

### List Keys
```bash
curl http://localhost:3001/v1/keys
```

---


