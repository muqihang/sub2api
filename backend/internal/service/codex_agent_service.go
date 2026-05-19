package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/golang-jwt/jwt/v5"
)

const (
	codexManagedTokenIssuer   = "sub2api-codex-managed"
	codexManagedTokenAudience = "codex-managed-device"
	codexManagedTokenVersion  = 1
	codexManagedClient        = "codex"
	codexManagedMode          = "managed_proxy"
	codexDeeplinkScheme       = "zhumeng-agent://setup"
)

var (
	ErrCodexSetupGrantNotActive          = infraerrors.BadRequest("CODEX_SETUP_GRANT_NOT_ACTIVE", "setup grant is invalid, expired, or already used")
	ErrCodexSetupGrantOriginInvalid      = infraerrors.BadRequest("CODEX_SETUP_GRANT_ORIGIN_INVALID", "server origin must be a trusted https or local loopback origin")
	ErrCodexSetupGrantOriginMismatch     = infraerrors.BadRequest("CODEX_SETUP_GRANT_ORIGIN_MISMATCH", "server origin does not match setup grant")
	ErrCodexManagedDeviceNotFound        = infraerrors.NotFound("CODEX_MANAGED_DEVICE_NOT_FOUND", "managed device not found")
	ErrCodexManagedDeviceRevoked         = infraerrors.Forbidden("CODEX_MANAGED_DEVICE_REVOKED", "managed device has been revoked")
	ErrCodexManagedDeviceOwnershipDenied = infraerrors.Forbidden("CODEX_MANAGED_DEVICE_OWNERSHIP_DENIED", "device does not belong to the current user")
	ErrCodexManagedAPIKeyOwnershipDenied = infraerrors.Forbidden("CODEX_MANAGED_APIKEY_OWNERSHIP_DENIED", "api key does not belong to the current user")
	ErrCodexManagedAccessInvalid         = infraerrors.Unauthorized("CODEX_MANAGED_ACCESS_INVALID", "invalid managed device access token")
	ErrCodexManagedAccessExpired         = infraerrors.Unauthorized("CODEX_MANAGED_ACCESS_EXPIRED", "managed device access token has expired")
	ErrCodexManagedSessionMismatch       = infraerrors.Unauthorized("CODEX_MANAGED_SESSION_MISMATCH", "managed session does not match access token")
	ErrCodexManagedRefreshTokenInvalid   = infraerrors.Unauthorized("CODEX_MANAGED_REFRESH_TOKEN_INVALID", "invalid managed device refresh token")
)

type CodexAgentRepository interface {
	CreateSetupGrant(ctx context.Context, params CreateCodexSetupGrantParams) (*dbent.CodexSetupGrant, error)
	ConsumeSetupGrant(ctx context.Context, codeHash string, now time.Time) (*dbent.CodexSetupGrant, error)
	CreateManagedDevice(ctx context.Context, params CreateCodexManagedDeviceParams) (*dbent.CodexManagedDevice, error)
	GetManagedDevice(ctx context.Context, id int64) (*dbent.CodexManagedDevice, error)
	ListManagedDevicesByUser(ctx context.Context, userID int64) ([]*dbent.CodexManagedDevice, error)
	RevokeManagedDevice(ctx context.Context, id int64, revokedAt time.Time) error
	CreateDeviceToken(ctx context.Context, params CreateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error)
	RotateDeviceToken(ctx context.Context, params RotateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error)
	FindActiveTokenByHash(ctx context.Context, refreshTokenHash string, now time.Time) (*dbent.CodexDeviceToken, error)
	InsertAuditLog(ctx context.Context, params InsertCodexDeviceAuditLogParams) (*dbent.CodexDeviceAuditLog, error)
	ListPendingSetupGrantsByUser(ctx context.Context, userID int64, now time.Time) ([]*dbent.CodexSetupGrant, error)
	GetSetupGrantByID(ctx context.Context, id int64) (*dbent.CodexSetupGrant, error)
}

type CreateCodexSetupGrantParams struct {
	CodeHash      string
	UserID        int64
	APIKeyID      int64
	Mode          string
	ServerOrigin  string
	GatewayOrigin string
	ExpiresAt     time.Time
}

type CreateCodexManagedDeviceParams struct {
	UserID         int64
	APIKeyID       int64
	Name           string
	Platform       string
	Arch           string
	ManagerVersion string
	LastSeenAt     *time.Time
}

type CreateCodexDeviceTokenParams struct {
	DeviceID         int64
	RefreshTokenHash string
	ExpiresAt        time.Time
}

type RotateCodexDeviceTokenParams struct {
	CurrentRefreshTokenHash string
	NewRefreshTokenHash     string
	NewExpiresAt            time.Time
	Now                     time.Time
}

type InsertCodexDeviceAuditLogParams struct {
	DeviceID   int64
	UserID     int64
	Event      string
	IP         string
	UserAgent  string
	Metadata   map[string]any
	OccurredAt time.Time
}

type CodexConfigProfile struct {
	ModelProvider      string `json:"model_provider"`
	WireAPI            string `json:"wire_api"`
	RequiresOpenAIAuth bool   `json:"requires_openai_auth"`
	SupportsWebsockets bool   `json:"supports_websockets"`
}

type CreateCodexSetupGrantRequest struct {
	UserID        int64
	APIKeyID      int64
	Client        string
	Mode          string
	ServerOrigin  string
	GatewayOrigin string
}

type CreateCodexSetupGrantResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
	DeepLink  string    `json:"deeplink"`
}

type ExchangeCodexSetupGrantRequest struct {
	Code           string
	ServerOrigin   string
	DeviceName     string
	Platform       string
	Arch           string
	ManagerVersion string
}

type ExchangeCodexSetupGrantResponse struct {
	AccessToken      string             `json:"access_token"`
	RefreshToken     string             `json:"refresh_token"`
	ManagedSessionID string             `json:"managed_session_id"`
	ExpiresAt        time.Time          `json:"expires_at"`
	DeviceID         int64              `json:"device_id"`
	ServerBaseURL    string             `json:"server_base_url"`
	GatewayBaseURL   string             `json:"gateway_base_url"`
	ConfigProfile    CodexConfigProfile `json:"config_profile"`
}

type RefreshCodexDeviceTokenRequest struct {
	DeviceID     int64
	RefreshToken string
}

type RefreshCodexDeviceTokenResponse struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ManagedSessionID string    `json:"managed_session_id"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type ValidateManagedDeviceAccessRequest struct {
	AccessToken      string
	DeviceID         int64
	ManagedSessionID string
}

type ManagedDeviceAccessContext struct {
	APIKey           *APIKey
	User             *User
	Device           *dbent.CodexManagedDevice
	ManagedSessionID string
	ExpiresAt        time.Time
}

type codexManagedAPIKeyReader interface {
	VerifyOwnership(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error)
	GetByID(ctx context.Context, id int64) (*APIKey, error)
}

type codexManagedAccessClaims struct {
	DeviceID         int64  `json:"device_id"`
	APIKeyID         int64  `json:"api_key_id"`
	ManagedSessionID string `json:"managed_session_id"`
	TokenVersion     int    `json:"token_version"`
	jwt.RegisteredClaims
}

type CodexAgentService struct {
	entClient    *dbent.Client
	repo         CodexAgentRepository
	apiKeyReader codexManagedAPIKeyReader
	cfg          *config.Config
}

func NewCodexAgentService(entClient *dbent.Client, repo CodexAgentRepository, apiKeyReader codexManagedAPIKeyReader, cfg *config.Config) *CodexAgentService {
	return &CodexAgentService{
		entClient:    entClient,
		repo:         repo,
		apiKeyReader: apiKeyReader,
		cfg:          cfg,
	}
}

func (s *CodexAgentService) CreateSetupGrant(ctx context.Context, req CreateCodexSetupGrantRequest) (*CreateCodexSetupGrantResponse, error) {
	serverOrigin, err := validateTrustedHTTPSOrigin(req.ServerOrigin)
	if err != nil {
		return nil, ErrCodexSetupGrantOriginInvalid.WithCause(err)
	}
	if strings.TrimSpace(req.Client) == "" {
		req.Client = codexManagedClient
	}
	if strings.TrimSpace(req.Mode) == "" {
		req.Mode = codexManagedMode
	}

	validIDs, err := s.apiKeyReader.VerifyOwnership(ctx, req.UserID, []int64{req.APIKeyID})
	if err != nil {
		return nil, err
	}
	if len(validIDs) != 1 {
		return nil, ErrCodexManagedAPIKeyOwnershipDenied
	}

	code, err := randomHexString(24)
	if err != nil {
		return nil, fmt.Errorf("generate setup grant code: %w", err)
	}
	expiresAt := time.Now().Add(10 * time.Minute)
	gatewayOrigin := strings.TrimSpace(req.GatewayOrigin)
	if gatewayOrigin == "" {
		gatewayOrigin = serverOrigin
	}

	_, err = s.repo.CreateSetupGrant(ctx, CreateCodexSetupGrantParams{
		CodeHash:      hashManagedSecret(code),
		UserID:        req.UserID,
		APIKeyID:      req.APIKeyID,
		Mode:          req.Mode,
		ServerOrigin:  serverOrigin,
		GatewayOrigin: gatewayOrigin,
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		return nil, err
	}

	return &CreateCodexSetupGrantResponse{
		Code:      code,
		ExpiresAt: expiresAt,
		DeepLink:  fmt.Sprintf("%s?client=%s&code=%s&server=%s", codexDeeplinkScheme, req.Client, url.QueryEscape(code), url.QueryEscape(serverOrigin)),
	}, nil
}

func (s *CodexAgentService) ExchangeSetupGrant(ctx context.Context, req ExchangeCodexSetupGrantRequest) (*ExchangeCodexSetupGrantResponse, error) {
	serverOrigin, err := validateTrustedHTTPSOrigin(req.ServerOrigin)
	if err != nil {
		return nil, ErrCodexSetupGrantOriginInvalid.WithCause(err)
	}

	repoCtx := ctx
	var tx *dbent.Tx
	if s.entClient != nil {
		tx, err = s.entClient.Tx(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback() }()
		repoCtx = dbent.NewTxContext(ctx, tx)
	}

	grant, err := s.repo.ConsumeSetupGrant(repoCtx, hashManagedSecret(req.Code), time.Now())
	if err != nil {
		if errors.Is(err, ErrCodexSetupGrantNotActive) {
			return nil, ErrCodexSetupGrantNotActive
		}
		return nil, err
	}
	if grant.ServerOrigin != serverOrigin {
		return nil, ErrCodexSetupGrantOriginMismatch
	}

	device, err := s.repo.CreateManagedDevice(repoCtx, CreateCodexManagedDeviceParams{
		UserID:         grant.UserID,
		APIKeyID:       grant.APIKeyID,
		Name:           firstNonEmpty(req.DeviceName, "Codex Device"),
		Platform:       firstNonEmpty(req.Platform, "unknown"),
		Arch:           firstNonEmpty(req.Arch, "unknown"),
		ManagerVersion: firstNonEmpty(req.ManagerVersion, "unknown"),
	})
	if err != nil {
		return nil, err
	}

	refreshToken, err := randomHexString(32)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	_, err = s.repo.CreateDeviceToken(repoCtx, CreateCodexDeviceTokenParams{
		DeviceID:         device.ID,
		RefreshTokenHash: hashManagedSecret(refreshToken),
		ExpiresAt:        time.Now().Add(s.refreshTokenTTL()),
	})
	if err != nil {
		return nil, err
	}

	apiKey, err := s.apiKeyReader.GetByID(ctx, grant.APIKeyID)
	if err != nil {
		return nil, err
	}
	managedSessionID, err := randomHexString(16)
	if err != nil {
		return nil, fmt.Errorf("generate managed session id: %w", err)
	}
	accessToken, expiresAt, err := s.signManagedAccessToken(device.ID, apiKey.ID, managedSessionID, apiKey.User)
	if err != nil {
		return nil, err
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}

	return &ExchangeCodexSetupGrantResponse{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		ManagedSessionID: managedSessionID,
		ExpiresAt:        expiresAt,
		DeviceID:         device.ID,
		ServerBaseURL:    grant.ServerOrigin,
		GatewayBaseURL:   grant.GatewayOrigin,
		ConfigProfile:    s.DefaultCodexConfigProfile(),
	}, nil
}

func (s *CodexAgentService) RefreshDeviceToken(ctx context.Context, req RefreshCodexDeviceTokenRequest) (*RefreshCodexDeviceTokenResponse, error) {
	token, err := s.repo.FindActiveTokenByHash(ctx, hashManagedSecret(req.RefreshToken), time.Now())
	if err != nil {
		return nil, ErrCodexManagedRefreshTokenInvalid.WithCause(err)
	}
	if token.DeviceID != req.DeviceID {
		return nil, ErrCodexManagedRefreshTokenInvalid
	}
	if token.Edges.Device == nil || token.Edges.Device.Status != "active" || token.Edges.Device.RevokedAt != nil {
		return nil, ErrCodexManagedDeviceRevoked
	}

	apiKey, err := s.apiKeyReader.GetByID(ctx, token.Edges.Device.APIKeyID)
	if err != nil {
		return nil, err
	}

	nextRefreshToken, err := randomHexString(32)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	if _, err := s.repo.RotateDeviceToken(ctx, RotateCodexDeviceTokenParams{
		CurrentRefreshTokenHash: hashManagedSecret(req.RefreshToken),
		NewRefreshTokenHash:     hashManagedSecret(nextRefreshToken),
		NewExpiresAt:            time.Now().Add(s.refreshTokenTTL()),
		Now:                     time.Now(),
	}); err != nil {
		if errors.Is(err, ErrCodexManagedRefreshTokenInvalid) {
			return nil, ErrCodexManagedRefreshTokenInvalid
		}
		return nil, err
	}

	managedSessionID, err := randomHexString(16)
	if err != nil {
		return nil, fmt.Errorf("generate managed session id: %w", err)
	}
	accessToken, expiresAt, err := s.signManagedAccessToken(token.DeviceID, apiKey.ID, managedSessionID, apiKey.User)
	if err != nil {
		return nil, err
	}

	return &RefreshCodexDeviceTokenResponse{
		AccessToken:      accessToken,
		RefreshToken:     nextRefreshToken,
		ManagedSessionID: managedSessionID,
		ExpiresAt:        expiresAt,
	}, nil
}

func (s *CodexAgentService) ListDevices(ctx context.Context, userID int64, apiKeyID *int64) ([]*dbent.CodexManagedDevice, error) {
	devices, err := s.repo.ListManagedDevicesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if apiKeyID == nil {
		return devices, nil
	}
	validIDs, err := s.apiKeyReader.VerifyOwnership(ctx, userID, []int64{*apiKeyID})
	if err != nil {
		return nil, err
	}
	if len(validIDs) != 1 {
		return nil, ErrCodexManagedAPIKeyOwnershipDenied
	}
	filtered := make([]*dbent.CodexManagedDevice, 0, len(devices))
	for _, device := range devices {
		if device.APIKeyID == *apiKeyID {
			filtered = append(filtered, device)
		}
	}
	return filtered, nil
}

func (s *CodexAgentService) RevokeDevice(ctx context.Context, userID, deviceID int64) error {
	device, err := s.repo.GetManagedDevice(ctx, deviceID)
	if err != nil {
		return ErrCodexManagedDeviceNotFound.WithCause(err)
	}
	validIDs, err := s.apiKeyReader.VerifyOwnership(ctx, userID, []int64{device.APIKeyID})
	if err != nil {
		return err
	}
	if len(validIDs) != 1 || device.UserID != userID {
		return ErrCodexManagedDeviceOwnershipDenied
	}
	return s.repo.RevokeManagedDevice(ctx, deviceID, time.Now())
}

func (s *CodexAgentService) ValidateManagedDeviceAccess(ctx context.Context, req ValidateManagedDeviceAccessRequest) (*ManagedDeviceAccessContext, error) {
	bearerToken, err := extractBearerToken(req.AccessToken)
	if err != nil {
		return nil, err
	}

	claims, err := s.parseManagedAccessToken(bearerToken)
	if err != nil {
		return nil, err
	}
	if claims.DeviceID != req.DeviceID {
		return nil, ErrCodexManagedAccessInvalid
	}
	if claims.ManagedSessionID != req.ManagedSessionID {
		return nil, ErrCodexManagedSessionMismatch
	}

	device, err := s.repo.GetManagedDevice(ctx, claims.DeviceID)
	if err != nil {
		return nil, ErrCodexManagedDeviceNotFound.WithCause(err)
	}
	if device.APIKeyID != claims.APIKeyID {
		return nil, ErrCodexManagedAccessInvalid
	}
	if device.Status != "active" || device.RevokedAt != nil {
		return nil, ErrCodexManagedDeviceRevoked
	}

	apiKey, err := s.apiKeyReader.GetByID(ctx, claims.APIKeyID)
	if err != nil {
		return nil, err
	}
	if apiKey == nil || apiKey.User == nil {
		return nil, ErrAPIKeyNotFound
	}
	if !apiKey.IsActive() {
		return nil, ErrAPIKeyExpired
	}
	if !apiKey.User.IsActive() {
		return nil, ErrUserNotActive
	}

	return &ManagedDeviceAccessContext{
		APIKey:           apiKey,
		User:             apiKey.User,
		Device:           device,
		ManagedSessionID: claims.ManagedSessionID,
		ExpiresAt:        claims.ExpiresAt.Time,
	}, nil
}

func (s *CodexAgentService) DefaultCodexConfigProfile() CodexConfigProfile {
	return CodexConfigProfile{
		ModelProvider:      "zhumeng-codex",
		WireAPI:            "responses",
		RequiresOpenAIAuth: true,
		SupportsWebsockets: false,
	}
}

func (s *CodexAgentService) signManagedAccessToken(deviceID, apiKeyID int64, managedSessionID string, user *User) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(s.accessTokenTTL())
	userTokenVersion := int64(0)
	if user != nil {
		userTokenVersion = user.TokenVersion
	}
	claims := &codexManagedAccessClaims{
		DeviceID:         deviceID,
		APIKeyID:         apiKeyID,
		ManagedSessionID: managedSessionID,
		TokenVersion:     codexManagedTokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{codexManagedTokenAudience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    codexManagedTokenIssuer,
			Subject:   fmt.Sprintf("%d:%d:%d", deviceID, apiKeyID, userTokenVersion),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign managed access token: %w", err)
	}
	return signed, expiresAt, nil
}

func (s *CodexAgentService) parseManagedAccessToken(tokenString string) (*codexManagedAccessClaims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}),
		jwt.WithAudience(codexManagedTokenAudience),
		jwt.WithIssuer(codexManagedTokenIssuer),
	)
	token, err := parser.ParseWithClaims(tokenString, &codexManagedAccessClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWT.Secret), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrCodexManagedAccessExpired
		}
		return nil, ErrCodexManagedAccessInvalid.WithCause(err)
	}
	claims, ok := token.Claims.(*codexManagedAccessClaims)
	if !ok || !token.Valid {
		return nil, ErrCodexManagedAccessInvalid
	}
	return claims, nil
}

func (s *CodexAgentService) accessTokenTTL() time.Duration {
	if s.cfg != nil && s.cfg.JWT.AccessTokenExpireMinutes > 0 {
		return time.Duration(s.cfg.JWT.AccessTokenExpireMinutes) * time.Minute
	}
	return 15 * time.Minute
}

func (s *CodexAgentService) refreshTokenTTL() time.Duration {
	if s.cfg != nil && s.cfg.JWT.RefreshTokenExpireDays > 0 {
		return time.Duration(s.cfg.JWT.RefreshTokenExpireDays) * 24 * time.Hour
	}
	return 30 * 24 * time.Hour
}

func validateTrustedHTTPSOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("origin must not include query or fragment")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("origin must not include userinfo")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("origin must not include path")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("origin host is required")
	}
	if ipAddr := net.ParseIP(host); ipAddr != nil {
		if strings.EqualFold(parsed.Scheme, "http") && ipAddr.IsLoopback() {
			return strings.TrimRight(parsed.String(), "/"), nil
		}
		if ipAddr.IsLoopback() || ipAddr.IsPrivate() || ipAddr.IsLinkLocalUnicast() || ipAddr.IsLinkLocalMulticast() {
			return "", fmt.Errorf("private or loopback ip is not allowed")
		}
	}
	if strings.EqualFold(parsed.Scheme, "http") && strings.EqualFold(host, "localhost") {
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", fmt.Errorf("origin must use https")
	}
	if strings.EqualFold(host, "localhost") {
		return "", fmt.Errorf("localhost is not allowed")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func extractBearerToken(raw string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", ErrCodexManagedAccessInvalid
	}
	return strings.TrimSpace(parts[1]), nil
}

func hashManagedSecret(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
