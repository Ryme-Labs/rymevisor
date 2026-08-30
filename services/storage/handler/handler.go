package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rymelabs/rymevisor/internal/jsonutil"
	"github.com/rymelabs/rymevisor/services/storage"
	"github.com/rymelabs/rymevisor/services/storage/domain"
)

type Handler struct {
	svc *storage.Service
}

func NewHandler(svc *storage.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v1/storage/pools", h.CreatePool)
	r.Get("/api/v1/storage/pools", h.ListPools)
	r.Get("/api/v1/storage/pools/{id}", h.GetPool)

	r.Post("/api/v1/storage/volumes", h.CreateVolume)
	r.Get("/api/v1/storage/volumes", h.ListVolumes)
	r.Get("/api/v1/storage/volumes/{id}", h.GetVolume)
	r.Delete("/api/v1/storage/volumes/{id}", h.DeleteVolume)
	r.Put("/api/v1/storage/volumes/{id}/resize", h.ResizeVolume)
	r.Post("/api/v1/storage/volumes/{id}/clone", h.CloneVolume)

	r.Post("/api/v1/storage/volumes/{id}/snapshots", h.CreateSnapshot)
	r.Delete("/api/v1/storage/snapshots/{id}", h.DeleteSnapshot)
	r.Post("/api/v1/storage/snapshots/{id}/restore", h.RestoreSnapshot)
}

func (h *Handler) CreatePool(w http.ResponseWriter, r *http.Request) {
	var req domain.CreatePoolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	pool, err := h.svc.CreatePool(r.Context(), &req)
	if err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusCreated, pool)
}

func (h *Handler) GetPool(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	pool, err := h.svc.GetPool(r.Context(), id)
	if err != nil {
		jsonutil.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	if pool == nil {
		jsonutil.WriteError(w, http.StatusNotFound, "pool not found")
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, pool)
}

func (h *Handler) ListPools(w http.ResponseWriter, r *http.Request) {
	pools, err := h.svc.ListPools(r.Context())
	if err != nil {
		jsonutil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"pools": pools,
	})
}

func (h *Handler) CreateVolume(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateVolumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vol, err := h.svc.CreateVolume(r.Context(), &req)
	if err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusCreated, vol)
}

func (h *Handler) GetVolume(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	vol, err := h.svc.GetVolume(r.Context(), id)
	if err != nil {
		jsonutil.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	if vol == nil {
		jsonutil.WriteError(w, http.StatusNotFound, "volume not found")
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, vol)
}

func (h *Handler) ListVolumes(w http.ResponseWriter, r *http.Request) {
	poolID := r.URL.Query().Get("pool_id")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))

	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 20
	}

	volumes, total, err := h.svc.ListVolumes(r.Context(), domain.VolumeFilter{
		PoolID:  poolID,
		Page:    page,
		PerPage: perPage,
	})
	if err != nil {
		jsonutil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"volumes": volumes,
		"total":   total,
		"page":    page,
		"per_page": perPage,
	})
}

func (h *Handler) DeleteVolume(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	force, _ := strconv.ParseBool(r.URL.Query().Get("force"))

	if err := h.svc.DeleteVolume(r.Context(), id, force); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, map[string]string{"message": "volume deleted"})
}

func (h *Handler) ResizeVolume(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		SizeBytes int64 `json:"size_bytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vol, err := h.svc.ResizeVolume(r.Context(), id, req.SizeBytes)
	if err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, vol)
}

func (h *Handler) CloneVolume(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vol, err := h.svc.CloneVolume(r.Context(), id, req.Name)
	if err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusCreated, vol)
}

func (h *Handler) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	volumeID := chi.URLParam(r, "id")

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	snap, err := h.svc.CreateSnapshot(r.Context(), volumeID, req.Name)
	if err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusCreated, snap)
}

func (h *Handler) DeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteSnapshot(r.Context(), id); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, map[string]string{"message": "snapshot deleted"})
}

func (h *Handler) RestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	vol, err := h.svc.RestoreSnapshot(r.Context(), id)
	if err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, vol)
}
