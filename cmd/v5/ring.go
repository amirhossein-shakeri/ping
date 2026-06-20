package main

import "sync"

type Ring[T any] struct {
	mu     sync.RWMutex
	values []T
	next   int
	filled bool
}

func NewRing[T any](size int) *Ring[T] {
	return &Ring[T]{values: make([]T, size)}
}

func (ring *Ring[T]) Add(value T) {
	ring.mu.Lock()
	defer ring.mu.Unlock()

	ring.values[ring.next] = value
	ring.next++
	if ring.next >= len(ring.values) {
		ring.next = 0
		ring.filled = true
	}
}

func (ring *Ring[T]) Snapshot() []T {
	ring.mu.RLock()
	defer ring.mu.RUnlock()

	if len(ring.values) == 0 {
		return nil
	}

	if !ring.filled {
		out := make([]T, ring.next)
		copy(out, ring.values[:ring.next])
		return out
	}

	out := make([]T, len(ring.values))
	copy(out, ring.values[ring.next:])
	copy(out[len(ring.values)-ring.next:], ring.values[:ring.next])
	return out
}
