package store

import "testing"

func TestStateMachine_ApplySetAndGet(t *testing.T) {
	sm := NewStateMachine()

	// Initial get should fail
	_, ok := sm.Get(DefaultNamespace, "foo")
	if ok {
		t.Errorf("Expected foo to not exist")
	}

	// Set a value
	res := sm.Apply(Command{Type: CommandSet, Key: "foo", Value: "bar"})
	if res.Error != "" {
		t.Errorf("Expected empty error, got %v", res.Error)
	}

	// Get should now return the value
	val, ok := sm.Get(DefaultNamespace, "foo")
	if !ok || val != "bar" {
		t.Errorf("Expected foo to be bar, got %v (ok=%v)", val, ok)
	}
}

func TestStateMachine_ApplyDelete(t *testing.T) {
	sm := NewStateMachine()
	sm.Apply(Command{Type: CommandSet, Key: "foo", Value: "bar"})
	sm.Apply(Command{Type: CommandDelete, Key: "foo"})

	_, ok := sm.Get(DefaultNamespace, "foo")
	if ok {
		t.Errorf("Expected foo to be deleted")
	}
}

func TestStateMachine_KeysAndLen(t *testing.T) {
	sm := NewStateMachine()
	sm.Apply(Command{Type: CommandSet, Key: "k1", Value: "v1"})
	sm.Apply(Command{Type: CommandSet, Key: "k2", Value: "v2"})

	if sm.Len(DefaultNamespace) != 2 {
		t.Errorf("Expected length 2, got %d", sm.Len(DefaultNamespace))
	}

	keys := sm.Keys(DefaultNamespace)
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

func TestStateMachine_NamespaceIsolation(t *testing.T) {
	sm := NewStateMachine()

	// Set same key in different namespaces
	sm.Apply(Command{Type: CommandSet, Namespace: "ns1", Key: "foo", Value: "val1"})
	sm.Apply(Command{Type: CommandSet, Namespace: "ns2", Key: "foo", Value: "val2"})

	// Verify isolation
	val1, _ := sm.Get("ns1", "foo")
	val2, _ := sm.Get("ns2", "foo")

	if val1 != "val1" {
		t.Errorf("Expected ns1/foo to be val1, got %s", val1)
	}
	if val2 != "val2" {
		t.Errorf("Expected ns2/foo to be val2, got %s", val2)
	}

	// Delete from one namespace
	sm.Apply(Command{Type: CommandDelete, Namespace: "ns1", Key: "foo"})
	
	_, ok1 := sm.Get("ns1", "foo")
	_, ok2 := sm.Get("ns2", "foo")

	if ok1 {
		t.Errorf("Expected ns1/foo to be deleted")
	}
	if !ok2 {
		t.Errorf("Expected ns2/foo to still exist")
	}

	// Wipe whole namespace
	sm.Apply(Command{Type: CommandDeleteNamespace, Namespace: "ns2"})
	if sm.Len("ns2") != 0 {
		t.Errorf("Expected ns2 to be empty after wipe")
	}
}
