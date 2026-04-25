package store

import (
	"sync"
)

type StateMachine struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewStateMachine() *StateMachine {
	return &StateMachine{
		data: make(map[string]string),
	}
}


func (sm *StateMachine) Apply(cmd Command) CommandResult {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	switch cmd.Type {
	case CommandSet:
		sm.data[cmd.Key] = cmd.Value
		return CommandResult{}
	case CommandDelete:
		delete(sm.data, cmd.Key)
		return CommandResult{}
	case CommandNoop:
		return CommandResult{}
	default:
		return CommandResult{Error: "unknown command type"}
	}
}

func (sm *StateMachine) Get(key string) (string, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	val, ok := sm.data[key]
	return val, ok
}

func (sm *StateMachine) Keys() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	keys := make([]string, 0, len(sm.data))
	for k := range sm.data {
		keys = append(keys, k)
	}
	return keys
}

func (sm *StateMachine) Len() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.data)
}
