const driver = neo4j.driver("bolt://localhost:7687", neo4j.auth.basic("neo4j", "password"));

const API = {
    async getClusterState() {
        const session = driver.session();
        try {
            // Get servers and their current roles/terms
            const result = await session.run(`
                MATCH (s:Server)
                OPTIONAL MATCH (s)-[r:CURRENT_ROLE]->()
                OPTIONAL MATCH (s)-[:CURRENT_TERM]->(t:Term)
                RETURN s.id as id, r.role as role, t.value as term
            `);
            
            return result.records.map(r => ({
                id: r.get('id'),
                role: r.get('role') || 'Follower',
                term: r.get('term')?.toNumber() || 0
            }));
        } finally {
            await session.close();
        }
    },

    async getRecentEvents(limit = 20) {
        const session = driver.session();
        try {
            const result = await session.run(`
                MATCH (e:Event)
                ORDER BY e.timestamp_unix DESC
                LIMIT $limit
                RETURN e.id as id, e.type as type, e.timestamp as ts, e.term as term, e.details as details
            `, { limit: neo4j.int(limit) });
            
            return result.records.map(r => ({
                id: r.get('id'),
                type: r.get('type'),
                ts: r.get('ts'),
                term: r.get('term').toNumber(),
                details: r.get('details')
            }));
        } finally {
            await session.close();
        }
    }
};
