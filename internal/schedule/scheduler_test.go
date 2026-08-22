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
