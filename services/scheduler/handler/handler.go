package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rymelabs/rymevisor/services/scheduler"
	"github.com/rymelabs/rymevisor/services/scheduler/domain"
)

type Handler struct {
	svc *scheduler.Service
}

func NewHandler(svc *scheduler.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/api/v1/scheduler", func(r chi.Router) {
		r.Post("/schedule", h.handleSchedule)
		r.Delete("/cancel", h.handleCancel)
		r.Get("/jobs", h.handleListJobs)
	})
}

type scheduleRequest struct {
	VMID        string                      `json:"vm_id"`
	Constraints *domain.SchedulingConstraints `json:"constraints"`
	Priority    int32                       `json:"priority"`
}

type scheduleResponse struct {
	NodeID string `json:"node_id"`
}

type cancelRequest struct {
	VMID string `json:"vm_id"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type jobListResponse struct {
	Jobs  []*domain.ScheduledJob `json:"jobs"`
	Total int                    `json:"total"`
	Page  int                    `json:"page"`
}

func (h *Handler) handleSchedule(w http.ResponseWriter, r *http.Request) {
	var req scheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	if req.VMID == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "vm_id is required"})
		return
	}
	if req.Constraints == nil {
		req.Constraints = &domain.SchedulingConstraints{}
	}

	nodeID, err := h.svc.Schedule(r.Context(), req.VMID, req.Constraints, req.Priority)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, scheduleResponse{NodeID: nodeID})
}

func (h *Handler) handleCancel(w http.ResponseWriter, r *http.Request) {
	var req cancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	if req.VMID == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "vm_id is required"})
		return
	}

	if err := h.svc.CancelSchedule(r.Context(), req.VMID); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListJobs(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("node_id")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))

	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	jobs, total, err := h.svc.ListScheduledJobs(r.Context(), nodeID, page, perPage)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	if jobs == nil {
		jobs = []*domain.ScheduledJob{}
	}

	writeJSON(w, http.StatusOK, jobListResponse{
		Jobs:  jobs,
		Total: total,
		Page:  page,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
