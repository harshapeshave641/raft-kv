import subprocess
import time
import requests
import json
import threading
import urllib3

# Disable insecure request warnings for self-signed certs
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

def run_watcher(results):
    print(" Watcher started...")
    # Use -N for unbuffered output and -k for insecure
    process = subprocess.Popen(
        ["curl", "-s", "-N", "-L", "-k", "https://localhost:3001/v1/n/watch-test/watch/mykey"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True
    )
    
    # Read the first few lines
    count = 0
    while count < 3: # 1 for connected, 1 for set, 1 for delete
        line = process.stdout.readline()
        if line.startswith("data:"):
            data = json.loads(line[5:])
            print(f" Watcher received: {data}")
            results.append(data)
            count += 1
    
    process.terminate()

def test_watch():
    print(" Starting Watch API Test...")
    
    # 1. Start Cluster
    subprocess.Popen(["python3", "scripts/cluster.py", "--nodes", "3", "--data", "new", "--env", "dev", "--no-neo4j"])
    time.sleep(10) # Wait for election
    
    results = []
    watch_thread = threading.Thread(target=run_watcher, args=(results,))
    watch_thread.start()
    
    time.sleep(2) # Give watcher time to connect
    
    # 2. Trigger Events
    print(" Sending PUT...")
    requests.put("https://localhost:3001/v1/n/watch-test/keys/mykey", json={"value": "pulse-1"}, verify=False)
    
    time.sleep(1)
    
    print(" Sending DELETE...")
    requests.delete("https://localhost:3001/v1/n/watch-test/keys/mykey", verify=False)
    
    watch_thread.join(timeout=10)
    
    # 3. Verify
    if len(results) >= 3:
        print(" WATCH TEST PASSED!")
        print(f"Summary of events: {results}")
    else:
        print(f" WATCH TEST FAILED! Only received {len(results)} events.")

    # Cleanup
    subprocess.run(["pkill", "raftkv-binary"])

if __name__ == "__main__":
    test_watch()
