package profile

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gopurr/internal/patch"
)

func TestCheckedReimportRejectsWithoutChangingStoredSource(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "url"}[remote], func(t *testing.T) {
			dir := t.TempDir()
			s, _ := Open(dir)
			body := "proxies: [{name: old, type: ss, server: old.example, port: 443, cipher: aes-128-gcm, password: old}]\n"
			path := filepath.Join(t.TempDir(), "source.yaml")
			var address string
			var remoteBody atomic.Value
			remoteBody.Store(body)
			if remote {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(remoteBody.Load().(string))) }))
				defer server.Close()
				address = server.URL
			}
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			var p Profile
			var err error
			if remote {
				p, err = s.Add(address, "custom name")
			} else {
				p, err = s.AddFile(path, "custom name")
			}
			if err != nil {
				t.Fatal(err)
			}
			hours := 24
			p.UpdateHours = &hours
			p.UserAgent = "custom-UA"
			if err := s.Edit(p); err != nil {
				t.Fatal(err)
			}
			if err := s.SetOverrides(p.ID, patch.ProfileOverrides{ExtraRules: []string{"DOMAIN,work.example,DIRECT"}}); err != nil {
				t.Fatal(err)
			}
			before, _ := s.Content(p.ID)
			indexBefore, _ := os.ReadFile(filepath.Join(dir, "profiles.json"))
			body = strings.ReplaceAll(body, "old", "new")
			remoteBody.Store(body)
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			called := false
			accept := func(candidate Profile, raw []byte) error {
				called = true
				// These calls prove acceptance happens outside s.mu.
				old, err := s.Get(p.ID)
				if err != nil {
					t.Fatal(err)
				}
				if candidate.Name != "custom name" || candidate.UserAgent != "custom-UA" || candidate.UpdateHours == nil || *candidate.UpdateHours != 24 || candidate.Overrides == nil || len(candidate.Overrides.ExtraRules) != 1 || old.Name != candidate.Name {
					t.Fatal("candidate lost prior source metadata")
				}
				if !bytes.Contains(raw, []byte("new.example")) {
					t.Fatal("accept saw old content")
				}
				candidate.Overrides.ExtraRules[0] = "mutated"
				return errors.New("kernel rejected")
			}
			if remote {
				_, err = s.AddChecked(address, "", accept)
			} else {
				_, err = s.AddFileChecked(path, "", accept)
			}
			if err == nil || !called {
				t.Fatal("rejected reimport succeeded")
			}
			after, _ := s.Content(p.ID)
			indexAfter, _ := os.ReadFile(filepath.Join(dir, "profiles.json"))
			if !bytes.Equal(before, after) || !bytes.Equal(indexBefore, indexAfter) {
				t.Fatal("rejection changed source cache or index")
			}
		})
	}
}

func TestSetActiveIndexFailureKeepsPreviousSelection(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	first := aggregateSourceFile(t, s, "first", "rules: [MATCH,DIRECT]\n")
	second := aggregateSourceFile(t, s, "second", "rules: [MATCH,DIRECT]\n")
	index := filepath.Join(dir, "profiles.json")
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0700); err != nil {
		t.Fatal(err)
	}
	for _, next := range []string{second.ID, ""} {
		if err := s.SetActive(next); err == nil {
			t.Fatal("expected index write failure")
		}
		if s.ActiveID() != first.ID {
			t.Fatal("failed activation changed selection")
		}
	}
}

func TestCheckedImportIndexFailureRestoresCacheAndActivation(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "replacement"}[existing], func(t *testing.T) {
			dir := t.TempDir()
			s, _ := Open(dir)
			path := filepath.Join(t.TempDir(), "source.yaml")
			old := []byte("rules: [MATCH,DIRECT]\n")
			if err := os.WriteFile(path, old, 0600); err != nil {
				t.Fatal(err)
			}
			var p Profile
			var err error
			if existing {
				p, err = s.AddFile(path, "old")
				if err != nil {
					t.Fatal(err)
				}
			}
			previousActive := s.ActiveID()
			indexPath := filepath.Join(dir, "profiles.json")
			if existing {
				if err := os.Remove(indexPath); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(indexPath, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("rules: [MATCH,REJECT]\n"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = s.AddFileChecked(path, "new", func(Profile, []byte) error { return nil })
			if err == nil {
				t.Fatal("expected index write failure")
			}
			if s.ActiveID() != previousActive {
				t.Fatal("failed import changed active profile")
			}
			if existing {
				raw, _ := s.Content(p.ID)
				saved, _ := s.Get(p.ID)
				if !bytes.Equal(raw, old) || saved.Name != "old" {
					t.Fatal("replacement failure lost old cache or metadata")
				}
			} else {
				if len(s.List()) != 0 {
					t.Fatal("failed new import left index entry")
				}
				entries, _ := os.ReadDir(filepath.Join(dir, "profiles"))
				if len(entries) != 0 {
					t.Fatal("failed new import left cached file")
				}
			}
		})
	}
}
