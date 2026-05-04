import subprocess
import time
import requests
import os
import signal
import json
import random
import urllib3
from concurrent.futures import ThreadPoolExecutor

# Disable warnings for self-signed certificates
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

NODES = {
    "node1": "https://localhost:3001",
    "node2": "https://localhost:3002",
    "node3": "https://localhost:3003",
}

def get_leader():
    for name, addr in NODES.items():
        try:
            resp = requests.get(f"{addr}/v1/keys", timeout=1, verify=False)
            if resp.status_code == 200:
                return name, addr
            if resp.status_code == 307:
                loc = resp.headers.get("Location")
                for n_name, n_addr in NODES.items():
                    if n_addr in loc: return n_name, n_addr
        except: continue
    return None, None

def write_key(addr, ns, key, val):
    try:
        resp = requests.put(f"{addr}/v1/n/{ns}/keys/{key}", 
                            json={"value": val}, 
                            verify=False, timeout=6)
        return resp.status_code == 200
    except: return False

def check_key(addr, ns, key, expected_val):
    try:
        resp = requests.get(f"{addr}/v1/n/{ns}/keys/{key}", verify=False, timeout=6)
        if resp.status_code == 200 and resp.json()["value"] == expected_val:
            return True
        print(f"   [DEBUG] Check failed for {key} at {addr}: Status {resp.status_code}, Body {resp.text}")
        return False
    except Exception as e:
        print(f"   [DEBUG] Exception checking {key} at {addr}: {e}")
        return False

def run_advanced_chaos():
    print("====================================================")
    print(" RAFT TLS ULTIMATE STRESS & CHAOS AUDIT")
    print("====================================================")

    # PHASE 1: Bootstrap & Identity Validation
    print("\n[PHASE 1] Bootstrapping Secure Cluster...")
    subprocess.run(["rm", "-rf", "chaos_data"])
    subprocess.Popen(["python3", "scripts/cluster.py", "--nodes", "3", "--data", "chaos_data"])
    time.sleep(12)
    
    leader_name, leader_addr = get_leader()
    if not leader_name:
        print("FAILED: Cluster bootstrap failed.")
        return

    print(f"Found Leader: {leader_name} ({leader_addr})")
    
    # Verify TOFU Registry
    print("Checking TOFU Fingerprint Registry...")
    for i in range(1, 4):
        p = f"chaos_data/node{i}/known_peers.json"
        if not os.path.exists(p):
            print(f"FAILED: node{i} registry missing.")
            return
    print(" [OK] Identities pinned on all nodes.")

    # PHASE 2: High Concurrency Burst (250 writes to trigger snapshots)
    print("\n[PHASE 2] High Concurrency Burst (250 writes to trigger snapshots)...")
    success_count = 0
    with ThreadPoolExecutor(max_workers=10) as executor:
        futures = [executor.submit(write_key, leader_addr, "chaos", f"k{i}", f"v{i}") for i in range(250)]
        results = [f.result() for f in futures]
        success_count = sum(1 for r in results if r)
    print(f" [RESULT] {success_count}/250 writes successful.")

    # Verify Snapshot Files
    time.sleep(2)
    for i in range(1, 4):
        if os.path.exists(f"chaos_data/node{i}/snapshot.json"):
            print(f" [OK] Snapshot found for node{i}")
        else:
            print(f" [WARN] No snapshot for node{i}")

    # PHASE 3: Network Partition (Leader Isolation)
    print(f"\n[PHASE 3] Simulating Network Partition (Isolating {leader_name})...")
    # We'll use SIGSTOP to freeze the leader, simulating a total network hang
    leader_pids = subprocess.check_output(["pgrep", "-f", f"raftkv-binary.*{leader_name}"]).decode().split()
    for pid in leader_pids:
        os.kill(int(pid), signal.SIGSTOP)
    
    print(f" {leader_name} FROZEN. Waiting for election...")
    time.sleep(15) # Wait for others to detect failure
    
    new_leader_name, new_leader_addr = get_leader()
    if not new_leader_name or new_leader_name == leader_name:
        print("FAILED: No new leader elected after partition.")
    else:
        print(f" New Leader Elected: {new_leader_name}")
        # Try a write to the new leader
        if write_key(new_leader_addr, "chaos", "partition_key", "survived"):
            print(" [OK] Writes successful during partition.")
        else:
            print(" [FAIL] Cluster unhealthy during partition.")

    # PHASE 4: Partition Recovery & Catch-up
    print(f"\n[PHASE 4] Resuming {leader_name} (Recovery Catch-up)...")
    for pid in leader_pids:
        os.kill(int(pid), signal.SIGCONT)
    time.sleep(5) # Wait for it to catch up
    
    # PHASE 5: Cascading Failure (Double Kill)
    print("\n[PHASE 5] Cascading Failure (Killing 2 nodes)...")
    others = [n for n in NODES.keys() if n != new_leader_name]
    for n in others:
        subprocess.run(["pkill", "-f", f"raftkv-binary.*{n}"])
    
    print(" Cluster is now below Quorum. Verifying writes fail...")
    try:
        resp = requests.put(f"{new_leader_addr}/v1/n/fail/keys/k", json={"value":"v"}, verify=False, timeout=2)
        if resp.status_code == 200: print(" [FAIL] Cluster accepted write without quorum!")
        else: print(" [OK] Cluster correctly rejected write (No Quorum).")
    except:
        print(" [OK] Write timed out/failed as expected.")

    # PHASE 6: Snapshot & Recovery Check
    print("\n[PHASE 6] Final Integrity Audit...")
    # Clean restart of everything
    subprocess.run(["pkill", "raftkv-binary"])
    time.sleep(2)
    subprocess.Popen(["python3", "scripts/cluster.py", "--nodes", "3", "--data", "chaos_data", "--no-neo4j"])
    time.sleep(10)
    
    print("\n[PHASE 6] Final Global Integrity Audit...")
    # Audit EVERY node, not just the leader
    missing_keys = 0
    for name, addr in NODES.items():
        print(f" Auditing {name} ({addr})...")
        node_missing = 0
        for i in range(250):
            # We use verify=False and follow_redirects=True
            if not check_key(addr, "chaos", f"k{i}", f"v{i}"):
                node_missing += 1
        
        if node_missing == 0:
            print(f"  [OK] {name} passed integrity check.")
        else:
            print(f"  [FAIL] {name} is missing {node_missing} keys.")
            missing_keys += node_missing
    
    if missing_keys == 0:
        print("\n====================================================")
        print(" GLOBAL AUDIT PASSED: ALL NODES CONSISTENT")
        print("====================================================")
    else:
        print(f"\n AUDIT FAILED: {missing_keys} keys lost during chaos.")

    subprocess.run(["pkill", "raftkv-binary"])

if __name__ == "__main__":
    run_advanced_chaos()
