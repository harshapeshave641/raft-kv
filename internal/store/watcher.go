package store

import (
	"sync"
)

type WatchEvent struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	Type      string `json:"type"` // "set", "delete", "wipe"
}

type WatcherRegistry struct {
	mu          sync.RWMutex
	keyWatchers map[string][]chan WatchEvent // "ns:key" -> channels
	nsWatchers  map[string][]chan WatchEvent // "ns" -> channels
}

func NewWatcherRegistry() *WatcherRegistry {
	return &WatcherRegistry{
		keyWatchers: make(map[string][]chan WatchEvent),
		nsWatchers:  make(map[string][]chan WatchEvent),
	}
}

func (wr *WatcherRegistry) SubscribeKey(ns, key string) chan WatchEvent {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	ch := make(chan WatchEvent, 64) // Larger buffer for bursty updates
	fullKey := ns + ":" + key
	wr.keyWatchers[fullKey] = append(wr.keyWatchers[fullKey], ch)
	return ch
}

func (wr *WatcherRegistry) SubscribeNamespace(ns string) chan WatchEvent {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	ch := make(chan WatchEvent, 64)
	wr.nsWatchers[ns] = append(wr.nsWatchers[ns], ch)
	return ch
}

func (wr *WatcherRegistry) UnsubscribeKey(ns, key string, ch chan WatchEvent) {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	fullKey := ns + ":" + key
	watchers := wr.keyWatchers[fullKey]
	for i, w := range watchers {
		if w == ch {
			// Efficient delete without memory leaks
			copy(watchers[i:], watchers[i+1:])
			watchers[len(watchers)-1] = nil
			wr.keyWatchers[fullKey] = watchers[:len(watchers)-1]
			
			// Clean up map entry if no more watchers
			if len(wr.keyWatchers[fullKey]) == 0 {
				delete(wr.keyWatchers, fullKey)
			}
			return
		}
	}
}

func (wr *WatcherRegistry) UnsubscribeNamespace(ns string, ch chan WatchEvent) {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	watchers := wr.nsWatchers[ns]
	for i, w := range watchers {
		if w == ch {
			// Efficient delete without memory leaks
			copy(watchers[i:], watchers[i+1:])
			watchers[len(watchers)-1] = nil
			wr.nsWatchers[ns] = watchers[:len(watchers)-1]

			if len(wr.nsWatchers[ns]) == 0 {
				delete(wr.nsWatchers, ns)
			}
			return
		}
	}
}

func (wr *WatcherRegistry) Notify(event WatchEvent) {
	wr.mu.RLock()
	defer wr.mu.RUnlock()

	// Notify key watchers
	fullKey := event.Namespace + ":" + event.Key
	if watchers, ok := wr.keyWatchers[fullKey]; ok {
		for _, ch := range watchers {
			select {
			case ch <- event:
			default:
				// Buffer full, skip to avoid blocking the leader
			}
		}
	}

	// Notify namespace watchers
	if watchers, ok := wr.nsWatchers[event.Namespace]; ok {
		for _, ch := range watchers {
			select {
			case ch <- event:
			default:
				// Buffer full, skip
			}
		}
	}
}
