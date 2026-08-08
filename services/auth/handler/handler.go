package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rymelabs/rymevisor/services/auth"
	"github.com/rymelabs/rymevisor/services/auth/domain"
)

type ctxKey string

const (
	ctxUserID      ctxKey = "user_id"
	ctxEmail       ctxKey = "user_email"
	ctxPermissions ctxKey = "user_permissions"
)

type Handler struct {
	svc *auth.Service
}

func NewHandler(svc *auth.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/login", h.Login)
	r.Post("/register", h.Register)
	r.Post("/refresh", h.Refresh)

	r.Group(func(r chi.Router) {
		r.Use(h.authMiddleware)

		r.Get("/me", h.GetMe)
		r.Post("/change-password", h.ChangePassword)

		r.Group(func(r chi.Router) {
			r.Use(h.adminMiddleware)

			r.Post("/users", h.CreateUser)
			r.Get("/users", h.ListUsers)
			r.Get("/users/{id}", h.GetUser)
			r.Put("/users/{id}", h.UpdateUser)
			r.Delete("/users/{id}", h.DeleteUser)
		})

		r.Post("/api-keys", h.CreateAPIKey)
		r.Get("/api-keys", h.ListAPIKeys)
		r.Delete("/api-keys/{id}", h.RevokeAPIKey)
	})

	return r
}

// --- Auth middleware ---

func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(w, http.StatusUnauthorized, "invalid authorization format")
			return
		}

		claims, err := h.svc.ValidateToken(parts[1])
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), ctxUserID, claims.UserID)
		ctx = context.WithValue(ctx, ctxEmail, claims.Email)
		ctx = context.WithValue(ctx, ctxPermissions, claims.Permissions)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) adminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		perms, ok := r.Context().Value(ctxPermissions).([]string)
		if !ok {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		for _, p := range perms {
			if p == "admin" || p == "*" {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeError(w, http.StatusForbidden, "admin access required")
	})
}

// --- Handlers ---

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	User         *domain.User `json:"user"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, accessToken, refreshToken, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidPassword):
			writeError(w, http.StatusUnauthorized, "invalid email or password")
		case errors.Is(err, domain.ErrUnauthorized):
			writeError(w, http.StatusForbidden, "account is not active")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "email, password, and name are required")
		return
	}

	user, err := h.svc.Register(r.Context(), req.Email, req.Password, req.Name)
	if err != nil {
		if errors.Is(err, domain.ErrEmailExists) {
			writeError(w, http.StatusConflict, "email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"user": user})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	accessToken, refreshToken, err := h.svc.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(ctxUserID).(string)

	user, permissions, err := h.svc.GetMe(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user":        user,
		"permissions": permissions,
	})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(ctxUserID).(string)

	var req changePasswordRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}

	if err := h.svc.ChangePassword(r.Context(), userID, req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, domain.ErrInvalidPassword) {
			writeError(w, http.StatusUnauthorized, "current password is incorrect")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "password changed"})
}

type createUserRequest struct {
	Email          string   `json:"email"`
	Password       string   `json:"password"`
	Name           string   `json:"name"`
	Roles          []string `json:"roles"`
	OrganizationID string   `json:"organization_id"`
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "email, password, and name are required")
		return
	}

	user, err := h.svc.CreateUser(r.Context(), &domain.CreateUserRequest{
		Email:          req.Email,
		Password:       req.Password,
		Name:           req.Name,
		Roles:          req.Roles,
		OrganizationID: req.OrganizationID,
	})
	if err != nil {
		if errors.Is(err, domain.ErrEmailExists) {
			writeError(w, http.StatusConflict, "email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"user": user})
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	filter := domain.UserFilter{
		Status:         r.URL.Query().Get("status"),
		Search:         r.URL.Query().Get("search"),
		OrganizationID: r.URL.Query().Get("organization_id"),
	}

	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			filter.Page = v
		}
	}
	if pp := r.URL.Query().Get("per_page"); pp != "" {
		if v, err := strconv.Atoi(pp); err == nil {
			filter.PerPage = v
		}
	}

	users, total, err := h.svc.ListUsers(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"users": users,
		"total": total,
	})
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := h.svc.GetUser(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"user": user})
}

type updateUserRequest struct {
	Name   string            `json:"name"`
	Email  string            `json:"email"`
	Status domain.UserStatus `json:"status"`
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req updateUserRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.svc.UpdateUser(r.Context(), id, &domain.UpdateUserRequest{
		Name:   req.Name,
		Email:  req.Email,
		Status: req.Status,
	})
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"user": user})
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "user deleted"})
}

type createAPIKeyRequest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	OrganizationID string   `json:"organization_id"`
	Permissions    []string `json:"permissions"`
	ExpiresAt      string   `json:"expires_at"`
	AllowedIPs     []string `json:"allowed_ips"`
}

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.OrganizationID == "" {
		writeError(w, http.StatusBadRequest, "name and organization_id are required")
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid expires_at format (use RFC3339)")
			return
		}
		expiresAt = &t
	}

	apiKey, rawKey, err := h.svc.CreateAPIKey(r.Context(), &domain.CreateAPIKeyRequest{
		Name:           req.Name,
		Description:    req.Description,
		OrganizationID: req.OrganizationID,
		Permissions:    req.Permissions,
		ExpiresAt:      expiresAt,
		AllowedIPs:     req.AllowedIPs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"api_key": apiKey,
		"key":     rawKey,
	})
}

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("organization_id")
	if orgID == "" {
		writeError(w, http.StatusBadRequest, "organization_id is required")
		return
	}

	keys, err := h.svc.ListAPIKeys(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"api_keys": keys})
}

func (h *Handler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.RevokeAPIKey(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrAPIKeyNotFound) {
			writeError(w, http.StatusNotFound, "api key not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "api key revoked"})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func readJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}
