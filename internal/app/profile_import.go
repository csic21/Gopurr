package app

import (
	"errors"
	"time"

	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

// importManagedProfile keeps a reimport's source cache and running kernel in
// the same transaction, then rebuilds dependent aggregates without fetching
// the source a second time. Callers hold beginProfileWork for this operation.
func (a *App) importManagedProfile(importer func(func(profile.Profile, []byte) error) (profile.Profile, error)) (profile.Profile, error) {
	running := a.k != nil && a.k.Running()
	var previous []byte
	var err error
	if running {
		previous, err = a.lastAcceptedConfig()
		if err != nil {
			return profile.Profile{}, err
		}
	}
	activeID := a.store.ActiveID()
	attempted := false
	p, err := importer(func(candidate profile.Profile, raw []byte) error {
		active := activeID == "" || activeID == candidate.ID
		local := patch.ProfileOverrides{}
		if candidate.Overrides != nil {
			local = *candidate.Overrides
		}
		if !active && candidate.Overrides == nil {
			return nil
		}
		global := a.runtimeOverrides()
		if !active {
			global.ExtraRules, global.AppRules, global.TunEnable = nil, nil, false
		}
		composed, err := patch.ApplyLayers(raw, global, local)
		if err != nil {
			return err
		}
		if active && a.k != nil {
			attempted = true
			return a.k.Start(composed)
		}
		return nil
	})
	if err != nil {
		if attempted {
			if running {
				err = errors.Join(err, a.k.Start(previous))
			} else {
				err = errors.Join(err, a.k.Stop())
			}
		}
		return p, err
	}
	a.featureCacheAt = time.Time{}
	if attempted {
		a.cancelDelayTest()
		a.proxyGroup = ""
	}
	return p, a.refreshDependentAggregates(p.ID)
}
