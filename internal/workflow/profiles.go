package workflow

import (
	"context"
	"errors"

	"gopurr/internal/profile"
)

// ImportSource has exactly one source. Path is desktop-only; a mobile document
// picker supplies YAML bytes so temporary platform URIs never become sources.
type ImportSource struct{ Name, URL, YAML, Path string }

func Import(ctx context.Context, store *profile.Store, source ImportSource, accept func(profile.Profile, []byte) error) (profile.Profile, error) {
	if store == nil {
		return profile.Profile{}, errors.New("profile store is required")
	}
	if err := ctx.Err(); err != nil {
		return profile.Profile{}, err
	}
	count := 0
	for _, value := range []string{source.URL, source.YAML, source.Path} {
		if value != "" {
			count++
		}
	}
	if count != 1 {
		return profile.Profile{}, errors.New("exactly one profile source is required")
	}
	switch {
	case source.URL != "":
		return store.AddCheckedContext(ctx, source.URL, source.Name, accept)
	case source.Path != "":
		return store.AddFileCheckedContext(ctx, source.Path, source.Name, accept)
	default:
		return store.AddBytesCheckedContext(ctx, source.Name, []byte(source.YAML), accept)
	}
}

func Refresh(ctx context.Context, store *profile.Store, id string, accept func([]byte) error) error {
	if store == nil {
		return errors.New("profile store is required")
	}
	return store.RefreshCheckedContext(ctx, id, accept)
}

// ProfilePlan separates source validation from runtime policy. Desktop active
// imports may apply immediately; a connected mobile host may stage the same
// valid source and explicitly reconnect later. No FD or OS policy crosses here.
type ProfilePlan struct {
	Config []byte
	Apply  bool
}
type ProfileChange struct {
	Persist func(func(profile.Profile, []byte) error) (profile.Profile, error)
	Prepare func(context.Context, profile.Profile, []byte) (ProfilePlan, error)
	Apply   func(context.Context, []byte) error
	Restore func(context.Context) error
}
type ProfileResult struct {
	Profile profile.Profile
	Applied bool
}

// RunProfileChange joins checked cache/index writes with runtime acceptance.
// Persist owns its atomic disk commit and must observe ctx before committing;
// Store's *CheckedContext methods provide that guarantee. A failed index/cache
// write after acceptance restores the last runtime, not the candidate source.
func RunProfileChange(ctx context.Context, change ProfileChange) (ProfileResult, error) {
	if change.Persist == nil {
		return ProfileResult{}, errors.New("profile persistence is required")
	}
	if err := ctx.Err(); err != nil {
		return ProfileResult{}, err
	}
	attempted := false
	item, err := change.Persist(func(item profile.Profile, raw []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		plan := ProfilePlan{Config: raw}
		if change.Prepare != nil {
			var err error
			plan, err = change.Prepare(ctx, item, append([]byte(nil), raw...))
			if err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if plan.Apply {
			if change.Apply == nil {
				return errors.New("profile runtime application is required")
			}
			attempted = true
			if err := change.Apply(ctx, append([]byte(nil), plan.Config...)); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
	if err != nil {
		if attempted {
			err = restore(ctx, change.Restore, err)
		}
		return ProfileResult{Profile: item}, err
	}
	return ProfileResult{Profile: item, Applied: attempted}, nil
}
