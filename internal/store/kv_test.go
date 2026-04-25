package store

import "testing"

func TestStateMachine_ApplySetAndGet(t *testing.T) {
	sm := NewStateMachine()

	// Initial get should fail
	_, ok := sm.Get("foo")
	if ok {
		t.Errorf("Expected foo to not exist")
	}

	// Set a value
	res := sm.Apply(Command{Type: CommandSet, Key: "foo", Value: "bar"})
	if res.Error != "" {
		t.Errorf("Expected empty error, got %v", res.Error)
	}

	// Get should now return the value
	val, ok := sm.Get("foo")
	if !ok || val != "bar" {
		t.Errorf("Expected foo to be bar, got %v (ok=%v)", val, ok)
	}
}

func TestStateMachine_ApplyDelete(t *testing.T) {
	sm := NewStateMachine()
	sm.Apply(Command{Type: CommandSet, Key: "foo", Value: "bar"})
	sm.Apply(Command{Type: CommandDelete, Key: "foo"})

	_, ok := sm.Get("foo")
	if ok {
		t.Errorf("Expected foo to be deleted")
	}
}

func TestStateMachine_KeysAndLen(t *testing.T) {
	sm := NewStateMachine()
	sm.Apply(Command{Type: CommandSet, Key: "k1", Value: "v1"})
	sm.Apply(Command{Type: CommandSet, Key: "k2", Value: "v2"})

	if sm.Len() != 2 {
		t.Errorf("Expected length 2, got %d", sm.Len())
	}

	keys := sm.Keys()
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(keys))
	}
	
	hasK1, hasK2 := false, false
	for _, k := range keys {
		if k == "k1" {
			hasK1 = true
		}
		if k == "k2" {
			hasK2 = true
		}
	}
	
	if !hasK1 || !hasK2 {
		t.Errorf("Expected keys to contain k1 and k2, got %v", keys)
	}
}
