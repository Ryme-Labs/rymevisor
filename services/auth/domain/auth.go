package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUserNotFound    = errors.New("user not found")
	ErrEmailExists     = errors.New("email already exists")
	ErrInvalidPassword = errors.New("invalid password")
	ErrAPIKeyNotFound  = errors.New("api key not found")
	ErrUnauthorized    = errors.New("unauthorized")
)

type UserStatus string

const (
	UserStatusActive    UserStatus = "active"
	UserStatusSuspended UserStatus = "suspended"
	UserStatusLocked    UserStatus = "locked"
	UserStatusDeleted   UserStatus = "deleted"
)

type User struct {
	ID              string
	Email           string
	Name            string
	PasswordHash    string
	Status          UserStatus
	MFAEnabled      bool
	TOTPSecret      *string
	LastLoginAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Roles           []string
	Organizations   []string
	Permissions     []string
}

type Organization struct {
	ID        string
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type APIKey struct {
	ID             string
	Name           string
	Description    string
	Prefix         string
	KeyHash        string
	UserID         string
	OrganizationID string
	Permissions    []string
	AllowedIPs     []string
	Active         bool
	ExpiresAt      *time.Time
	LastUsedAt     *time.Time
	CreatedAt      time.Time
}

type Session struct {
	ID        string
	UserID    string
	TokenHash string
	IPAddress string
	UserAgent string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type Role struct {
	ID          string
	Name        string
	Description string
	CreatedAt   time.Time
}

type Permission struct {
	ID          string
	Name        string
	Description string
	Resource    string
	Action      string
	CreatedAt   time.Time
}

type JWTClaims struct {
	UserID        string   `json:"user_id"`
	Email         string   `json:"email"`
	Organizations []string `json:"organizations"`
	Permissions   []string `json:"permissions"`
	ExpiresAt     int64    `json:"exp"`
	IssuedAt      int64    `json:"iat"`
}

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context, filter UserFilter) ([]*User, int, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id string) error
	UpdatePassword(ctx context.Context, id string, hash string) error
	UpdateMFA(ctx context.Context, id string, enabled bool, totpSecret string) error
}

type UserFilter struct {
	OrganizationID string
	Status         string
	Search         string
	Page           int
	PerPage        int
}

type OrganizationRepository interface {
	Create(ctx context.Context, org *Organization) error
	GetByID(ctx context.Context, id string) (*Organization, error)
	GetBySlug(ctx context.Context, slug string) (*Organization, error)
	List(ctx context.Context) ([]*Organization, error)
	Update(ctx context.Context, org *Organization) error
	Delete(ctx context.Context, id string) error
}

type APIKeyRepository interface {
	Create(ctx context.Context, key *APIKey) error
	GetByID(ctx context.Context, id string) (*APIKey, error)
	GetByPrefix(ctx context.Context, prefix string) (*APIKey, error)
	List(ctx context.Context, organizationID string) ([]*APIKey, error)
	Delete(ctx context.Context, id string) error
	UpdateLastUsed(ctx context.Context, id string) error
}

type SessionRepository interface {
	Create(ctx context.Context, session *Session) error
	GetByToken(ctx context.Context, tokenHash string) (*Session, error)
	Delete(ctx context.Context, id string) error
	DeleteByUser(ctx context.Context, userID string) error
}

type AuthService interface {
	Login(ctx context.Context, email, password string) (*User, string, string, error)
	Register(ctx context.Context, email, password, name string) (*User, error)
	RefreshToken(ctx context.Context, refreshToken string) (string, string, error)
	GetMe(ctx context.Context, userID string) (*User, []string, error)
	ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error
	CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error)
	GetUser(ctx context.Context, id string) (*User, error)
	ListUsers(ctx context.Context, filter UserFilter) ([]*User, int, error)
	UpdateUser(ctx context.Context, id string, req *UpdateUserRequest) (*User, error)
	DeleteUser(ctx context.Context, id string) error
	CreateAPIKey(ctx context.Context, req *CreateAPIKeyRequest) (*APIKey, string, error)
	ListAPIKeys(ctx context.Context, organizationID string) ([]*APIKey, error)
	RevokeAPIKey(ctx context.Context, id string) error
}

type CreateUserRequest struct {
	Email          string
	Password       string
	Name           string
	Roles          []string
	OrganizationID string
}

type UpdateUserRequest struct {
	Name   string
	Email  string
	Status UserStatus
}

type CreateAPIKeyRequest struct {
	Name           string
	Description    string
	OrganizationID string
	Permissions    []string
	ExpiresAt      *time.Time
	AllowedIPs     []string
}
