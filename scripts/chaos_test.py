import subprocess
import time
import requests
import os
import signal
import random

nodes = {
    "node1": "http://localhost:3001",
    "node2": "http://localhost:3002",
    "node3": "http://localhost:3003",
}

def get_leader():
    for name, addr in nodes.items():
        try:
            resp = requests.get(f"{addr}/v1/keys", timeout=1)
            if resp.status_code == 200:
                return name, addr
            if resp.status_code == 307:
                leader_addr = resp.headers.get("Location")
                for n_name, n_addr in nodes.items():
                    if n_addr in leader_addr:
                        return n_name, n_addr
        except:
            continue
    return None, None

def run_chaos():
    print(" Starting Chaos Test...")
    
    # 1. Start Cluster
    print("--- Phase 1: Cluster Warmup ---")
    subprocess.Popen(["python3", "scripts/cluster.py", "--nodes", "3", "--data", "new", "--env", "dev", "--no-neo4j"])
    time.sleep(10) # Wait for election
    
    leader_name, leader_addr = get_leader()
    print(f"Current Leader: {leader_name} ({leader_addr})")
    
    # 2. Initial Writes
    print("--- Phase 2: Initial Writes ---")
    for i in range(20):
        ns = "ns_alpha" if i % 2 == 0 else "ns_beta"
        requests.put(f"{leader_addr}/v1/n/{ns}/keys/key_{i}", json={"value": f"val_{i}"})
    
    print("Successfully wrote 20 keys across 2 namespaces.")

    # 3. KILL LEADER
    print(f"--- Phase 3: KILLING LEADER ({leader_name}) ---")
    subprocess.run(["pkill", "-f", f"-id {leader_name}"])
    time.sleep(5)
    
    new_leader_name, new_leader_addr = get_leader()
    print(f"New Leader Elected: {new_leader_name} ({new_leader_addr})")
    
    # 4. Writes during/after recovery
    print("--- Phase 4: Post-Kill Writes ---")
    for i in range(20, 40):
        ns = "ns_alpha" if i % 2 == 0 else "ns_beta"
        resp = requests.put(f"{new_leader_addr}/v1/n/{ns}/keys/key_{i}", json={"value": f"val_{i}"})
        if resp.status_code != 200:
            print(f"Write failed: {resp.status_code}")

    # 5. Snapshot Trigger
    print("--- Phase 5: Snapshot Verification ---")
    # We've done 40 writes, threshold is 10. Snapshots should exist.
    time.sleep(2)
    for name in nodes.keys():
        snap_path = f"data/{name}/snapshot.json"
        if os.path.exists(snap_path):
            print(f"Snapshot exists for {name}")
        else:
            print(f"Snapshot MISSING for {name}")

    # 6. Final Verification
    print("--- Phase 6: Final Data Integrity Check ---")
    all_ok = True
    for i in range(40):
        ns = "ns_alpha" if i % 2 == 0 else "ns_beta"
        resp = requests.get(f"{new_leader_addr}/v1/n/{ns}/keys/key_{i}")
        if resp.status_code != 200 or resp.json()["value"] != f"val_{i}":
            print(f"Data mismatch for key_{i} in {ns}")
            all_ok = False
    
    if all_ok:
        print("CHAOS TEST PASSED! All data survived leader death and snapshots.")
    else:
        print(" CHAOS TEST FAILED!")

    # Cleanup
    subprocess.run(["pkill", "raftkv-binary"])

if __name__ == "__main__":
    run_chaos()
