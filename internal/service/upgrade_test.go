package service

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestUpgradeRestoresPreviousFileAfterFailure(t *testing.T) {
	for _, failed := range []string{"prepare", "stop", "commit", "start", "verify", "none"} {
		t.Run(failed, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "core")
			os.WriteFile(path, []byte("old"), 0600)
			var calls []string
			step := func(name string, fn func() error) func() error {
				return func() error {
					calls = append(calls, name)
					if name == failed {
						return errors.New("injected " + name)
					}
					if fn != nil {
						return fn()
					}
					return nil
				}
			}
			update := serviceUpgrade{
				prepare: step("prepare", nil), stop: step("stop", nil),
				commit: step("commit", func() error { return os.WriteFile(path, []byte("new"), 0600) }),
				start:  step("start", nil), verify: step("verify", nil),
				rollback: step("rollback", func() error { return os.WriteFile(path, []byte("old"), 0600) }),
			}
			err := update.run()
			data, _ := os.ReadFile(path)
			if failed == "none" {
				if err != nil || string(data) != "new" {
					t.Fatal(err, string(data))
				}
			} else if err == nil || string(data) != "old" {
				t.Fatal("failed upgrade lost previous binary", err, string(data))
			}
			if failed == "prepare" && !reflect.DeepEqual(calls, []string{"prepare"}) {
				t.Fatal("preparation stopped live service", calls)
			}
			if failed == "stop" && !reflect.DeepEqual(calls, []string{"prepare", "stop"}) {
				t.Fatal("stop failure mutated service", calls)
			}
		})
	}
}
func TestUpgradePreservesRollbackFailure(t *testing.T) {
	broken := errors.New("new service failed")
	restore := errors.New("old service failed")
	ok := func() error { return nil }
	err := (serviceUpgrade{ok, ok, ok, func() error { return broken }, ok, func() error { return restore }}).run()
	if !errors.Is(err, broken) || !errors.Is(err, restore) {
		t.Fatal("lost actionable recovery error", err)
	}
}
