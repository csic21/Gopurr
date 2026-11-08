// Package workflow coordinates configuration and profile commits without UI,
// transport, privileged-service or VPN-descriptor ownership. Hosts reserve a
// single writer before calling it and publish UI state only after success.
package workflow

import (
	"context"
	"errors"
)

// ConfigChange applies a prepared candidate before committing persistent state.
// A nil Apply stages a change without touching the current runtime. Restore is
// captured from the last accepted runtime, including host-owned mode/selection
// state when needed; it must not reconstruct an already invalid source.
type ConfigChange struct {
	Candidate []byte
	Apply     func(context.Context, []byte) error
	Restore   func(context.Context) error
	Commit    func(context.Context) error
}

type ConfigResult struct{ Applied bool }

// RunConfigChange treats a successful Commit as the point of no return. A
// simultaneous cancellation after it must not restore the old runtime behind
// committed disk state. Cancellation before commit or any attempted Apply
// failure restores independently of the cancelled request's context.
func RunConfigChange(ctx context.Context, change ConfigChange) (ConfigResult, error) {
	if change.Commit == nil {
		return ConfigResult{}, errors.New("configuration commit is required")
	}
	if err := ctx.Err(); err != nil {
		return ConfigResult{}, err
	}
	attempted := change.Apply != nil
	if attempted {
		if err := change.Apply(ctx, append([]byte(nil), change.Candidate...)); err != nil {
			return ConfigResult{}, restore(ctx, change.Restore, err)
		}
		if err := ctx.Err(); err != nil {
			return ConfigResult{}, restore(ctx, change.Restore, err)
		}
	}
	if err := ctx.Err(); err != nil {
		if attempted {
			err = restore(ctx, change.Restore, err)
		}
		return ConfigResult{}, err
	}
	if err := change.Commit(ctx); err != nil {
		if attempted {
			err = restore(ctx, change.Restore, err)
		}
		return ConfigResult{}, err
	}
	return ConfigResult{Applied: attempted}, nil
}

func restore(ctx context.Context, fn func(context.Context) error, cause error) error {
	if fn == nil {
		return cause
	}
	return errors.Join(cause, fn(context.WithoutCancel(ctx)))
}
