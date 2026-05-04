package auth

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type TOFURegistry struct {
	mu         sync.Mutex
	filePath   string
	KnownPeers map[string]string `json:"known_peers"` // NodeID -> Fingerprint
}

func NewTOFURegistry(dataDir string) (*TOFURegistry, error) {
	path := filepath.Join(dataDir, "known_peers.json")
	reg := &TOFURegistry{
		filePath:   path,
		KnownPeers: make(map[string]string),
	}

	if err := reg.load(); err != nil {
		return nil, err
	}

	return reg, nil
}

func (r *TOFURegistry) VerifyPeer(nodeID string, cert *x509.Certificate) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	fingerprint := CalculateFingerprint(cert)
	
	known, exists := r.KnownPeers[nodeID]
	if !exists {
		// Trust On First Use
		log.Printf("[TOFU] Trusting new peer %s for the first time. Fingerprint: %s", nodeID, fingerprint)
		r.KnownPeers[nodeID] = fingerprint
		return r.save()
	}

	if known != fingerprint {
		return fmt.Errorf("IDENTITY CRITICAL FAILURE: Peer %s presented fingerprint %s, but we expected %s. Possible Man-in-the-Middle attack!", nodeID, fingerprint, known)
	}

	return nil
}

func (r *TOFURegistry) load() error {
	data, err := os.ReadFile(r.filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &r.KnownPeers)
}

func (r *TOFURegistry) save() error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.filePath, data, 0644)
}
