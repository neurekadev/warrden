// Package schedule owns cron registration and graceful scheduler shutdown.
package schedule

import (
	"context"
	"fmt"
	"time"

	"github.com/go-co-op/gocron/v2"
)

// Scheduler owns all configured cron jobs.
type Scheduler struct{ scheduler gocron.Scheduler }

// New creates a scheduler in location.
func New(location *time.Location) (*Scheduler, error) {
	scheduler, err := gocron.NewScheduler(gocron.WithLocation(location))
	if err != nil {
		return nil, fmt.Errorf("create scheduler: %w", err)
	}
	return &Scheduler{scheduler: scheduler}, nil
}

// Add registers a non-overlapping five-field cron task.
func (s *Scheduler) Add(name, expression string, task func(context.Context) error, onError func(error)) error {
	return s.add(name, gocron.CronJob(expression, false), task, onError)
}

func (s *Scheduler) add(name string, definition gocron.JobDefinition, task func(context.Context) error, onError func(error), extra ...gocron.JobOption) error {
	options := []gocron.JobOption{
		gocron.WithName(name),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	}
	options = append(options, extra...)
	_, err := s.scheduler.NewJob(
		definition,
		gocron.NewTask(func(ctx context.Context) { execute(ctx, task, onError) }),
		options...,
	)
	if err != nil {
		return fmt.Errorf("schedule %s: %w", name, err)
	}
	return nil
}

// Start begins cron scheduling.
func (s *Scheduler) Start() { s.scheduler.Start() }

// Shutdown cancels jobs and waits until ctx expires.
func (s *Scheduler) Shutdown(ctx context.Context) error { return s.scheduler.ShutdownWithContext(ctx) }

func execute(ctx context.Context, task func(context.Context) error, onError func(error)) {
	defer func() {
		if recovered := recover(); recovered != nil && onError != nil {
			onError(fmt.Errorf("scheduled task panic: %v", recovered))
		}
	}()
	if err := task(ctx); err != nil && onError != nil {
		onError(err)
	}
}
