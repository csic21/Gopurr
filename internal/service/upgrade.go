package service

import (
	"errors"
	"fmt"
)

// Preparation must finish before the working service is stopped. Once stopped,
// every failed mutation/start/readiness check restores the previous deployment.
type serviceUpgrade struct {
	prepare, stop, commit, start, verify, rollback func() error
}

func (u serviceUpgrade) run() error {
	if err := u.prepare(); err != nil {
		return fmt.Errorf("prepare service update: %w", err)
	}
	if err := u.stop(); err != nil {
		return fmt.Errorf("stop service for update: %w", err)
	}
	for _, step := range []struct {
		name string
		run  func() error
	}{
		{"replace service", u.commit}, {"start updated service", u.start}, {"verify updated service", u.verify},
	} {
		if err := step.run(); err != nil {
			failure := fmt.Errorf("%s: %w", step.name, err)
			if rollbackErr := u.rollback(); rollbackErr != nil {
				return errors.Join(failure, fmt.Errorf("restore previous service: %w", rollbackErr))
			}
			return failure
		}
	}
	return nil
}
