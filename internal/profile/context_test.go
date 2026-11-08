package profile

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCanceledDownloadDoesNotCommitProfile(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(canceled) }))
	defer server.Close()
	store, _ := Open(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := store.AddCheckedContext(ctx, server.URL, "Source", nil); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("download did not begin")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("download ignored cancellation")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("HTTP connection did not close")
	}
	if len(store.List()) != 0 || store.ActiveID() != "" {
		t.Fatal("canceled download persisted a profile")
	}
}
func TestContextImportAndRefreshCancelAfterAcceptWithoutChangingFiles(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "reimport", true: "refresh"}[refresh], func(t *testing.T) {
			home := t.TempDir()
			store, _ := Open(home)
			path := filepath.Join(home, "source.yaml")
			os.WriteFile(path, []byte("rules: [\"MATCH,DIRECT\"]\n# old\n"), 0600)
			item, err := store.AddFile(path, "Source")
			if err != nil {
				t.Fatal(err)
			}
			old, _ := store.Content(item.ID)
			index, _ := os.ReadFile(filepath.Join(home, "profiles.json"))
			os.WriteFile(path, []byte("rules: [\"MATCH,DIRECT\"]\n# new\n"), 0600)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if refresh {
				err = store.RefreshCheckedContext(ctx, item.ID, func([]byte) error { cancel(); return nil })
			} else {
				_, err = store.AddFileCheckedContext(ctx, path, "Updated", func(Profile, []byte) error { cancel(); return nil })
			}
			body, _ := store.Content(item.ID)
			after, _ := os.ReadFile(filepath.Join(home, "profiles.json"))
			if !errors.Is(err, context.Canceled) || string(body) != string(old) || string(after) != string(index) {
				t.Fatal("canceled acceptance reached disk", err)
			}
		})
	}
}
func TestRefreshRejectsChangedMetadataAfterAccept(t *testing.T) {
	home := t.TempDir()
	store, _ := Open(home)
	path := filepath.Join(home, "source.yaml")
	os.WriteFile(path, []byte("rules: [\"MATCH,DIRECT\"]\n# old\n"), 0600)
	item, _ := store.AddFile(path, "Source")
	before, _ := store.Content(item.ID)
	os.WriteFile(path, []byte("rules: [\"MATCH,DIRECT\"]\n# new\n"), 0600)
	err := store.RefreshCheckedContext(context.Background(), item.ID, func([]byte) error { item.Name = "Changed during acceptance"; return store.Edit(item) })
	after, _ := store.Content(item.ID)
	saved, _ := store.Get(item.ID)
	if err == nil || string(before) != string(after) || saved.Name != item.Name {
		t.Fatal("stale refresh overwrote a concurrent edit", err)
	}
}
func TestOversizedDownloadRejectedBeforeValidationOrCommit(t *testing.T) {
	body := strings.Repeat("#", MaxDocumentBytes+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
	defer server.Close()
	store, _ := Open(t.TempDir())
	_, err := store.AddCheckedContext(context.Background(), server.URL, "Too large", nil)
	if !errors.Is(err, ErrDocumentTooLarge) || len(store.List()) != 0 {
		t.Fatal("oversized document accepted", err)
	}
}
func TestRemoveIndexFailurePreservesMemorySelectionAndCache(t *testing.T) {
	home := t.TempDir()
	store, _ := Open(home)
	item, err := store.AddBytesChecked("Source", []byte("rules: [\"MATCH,DIRECT\"]\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := store.Content(item.ID)
	revision := store.Revision()
	index := filepath.Join(home, "profiles.json")
	os.Remove(index)
	os.Mkdir(index, 0700)
	if err := store.RemoveContext(context.Background(), item.ID); err == nil {
		t.Fatal("index write failure ignored")
	}
	body, err := store.Content(item.ID)
	if err != nil || string(body) != string(before) || store.ActiveID() != item.ID || len(store.List()) != 1 || store.Revision() != revision {
		t.Fatal("failed delete lost source/selection", err)
	}
}
func TestCanceledSelectionAndRemoveCannotCommit(t *testing.T) {
	store, _ := Open(t.TempDir())
	first, _ := store.AddBytesChecked("First", []byte("rules: [\"MATCH,DIRECT\"]\n# first\n"), nil)
	next, _ := store.AddBytesChecked("Next", []byte("rules: [\"MATCH,DIRECT\"]\n# second\n"), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.SetActiveContext(ctx, next.ID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.RemoveContext(ctx, first.ID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if store.ActiveID() != first.ID || len(store.List()) != 2 {
		t.Fatal("canceled selection/delete committed")
	}
}
