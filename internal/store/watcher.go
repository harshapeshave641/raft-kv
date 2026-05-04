package store

import (
	"sync"
)

type WatchEvent struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	Type      string `json:"type"` // "set" or "delete"
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

	ch := make(chan WatchEvent, 10)
	fullKey := ns + ":" + key
	wr.keyWatchers[fullKey] = append(wr.keyWatchers[fullKey], ch)
	return ch
}

func (wr *WatcherRegistry) SubscribeNamespace(ns string) chan WatchEvent {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	ch := make(chan WatchEvent, 10)
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
			wr.keyWatchers[fullKey] = append(watchers[:i], watchers[i+1:]...)
			close(ch)
			break
		}
	}
}

func (wr *WatcherRegistry) UnsubscribeNamespace(ns string, ch chan WatchEvent) {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	watchers := wr.nsWatchers[ns]
	for i, w := range watchers {
		if w == ch {
			wr.nsWatchers[ns] = append(watchers[:i], watchers[i+1:]...)
			close(ch)
			break
		}
	}
}

func (wr *WatcherRegistry) Notify(event WatchEvent) {
	wr.mu.RLock()
	defer wr.mu.RUnlock()

	// Notify key watchers
	fullKey := event.Namespace + ":" + event.Key
	for _, ch := range wr.keyWatchers[fullKey] {
		select {
		case ch <- event:
		default:
			// Buffer full, skip to avoid blocking the whole cluster
		}
	}

	// Notify namespace watchers
	for _, ch := range wr.nsWatchers[event.Namespace] {
		select {
		case ch <- event:
		default:
			// Buffer full, skip
		}
	}
}
