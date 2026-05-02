const App = {
    async init() {
        Graph.init('cy');
        
        document.getElementById('reset-layout').onclick = () => {
            Graph.instance.layout({ name: 'circle', animate: true }).run();
        };

        this.sync();
        setInterval(() => this.sync(), 2000);
    },

    async sync() {
        try {
            const nodes = await API.getClusterState();
            Graph.update(nodes);

            // Update Stats
            if (nodes.length > 0) {
                const maxTerm = Math.max(...nodes.map(n => n.term));
                document.getElementById('term-val').innerText = maxTerm;
            }

            const events = await API.getRecentEvents();
            this.updateEventFeed(events);

        } catch (err) {
            console.error("Sync Error:", err);
            document.getElementById('health-val').innerText = "ERROR";
            document.getElementById('health-val').style.color = "var(--leader)";
        }
    },

    updateEventFeed(events) {
        const feed = document.getElementById('event-feed');
        feed.innerHTML = events.map(e => `
            <div class="event-item">
                <div class="event-type">${e.type}</div>
                <div class="event-meta">Term: ${e.term} | ${new Date(e.ts).toLocaleTimeString()}</div>
            </div>
        `).join('');
    }
};

window.onload = () => App.init();
