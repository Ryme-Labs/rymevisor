package domain

import (
	"context"
)

type SchedulingConstraints struct {
	PreferredNode  string
	RequiredLabels []string
	PreferredLabels []string
	AvoidLabels    []string
	RequireGPU     bool
	AvailabilityZone string
}

type ScheduledJob struct {
	VMID        string
	NodeID      string
	Priority    int32
	Constraints *SchedulingConstraints
}

type SchedulerService interface {
	Schedule(ctx context.Context, vmID string, constraints *SchedulingConstraints, priority int32) (string, error)
	CancelSchedule(ctx context.Context, vmID string) error
	ListScheduledJobs(ctx context.Context, nodeID string, page, perPage int) ([]*ScheduledJob, int, error)
}

type SchedulerRepository interface {
	Schedule(ctx context.Context, job *ScheduledJob) error
	GetByVMID(ctx context.Context, vmID string) (*ScheduledJob, error)
	Delete(ctx context.Context, vmID string) error
	List(ctx context.Context, nodeID string, page, perPage int) ([]*ScheduledJob, int, error)
}
