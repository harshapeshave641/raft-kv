#!/usr/bin/env python3
import argparse
import subprocess
import shutil
import sys
import os
import signal

def parse_args():
    parser = argparse.ArgumentParser(description="Start Raft KV Cluster")
    parser.add_argument("--nodes", type=int, default=3, help="Number of nodes to start")
    parser.add_argument("--data", choices=["old", "new"], default="old", help="Keep old data or start new")
    parser.add_argument("--env", type=str, default="dev", help="Environment config to use (dev, staging, prod)")
    parser.add_argument("--no-neo4j", action="store_true", help="Do not start Neo4j container via docker-compose")
    return parser.parse_args()

processes = []

def signal_handler(sig, frame):
    print("\nShutting down cluster...")
    for p, f in processes:
        p.terminate()
        f.close()
    sys.exit(0)

def main():
    args = parse_args()
    
    if not args.no_neo4j:
        print("Ensuring Neo4j is running...")
        subprocess.run(["docker-compose", "up", "-d", "neo4j"], check=False)

    print("Building raftkv binary...")
    subprocess.run(["go", "build", "-o", "raftkv-binary", "cmd/raftkv/main.go"], check=True)

    if args.data == "new":
        print("Clearing old data...")
        if os.path.exists("data"):
            # Only remove subdirectories matching node*
            for item in os.listdir("data"):
                if item.startswith("node"):
                    shutil.rmtree(os.path.join("data", item))
    
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)

    base_port = 3000
    
    # Pre-calculate nodes
    node_configs = []
    for i in range(1, args.nodes + 1):
        node_id = f"node{i}"
        port = base_port + i
        data_dir = os.path.join("data", node_id)
        os.makedirs(data_dir, exist_ok=True)
        node_configs.append({"id": node_id, "port": port, "data": data_dir})
        
    print(f"Starting Raft Cluster with {args.nodes} nodes (Environment: {args.env})...")
    
    for i, config in enumerate(node_configs):
        peers = []
        for j, other_config in enumerate(node_configs):
            if i != j:
                peers.append(f"{other_config['id']}=localhost:{other_config['port']}")
        
        peers_str = ",".join(peers)
        
        cmd = [
            "./raftkv-binary",
            "--id", config["id"],
            "--port", str(config["port"]),
            "--data", config["data"],
            "--env", args.env
        ]
        
        if peers_str:
            cmd.extend(["--peers", peers_str])
            
        print(f"Starting {config['id']} on port {config['port']}...")
        log_file_path = os.path.join(config["data"], "raft.log")
        log_file = open(log_file_path, "w")
        p = subprocess.Popen(cmd, stdout=log_file, stderr=subprocess.STDOUT)
        processes.append((p, log_file))
        
    print("-" * 48)
    print("CLUSTER ACTIVE")
    for config in node_configs:
        print(f"{config['id']}: https://localhost:{config['port']}")
    print("-" * 48)
    print("To start UI: python3 -m http.server 8080 --directory ui")
    print("Then visit: http://localhost:8080")
    print("-" * 48)
    print("Press Ctrl+C to stop the cluster.")
    
    for p, _ in processes:
        p.wait()

if __name__ == "__main__":
    main()
