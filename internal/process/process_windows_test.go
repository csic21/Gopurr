package process

import (
	"os"
	"testing"
	"time"
)

func TestWindowsGracefulStopEvent(t *testing.T) {
	signals := make(chan os.Signal, 1)
	cleanup, err := NotifyStop(signals)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if !Alive(os.Getpid()) {
		t.Fatal("current process reported dead")
	}
	if err := Stop(self); err != nil {
		t.Fatal(err)
	}
	select {
	case <-signals:
	case <-time.After(2 * time.Second):
		t.Fatal("stop event was not delivered")
	}
}
