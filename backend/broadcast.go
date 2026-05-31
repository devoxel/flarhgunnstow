package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
)

// SessionBroadcaster is a per-session pub/sub hub. WebSocket clients subscribe
// to receive push notifications whenever the session state changes. Each
// broadcast carries a monotonically-increasing generation counter and a
// content hash so clients can detect gaps and self-correct.
type SessionBroadcaster struct {
	mu          sync.Mutex
	subscribers map[chan []byte]struct{}
	generation  atomic.Uint64
}

// NewSessionBroadcaster returns an empty broadcaster.
func NewSessionBroadcaster() *SessionBroadcaster {
	return &SessionBroadcaster{
		subscribers: map[chan []byte]struct{}{},
	}
}

// Subscribe registers a new client. The returned channel receives state
// snapshots as JSON-encoded wsMsg values. The channel is buffered so a slow
// client does not block the broadcaster; if the buffer is full the message is
// dropped (the client will self-correct via heartbeat).
func (sb *SessionBroadcaster) Subscribe() chan []byte {
	ch := make(chan []byte, 8)
	sb.mu.Lock()
	sb.subscribers[ch] = struct{}{}
	sb.mu.Unlock()
	return ch
}

// Unsubscribe removes a client. The channel is drained then closed.
func (sb *SessionBroadcaster) Unsubscribe(ch chan []byte) {
	sb.mu.Lock()
	delete(sb.subscribers, ch)
	sb.mu.Unlock()
	// Drain any lingering messages before closing so the sender never blocks.
	for {
		select {
		case <-ch:
		default:
			close(ch)
			return
		}
	}
}

// Broadcast pushes a state snapshot to every subscriber. It increments the
// generation counter and computes a SHA-256 hash of the serialised payload so
// clients can validate integrity. Slow subscribers are silently skipped.
func (sb *SessionBroadcaster) Broadcast(msg wsMsg) {
	gen := sb.generation.Add(1)
	msg.Generation = gen

	// Compute a compact content hash for client-side validation.
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("SessionBroadcaster: marshal: %v", err)
		return
	}
	h := sha256.Sum256(payload)
	msg.StateHash = fmt.Sprintf("%x", h[:8]) // first 8 bytes = 16 hex chars

	// Re-marshal with hash included.
	payload, err = json.Marshal(msg)
	if err != nil {
		log.Printf("SessionBroadcaster: marshal with hash: %v", err)
		return
	}

	sb.mu.Lock()
	defer sb.mu.Unlock()
	for ch := range sb.subscribers {
		select {
		case ch <- payload:
		default:
			// Subscriber too slow; drop. It will self-correct on next heartbeat.
		}
	}
}

// SubscriberCount returns the number of connected clients (useful for
// debugging and metrics).
func (sb *SessionBroadcaster) SubscriberCount() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return len(sb.subscribers)
}
