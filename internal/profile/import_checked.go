package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopurr/internal/localize"
)

// AddChecked imports a URL, exposing the complete candidate to accept before
// committing either its cache or index. The callback runs without the store
// mutex and can inspect the old profile or apply a candidate to the kernel.
func (s *Store) AddChecked(address, name string, accept func(Profile, []byte) error) (Profile, error) {
	source := Profile{URL: address}
	if old, err := s.Get(idOf(address)); err == nil {
		source.UserAgent, source.UseProxy = old.UserAgent, old.UseProxy
	}
	body, info, title, err := s.fetchProfile(source)
	if err != nil {
		return Profile{}, err
	}
	if err := validate(body); err != nil {
		return Profile{}, err
	}
	explicitName := name != ""
	if name == "" {
		name = firstNonEmpty(title, hostOf(address))
	}
	p := Profile{ID: idOf(address), Name: name, Kind: KindURL, URL: address, File: idOf(address) + ".yaml", Updated: time.Now(), Info: info}
	return s.importChecked(p, body, explicitName, accept)
}

func (s *Store) AddFileChecked(path, name string, accept func(Profile, []byte) error) (Profile, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Profile{}, err
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return Profile{}, fmt.Errorf(localize.Text("读取失败: %w"), err)
	}
	if err := validate(body); err != nil {
		return Profile{}, err
	}
	explicitName := name != ""
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	}
	id := idOf("file:" + abs)
	p := Profile{ID: id, Name: name, Kind: KindFile, Path: abs, File: id + ".yaml", Updated: time.Now()}
	return s.importChecked(p, body, explicitName, accept)
}

func (s *Store) importChecked(candidate Profile, body []byte, explicitName bool, accept func(Profile, []byte) error) (Profile, error) {
	s.mu.Lock()
	previousActive := s.Active
	var previous *Profile
	for _, p := range s.Items {
		if p.ID == candidate.ID {
			old := cloneProfile(p)
			previous = &old
			break
		}
	}
	if previous != nil {
		if !explicitName && previous.Name != "" {
			candidate.Name = previous.Name
		}
		candidate.UserAgent, candidate.UpdateHours, candidate.UseProxy = previous.UserAgent, previous.UpdateHours, previous.UseProxy
		candidate.Overrides = previous.Overrides
		candidate = cloneProfile(candidate)
	}
	path := filepath.Join(s.dir, "profiles", candidate.File)
	oldBody, err := os.ReadFile(path)
	hadFile := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.mu.Unlock()
		return Profile{}, err
	}
	s.mu.Unlock()
	if accept != nil {
		if err := accept(cloneProfile(candidate), body); err != nil {
			return Profile{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for i, p := range s.Items {
		if p.ID == candidate.ID {
			index = i
			if previous == nil || !sameImportProfile(p, *previous) {
				return Profile{}, errors.New(localize.Text("订阅正在更新，请稍后重试"))
			}
			break
		}
	}
	if previous != nil && index < 0 || s.Active != previousActive {
		return Profile{}, errors.New(localize.Text("订阅正在更新，请稍后重试"))
	}
	currentBody, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Profile{}, err
	}
	if hadFile != (err == nil) || !bytes.Equal(oldBody, currentBody) {
		return Profile{}, errors.New(localize.Text("订阅正在更新，请稍后重试"))
	}
	if err := writeContent(path, body); err != nil {
		return Profile{}, err
	}
	if index >= 0 {
		s.Items[index] = candidate
	} else {
		s.Items = append(s.Items, candidate)
	}
	if s.Active == "" {
		s.Active = candidate.ID
	}
	if err := s.save(); err != nil {
		s.Active = previousActive
		if index >= 0 {
			s.Items[index] = *previous
		} else {
			s.Items = s.Items[:len(s.Items)-1]
		}
		if hadFile {
			err = errors.Join(err, writeContent(path, oldBody))
		} else {
			err = errors.Join(err, os.Remove(path))
		}
		return Profile{}, err
	}
	return cloneProfile(candidate), nil
}

func sameImportProfile(a, b Profile) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && bytes.Equal(left, right)
}
