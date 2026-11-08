package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestConfigChangeRestoresAfterApplyOrCommitFailure(t *testing.T) {
	applyErr, commitErr, restoreErr := errors.New("partial apply"), errors.New("disk commit"), errors.New("restore failed")
	for _, stage := range []string{"apply", "commit", "restore"} {
		t.Run(stage, func(t *testing.T) {
			runtime := "old"
			events := []string{}
			_, err := RunConfigChange(context.Background(), ConfigChange{Candidate: []byte("new"), Apply: func(_ context.Context, raw []byte) error {
				events = append(events, "apply")
				runtime = string(raw)
				if stage == "apply" {
					return applyErr
				}
				return nil
			}, Commit: func(context.Context) error { events = append(events, "commit"); return commitErr }, Restore: func(ctx context.Context) error {
				events = append(events, "restore")
				if ctx.Err() != nil {
					t.Fatal("restore received canceled context")
				}
				runtime = "old"
				if stage == "restore" {
					return restoreErr
				}
				return nil
			}})
			expected := commitErr
			if stage == "apply" {
				expected = applyErr
			}
			if !errors.Is(err, expected) || runtime != "old" {
				t.Fatal("failed change lost accepted runtime", runtime, err)
			}
			want := []string{"apply", "commit", "restore"}
			if stage == "apply" {
				want = []string{"apply", "restore"}
			}
			if !reflect.DeepEqual(events, want) {
				t.Fatal(events)
			}
			if stage == "restore" && !errors.Is(err, restoreErr) {
				t.Fatal("restore failure was hidden", err)
			}
		})
	}
}
func TestConfigChangeCancellationAndCommitBoundary(t *testing.T) {
	for _, commitSucceeded := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel before commit", true: "cancel after commit"}[commitSucceeded], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runtime := "old"
			committed, restored := false, false
			result, err := RunConfigChange(ctx, ConfigChange{Candidate: []byte("new"), Apply: func(_ context.Context, raw []byte) error {
				runtime = string(raw)
				if !commitSucceeded {
					cancel()
				}
				return nil
			}, Commit: func(context.Context) error { committed = true; cancel(); return nil }, Restore: func(ctx context.Context) error {
				if ctx.Err() != nil {
					t.Fatal("cannot restore with canceled context")
				}
				restored = true
				runtime = "old"
				return nil
			}})
			if commitSucceeded {
				if err != nil || !result.Applied || !committed || restored || runtime != "new" {
					t.Fatal(result, err)
				}
			} else if !errors.Is(err, context.Canceled) || committed || !restored || runtime != "old" {
				t.Fatal("canceled candidate committed", result, err)
			}
		})
	}
}
func TestConfigChangeStagesWithoutRuntimeAndRejectsCanceledWork(t *testing.T) {
	committed := false
	result, err := RunConfigChange(context.Background(), ConfigChange{Commit: func(context.Context) error { committed = true; return nil }})
	if err != nil || !committed || result.Applied {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	committed = false
	_, err = RunConfigChange(ctx, ConfigChange{Commit: func(context.Context) error { committed = true; return nil }})
	if !errors.Is(err, context.Canceled) || committed {
		t.Fatal("canceled transaction entered commit", err)
	}
}
