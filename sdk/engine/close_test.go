package engine

import (
	"testing"
	"time"

	"gopurr/internal/core"
)

func TestCloseDrainsProfileCommitWithoutHoldingStateLock(t *testing.T) {
	logsDone := make(chan struct{})
	close(logsDone)
	e := &Engine{core: &core.Core{}, logsDone: logsDone}
	// Model a profile writer which has crossed Store's final cancellation
	// check. Ownership must stay reserved until that commit exits its section.
	e.profileMu.Lock()
	done := make(chan error, 1)
	go func() { done <- e.Close() }()
	deadline := time.After(time.Second)
	for {
		e.mu.Lock()
		closing := e.closed
		e.mu.Unlock()
		if closing {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Close did not block new calls before draining")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case <-done:
		t.Fatal("Close released ownership while a profile commit was active")
	default:
	}
	stateAvailable := make(chan struct{})
	go func() { e.mu.Lock(); e.mu.Unlock(); close(stateAvailable) }()
	select {
	case <-stateAvailable:
	case <-time.After(time.Second):
		t.Fatal("Close waited for writer while holding state mutex")
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- e.Close() }()
	select {
	case <-secondDone:
		t.Fatal("second Close returned before cleanup completed")
	default:
	}
	e.profileMu.Unlock()
	for _, result := range []chan error{done, secondDone} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("Close did not finish after profile commit drained")
		}
	}
}
