#!/bin/bash
# scripts/start_cluster.sh

# 1. Start Neo4j if not running
echo "Ensuring Neo4j is running..."
docker-compose up -d neo4j

# 2. Build the binary
echo "Building raftkv binary..."
go build -o raftkv-binary cmd/raftkv/main.go

# 3. Cleanup old data
rm -rf data/node*
mkdir -p data/node1 data/node2 data/node3

# 4. Start 3 Nodes in background
echo "Starting Raft Cluster..."

./raftkv-binary --id node1 --port 3001 --data data/node1 \
    --peers node2=localhost:3002,node3=localhost:3003 &

./raftkv-binary --id node2 --port 3002 --data data/node2 \
    --peers node1=localhost:3001,node3=localhost:3003 &

./raftkv-binary --id node3 --port 3003 --data data/node3 \
    --peers node1=localhost:3001,node2=localhost:3002 &

echo "------------------------------------------------"
echo "CLUSTER ACTIVE"
echo "Node 1: http://localhost:3001"
echo "Node 2: http://localhost:3002"
echo "Node 3: http://localhost:3003"
echo "------------------------------------------------"
echo "To start UI: python3 -m http.server 8080 --directory ui"
echo "Then visit: http://localhost:8080"
echo "------------------------------------------------"

# Keep script running to handle Ctrl+C
trap "kill 0" EXIT
wait
