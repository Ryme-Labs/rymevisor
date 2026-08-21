package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/rymelabs/rymevisor/services/scheduler/domain"
)

type NodeRegistry interface {
	GetAvailableNodes(ctx context.Context) ([]NodeInfo, error)
}

type NodeInfo struct {
	ID              string
	Labels          map[string]string
	AvailableCPUs   int32
	AvailableMemory int64
	AvailableGPUs   int32
	RunningVMs      int32
}

type Service struct {
	repo     domain.SchedulerRepository
	registry NodeRegistry
}

func NewService(repo domain.SchedulerRepository, registry NodeRegistry) *Service {
	return &Service{
		repo:     repo,
		registry: registry,
	}
}

func (s *Service) Schedule(ctx context.Context, vmID string, constraints *domain.SchedulingConstraints, priority int32) (string, error) {
	existing, err := s.repo.GetByVMID(ctx, vmID)
	if err == nil && existing != nil {
		return "", fmt.Errorf("vm %s already scheduled on node %s", vmID, existing.NodeID)
	}

	if s.registry == nil {
		return "", errors.New("node registry not configured")
	}

	nodes, err := s.registry.GetAvailableNodes(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to query available nodes: %w", err)
	}

	candidates := s.filterNodes(nodes, constraints)
	if len(candidates) == 0 {
		return "", errors.New("no nodes match the scheduling constraints")
	}

	scored := s.scoreNodes(candidates, constraints)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	best := scored[0]

	job := &domain.ScheduledJob{
		VMID:        vmID,
		NodeID:      best.node.ID,
		Priority:    priority,
		Constraints: constraints,
	}

	if err := s.repo.Schedule(ctx, job); err != nil {
		return "", fmt.Errorf("failed to record scheduled job: %w", err)
	}

	return best.node.ID, nil
}

func (s *Service) CancelSchedule(ctx context.Context, vmID string) error {
	return s.repo.Delete(ctx, vmID)
}

func (s *Service) ListScheduledJobs(ctx context.Context, nodeID string, page, perPage int) ([]*domain.ScheduledJob, int, error) {
	return s.repo.List(ctx, nodeID, page, perPage)
}

func (s *Service) filterNodes(nodes []NodeInfo, constraints *domain.SchedulingConstraints) []NodeInfo {
	if constraints == nil {
		return nodes
	}

	if constraints.PreferredNode != "" {
		for _, n := range nodes {
			if n.ID == constraints.PreferredNode {
				return []NodeInfo{n}
			}
		}
		return nil
	}

	var result []NodeInfo
	for _, n := range nodes {
		if !s.nodeMatchesConstraints(n, constraints) {
			continue
		}
		result = append(result, n)
	}
	return result
}

func (s *Service) nodeMatchesConstraints(n NodeInfo, c *domain.SchedulingConstraints) bool {
	if len(c.RequiredLabels) > 0 {
		for _, label := range c.RequiredLabels {
			parts := strings.SplitN(label, "=", 2)
			key := parts[0]
			val := ""
			if len(parts) == 2 {
				val = parts[1]
			}
			nv, ok := n.Labels[key]
			if !ok || (val != "" && nv != val) {
				return false
			}
		}
	}

	if len(c.AvoidLabels) > 0 {
		for _, label := range c.AvoidLabels {
			parts := strings.SplitN(label, "=", 2)
			key := parts[0]
			val := ""
			if len(parts) == 2 {
				val = parts[1]
			}
			nv, ok := n.Labels[key]
			if ok && (val == "" || nv == val) {
				return false
			}
		}
	}

	if c.RequireGPU && n.AvailableGPUs <= 0 {
		return false
	}

	return true
}

type scoredNode struct {
	node  NodeInfo
	score float64
}

func (s *Service) scoreNodes(nodes []NodeInfo, constraints *domain.SchedulingConstraints) []scoredNode {
	result := make([]scoredNode, len(nodes))
	for i, n := range nodes {
		result[i] = scoredNode{
			node:  n,
			score: s.calculateScore(n, constraints),
		}
	}
	return result
}

func (s *Service) calculateScore(n NodeInfo, constraints *domain.SchedulingConstraints) float64 {
	var score float64

	if n.AvailableCPUs > 0 {
		score += float64(n.AvailableCPUs) * 10
	}

	if n.AvailableMemory > 0 {
		score += float64(n.AvailableMemory) / (1024 * 1024)
	}

	if n.RunningVMs >= 0 {
		score += float64(100-n.RunningVMs) * 5
	}

	if n.AvailableGPUs > 0 {
		score += float64(n.AvailableGPUs) * 50
	}

	if constraints != nil && len(constraints.PreferredLabels) > 0 {
		for _, label := range constraints.PreferredLabels {
			parts := strings.SplitN(label, "=", 2)
			key := parts[0]
			val := ""
			if len(parts) == 2 {
				val = parts[1]
			}
			nv, ok := n.Labels[key]
			if ok && (val == "" || nv == val) {
				score += 25
			}
		}
	}

	return score
}
