package agy

import "sync"

// syncBuffer is a goroutine-safe growing byte buffer. The agy ConPTY reader
// goroutine appends rendered output while Send reads a windowed slice of it to
// capture a single turn's response.
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func newSyncBuffer() *syncBuffer { return &syncBuffer{} }

// Write appends p (satisfies io.Writer for io.Copy from the ConPTY).
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// Len returns the current byte length.
func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}

// From returns a copy of the bytes from offset to the end (clamped).
func (b *syncBuffer) From(offset int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if offset < 0 {
		offset = 0
	}
	if offset > len(b.buf) {
		offset = len(b.buf)
	}
	return string(b.buf[offset:])
}

// Reset discards all buffered bytes.
func (b *syncBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = b.buf[:0]
}
