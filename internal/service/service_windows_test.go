package service

import (
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

func TestSCMStopWaitsUntilStartupAcceptsControls(t *testing.T) {
	states := []svc.Status{{State: svc.StartPending}, {State: svc.Running}, {State: svc.Running, Accepts: svc.AcceptStop}, {State: svc.StopPending}, {State: svc.Stopped}}
	index, stops := 0, 0
	err := waitWindowsServiceStopped(func() (svc.Status, error) {
		state := states[min(index, len(states)-1)]
		index++
		return state, nil
	}, func() error {
		if index != 3 {
			t.Fatal("stop sent before SCM accepted it")
		}
		stops++
		return nil
	}, time.Second, 0)
	if err != nil || stops != 1 || index != len(states) {
		t.Fatalf("stop lifecycle: calls=%d queries=%d err=%v", stops, index, err)
	}
}

func TestSCMStopRetriesOnlyTransitionalErrors(t *testing.T) {
	for _, transient := range []error{windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL, windows.ERROR_INVALID_SERVICE_CONTROL, windows.ERROR_SERVICE_NOT_ACTIVE} {
		t.Run(transient.Error(), func(t *testing.T) {
			stops := 0
			err := waitWindowsServiceStopped(func() (svc.Status, error) {
				if stops == 2 {
					return svc.Status{State: svc.Stopped}, nil
				}
				return svc.Status{State: svc.Running, Accepts: svc.AcceptStop}, nil
			}, func() error {
				stops++
				if stops == 1 {
					return transient
				}
				return nil
			}, time.Second, 0)
			if err != nil || stops != 2 {
				t.Fatalf("transitional control failure: calls=%d err=%v", stops, err)
			}
		})
	}
	denied := windows.ERROR_ACCESS_DENIED
	if err := waitWindowsServiceStopped(func() (svc.Status, error) { return svc.Status{State: svc.Running, Accepts: svc.AcceptStop}, nil }, func() error { return denied }, time.Second, 0); !errors.Is(err, denied) {
		t.Fatalf("permanent error was hidden: %v", err)
	}
}

func TestSCMStopPendingHasBoundedWait(t *testing.T) {
	err := waitWindowsServiceStopped(func() (svc.Status, error) { return svc.Status{State: svc.StartPending}, nil }, func() error { t.Fatal("unexpected stop control"); return nil }, time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("stuck startup never timed out")
	}
}
