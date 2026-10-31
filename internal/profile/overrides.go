package profile

import (
	"fmt"
	"slices"

	"gopurr/internal/localize"
	"gopurr/internal/patch"
)

// Profiles returned to editors must not share their mutable settings with the
// store. Only a successful atomic save publishes an update.
func cloneProfile(p Profile) Profile {
	if p.Aggregate != nil {
		spec := *p.Aggregate
		spec.Sources = slices.Clone(spec.Sources)
		p.Aggregate = &spec
	}
	if p.UpdateHours != nil {
		hours := *p.UpdateHours
		p.UpdateHours = &hours
	}
	if p.Overrides != nil {
		local := *p.Overrides
		local.ExtraRules = slices.Clone(local.ExtraRules)
		local.TemplateIDs = slices.Clone(local.TemplateIDs)
		local.ProxyEdits = patch.CloneProxyEdits(local.ProxyEdits)
		if local.DNS != nil {
			dns := *local.DNS
			dns.Nameserver = slices.Clone(dns.Nameserver)
			dns.DefaultNameserver = slices.Clone(dns.DefaultNameserver)
			dns.Fallback = slices.Clone(dns.Fallback)
			dns.FakeIPFilter = slices.Clone(dns.FakeIPFilter)
			local.DNS = &dns
		}
		p.Overrides = &local
	}
	return p
}

func (s *Store) Get(id string) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.Items {
		if p.ID == id {
			return cloneProfile(p), nil
		}
	}
	return Profile{}, fmt.Errorf("%s", localize.Text("订阅不存在，请重新选择"))
}

func (s *Store) SetOverrides(id string, next patch.ProfileOverrides) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.Items {
		if p.ID != id {
			continue
		}
		p.Overrides = &next
		p = cloneProfile(p)
		old := s.Items[i]
		s.Items[i] = p
		if err := s.save(); err != nil {
			s.Items[i] = old
			return err
		}
		return nil
	}
	return fmt.Errorf("%s", localize.Text("订阅不存在，请重新选择"))
}
