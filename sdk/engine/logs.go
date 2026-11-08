package engine

const logCapacity = 300

// logBuffer overwrites the oldest entry without allocating on the log hot path.
// Its owner holds logsMu; only an explicit diagnostic read copies the buffer.
type logBuffer struct {
	entries [logCapacity]Log
	next    int
	count   int
}

func (b *logBuffer) add(entry Log) {
	b.entries[b.next] = entry
	b.next = (b.next + 1) % logCapacity
	if b.count < logCapacity {
		b.count++
	}
}

func (b *logBuffer) snapshot() []Log {
	entries := make([]Log, b.count)
	start := (b.next - b.count + logCapacity) % logCapacity
	n := copy(entries, b.entries[start:min(start+b.count, logCapacity)])
	copy(entries[n:], b.entries[:b.count-n])
	return entries
}
