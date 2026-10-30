package mesh

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/metacubex/tailscale/ipn"
	"gopurr/internal/securefs"
)

// Tailscale's node keys are kept separately from UI preferences and backups.
// Replacements are atomic and never follow a destination symlink.
type privateStore struct {
	mu     sync.Mutex
	path   string
	values map[ipn.StateKey][]byte
}

func newPrivateStore(path string) (*privateStore, error) {
	s := &privateStore{path: path, values: map[ipn.StateKey][]byte{}}
	if err := securefs.PrivateFile(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > 8<<20 {
		return nil, errors.New("组网身份文件无法读取或过大")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.values); err != nil {
		return nil, errors.New("组网身份文件损坏，未启动组网")
	}
	return s, nil
}

func (s *privateStore) ReadState(key ipn.StateKey) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	if !ok {
		return nil, ipn.ErrStateNotExist
	}
	return append([]byte(nil), v...), nil
}

func (s *privateStore) WriteState(key ipn.StateKey, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[ipn.StateKey][]byte, len(s.values)+1)
	for k, v := range s.values {
		next[k] = v
	}
	if value == nil {
		delete(next, key)
	} else {
		next[key] = append([]byte(nil), value...)
	}
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := securefs.WriteFile(s.path, b, 0600); err != nil {
		return err
	}
	s.values = next
	return nil
}
