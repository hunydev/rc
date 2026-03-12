package main

import "sync"

// OutputBuffer is a thread-safe growable buffer that stores all PTY output
// for session history replay. When capacity is exceeded, older data is discarded.
type OutputBuffer struct {
	mu       sync.RWMutex
	data     []byte
	maxSize  int
}

// NewOutputBuffer creates a buffer with the given max size in bytes.
func NewOutputBuffer(maxSize int) *OutputBuffer {
	return &OutputBuffer{
		data:    make([]byte, 0, maxSize),
		maxSize: maxSize,
	}
}

// Write appends data to the buffer. If the buffer would exceed maxSize,
// older data is trimmed from the front.
func (b *OutputBuffer) Write(p []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.data = append(b.data, p...)

	// Trim if over capacity
	if len(b.data) > b.maxSize {
		excess := len(b.data) - b.maxSize
		b.data = b.data[excess:]
	}
}

// Snapshot returns a copy of the entire buffered output.
func (b *OutputBuffer) Snapshot() []byte {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]byte, len(b.data))
	copy(out, b.data)
	return out
}

// Len returns the current buffer size.
func (b *OutputBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.data)
}

// Reset clears all buffered data.
func (b *OutputBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = b.data[:0]
}
