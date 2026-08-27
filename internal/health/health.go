package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type Status string

const (
	StatusUp   Status = "up"
	StatusDown Status = "down"
)

type Check struct {
	Status  Status         `json:"status"`
	Checks  map[string]Status `json:"checks"`
}

type Checker func(ctx context.Context) error

type Handler struct {
	mu      sync.RWMutex
	checks  map[string]Checker
}

func NewHandler() *Handler {
	return &Handler{
		checks: make(map[string]Checker),
	}
}

func (h *Handler) Register(name string, check Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
}

func (h *Handler) Liveness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Check{Status: StatusUp})
	})
}

func (h *Handler) Readiness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.RLock()
		defer h.mu.RUnlock()

		checks := make(map[string]Status)
		overall := StatusUp

		for name, check := range h.checks {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			err := check(ctx)
			cancel()

			if err != nil {
				checks[name] = StatusDown
				overall = StatusDown
			} else {
				checks[name] = StatusUp
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if overall == StatusDown {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_ = json.NewEncoder(w).Encode(Check{Status: overall, Checks: checks})
	})
}
