package postgres

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/rymelabs/rymevisor/services/scheduler/domain"
)

type SchedulerRepository struct {
	mu   sync.RWMutex
	jobs map[string]*domain.ScheduledJob
}

func NewSchedulerRepository() *SchedulerRepository {
	return &SchedulerRepository{
		jobs: make(map[string]*domain.ScheduledJob),
	}
}

func (r *SchedulerRepository) Schedule(_ context.Context, job *domain.ScheduledJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.jobs[job.VMID] = job
	return nil
}

func (r *SchedulerRepository) GetByVMID(_ context.Context, vmID string) (*domain.ScheduledJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	job, ok := r.jobs[vmID]
	if !ok {
		return nil, fmt.Errorf("scheduled job not found for vm %s", vmID)
	}
	return job, nil
}

func (r *SchedulerRepository) Delete(_ context.Context, vmID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.jobs[vmID]; !ok {
		return fmt.Errorf("scheduled job not found for vm %s", vmID)
	}
	delete(r.jobs, vmID)
	return nil
}

func (r *SchedulerRepository) List(_ context.Context, nodeID string, page, perPage int) ([]*domain.ScheduledJob, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []*domain.ScheduledJob
	for _, job := range r.jobs {
		if nodeID == "" || job.NodeID == nodeID {
			filtered = append(filtered, job)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Priority > filtered[j].Priority
	})

	total := len(filtered)
	start := (page - 1) * perPage
	if start >= total {
		return []*domain.ScheduledJob{}, total, nil
	}
	end := start + perPage
	if end > total {
		end = total
	}

	return filtered[start:end], total, nil
}
