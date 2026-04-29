package persistence

import (
    "encoding/json"
    "os"
    "path/filepath"
    "sync"
)

type SnapshotStore struct {
    mu   sync.Mutex
    path string
}

type Snapshot struct {
    LastIncludedIndex uint64            `json:"last_included_index"`
    LastIncludedTerm  uint64            `json:"last_included_term"`
    Data              map[string]string `json:"data"`
}

func NewSnapshotStore(dataDir string) (*SnapshotStore, error) {
    if err := os.MkdirAll(dataDir, 0755); err != nil {
        return nil, err
    }
    return &SnapshotStore{
        path: filepath.Join(dataDir, "snapshot.json"),
    }, nil
}

func (s *SnapshotStore) SaveSnapshot(lastIncludedIndex uint64, lastIncludedTerm uint64, data map[string]string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    snapshot := Snapshot{
        LastIncludedIndex: lastIncludedIndex,
        LastIncludedTerm:  lastIncludedTerm,
        Data:              data,
    }

    payload, err := json.Marshal(snapshot)
    if err != nil {
        return err
    }

    tmpPath := s.path + ".tmp"
    tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
    if err != nil {
        return err
    }

    if _, err := tmp.Write(payload); err != nil {
        tmp.Close()
        os.Remove(tmpPath)
        return err
    }
    if err := tmp.Sync(); err != nil {
        tmp.Close()
        os.Remove(tmpPath)
        return err
    }
    if err := tmp.Close(); err != nil {
        os.Remove(tmpPath)
        return err
    }

    return os.Rename(tmpPath, s.path)
}

func (s *SnapshotStore) LoadSnapshot() (*Snapshot, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    data, err := os.ReadFile(s.path)
    if os.IsNotExist(err) {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }

    var snapshot Snapshot
    if err := json.Unmarshal(data, &snapshot); err != nil {
        return nil, err
    }

    return &snapshot, nil
}
