const Graph = {
    instance: null,

    init(containerId) {
        this.instance = cytoscape({
            container: document.getElementById(containerId),
            style: [
                {
                    selector: 'node',
                    style: {
                        'label': 'data(id)',
                        'color': '#fff',
                        'font-family': 'Outfit',
                        'font-size': '12px',
                        'text-valign': 'bottom',
                        'text-margin-y': '8px',
                        'background-color': '#30363d',
                        'width': '40px',
                        'height': '40px',
                        'transition-property': 'background-color, border-color, border-width',
                        'transition-duration': '0.3s'
                    }
                },
                {
                    selector: 'node[role="Leader"]',
                    style: {
                        'background-color': '#f2cc60',
                        'border-width': '4px',
                        'border-color': '#e3b341',
                        'width': '50px',
                        'height': '50px'
                    }
                },
                {
                    selector: 'node[role="Candidate"]',
                    style: {
                        'background-color': '#e07b3a',
                        'border-width': '2px',
                        'border-color': '#d16a29'
                    }
                },
                {
                    selector: 'node[role="Follower"]',
                    style: {
                        'background-color': '#3fb950'
                    }
                }
            ],
            layout: { name: 'circle' }
        });
    },

    update(nodes) {
        if (!this.instance) return;

        const elements = nodes.map(n => ({
            data: { id: n.id, role: n.role }
        }));

        this.instance.json({ elements });
        this.instance.layout({ name: 'circle', animate: true }).run();
    }
};
