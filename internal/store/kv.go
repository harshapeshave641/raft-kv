package store

import (
	"sync"
)

const DefaultNamespace = "default"

type StateMachine struct {
	mu   sync.RWMutex
	data map[string]map[string]string // Namespace -> Key -> Value
}

func NewStateMachine() *StateMachine {
	return &StateMachine{
		data: make(map[string]map[string]string),
	}
}

func (sm *StateMachine) Apply(cmd Command) CommandResult {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	ns := cmd.Namespace
	if ns == "" {
		ns = DefaultNamespace
	}

	if _, ok := sm.data[ns]; !ok {
		sm.data[ns] = make(map[string]string)
	}

	switch cmd.Type {
	case CommandSet:
		sm.data[ns][cmd.Key] = cmd.Value
		return CommandResult{}
	case CommandDelete:
		delete(sm.data[ns], cmd.Key)
		return CommandResult{}
	case CommandNoop:
		return CommandResult{}
	default:
		return CommandResult{Error: "unknown command type"}
	}
}

func (sm *StateMachine) Snapshot() map[string]map[string]string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	snapshot := make(map[string]map[string]string, len(sm.data))
	for ns, keys := range sm.data {
		nsCopy := make(map[string]string, len(keys))
		for k, v := range keys {
			nsCopy[k] = v
		}
		snapshot[ns] = nsCopy
	}
	return snapshot
}

func (sm *StateMachine) Restore(snapshot map[string]map[string]string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.data = make(map[string]map[string]string, len(snapshot))
	for ns, keys := range snapshot {
		nsCopy := make(map[string]string, len(keys))
		for k, v := range keys {
			nsCopy[k] = v
		}
		sm.data[ns] = nsCopy
	}
}

func (sm *StateMachine) Get(ns, key string) (string, bool) {
	if ns == "" {
		ns = DefaultNamespace
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	keys, ok := sm.data[ns]
	if !ok {
		return "", false
	}
	val, ok := keys[key]
	return val, ok
}

func (sm *StateMachine) Keys(ns string) []string {
	if ns == "" {
		ns = DefaultNamespace
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	keysMap, ok := sm.data[ns]
	if !ok {
		return nil
	}

	keys := make([]string, 0, len(keysMap))
	for k := range keysMap {
		keys = append(keys, k)
	}
	return keys
}

func (sm *StateMachine) Len(ns string) int {
	if ns == "" {
		ns = DefaultNamespace
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	keys, ok := sm.data[ns]
	if !ok {
		return 0
	}
	return len(keys)
}
