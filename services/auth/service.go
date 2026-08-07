package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/rymelabs/rymevisor/services/auth/domain"
)

type Service struct {
	userRepo    domain.UserRepository
	orgRepo     domain.OrganizationRepository
	keyRepo     domain.APIKeyRepository
	sessionRepo domain.SessionRepository
	jwtSecret   []byte
	bcryptCost  int
	jwtExpiry   time.Duration
	refreshExpiry time.Duration
}

func NewService(
	userRepo domain.UserRepository,
	orgRepo domain.OrganizationRepository,
	keyRepo domain.APIKeyRepository,
	sessionRepo domain.SessionRepository,
	jwtSecret string,
	bcryptCost int,
	jwtExpiry time.Duration,
	refreshExpiry time.Duration,
) *Service {
	if bcryptCost <= 0 {
		bcryptCost = 12
	}
	if jwtExpiry <= 0 {
		jwtExpiry = time.Hour
	}
	if refreshExpiry <= 0 {
		refreshExpiry = 7 * 24 * time.Hour
	}
	return &Service{
		userRepo:      userRepo,
		orgRepo:       orgRepo,
		keyRepo:       keyRepo,
		sessionRepo:   sessionRepo,
		jwtSecret:     []byte(jwtSecret),
		bcryptCost:    bcryptCost,
		jwtExpiry:     jwtExpiry,
		refreshExpiry: refreshExpiry,
	}
}

func (s *Service) Login(ctx context.Context, email, password string) (*domain.User, string, string, error) {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, "", "", domain.ErrInvalidPassword
		}
		return nil, "", "", err
	}

	if user.Status != domain.UserStatusActive {
		return nil, "", "", domain.ErrUnauthorized
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, "", "", domain.ErrInvalidPassword
	}

	accessToken, err := s.generateToken(user, s.jwtExpiry)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate access token: %w", err)
	}

	refreshToken, err := s.generateToken(user, s.refreshExpiry)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	tokenHash := hashToken(refreshToken)
	session := &domain.Session{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(s.refreshExpiry),
	}
	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, "", "", fmt.Errorf("create session: %w", err)
	}

	return user, accessToken, refreshToken, nil
}

func (s *Service) Register(ctx context.Context, email, password, name string) (*domain.User, error) {
	existing, err := s.userRepo.GetByEmail(ctx, email)
	if err == nil && existing != nil {
		return nil, domain.ErrEmailExists
	}
	if err != nil && !errors.Is(err, domain.ErrUserNotFound) {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &domain.User{
		Email:        email,
		Name:         name,
		PasswordHash: string(hash),
		Status:       domain.UserStatusActive,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return user, nil
}

func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (string, string, error) {
	claims, err := ValidateToken(refreshToken, s.jwtSecret)
	if err != nil {
		return "", "", fmt.Errorf("invalid refresh token: %w", err)
	}

	tokenHash := hashToken(refreshToken)
	session, err := s.sessionRepo.GetByToken(ctx, tokenHash)
	if err != nil {
		return "", "", fmt.Errorf("session not found or expired")
	}

	_ = s.sessionRepo.Delete(ctx, session.ID)

	user, err := s.userRepo.GetByID(ctx, claims.UserID)
	if err != nil {
		return "", "", fmt.Errorf("user not found: %w", err)
	}

	newAccess, err := s.generateToken(user, s.jwtExpiry)
	if err != nil {
		return "", "", fmt.Errorf("generate access token: %w", err)
	}

	newRefresh, err := s.generateToken(user, s.refreshExpiry)
	if err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	newSession := &domain.Session{
		UserID:    user.ID,
		TokenHash: hashToken(newRefresh),
		ExpiresAt: time.Now().Add(s.refreshExpiry),
	}
	if err := s.sessionRepo.Create(ctx, newSession); err != nil {
		return "", "", fmt.Errorf("create session: %w", err)
	}

	return newAccess, newRefresh, nil
}

func (s *Service) GetMe(ctx context.Context, userID string) (*domain.User, []string, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return user, user.Permissions, nil
}

func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return domain.ErrInvalidPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	return s.userRepo.UpdatePassword(ctx, userID, string(hash))
}

func (s *Service) CreateUser(ctx context.Context, req *domain.CreateUserRequest) (*domain.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &domain.User{
		Email:        req.Email,
		Name:         req.Name,
		PasswordHash: string(hash),
		Status:       domain.UserStatusActive,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *Service) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return s.userRepo.GetByID(ctx, id)
}

func (s *Service) ListUsers(ctx context.Context, filter domain.UserFilter) ([]*domain.User, int, error) {
	return s.userRepo.List(ctx, filter)
}

func (s *Service) UpdateUser(ctx context.Context, id string, req *domain.UpdateUserRequest) (*domain.User, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		user.Name = req.Name
	}
	if req.Email != "" {
		user.Email = req.Email
	}
	if req.Status != "" {
		user.Status = req.Status
	}

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *Service) DeleteUser(ctx context.Context, id string) error {
	return s.userRepo.Delete(ctx, id)
}

func (s *Service) CreateAPIKey(ctx context.Context, req *domain.CreateAPIKeyRequest) (*domain.APIKey, string, error) {
	rawKey, err := generateRandomKey(32)
	if err != nil {
		return nil, "", fmt.Errorf("generate key: %w", err)
	}

	prefix := rawKey[:8]
	keyHash := sha256Hex(rawKey)

	key := &domain.APIKey{
		Name:           req.Name,
		Description:    req.Description,
		Prefix:         prefix,
		KeyHash:        keyHash,
		UserID:         "",
		OrganizationID: req.OrganizationID,
		Permissions:    req.Permissions,
		AllowedIPs:     req.AllowedIPs,
		Active:         true,
		ExpiresAt:      req.ExpiresAt,
	}

	if err := s.keyRepo.Create(ctx, key); err != nil {
		return nil, "", fmt.Errorf("create api key: %w", err)
	}

	return key, rawKey, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, organizationID string) ([]*domain.APIKey, error) {
	return s.keyRepo.List(ctx, organizationID)
}

func (s *Service) RevokeAPIKey(ctx context.Context, id string) error {
	return s.keyRepo.Delete(ctx, id)
}

func (s *Service) ValidateToken(tokenString string) (*domain.JWTClaims, error) {
	return ValidateToken(tokenString, s.jwtSecret)
}

func (s *Service) generateToken(user *domain.User, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := domain.JWTClaims{
		UserID:        user.ID,
		Email:         user.Email,
		Organizations: user.Organizations,
		Permissions:   user.Permissions,
		ExpiresAt:     now.Add(expiry).Unix(),
		IssuedAt:      now.Unix(),
	}
	return SignToken(claims, s.jwtSecret)
}

// --- JWT helpers ---

func SignToken(claims domain.JWTClaims, secret []byte) (string, error) {
	header := base64URLEncode([]byte(`{"alg":"HS256","typ":"JWT"}`))

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64URLEncode(payloadBytes)

	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	signature := base64URLEncode(mac.Sum(nil))

	return signingInput + "." + signature, nil
}

func ValidateToken(tokenString string, secret []byte) (*domain.JWTClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expectedSig := base64URLEncode(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, fmt.Errorf("invalid token signature")
	}

	payloadBytes, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var claims domain.JWTClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("token expired")
	}

	return &claims, nil
}

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// --- Token hashing ---

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

func sha256Hex(data string) string {
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h)
}

func generateRandomKey(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
