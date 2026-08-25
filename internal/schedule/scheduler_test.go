package schedule

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-co-op/gocron/v2"
)

func TestAddRejectsNonFiveFieldCron(t *testing.T) {
	t.Parallel()
	scheduler, err := New(time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Add("bad", "0 0 * * * *", func(context.Context) error { return nil }, nil); err == nil {
		t.Fatal("expected six-field cron expression to be rejected")
	}
}

func TestExecuteReportsErrorsAndPanics(t *testing.T) {
	t.Parallel()
	want := errors.New("failed")
	var got error
	execute(context.Background(), func(context.Context) error { return want }, func(err error) { got = err })
	if !errors.Is(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	got = nil
	execute(context.Background(), func(context.Context) error { panic("boom") }, func(err error) { got = err })
	if got == nil || !strings.Contains(got.Error(), "scheduled task panic: boom") {
		t.Fatalf("got panic error %v", got)
	}
}

func TestSchedulerPreventsOverlappingInvocations(t *testing.T) {
	scheduler, err := New(time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	if err := scheduler.add("overlap", gocron.DurationJob(100*time.Millisecond), func(context.Context) error {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return nil
	}, nil, gocron.WithStartAt(gocron.WithStartImmediately())); err != nil {
		t.Fatal(err)
	}
	scheduler.Start()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}
	time.Sleep(220 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("got %d concurrent calls while the first was blocked, want one", got)
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerQueuesDifferentJobsAndRunsSerially(t *testing.T) {
	scheduler, err := New(time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	completed := make(chan string, 2)
	started := make(chan string, 2)
	var active atomic.Int32
	var overlap atomic.Bool
	task := func(name string) func(context.Context) error {
		return func(context.Context) error {
			if active.Add(1) != 1 {
				overlap.Store(true)
			}
			defer active.Add(-1)
			started <- name
			<-release
			completed <- name
			return nil
		}
	}

	for _, name := range []string{"first", "second"} {
		if err := scheduler.add(name, gocron.OneTimeJob(gocron.OneTimeJobStartImmediately()), task(name), nil); err != nil {
			t.Fatal(err)
		}
	}

	scheduler.Start()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := scheduler.Shutdown(ctx); err != nil {
			t.Errorf("shutdown scheduler: %v", err)
		}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first job did not start")
	}

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	for scheduler.scheduler.JobsWaitingInQueue() != 1 {
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatalf("got %d queued jobs, want one", scheduler.scheduler.JobsWaitingInQueue())
		}
	}

	if got := active.Load(); got != 1 {
		t.Fatalf("got %d active jobs while one was queued, want one", got)
	}
	close(release)

	finished := make(map[string]bool, 2)
	for len(finished) < 2 {
		select {
		case name := <-completed:
			finished[name] = true
		case <-time.After(time.Second):
			t.Fatalf("completed jobs %v, want first and second", finished)
		}
	}
	if !finished["first"] || !finished["second"] {
		t.Fatalf("completed jobs %v, want first and second", finished)
	}
	if overlap.Load() {
		t.Fatal("jobs overlapped")
	}
}
