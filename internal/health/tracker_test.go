package health

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestTrackerKeepsFirstDisableReason(t *testing.T) {
	t.Parallel()
	tracker := New()
	if !tracker.Enabled("sonarr") {
		t.Fatal("new instance is disabled")
	}
	if !tracker.Disable("sonarr", "first") {
		t.Fatal("first disable was not recorded")
	}
	if tracker.Disable("sonarr", "second") {
		t.Fatal("second disable replaced the first")
	}
	if tracker.Enabled("sonarr") || tracker.Reason("sonarr") != "first" {
		t.Fatalf("enabled=%t reason=%q", tracker.Enabled("sonarr"), tracker.Reason("sonarr"))
	}
}

func TestTrackerConcurrentDisable(t *testing.T) {
	t.Parallel()
	tracker := New()
	var winners atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 50; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if tracker.Disable("radarr", "rejected") {
				winners.Add(1)
			}
		}()
	}
	wait.Wait()
	if winners.Load() != 1 {
		t.Fatalf("got %d successful disables, want 1", winners.Load())
	}
}
