package app

import (
	"context"
	"time"

	"gopurr/internal/patch"
	"gopurr/internal/profile"
	"gopurr/internal/workflow"
)

// importManagedProfile keeps reimported source/cache and running configuration
// in one transaction. Legacy callers keep their existing background lifetime.
func (a *App) importManagedProfile(importer func(func(profile.Profile, []byte) error) (profile.Profile, error)) (profile.Profile, error) {
	return a.importManagedProfileContext(context.Background(), importer)
}

func (a *App) importManagedProfileContext(ctx context.Context, importer func(func(profile.Profile, []byte) error) (profile.Profile, error)) (profile.Profile, error) {
	running := a.k != nil && a.k.RunningForChange()
	var previous []byte
	if running {
		var err error
		previous, err = a.lastAcceptedConfig()
		if err != nil {
			return profile.Profile{}, err
		}
	}
	activeID := a.store.ActiveID()
	result, err := workflow.RunProfileChange(ctx, workflow.ProfileChange{
		Persist: importer,
		Prepare: func(_ context.Context, candidate profile.Profile, raw []byte) (workflow.ProfilePlan, error) {
			active := activeID == "" || activeID == candidate.ID
			local := patch.ProfileOverrides{}
			if candidate.Overrides != nil {
				local = *candidate.Overrides
			}
			if !active && candidate.Overrides == nil {
				return workflow.ProfilePlan{}, nil
			}
			global := a.runtimeOverrides()
			if !active {
				global.ExtraRules, global.AppRules, global.TunEnable = nil, nil, false
			}
			composed, err := patch.ApplyLayers(raw, global, local)
			return workflow.ProfilePlan{Config: composed, Apply: active && a.k != nil}, err
		},
		Apply: func(_ context.Context, raw []byte) error { return a.k.Start(raw) },
		Restore: func(context.Context) error {
			if running {
				return a.k.Start(previous)
			}
			return a.k.Stop()
		},
	})
	if err != nil {
		return result.Profile, err
	}
	a.featureCacheAt = time.Time{}
	if result.Applied {
		a.cancelDelayTest()
		a.proxyGroup = ""
	}
	// A committed source is authoritative. Rebuild dependents as before, even
	// if its original requester disappears after the source commit succeeded.
	return result.Profile, a.refreshDependentAggregatesContext(context.WithoutCancel(ctx), result.Profile.ID)
}
