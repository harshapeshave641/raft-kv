import time
from neo4j import GraphDatabase

# Configuration
URI = "bolt://localhost:7687"
AUTH = ("neo4j", "password")
RETENTION_HOURS = 2

def prune_events():
    driver = GraphDatabase.driver(URI, auth=AUTH)
    
    # Calculate cutoff in milliseconds (normalized Unix TS)
    cutoff_ms = int((time.time() - (RETENTION_HOURS * 3600)) * 1000)
    
    query = """
    MATCH (e:Event)
    WHERE e.timestamp_unix < $cutoff
    WITH e LIMIT 10000
    DETACH DELETE e
    RETURN count(*) as deleted
    """
    
    total_deleted = 0
    try:
        with driver.session() as session:
            while True:
                # We delete in batches to avoid transaction memory overflow
                result = session.run(query, cutoff=cutoff_ms)
                deleted = result.single()["deleted"]
                if deleted == 0:
                    break
                total_deleted += deleted
                print(f"Deleted {total_deleted} old events...")
                
        print(f" Success. Total pruned: {total_deleted} events.")
    except Exception as e:
        print(f" Error during pruning: {e}")
    finally:
        driver.close()

if __name__ == "__main__":
    print(f"🧹 Pruning events older than {RETENTION_HOURS} hours...")
    prune_events()
