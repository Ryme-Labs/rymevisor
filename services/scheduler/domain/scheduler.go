package domain

import (
	"context"
)

type SchedulingConstraints struct {
	PreferredNode    string   `json:"preferred_node"`
	RequiredLabels   []string `json:"required_labels,omitempty"`
	PreferredLabels  []string `json:"preferred_labels,omitempty"`
	AvoidLabels      []string `json:"avoid_labels,omitempty"`
	RequireGPU       bool     `json:"require_gpu"`
	AvailabilityZone string   `json:"availability_zone"`
}

type ScheduledJob struct {
	VMID        string                 `json:"vm_id"`
	NodeID      string                 `json:"node_id"`
	Priority    int32                  `json:"priority"`
	Constraints *SchedulingConstraints `json:"constraints,omitempty"`
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
