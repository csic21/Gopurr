package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopurr/internal/profile"
)

func profileFixture(t *testing.T) (*profile.Store, string, profile.Profile) {
	t.Helper()
	home := t.TempDir()
	store, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "source.yaml")
	os.WriteFile(path, []byte("rules: [\"MATCH,DIRECT\"]\n# old\n"), 0600)
	item, err := store.AddFile(path, "Source")
	if err != nil {
		t.Fatal(err)
	}
	return store, path, item
}
func TestProfileChangeRestoresRuntimeAndSourceOnIndexFailure(t *testing.T) {
	store, path, item := profileFixture(t)
	previous, _ := store.Content(item.ID)
	runtime := string(previous)
	os.WriteFile(path, []byte("rules: [\"MATCH,DIRECT\"]\n# new\n"), 0600)
	index := filepath.Join(filepath.Dir(path), "profiles.json")
	os.Remove(index)
	os.Mkdir(index, 0700)
	result, err := RunProfileChange(context.Background(), ProfileChange{Persist: func(accept func(profile.Profile, []byte) error) (profile.Profile, error) {
		return Import(context.Background(), store, ImportSource{Name: "Updated", Path: path}, accept)
	}, Prepare: func(_ context.Context, _ profile.Profile, raw []byte) (ProfilePlan, error) {
		return ProfilePlan{Config: raw, Apply: true}, nil
	}, Apply: func(_ context.Context, raw []byte) error { runtime = string(raw); return nil }, Restore: func(context.Context) error { runtime = string(previous); return nil }})
	after, _ := store.Content(item.ID)
	saved, _ := store.Get(item.ID)
	if err == nil || result.Applied || string(after) != string(previous) || runtime != string(previous) || saved.Name != item.Name || store.ActiveID() != item.ID {
		t.Fatal("index failure left source/runtime changed", result, err)
	}
}
func TestProfileChangeCanceledAfterAcceptanceCannotPersist(t *testing.T) {
	store, path, item := profileFixture(t)
	previous, _ := store.Content(item.ID)
	index, _ := os.ReadFile(filepath.Join(filepath.Dir(path), "profiles.json"))
	runtime := string(previous)
	os.WriteFile(path, []byte("rules: [\"MATCH,DIRECT\"]\n# new\n"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := RunProfileChange(ctx, ProfileChange{Persist: func(accept func(profile.Profile, []byte) error) (profile.Profile, error) {
		return Import(ctx, store, ImportSource{Path: path}, accept)
	}, Prepare: func(_ context.Context, _ profile.Profile, raw []byte) (ProfilePlan, error) {
		return ProfilePlan{Config: raw, Apply: true}, nil
	}, Apply: func(_ context.Context, raw []byte) error { runtime = string(raw); cancel(); return nil }, Restore: func(ctx context.Context) error {
		if ctx.Err() != nil {
			t.Fatal("restore was canceled")
		}
		runtime = string(previous)
		return nil
	}})
	after, _ := store.Content(item.ID)
	indexAfter, _ := os.ReadFile(filepath.Join(filepath.Dir(path), "profiles.json"))
	if !errors.Is(err, context.Canceled) || runtime != string(previous) || string(after) != string(previous) || string(indexAfter) != string(index) {
		t.Fatal("canceled acceptance committed", err)
	}
}
func TestProfileChangeStageOnlyPreservesRuntimeAndSharesRefresh(t *testing.T) {
	store, path, item := profileFixture(t)
	runtime := "working"
	os.WriteFile(path, []byte("rules: [\"MATCH,DIRECT\"]\n# staged\n"), 0600)
	result, err := RunProfileChange(context.Background(), ProfileChange{Persist: func(accept func(profile.Profile, []byte) error) (profile.Profile, error) {
		err := Refresh(context.Background(), store, item.ID, func(raw []byte) error { return accept(item, raw) })
		if err != nil {
			return profile.Profile{}, err
		}
		return store.Get(item.ID)
	}, Prepare: func(_ context.Context, _ profile.Profile, raw []byte) (ProfilePlan, error) {
		return ProfilePlan{Config: raw, Apply: false}, nil
	}, Apply: func(context.Context, []byte) error { runtime = "unexpected"; return nil }})
	after, _ := store.Content(item.ID)
	if err != nil || result.Applied || runtime != "working" || !strings.Contains(string(after), "# staged") {
		t.Fatal("staged source changed live runtime", result, err)
	}
}
func TestProfileChangeValidationFailureDoesNotTouchRuntimeOrDisk(t *testing.T) {
	store, path, item := profileFixture(t)
	before, _ := store.Content(item.ID)
	validation := errors.New("invalid platform config")
	applied := false
	_, err := RunProfileChange(context.Background(), ProfileChange{Persist: func(accept func(profile.Profile, []byte) error) (profile.Profile, error) {
		return Import(context.Background(), store, ImportSource{Path: path}, accept)
	}, Prepare: func(context.Context, profile.Profile, []byte) (ProfilePlan, error) { return ProfilePlan{}, validation }, Apply: func(context.Context, []byte) error { applied = true; return nil }})
	after, _ := store.Content(item.ID)
	if !errors.Is(err, validation) || applied || string(before) != string(after) {
		t.Fatal("validation changed accepted state", err)
	}
}
