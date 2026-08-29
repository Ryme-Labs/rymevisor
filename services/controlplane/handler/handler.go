package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rymelabs/rymevisor/services/controlplane"
	"github.com/rymelabs/rymevisor/services/controlplane/domain"
)

type Handler struct {
	svc       *controlplane.Service
	nodeSvc   *controlplane.NodeServiceImpl
}

func NewHandler(svc *controlplane.Service) *Handler {
	return &Handler{
		svc:     svc,
		nodeSvc: svc.NodeService(),
	}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Route("/vms", func(r chi.Router) {
		r.Post("/", h.CreateVM)
		r.Get("/", h.ListVMs)
		r.Get("/{id}", h.GetVM)
		r.Put("/{id}", h.UpdateVM)
		r.Delete("/{id}", h.DeleteVM)
		r.Post("/{id}/power-on", h.PowerOnVM)
		r.Post("/{id}/power-off", h.PowerOffVM)
		r.Post("/{id}/reboot", h.RebootVM)
		r.Post("/{id}/resize", h.ResizeVM)
		r.Post("/{id}/snapshot", h.SnapshotVM)
		r.Post("/{id}/clone", h.CloneVM)
		r.Post("/{id}/restore-snapshot", h.RestoreSnapshot)
	})

	r.Route("/nodes", func(r chi.Router) {
		r.Post("/", h.RegisterNode)
		r.Get("/", h.ListNodes)
		r.Get("/{id}", h.GetNode)
		r.Put("/{id}", h.UpdateNode)
		r.Post("/{id}/drain", h.DrainNode)
		r.Post("/{id}/heartbeat", h.Heartbeat)
	})

	r.Route("/images", func(r chi.Router) {
		r.Get("/official", h.ListOfficialImages)
		r.Post("/pull", h.PullOfficialImage)
		r.Post("/import", h.ImportImage)
		r.Post("/", h.CreateImage)
		r.Get("/", h.ListImages)
		r.Get("/{id}", h.GetImage)
		r.Delete("/{id}", h.DeleteImage)
	})

	r.Route("/flavors", func(r chi.Router) {
		r.Post("/", h.CreateFlavor)
		r.Get("/", h.ListFlavors)
		r.Get("/{id}", h.GetFlavor)
		r.Delete("/{id}", h.DeleteFlavor)
	})

	r.Route("/keypairs", func(r chi.Router) {
		r.Post("/", h.CreateKeypair)
		r.Get("/", h.ListKeypairs)
		r.Get("/{id}", h.GetKeypair)
		r.Delete("/{id}", h.DeleteKeypair)
	})

	r.Route("/backups", func(r chi.Router) {
		r.Post("/", h.CreateBackup)
		r.Get("/", h.ListBackups)
		r.Get("/{id}", h.GetBackup)
		r.Delete("/{id}", h.DeleteBackup)
		r.Post("/{id}/restore", h.RestoreBackup)
	})

	r.Route("/ws", func(r chi.Router) {
		r.Get("/logs", h.HandleWsLogs)
		r.Get("/console", h.HandleWsConsole)
		r.Get("/console/{id}", h.HandleWsConsole)
	})

	return r
}

func (h *Handler) CreateVM(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vm, err := h.svc.CreateVM(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, vm)
}

func (h *Handler) GetVM(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	vm, err := h.svc.GetVM(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if vm == nil {
		writeError(w, http.StatusNotFound, "vm not found")
		return
	}

	writeJSON(w, http.StatusOK, vm)
}

func (h *Handler) ListVMs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))

	filter := domain.VMFilter{
		OrganizationID: q.Get("organization_id"),
		ProjectID:      q.Get("project_id"),
		NodeID:         q.Get("node_id"),
		Status:         q.Get("status"),
		Search:         q.Get("search"),
		Page:           page,
		PerPage:        perPage,
	}

	vms, total, err := h.svc.ListVMs(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": vms,
		"total": total,
	})
}

func (h *Handler) UpdateVM(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req domain.UpdateVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vm, err := h.svc.UpdateVM(r.Context(), id, &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, vm)
}

func (h *Handler) DeleteVM(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	force := r.URL.Query().Get("force") == "true"

	if err := h.svc.DeleteVM(r.Context(), id, force); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) PowerOnVM(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	vm, err := h.svc.PowerOn(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, vm)
}

func (h *Handler) PowerOffVM(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	force := r.URL.Query().Get("force") == "true"

	vm, err := h.svc.PowerOff(r.Context(), id, force)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, vm)
}

func (h *Handler) RebootVM(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	force := r.URL.Query().Get("force") == "true"

	vm, err := h.svc.Reboot(r.Context(), id, force)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, vm)
}

func (h *Handler) ResizeVM(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		VCpus    int32 `json:"vcpus"`
		MemoryMB int64 `json:"memory_mb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vm, err := h.svc.Resize(r.Context(), id, req.VCpus, req.MemoryMB)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, vm)
}

func (h *Handler) SnapshotVM(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	snap, err := h.svc.Snapshot(r.Context(), id, req.Name, req.Description)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, snap)
}

func (h *Handler) CloneVM(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Name   string `json:"name"`
		NodeID string `json:"node_id"`
		Linked bool   `json:"linked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vm, err := h.svc.Clone(r.Context(), id, req.Name, req.NodeID, req.Linked)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, vm)
}

func (h *Handler) RestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vm, err := h.svc.RestoreSnapshot(r.Context(), req.SnapshotID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, vm)
}

func (h *Handler) RegisterNode(w http.ResponseWriter, r *http.Request) {
	var req domain.RegisterNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	node, err := h.nodeSvc.Register(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, node)
}

func (h *Handler) GetNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	node, err := h.nodeSvc.GetNode(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if node == nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}

	writeJSON(w, http.StatusOK, node)
}

func (h *Handler) ListNodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))

	filter := domain.NodeFilter{
		Status:  q.Get("status"),
		Search:  q.Get("search"),
		Page:    page,
		PerPage: perPage,
	}

	nodes, total, err := h.nodeSvc.ListNodes(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": nodes,
		"total": total,
	})
}

func (h *Handler) UpdateNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	node, err := h.nodeSvc.UpdateNode(r.Context(), id, req.Labels)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, node)
}

func (h *Handler) DrainNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Timeout int32 `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Timeout = 60
	}

	if err := h.nodeSvc.Drain(r.Context(), id, req.Timeout); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req domain.NodeResources
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.nodeSvc.Heartbeat(r.Context(), id, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string            `json:"name"`
		Description  string            `json:"description"`
		OS           string            `json:"os"`
		OSVersion    string            `json:"os_version"`
		Architecture string            `json:"architecture"`
		Type         domain.ImageType  `json:"type"`
		SizeBytes    int64             `json:"size_bytes"`
		Checksum     string            `json:"checksum"`
		Tags         []string          `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	img := &domain.Image{
		Name:         req.Name,
		Description:  req.Description,
		OS:           req.OS,
		OSVersion:    req.OSVersion,
		Architecture: req.Architecture,
		Type:         req.Type,
		SizeBytes:    req.SizeBytes,
		Checksum:     req.Checksum,
		Tags:         req.Tags,
	}

	if err := h.svc.CreateImage(r.Context(), img); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, img)
}

func (h *Handler) GetImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	img, err := h.svc.GetImage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if img == nil {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}

	writeJSON(w, http.StatusOK, img)
}

func (h *Handler) ListImages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))

	filter := domain.ImageFilter{
		OS:           q.Get("os"),
		Architecture: q.Get("architecture"),
		Type:         q.Get("type"),
		Search:       q.Get("search"),
		Page:         page,
		PerPage:      perPage,
	}

	images, total, err := h.svc.ListImages(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": images,
		"total": total,
	})
}

func (h *Handler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteImage(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListOfficialImages(w http.ResponseWriter, r *http.Request) {
	images, err := h.svc.ListOfficialImages(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": images, "total": len(images)})
}

func (h *Handler) PullOfficialImage(w http.ResponseWriter, r *http.Request) {
	var req domain.PullImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	img, err := h.svc.PullOfficialImage(r.Context(), req.OS, req.OSVersion, req.Architecture)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, img)
}

func (h *Handler) ImportImage(w http.ResponseWriter, r *http.Request) {
	var req domain.ImportImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	img, err := h.svc.ImportFromURL(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, img)
}

func (h *Handler) CreateFlavor(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateFlavorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	flavor, err := h.svc.CreateFlavor(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, flavor)
}

func (h *Handler) ListFlavors(w http.ResponseWriter, r *http.Request) {
	flavors, err := h.svc.ListFlavors(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": flavors, "total": len(flavors)})
}

func (h *Handler) GetFlavor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	flavor, err := h.svc.GetFlavor(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if flavor == nil {
		writeError(w, http.StatusNotFound, "flavor not found")
		return
	}
	writeJSON(w, http.StatusOK, flavor)
}

func (h *Handler) DeleteFlavor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.DeleteFlavor(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateKeypair(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateKeypairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	kp, err := h.svc.CreateKeypair(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, kp)
}

func (h *Handler) ListKeypairs(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("organization_id")
	kps, err := h.svc.ListKeypairs(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": kps, "total": len(kps)})
}

func (h *Handler) GetKeypair(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	kp, err := h.svc.GetKeypair(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if kp == nil {
		writeError(w, http.StatusNotFound, "keypair not found")
		return
	}
	writeJSON(w, http.StatusOK, kp)
}

func (h *Handler) DeleteKeypair(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.DeleteKeypair(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VMID        string              `json:"vm_id"`
		Name        string              `json:"name"`
		Type        domain.BackupType   `json:"type"`
		StoragePool string              `json:"storage_pool"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	backup := &domain.Backup{
		VMID:        req.VMID,
		Name:        req.Name,
		Type:        req.Type,
		StoragePool: req.StoragePool,
	}

	if err := h.svc.CreateBackup(r.Context(), backup); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, backup)
}

func (h *Handler) GetBackup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	backup, err := h.svc.GetBackup(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if backup == nil {
		writeError(w, http.StatusNotFound, "backup not found")
		return
	}

	writeJSON(w, http.StatusOK, backup)
}

func (h *Handler) ListBackups(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))

	filter := domain.BackupFilter{
		VMID:           q.Get("vm_id"),
		OrganizationID: q.Get("organization_id"),
		Page:           page,
		PerPage:        perPage,
	}

	backups, total, err := h.svc.ListBackups(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": backups,
		"total": total,
	})
}

func (h *Handler) DeleteBackup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteBackup(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		VMID string `json:"vm_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.svc.RestoreBackup(r.Context(), id, req.VMID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
