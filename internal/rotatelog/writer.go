// Package rotatelog bounds local diagnostic logs and coalesces repeated errors.
package rotatelog

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sync"
	"time"
)

const MaxBytes int64 = 10 << 20
const KeepFiles = 3 // active file plus two archives

var timestamp = regexp.MustCompile(`^(?:\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)? |time="[^"]+" )`)

type Writer struct {
	mu          sync.Mutex
	path        string
	file        *os.File
	size, limit int64
	keep        int
	key         string
	repeats     int
	last        time.Time
	closed      bool
}

func Open(path string) (*Writer, error) { return open(path, MaxBytes, KeepFiles) }

func open(path string, limit int64, keep int) (*Writer, error) {
	if limit <= 0 || keep < 1 {
		return nil, fmt.Errorf("invalid log limits")
	}
	w := &Writer{path: path, limit: limit, keep: keep}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	w.file = f
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	w.size = info.Size()
	// Adopt old unlimited logs without retaining hundreds of MB in an archive.
	if w.size > limit {
		data, err := Tail(path, limit)
		if err != nil {
			f.Close()
			return nil, err
		}
		if err = f.Truncate(0); err != nil {
			f.Close()
			return nil, err
		}
		if _, err = f.WriteAt(data, 0); err != nil {
			f.Close()
			return nil, err
		}
		w.size = int64(len(data))
	}
	_, err = f.Seek(0, io.SeekEnd)
	if err != nil {
		f.Close()
		return nil, err
	}
	return w, nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	key := ""
	// Logger writes are complete lines. Arbitrary subprocess chunks still rotate
	// correctly, but must not be coalesced unless they are a single whole record.
	if bytes.Count(p, []byte{'\n'}) == 1 && p[len(p)-1] == '\n' {
		key = timestamp.ReplaceAllString(string(p), "")
	}
	now := time.Now()
	if key != "" && key == w.key {
		w.repeats++
		if now.Sub(w.last) < 5*time.Second {
			return len(p), nil
		}
		if err := w.flushRepeats(); err != nil {
			return 0, err
		}
		w.last = now
		return len(p), nil
	}
	if err := w.flushRepeats(); err != nil {
		return 0, err
	}
	if err := w.write(p); err != nil {
		return 0, err
	}
	w.key, w.last = key, now
	return len(p), nil
}

func (w *Writer) flushRepeats() error {
	if w.repeats == 0 {
		return nil
	}
	n := w.repeats
	if err := w.write([]byte(fmt.Sprintf("Previous log message repeated %d times\n", n))); err != nil {
		return err
	}
	w.repeats = 0
	return nil
}

func (w *Writer) write(p []byte) error {
	for len(p) > 0 {
		if w.size >= w.limit {
			if err := w.rotate(); err != nil {
				return err
			}
		}
		count := min(int64(len(p)), w.limit-w.size)
		n, err := w.file.Write(p[:count])
		w.size += int64(n)
		p = p[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	for i := w.keep - 1; i >= 1; i-- {
		dest := fmt.Sprintf("%s.%d", w.path, i)
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			return w.reopenAfterError(err)
		}
		source := w.path
		if i > 1 {
			source = fmt.Sprintf("%s.%d", w.path, i-1)
		}
		if err := os.Rename(source, dest); err != nil && !os.IsNotExist(err) {
			return w.reopenAfterError(err)
		}
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	w.file, w.size = f, 0
	return nil
}

func (w *Writer) reopenAfterError(cause error) error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		w.file = f
	}
	return cause
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	err := w.flushRepeats()
	w.closed = true
	closeErr := w.file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Tail bounds diagnostic memory use even when reading an old unlimited log.
func Tail(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	offset := max(int64(0), info.Size()-limit)
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if offset > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return data, err
}
