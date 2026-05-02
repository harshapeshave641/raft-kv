#!/usr/bin/env python3
from neo4j import GraphDatabase
import sys

class RaftChecker:
    def __init__(self, uri, user, password):
        self.driver = GraphDatabase.driver(uri, auth=(user, password))

    def close(self):
        self.driver.close()

    def check_election_safety(self):
        """At most one leader can be elected in a given term."""
        query = """
        MATCH (s:Server)-[:LEADER_OF]->(t:Term)
        WITH t, collect(s.id) as leaders, count(s) as leaderCount
        WHERE leaderCount > 1
        RETURN t.value as term, leaders
        """
        with self.driver.session() as session:
            results = list(session.run(query))
            if results:
                print(" ELECTION SAFETY VIOLATION: Multiple leaders in term(s):")
                for r in results:
                    print(f"  Term {r['term']}: Leaders {r['leaders']}")
            else:
                print(" Election Safety: Passed (At most one leader per term)")

    def check_state_machine_safety(self):
        """If a server has applied a log entry at a given index, no other server will ever apply a different log entry for the same index."""
        query = """
        MATCH (s:Server)-[a:APPLIED]->(e:LogEntry)
        WITH e.index as idx, collect(DISTINCT {term: e.term, cmd: a.command}) as applications
        WHERE size(applications) > 1
        RETURN idx, applications
        """
        with self.driver.session() as session:
            results = list(session.run(query))
            if results:
                print(" STATE MACHINE SAFETY VIOLATION: Different commands applied at same index:")
                for r in results:
                    print(f"  Index {r['idx']}: {r['applications']}")
            else:
                print(" State Machine Safety: Passed")

    def run_all(self):
        print("--- Raft Invariant Audit ---")
        self.check_election_safety()
        self.check_state_machine_safety()
        print("--- Audit Complete ---")

if __name__ == "__main__":
    checker = RaftChecker("bolt://localhost:7687", "neo4j", "password")
    checker.run_all()
    checker.close()
