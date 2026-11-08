package engine

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestLogBufferRetainsNewestInChronologicalOrder(t *testing.T) {
	var buffer logBuffer
	for _, total := range []int{0, 7, logCapacity, logCapacity + 1, logCapacity*3 + 17} {
		buffer = logBuffer{}
		for i := 0; i < total; i++ {
			buffer.add(Log{Text: strconv.Itoa(i)})
		}
		entries := buffer.snapshot()
		want := min(total, logCapacity)
		if len(entries) != want {
			t.Fatalf("after %d writes: got %d entries, want %d", total, len(entries), want)
		}
		for i, entry := range entries {
			if entry.Text != strconv.Itoa(total-want+i) {
				t.Fatalf("after %d writes: entry %d = %q", total, i, entry.Text)
			}
		}
		if len(entries) == 0 {
			body, _ := json.Marshal(entries)
			if string(body) != "[]" {
				t.Fatalf("empty diagnostics must remain a JSON array: %s", body)
			}
		} else {
			entries[0].Text = "changed snapshot"
			buffer.add(Log{Text: "later"})
			if buffer.snapshot()[0].Text == "changed snapshot" || entries[len(entries)-1].Text == "later" {
				t.Fatal("diagnostic reads share mutable storage with the writer")
			}
		}
	}
}

func BenchmarkLogRetention(b *testing.B) {
	entry := Log{Level: "info", Text: "fixture connection event"}
	b.Run("previous-copy", func(b *testing.B) {
		entries := make([]Log, logCapacity)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			entries = append(entries, entry)
			if len(entries) > logCapacity {
				entries = append([]Log(nil), entries[len(entries)-logCapacity:]...)
			}
		}
		if len(entries) != logCapacity {
			b.Fatal("incorrect retention")
		}
	})
	b.Run("ring", func(b *testing.B) {
		var buffer logBuffer
		for i := 0; i < logCapacity; i++ {
			buffer.add(entry)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buffer.add(entry)
		}
		if buffer.snapshot()[0] != entry {
			b.Fatal("incorrect retention")
		}
	})
}
