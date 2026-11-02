package app

import (
	"errors"
	"testing"
	"time"
)

func TestServiceTransitionCancellationRestoresOrdinaryCore(t *testing.T) {
	var calls []string
	work := serviceTransition{stop: func() { calls = append(calls, "stop") }, change: func() error { return errors.New("authorization cancelled") },
		installed: func() bool { return false }, start: func(raw []byte) error { calls = append(calls, string(raw)); return nil }, previous: []byte("tun"), withoutTUN: []byte("ordinary"), wantedTUN: true}
	result := work.run()
	if result.err == nil || result.tun || len(calls) != 2 || calls[1] != "ordinary" {
		t.Fatalf("cancel did not restore core: %+v %v", result, calls)
	}
}

func TestServiceUninstallHandsBackToOrdinaryCore(t *testing.T) {
	var active string
	result := (serviceTransition{stop: func() {}, change: func() error { return nil }, installed: func() bool { return false },
		start: func(raw []byte) error { active = string(raw); return nil }, previous: []byte("tun"), withoutTUN: []byte("ordinary"), wantedTUN: true, removing: true}).run()
	if result.err != nil || result.tun || active != "ordinary" {
		t.Fatalf("bad handover: %+v active=%s", result, active)
	}
}

func TestServiceTransitionRestoresProxyCoreWhenTUNStartFails(t *testing.T) {
	failure := errors.New("TUN device unavailable")
	var active string
	result := (serviceTransition{stop: func() {}, change: func() error { return nil }, installed: func() bool { return true },
		start: func(raw []byte) error {
			if string(raw) == "tun" {
				return failure
			}
			active = string(raw)
			return nil
		}, previous: []byte("tun"), withoutTUN: []byte("ordinary"), wantedTUN: true}).run()
	if !errors.Is(result.err, failure) || !result.installed || result.tun || active != "ordinary" {
		t.Fatalf("TUN failure did not preserve a usable proxy core: %+v active=%s", result, active)
	}
}

func TestKernelRecoveryIsBoundedAndRequiresStableRecovery(t *testing.T) {
	var policy kernelRecoveryPolicy
	now := time.Now()
	if !policy.retry(now) || policy.retry(now) {
		t.Fatal("missing retry backoff")
	}
	if !policy.retry(now.Add(3*time.Second)) || !policy.retry(now.Add(10*time.Second)) || policy.retry(now.Add(time.Hour)) {
		t.Fatal("retry budget is not bounded")
	}
	policy.healthy(now.Add(time.Hour))
	policy.healthy(now.Add(time.Hour + 31*time.Second))
	if !policy.retry(now.Add(2 * time.Hour)) {
		t.Fatal("stable recovery did not reset retry budget")
	}
}
