package shortcuts

import (
	"errors"
	"testing"
)

type fakeBackend struct {
	keys    map[string]func()
	blocked string
}

func (b *fakeBackend) Register(key string, fn func()) error {
	if key == b.blocked || b.keys[key] != nil {
		return errors.New("already in use")
	}
	b.keys[key] = fn
	return nil
}
func (b *fakeBackend) Unregister(key string) { delete(b.keys, key) }

func TestNormalizePortableBindings(t *testing.T) {
	for _, tc := range []struct{ raw, os, want string }{
		{"", "darwin", ""}, {" option + ctrl + p ", "darwin", "Ctrl+Alt+P"},
		{"CmdOrCtrl+Shift+Space", "darwin", "Super+Shift+Space"},
		{"CmdOrCtrl+Shift+Space", "windows", "Ctrl+Shift+Space"},
		{"Alt+F12", "windows", "Alt+F12"},
	} {
		got, err := Normalize(tc.raw, tc.os)
		if err != nil || got != tc.want {
			t.Fatalf("%q: %q, %v", tc.raw, got, err)
		}
	}
	for _, raw := range []string{"P", "Shift+P", "Ctrl+", "Ctrl+Ctrl+P", "Cmd+Super+P", "Ctrl+F13", "Ctrl+F01", "Ctrl+Space+P", "Ctrl+💥", "Ctrl+\nP"} {
		if _, err := Normalize(raw, "darwin"); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if _, err := (Config{"Ctrl+Alt+P", "alt+control+p"}).Normalized(); err == nil {
		t.Fatal("equivalent duplicate accepted")
	}
}

func TestRegistrationConflictRollbackAndActionSwap(t *testing.T) {
	b := &fakeBackend{keys: map[string]func(){}}
	var invoked Action
	m := New(b, func(action Action) { invoked = action })
	apply := func(c Config) {
		t.Helper()
		x, err := m.Prepare(c)
		if err != nil {
			t.Fatal(err)
		}
		x.Commit()
	}
	apply(Config{"Ctrl+Alt+P", "Ctrl+Alt+T"})
	b.blocked = "Ctrl+Alt+X"
	if _, err := m.Prepare(Config{"Ctrl+Alt+N", "Ctrl+Alt+X"}); err == nil {
		t.Fatal("conflict accepted")
	}
	if len(b.keys) != 2 || b.keys["Ctrl+Alt+N"] != nil {
		t.Fatal("conflict lost old bindings or leaked new ones")
	}
	b.keys["Ctrl+Alt+T"]()
	if invoked != ToggleProxy {
		t.Fatal("conflict changed old dispatch")
	}
	x, err := m.Prepare(Config{"Ctrl+Alt+N"})
	if err != nil {
		t.Fatal(err)
	}
	x.Rollback() // simulate a disk save failure
	if len(b.keys) != 2 || b.keys["Ctrl+Alt+N"] != nil {
		t.Fatal("rollback changed registrations")
	}
	apply(Config{"Ctrl+Alt+T", "Ctrl+Alt+P"})
	b.keys["Ctrl+Alt+T"]()
	if invoked != ShowWindow {
		t.Fatal("action swap failed")
	}
	apply(Config{})
	if len(b.keys) != 0 {
		t.Fatal("clearing did not release keys")
	}
	m.Close()
}
