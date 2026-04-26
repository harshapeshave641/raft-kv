# RaftKV — Distributed Key-Value Store

A production-grade key-value store using the Raft consensus algorithm.

## Build

```bash
go build -o build/raft-kv .
```

## Run Single Node

```bash
./build/raft-kv --id=node1 --port=8080
```

## Run 3-Node Cluster (Linux/macOS)

```bash
# Terminal 1
./build/raft-kv --id=node1 --port=8080 --peers=localhost:8081,localhost:8082

# Terminal 2
./build/raft-kv --id=node2 --port=8081 --peers=localhost:8080,localhost:8082

# Terminal 3
./build/raft-kv --id=node3 --port=8082 --peers=localhost:8080,localhost:8081
```

## Run 3-Node Cluster (Windows)

```batch
# Terminal 1
.\build\raft-kv.exe --id=node1 --port=8080 --peers=localhost:8081,localhost:8082

# Terminal 2
.\build\raft-kv.exe --id=node2 --port=8081 --peers=localhost:8080,localhost:8082

# Terminal 3
.\build\raft-kv.exe --id=node3 --port=8082 --peers=localhost:8080,localhost:8081
```

## Endpoints

- **GET /ping** — Health check
- **GET /status** — Node status

## Example

```bash
curl http://localhost:8080/ping
curl http://localhost:8080/status
```
