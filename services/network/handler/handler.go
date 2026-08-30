package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rymelabs/rymevisor/internal/jsonutil"
	"github.com/rymelabs/rymevisor/services/network"
	"github.com/rymelabs/rymevisor/services/network/domain"
)

type Handler struct {
	svc *network.Service
}

func NewHandler(svc *network.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v1/networks", h.CreateNetwork)
	r.Get("/api/v1/networks", h.ListNetworks)
	r.Get("/api/v1/networks/{id}", h.GetNetwork)
	r.Delete("/api/v1/networks/{id}", h.DeleteNetwork)

	r.Post("/api/v1/networks/{id}/subnets", h.CreateSubnet)
	r.Delete("/api/v1/subnets/{id}", h.DeleteSubnet)

	r.Post("/api/v1/networks/{id}/firewall-rules", h.CreateFirewallRule)
	r.Delete("/api/v1/firewall-rules/{id}", h.DeleteFirewallRule)

	r.Post("/api/v1/floating-ips", h.AllocateFloatingIP)
	r.Get("/api/v1/floating-ips", h.ListFloatingIPs)
	r.Delete("/api/v1/floating-ips/{id}", h.ReleaseFloatingIP)
}

func (h *Handler) CreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateNetworkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	net, err := h.svc.CreateNetwork(r.Context(), &req)
	if err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusCreated, net)
}

func (h *Handler) GetNetwork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	net, err := h.svc.GetNetwork(r.Context(), id)
	if err != nil {
		jsonutil.WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, net)
}

func (h *Handler) ListNetworks(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("organization_id")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))

	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 20
	}

	networks, total, err := h.svc.ListNetworks(r.Context(), domain.NetworkFilter{
		OrganizationID: orgID,
		Page:           page,
		PerPage:        perPage,
	})
	if err != nil {
		jsonutil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"networks": networks,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

func (h *Handler) DeleteNetwork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteNetwork(r.Context(), id); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, map[string]string{"message": "network deleted"})
}

func (h *Handler) CreateSubnet(w http.ResponseWriter, r *http.Request) {
	networkID := chi.URLParam(r, "id")

	var req struct {
		Name string `json:"name"`
		CIDR string `json:"cidr"`
		DHCP bool   `json:"dhcp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	subnet, err := h.svc.CreateSubnet(r.Context(), networkID, req.Name, req.CIDR, req.DHCP)
	if err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusCreated, subnet)
}

func (h *Handler) DeleteSubnet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteSubnet(r.Context(), id); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, map[string]string{"message": "subnet deleted"})
}

func (h *Handler) CreateFirewallRule(w http.ResponseWriter, r *http.Request) {
	networkID := chi.URLParam(r, "id")

	var req domain.CreateFirewallRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.NetworkID = networkID

	rule, err := h.svc.CreateFirewallRule(r.Context(), &req)
	if err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusCreated, rule)
}

func (h *Handler) DeleteFirewallRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteFirewallRule(r.Context(), id); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, map[string]string{"message": "firewall rule deleted"})
}

func (h *Handler) AllocateFloatingIP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NetworkID string `json:"network_id"`
		VMID      string `json:"vm_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	fip, err := h.svc.AllocateFloatingIP(r.Context(), req.NetworkID, req.VMID)
	if err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusCreated, fip)
}

func (h *Handler) ReleaseFloatingIP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.ReleaseFloatingIP(r.Context(), id); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, map[string]string{"message": "floating IP released"})
}

func (h *Handler) ListFloatingIPs(w http.ResponseWriter, r *http.Request) {
	ips, err := h.svc.ListFloatingIPs(r.Context())
	if err != nil {
		jsonutil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonutil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"floating_ips": ips,
		"total":        len(ips),
	})
}
