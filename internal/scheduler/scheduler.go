package scheduler

import (
	"context"
	"slices"

	"github.com/go-co-op/gocron/v2"
)

type Scheduler struct {
	scheduler gocron.Scheduler
}

func NewScheduler() (*Scheduler, error) {
	s, err := gocron.NewScheduler()
	if err != nil {
		return nil, err
	}

	return &Scheduler{
		scheduler: s,
	}, nil
}

func (s *Scheduler) Start() {
	s.scheduler.Start()
}

func (s *Scheduler) Stop() error {
	return s.scheduler.Shutdown()
}

func (s *Scheduler) GetJob(name string) gocron.Job {
	for _, job := range s.scheduler.Jobs() {
		if job.Name() == name {
			return job
		}
	}

	return nil
}

func (s *Scheduler) AddJob(schedule string, name string, task func(ctx context.Context, params ...any), params ...any) (gocron.Job, error) {
	job, err := s.scheduler.NewJob(
		gocron.CronJob(schedule, false),
		gocron.NewTask(task, params...),
		gocron.WithTags(name, schedule),
		gocron.WithName(name),
	)
	if err != nil {
		return nil, err
	}

	return job, nil
}

func (s *Scheduler) RemoveJob(name string) error {
	// s.scheduler.RemoveByTags(name)
	// RemoveByTags is brittle, does not notify if it could not, not ideal,
	// can end up with dangling jobs, while the actual owner is deleted from the cluster
	job := s.GetJob(name)
	if job != nil {
		return s.scheduler.RemoveJob(job.ID())
	}

	return nil
}

func IsSameSchedule(job gocron.Job, schedule string) bool {
	return slices.Contains(job.Tags(), schedule)
}
