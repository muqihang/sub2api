package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	augmentPrincipalKindJWT    = "jwt"
	augmentPrincipalKindAPIKey = "api_key"

	augmentPluginGrantTTL               = 2 * time.Minute
	augmentLegacyStateSchemaVersion     = 1
	augmentLegacyDefaultNamespace       = "default"
	augmentLegacyPersistedStateSubdir   = "augment-compat"
	augmentLegacyPersistedStateFilename = "legacy-state.json"
)

const (
	AugmentQuickLoginModeOfficialPassthrough = "official_passthrough"
	AugmentQuickLoginModeLocalCompat         = "local_compat"

	AugmentSessionSourceOfficial    = "official"
	AugmentSessionSourceLocalCompat = AugmentQuickLoginModeLocalCompat
)

var (
	defaultAugmentPluginScopes = []string{"augment:session", "augment:summary", "augment:compat"}

	ErrAugmentPluginGrantInvalid = infraerrors.BadRequest("AUGMENT_PLUGIN_GRANT_INVALID", "invalid or expired augment quick login grant")
	ErrAugmentPluginAuthMissing  = infraerrors.Unauthorized("AUGMENT_PLUGIN_AUTH_MISSING", "authorization bearer token is required")
	ErrAugmentPluginModeInvalid  = infraerrors.BadRequest("AUGMENT_PLUGIN_MODE_INVALID", "unsupported augment quick login mode")
	ErrAugmentPluginOfficialAuth = infraerrors.BadRequest("AUGMENT_PLUGIN_OFFICIAL_SESSION_REQUIRED", "official passthrough mode requires explicit official session inputs")
)

type augmentPluginAuthAPI interface {
	GenerateTokenPair(ctx context.Context, user *User, familyID string) (*TokenPair, error)
	RefreshTokenPair(ctx context.Context, refreshToken string) (*TokenPairWithUser, error)
	ValidateToken(token string) (*JWTClaims, error)
}

type augmentPluginUserAPI interface {
	GetByID(ctx context.Context, id int64) (*User, error)
}

type augmentPluginAPIKeyAPI interface {
	GetByKey(ctx context.Context, key string) (*APIKey, error)
	List(ctx context.Context, userID int64, params pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error)
	GetAvailableGroups(ctx context.Context, userID int64) ([]Group, error)
	Create(ctx context.Context, userID int64, req CreateAPIKeyRequest) (*APIKey, error)
}

type augmentPluginSubscriptionAPI interface {
	ListActiveUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error)
}

type augmentPluginSettingAPI interface {
	GetPublicSettings(ctx context.Context) (*PublicSettings, error)
	GetSiteName(ctx context.Context) string
}

type augmentPluginGrantRecord struct {
	UserID        int64
	State         string
	ExpiresAt     time.Time
	Mode          string
	SessionBundle *AugmentSessionBundle
}

type augmentLegacyCheckpointState struct {
	BlobNames map[string]struct{}
}

type augmentLegacyNamespaceState struct {
	Checkpoints map[string]augmentLegacyCheckpointState
	Blobs       map[string]augmentLegacyBlobRecord
	Chats       map[string]augmentLegacyChatConversation
}

type augmentLegacyBlobRecord struct {
	BlobName   string
	Path       string
	Content    string
	UploadedAt time.Time
}

type augmentLegacyChatExchange struct {
	RequestMessage string
	ResponseText   string
	RequestID      string
}

type augmentLegacyChatConversation struct {
	ConversationID string
	Title          string
	Chat           []augmentLegacyChatExchange
	UpdatedAt      time.Time
}

type augmentLegacyPersistedState struct {
	SchemaVersion int                                             `json:"schema_version"`
	Namespaces    map[string]augmentLegacyPersistedNamespaceState `json:"namespaces"`
}

type augmentLegacyPersistedNamespaceState struct {
	Checkpoints map[string]augmentLegacyPersistedCheckpointState `json:"checkpoints"`
	Blobs       map[string]augmentLegacyBlobRecord               `json:"blobs"`
	Chats       map[string]augmentLegacyChatConversation         `json:"chats"`
}

type augmentLegacyPersistedCheckpointState struct {
	BlobNames []string `json:"blob_names"`
}

type AugmentQuickLoginGrant struct {
	Grant     string   `json:"grant"`
	State     string   `json:"state"`
	ExpiresAt string   `json:"expires_at"`
	TenantURL string   `json:"tenant_url,omitempty"`
	PortalURL string   `json:"portal_url,omitempty"`
	Scopes    []string `json:"scopes"`
}

type AugmentQuickLoginGrantOptions struct {
	TenantURL             string
	Mode                  string
	OfficialSessionBundle *AugmentSessionBundle
}

type AugmentSessionBundle struct {
	AccessToken   string   `json:"access_token"`
	RefreshToken  string   `json:"refresh_token"`
	ExpiresAt     string   `json:"expires_at"`
	TenantURL     string   `json:"tenant_url"`
	PortalURL     string   `json:"portal_url,omitempty"`
	Scopes        []string `json:"scopes"`
	SessionSource string   `json:"session_source"`
}

type AugmentLegacyLoginToken struct {
	TenantURL     string `json:"tenantUrl"`
	AccessToken   string `json:"accessToken"`
	SessionSource string `json:"sessionSource,omitempty"`
}

type AugmentSessionRefreshOptions struct {
	TenantURL             string
	Mode                  string
	OfficialSessionBundle *AugmentSessionBundle
}

type AugmentAPIKeyVerification struct {
	Valid        bool    `json:"valid"`
	APIKey       string  `json:"api_key,omitempty"`
	UserID       int64   `json:"user_id,omitempty"`
	UserEmail    string  `json:"user_email,omitempty"`
	UserStatus   string  `json:"user_status,omitempty"`
	APIKeyStatus string  `json:"api_key_status,omitempty"`
	Reason       string  `json:"reason,omitempty"`
	ExpiresAt    *string `json:"expires_at,omitempty"`
}

type AugmentPluginPrincipal struct {
	Kind   string
	User   *User
	APIKey *APIKey
}

type AugmentPluginUserSummary struct {
	ID          int64   `json:"id"`
	Email       string  `json:"email"`
	Username    string  `json:"username,omitempty"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	Balance     float64 `json:"balance,omitempty"`
	Concurrency int     `json:"concurrency,omitempty"`
}

type AugmentPluginPlanSummary struct {
	ActiveCount int      `json:"active_count"`
	GroupNames  []string `json:"group_names,omitempty"`
	ExpiresAt   *string  `json:"expires_at,omitempty"`
}

type AugmentPluginSummary struct {
	GatewayAPIKey string                    `json:"gateway_api_key,omitempty"`
	PrimaryAPIKey string                    `json:"primary_api_key,omitempty"`
	User          *AugmentPluginUserSummary `json:"user,omitempty"`
	Plan          *AugmentPluginPlanSummary `json:"plan,omitempty"`
}

type AugmentPluginRegistryGroup struct {
	ID                   int64    `json:"id"`
	Name                 string   `json:"name"`
	Platform             string   `json:"platform"`
	DefaultModel         string   `json:"default_model,omitempty"`
	SupportedModelScopes []string `json:"supported_model_scopes,omitempty"`
}

type AugmentPluginModelRegistry struct {
	SupportedModelScopes []string                     `json:"supported_model_scopes,omitempty"`
	Groups               []AugmentPluginRegistryGroup `json:"groups,omitempty"`
}

type AugmentPluginSessionDisplay struct {
	SiteName   string `json:"site_name"`
	UserLabel  string `json:"user_label"`
	AuthMethod string `json:"auth_method"`
}

type AugmentPluginAccountState struct {
	UserStatus         string `json:"user_status"`
	APIKeyStatus       string `json:"api_key_status,omitempty"`
	BackendModeEnabled bool   `json:"backend_mode_enabled,omitempty"`
}

type AugmentPluginCompatMetadata struct {
	GatewayBaseURL string                      `json:"gateway_base_url"`
	DefaultModel   string                      `json:"default_model"`
	ModelRegistry  AugmentPluginModelRegistry  `json:"model_registry"`
	SessionDisplay AugmentPluginSessionDisplay `json:"session_display"`
	AccountState   AugmentPluginAccountState   `json:"account_state"`
}

type AugmentPluginService struct {
	cfg           *config.Config
	auth          augmentPluginAuthAPI
	users         augmentPluginUserAPI
	apiKeys       augmentPluginAPIKeyAPI
	subscriptions augmentPluginSubscriptionAPI
	settings      augmentPluginSettingAPI

	now func() time.Time

	mu               sync.Mutex
	grants           map[string]augmentPluginGrantRecord
	legacyNamespaces map[string]*augmentLegacyNamespaceState
	legacyStateFile  string
}

func NewAugmentPluginService(
	cfg *config.Config,
	auth augmentPluginAuthAPI,
	users augmentPluginUserAPI,
	apiKeys augmentPluginAPIKeyAPI,
	subscriptions augmentPluginSubscriptionAPI,
	settings augmentPluginSettingAPI,
) *AugmentPluginService {
	svc := &AugmentPluginService{
		cfg:              cfg,
		auth:             auth,
		users:            users,
		apiKeys:          apiKeys,
		subscriptions:    subscriptions,
		settings:         settings,
		now:              time.Now,
		grants:           make(map[string]augmentPluginGrantRecord),
		legacyNamespaces: make(map[string]*augmentLegacyNamespaceState),
		legacyStateFile:  augmentLegacyStateFilePath(cfg),
	}
	if err := svc.loadLegacyCompatState(); err != nil {
		slog.Warn("augment legacy compat state load failed", "path", svc.legacyStateFile, "error", err)
	}
	return svc
}

func augmentLegacyStateFilePath(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	baseDir := strings.TrimSpace(cfg.Pricing.DataDir)
	if baseDir == "" {
		return ""
	}
	return filepath.Join(baseDir, augmentLegacyPersistedStateSubdir, augmentLegacyPersistedStateFilename)
}

func normalizeAugmentLegacyNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return augmentLegacyDefaultNamespace
	}
	return namespace
}

func newAugmentLegacyNamespaceState() *augmentLegacyNamespaceState {
	return &augmentLegacyNamespaceState{
		Checkpoints: make(map[string]augmentLegacyCheckpointState),
		Blobs:       make(map[string]augmentLegacyBlobRecord),
		Chats:       make(map[string]augmentLegacyChatConversation),
	}
}

func (s *AugmentPluginService) augmentLegacyNamespaceLocked(namespace string) *augmentLegacyNamespaceState {
	key := normalizeAugmentLegacyNamespace(namespace)
	state, ok := s.legacyNamespaces[key]
	if ok && state != nil {
		return state
	}
	state = newAugmentLegacyNamespaceState()
	s.legacyNamespaces[key] = state
	return state
}

func (s *AugmentPluginService) persistLegacyCompatStateLocked() error {
	if strings.TrimSpace(s.legacyStateFile) == "" {
		return nil
	}

	dir := filepath.Dir(s.legacyStateFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create augment legacy state dir: %w", err)
	}

	persisted := augmentLegacyPersistedState{
		SchemaVersion: augmentLegacyStateSchemaVersion,
		Namespaces:    make(map[string]augmentLegacyPersistedNamespaceState, len(s.legacyNamespaces)),
	}
	for namespace, state := range s.legacyNamespaces {
		if state == nil {
			continue
		}
		checkpoints := make(map[string]augmentLegacyPersistedCheckpointState, len(state.Checkpoints))
		for checkpointID, checkpointState := range state.Checkpoints {
			names := make([]string, 0, len(checkpointState.BlobNames))
			for name := range checkpointState.BlobNames {
				names = append(names, name)
			}
			sort.Strings(names)
			checkpoints[checkpointID] = augmentLegacyPersistedCheckpointState{BlobNames: names}
		}

		blobs := make(map[string]augmentLegacyBlobRecord, len(state.Blobs))
		for name, blob := range state.Blobs {
			blobs[name] = blob
		}

		chats := make(map[string]augmentLegacyChatConversation, len(state.Chats))
		for id, chat := range state.Chats {
			chats[id] = chat
		}

		persisted.Namespaces[namespace] = augmentLegacyPersistedNamespaceState{
			Checkpoints: checkpoints,
			Blobs:       blobs,
			Chats:       chats,
		}
	}

	body, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal augment legacy state: %w", err)
	}

	tmpPath := s.legacyStateFile + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o644); err != nil {
		return fmt.Errorf("write augment legacy state temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.legacyStateFile); err != nil {
		return fmt.Errorf("replace augment legacy state file: %w", err)
	}
	return nil
}

func (s *AugmentPluginService) loadLegacyCompatState() error {
	if strings.TrimSpace(s.legacyStateFile) == "" {
		return nil
	}

	body, err := os.ReadFile(s.legacyStateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read augment legacy state: %w", err)
	}

	var persisted augmentLegacyPersistedState
	if err := json.Unmarshal(body, &persisted); err != nil {
		return fmt.Errorf("decode augment legacy state: %w", err)
	}
	if persisted.SchemaVersion != augmentLegacyStateSchemaVersion {
		return nil
	}

	for namespace, state := range persisted.Namespaces {
		ns := newAugmentLegacyNamespaceState()
		for checkpointID, checkpointState := range state.Checkpoints {
			blobNames := make(map[string]struct{}, len(checkpointState.BlobNames))
			for _, name := range checkpointState.BlobNames {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				blobNames[name] = struct{}{}
			}
			ns.Checkpoints[checkpointID] = augmentLegacyCheckpointState{BlobNames: blobNames}
		}
		for name, blob := range state.Blobs {
			ns.Blobs[name] = blob
		}
		for id, chat := range state.Chats {
			ns.Chats[id] = chat
		}
		s.legacyNamespaces[normalizeAugmentLegacyNamespace(namespace)] = ns
	}
	return nil
}

func (s *AugmentPluginService) CreateQuickLoginGrant(ctx context.Context, userID int64, options AugmentQuickLoginGrantOptions) (*AugmentQuickLoginGrant, error) {
	if s.users == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_PLUGIN_UNAVAILABLE", "augment plugin service is unavailable")
	}

	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return nil, err
	}

	mode, err := normalizeAugmentQuickLoginMode(options.Mode)
	if err != nil {
		return nil, err
	}

	var sessionBundle *AugmentSessionBundle
	if mode == AugmentQuickLoginModeOfficialPassthrough {
		sessionBundle, err = normalizeOfficialAugmentSessionBundle(options.OfficialSessionBundle)
		if err != nil {
			return nil, err
		}
	}

	grant, err := randomHexToken(24)
	if err != nil {
		return nil, fmt.Errorf("generate quick login grant: %w", err)
	}
	state, err := randomHexToken(16)
	if err != nil {
		return nil, fmt.Errorf("generate quick login state: %w", err)
	}

	now := s.now()
	expiresAt := now.Add(augmentPluginGrantTTL)
	portalURL, _ := s.BuildQuickLoginPortalURL(ctx, userID, options.TenantURL)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredGrantsLocked(now)
	s.grants[grant] = augmentPluginGrantRecord{
		UserID:        userID,
		State:         state,
		ExpiresAt:     expiresAt,
		Mode:          mode,
		SessionBundle: sessionBundle,
	}

	responseTenantURL := strings.TrimSpace(options.TenantURL)
	responseScopes := append([]string(nil), defaultAugmentPluginScopes...)
	if sessionBundle != nil {
		responseTenantURL = sessionBundle.TenantURL
		responseScopes = append([]string(nil), sessionBundle.Scopes...)
	}

	return &AugmentQuickLoginGrant{
		Grant:     grant,
		State:     state,
		ExpiresAt: expiresAt.Format(time.RFC3339),
		TenantURL: responseTenantURL,
		PortalURL: portalURL,
		Scopes:    responseScopes,
	}, nil
}

func (s *AugmentPluginService) BuildQuickLoginPortalURL(ctx context.Context, userID int64, gatewayBaseURL string) (string, error) {
	if s.users == nil {
		return "", nil
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if user == nil || !user.IsActive() {
		return "", ErrUserNotActive
	}

	principal := AugmentPluginPrincipal{
		Kind: augmentPrincipalKindJWT,
		User: user,
	}

	summary, err := s.BuildSummary(ctx, principal)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(summary.GatewayAPIKey)
	if token == "" {
		return "", nil
	}

	compat, err := s.BuildCompatMetadata(ctx, principal, gatewayBaseURL)
	if err != nil {
		return "", err
	}

	baseURL := ""
	if settings, settingsErr := s.publicSettings(ctx); settingsErr == nil && settings != nil {
		baseURL = normalizeAugmentAbsoluteURL(settings.APIBaseURL)
	}
	if baseURL == "" && compat != nil {
		baseURL = normalizeAugmentAbsoluteURL(compat.GatewayBaseURL)
	}
	if baseURL == "" {
		baseURL = normalizeAugmentAbsoluteURL(gatewayBaseURL)
	}
	if baseURL == "" {
		return "", nil
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", nil
	}
	query := parsed.Query()
	query.Set("token", token)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (s *AugmentPluginService) ExchangeGrant(ctx context.Context, grant, state, tenantURL string) (*AugmentSessionBundle, error) {
	now := s.now()

	s.mu.Lock()
	record, ok := s.grants[grant]
	if ok {
		delete(s.grants, grant)
	}
	s.pruneExpiredGrantsLocked(now)
	s.mu.Unlock()

	if !ok || strings.TrimSpace(state) == "" || record.State != strings.TrimSpace(state) || !record.ExpiresAt.After(now) {
		return nil, ErrAugmentPluginGrantInvalid
	}

	portalURL, _ := s.BuildQuickLoginPortalURL(ctx, record.UserID, tenantURL)

	if record.Mode == AugmentQuickLoginModeOfficialPassthrough {
		if record.SessionBundle == nil {
			return nil, ErrAugmentPluginOfficialAuth
		}
		bundle := cloneAugmentSessionBundle(record.SessionBundle)
		if bundle != nil {
			bundle.PortalURL = portalURL
		}
		return bundle, nil
	}

	if s.auth == nil || s.users == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_PLUGIN_UNAVAILABLE", "augment plugin service is unavailable")
	}

	user, err := s.users.GetByID(ctx, record.UserID)
	if err != nil {
		return nil, err
	}

	pair, err := s.auth.GenerateTokenPair(ctx, user, "")
	if err != nil {
		return nil, err
	}

	bundle := s.newSessionBundle(pair, tenantURL, AugmentSessionSourceLocalCompat)
	bundle.PortalURL = portalURL
	return bundle, nil
}

func (s *AugmentPluginService) RefreshSession(ctx context.Context, refreshToken, tenantURL string) (*AugmentSessionBundle, error) {
	return s.RefreshSessionWithOptions(ctx, refreshToken, AugmentSessionRefreshOptions{
		TenantURL: tenantURL,
	})
}

func (s *AugmentPluginService) RefreshSessionWithOptions(ctx context.Context, refreshToken string, options AugmentSessionRefreshOptions) (*AugmentSessionBundle, error) {
	mode, err := normalizeAugmentQuickLoginMode(options.Mode)
	if err != nil {
		return nil, err
	}
	if mode == AugmentQuickLoginModeOfficialPassthrough {
		bundle, err := normalizeOfficialAugmentSessionBundle(options.OfficialSessionBundle)
		if err != nil {
			return nil, err
		}
		return bundle, nil
	}

	if s.auth == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_PLUGIN_UNAVAILABLE", "augment plugin service is unavailable")
	}

	pair, err := s.auth.RefreshTokenPair(ctx, strings.TrimSpace(refreshToken))
	if err != nil {
		return nil, err
	}

	return s.newSessionBundle(&pair.TokenPair, options.TenantURL, AugmentSessionSourceLocalCompat), nil
}

func (s *AugmentPluginService) VerifyPresentedAPIKey(ctx context.Context, key string) (*AugmentAPIKeyVerification, error) {
	if s.apiKeys == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_PLUGIN_UNAVAILABLE", "augment plugin service is unavailable")
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return nil, infraerrors.BadRequest("AUGMENT_PLUGIN_API_KEY_REQUIRED", "api key is required")
	}

	apiKey, err := s.apiKeys.GetByKey(ctx, key)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return &AugmentAPIKeyVerification{
				Valid:  false,
				Reason: "invalid_api_key",
			}, nil
		}
		return nil, err
	}

	user := apiKey.User
	if user == nil && s.users != nil && apiKey.UserID > 0 {
		user, err = s.users.GetByID(ctx, apiKey.UserID)
		if err != nil {
			return nil, err
		}
	}

	result := &AugmentAPIKeyVerification{
		Valid:        apiKey.Status == StatusActive && user != nil && user.IsActive() && !apiKey.IsExpired() && !apiKey.IsQuotaExhausted(),
		APIKey:       apiKey.Key,
		APIKeyStatus: apiKey.Status,
	}
	if user != nil {
		result.UserID = user.ID
		result.UserEmail = user.Email
		result.UserStatus = user.Status
	}
	if apiKey.ExpiresAt != nil {
		formatted := apiKey.ExpiresAt.Format(time.RFC3339)
		result.ExpiresAt = &formatted
	}
	if !result.Valid {
		result.Reason = s.apiKeyInvalidReason(apiKey, user)
	}
	return result, nil
}

func (s *AugmentPluginService) ResolvePrincipalFromBearer(ctx context.Context, bearer string) (*AugmentPluginPrincipal, error) {
	token := strings.TrimSpace(bearer)
	if token == "" {
		return nil, ErrAugmentPluginAuthMissing
	}

	if s.auth != nil {
		if claims, err := s.auth.ValidateToken(token); err == nil && claims != nil {
			user, getErr := s.users.GetByID(ctx, claims.UserID)
			if getErr != nil {
				return nil, getErr
			}
			if !user.IsActive() {
				return nil, ErrUserNotActive
			}
			return &AugmentPluginPrincipal{
				Kind: augmentPrincipalKindJWT,
				User: user,
			}, nil
		}
	}

	if s.apiKeys == nil {
		return nil, ErrInvalidToken
	}

	apiKey, err := s.apiKeys.GetByKey(ctx, token)
	if err != nil {
		return nil, ErrInvalidToken
	}
	user := apiKey.User
	if user == nil {
		user, err = s.users.GetByID(ctx, apiKey.UserID)
		if err != nil {
			return nil, err
		}
	}
	if !user.IsActive() {
		return nil, ErrUserNotActive
	}
	if apiKey.Status != StatusActive || apiKey.IsExpired() || apiKey.IsQuotaExhausted() {
		return nil, ErrInvalidToken
	}
	apiKey.User = user
	return &AugmentPluginPrincipal{
		Kind:   augmentPrincipalKindAPIKey,
		User:   user,
		APIKey: apiKey,
	}, nil
}

func (s *AugmentPluginService) BuildSummary(ctx context.Context, principal AugmentPluginPrincipal) (*AugmentPluginSummary, error) {
	if principal.User == nil {
		return nil, ErrUserNotFound
	}
	primaryKey := ""
	gatewayKey := ""

	if principal.Kind == augmentPrincipalKindAPIKey {
		if principal.APIKey != nil && strings.TrimSpace(principal.APIKey.Key) != "" {
			primaryKey = principal.APIKey.Key
			gatewayKey = principal.APIKey.Key
		}
	} else {
		keys, err := s.listUserAPIKeys(ctx, principal.User.ID)
		if err != nil {
			return nil, err
		}

		primaryKey = selectPrimaryActiveAPIKey(keys)
		gatewayKey = primaryKey
		if primaryKey == "" {
			created, createErr := s.ensurePluginAPIKey(ctx, principal.User.ID)
			if createErr != nil {
				return nil, createErr
			}
			if created != nil && strings.TrimSpace(created.Key) != "" {
				primaryKey = created.Key
				gatewayKey = created.Key
			}
		}
	}

	plan, err := s.buildPlanSummary(ctx, principal.User.ID)
	if err != nil {
		return nil, err
	}

	return &AugmentPluginSummary{
		GatewayAPIKey: gatewayKey,
		PrimaryAPIKey: primaryKey,
		User: &AugmentPluginUserSummary{
			ID:          principal.User.ID,
			Email:       principal.User.Email,
			Username:    principal.User.Username,
			Role:        principal.User.Role,
			Status:      principal.User.Status,
			Balance:     principal.User.Balance,
			Concurrency: principal.User.Concurrency,
		},
		Plan: plan,
	}, nil
}

func (s *AugmentPluginService) BuildCompatMetadata(ctx context.Context, principal AugmentPluginPrincipal, gatewayBaseURL string) (*AugmentPluginCompatMetadata, error) {
	if principal.User == nil {
		return nil, ErrUserNotFound
	}

	settings, _ := s.publicSettings(ctx)
	groups, err := s.apiKeys.GetAvailableGroups(ctx, principal.User.ID)
	if err != nil {
		return nil, err
	}

	siteName := "Sub2API"
	backendModeEnabled := false
	if settings != nil {
		if strings.TrimSpace(settings.SiteName) != "" {
			siteName = strings.TrimSpace(settings.SiteName)
		}
		backendModeEnabled = settings.BackendModeEnabled
	}
	if siteName == "Sub2API" && s.settings != nil {
		siteName = s.settings.GetSiteName(ctx)
	}

	baseURL := strings.TrimSpace(gatewayBaseURL)
	if baseURL == "" && settings != nil && strings.TrimSpace(settings.APIBaseURL) != "" {
		baseURL = strings.TrimSpace(settings.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = s.fallbackGatewayBaseURL()
	}

	userLabel := strings.TrimSpace(principal.User.Email)
	if userLabel == "" {
		userLabel = strings.TrimSpace(principal.User.Username)
	}

	return &AugmentPluginCompatMetadata{
		GatewayBaseURL: baseURL,
		DefaultModel:   selectDefaultModel(groups, currentGroupForPrincipal(principal, groups)),
		ModelRegistry:  buildModelRegistry(groups),
		SessionDisplay: AugmentPluginSessionDisplay{
			SiteName:   siteName,
			UserLabel:  userLabel,
			AuthMethod: principal.Kind,
		},
		AccountState: AugmentPluginAccountState{
			UserStatus:         principal.User.Status,
			APIKeyStatus:       pluginAPIKeyStatus(principal.APIKey),
			BackendModeEnabled: backendModeEnabled,
		},
	}, nil
}

func (s *AugmentPluginService) IssueLegacyLoginToken(ctx context.Context, principal AugmentPluginPrincipal, tenantURL string) (*AugmentLegacyLoginToken, error) {
	if s.auth == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_PLUGIN_UNAVAILABLE", "augment plugin service is unavailable")
	}

	user := principal.User
	if user == nil {
		return nil, ErrUserNotFound
	}
	if !user.IsActive() {
		return nil, ErrUserNotActive
	}

	pair, err := s.auth.GenerateTokenPair(ctx, user, "")
	if err != nil {
		return nil, err
	}

	return &AugmentLegacyLoginToken{
		TenantURL:     tenantURL,
		AccessToken:   pair.AccessToken,
		SessionSource: AugmentSessionSourceLocalCompat,
	}, nil
}

func (s *AugmentPluginService) AdvanceLegacyCheckpoint(baseID string, added, deleted []string) (string, error) {
	return s.AdvanceLegacyCheckpointForNamespace(augmentLegacyDefaultNamespace, baseID, added, deleted)
}

func (s *AugmentPluginService) AdvanceLegacyCheckpointForNamespace(namespace, baseID string, added, deleted []string) (string, error) {
	nextID, err := randomHexToken(16)
	if err != nil {
		return "", fmt.Errorf("generate checkpoint id: %w", err)
	}

	baseID = strings.TrimSpace(baseID)
	added = dedupeStrings(added)
	deleted = dedupeStrings(deleted)

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.augmentLegacyNamespaceLocked(namespace)
	blobNames := make(map[string]struct{})
	if baseID != "" {
		if checkpointState, ok := state.Checkpoints[baseID]; ok {
			for name := range checkpointState.BlobNames {
				blobNames[name] = struct{}{}
			}
		}
	}

	for _, name := range added {
		blobNames[name] = struct{}{}
	}
	for _, name := range deleted {
		delete(blobNames, name)
	}

	state.Checkpoints[nextID] = augmentLegacyCheckpointState{BlobNames: blobNames}
	if err := s.persistLegacyCompatStateLocked(); err != nil {
		return "", err
	}
	return nextID, nil
}

func (s *AugmentPluginService) newSessionBundle(pair *TokenPair, tenantURL, sessionSource string) *AugmentSessionBundle {
	now := s.now()
	return &AugmentSessionBundle{
		AccessToken:   pair.AccessToken,
		RefreshToken:  pair.RefreshToken,
		ExpiresAt:     now.Add(time.Duration(pair.ExpiresIn) * time.Second).Format(time.RFC3339),
		TenantURL:     tenantURL,
		Scopes:        append([]string(nil), defaultAugmentPluginScopes...),
		SessionSource: sessionSource,
	}
}

func normalizeAugmentAbsoluteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return strings.TrimRight(parsed.String(), "/")
}

func normalizeAugmentQuickLoginMode(mode string) (string, error) {
	switch strings.TrimSpace(mode) {
	case "":
		return AugmentQuickLoginModeLocalCompat, nil
	case AugmentQuickLoginModeOfficialPassthrough:
		return AugmentQuickLoginModeOfficialPassthrough, nil
	case AugmentQuickLoginModeLocalCompat:
		return AugmentQuickLoginModeLocalCompat, nil
	default:
		return "", ErrAugmentPluginModeInvalid
	}
}

func normalizeOfficialAugmentSessionBundle(bundle *AugmentSessionBundle) (*AugmentSessionBundle, error) {
	if bundle == nil {
		return nil, ErrAugmentPluginOfficialAuth
	}

	normalized := &AugmentSessionBundle{
		AccessToken:   strings.TrimSpace(bundle.AccessToken),
		RefreshToken:  strings.TrimSpace(bundle.RefreshToken),
		ExpiresAt:     strings.TrimSpace(bundle.ExpiresAt),
		TenantURL:     strings.TrimSpace(bundle.TenantURL),
		Scopes:        dedupeStrings(bundle.Scopes),
		SessionSource: AugmentSessionSourceOfficial,
	}
	if normalized.AccessToken == "" || normalized.TenantURL == "" {
		return nil, ErrAugmentPluginOfficialAuth
	}
	if len(normalized.Scopes) == 0 {
		normalized.Scopes = append([]string(nil), defaultAugmentPluginScopes...)
	}
	return normalized, nil
}

func cloneAugmentSessionBundle(bundle *AugmentSessionBundle) *AugmentSessionBundle {
	if bundle == nil {
		return nil
	}
	cloned := *bundle
	cloned.Scopes = append([]string(nil), bundle.Scopes...)
	return &cloned
}

func (s *AugmentPluginService) pruneExpiredGrantsLocked(now time.Time) {
	for grant, record := range s.grants {
		if !record.ExpiresAt.After(now) {
			delete(s.grants, grant)
		}
	}
}

func (s *AugmentPluginService) buildPlanSummary(ctx context.Context, userID int64) (*AugmentPluginPlanSummary, error) {
	if s.subscriptions == nil {
		return nil, nil
	}
	subs, err := s.subscriptions.ListActiveUserSubscriptions(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return &AugmentPluginPlanSummary{}, nil
	}

	names := make([]string, 0, len(subs))
	var earliestExpiry *time.Time
	for i := range subs {
		if subs[i].Group != nil && strings.TrimSpace(subs[i].Group.Name) != "" {
			names = append(names, strings.TrimSpace(subs[i].Group.Name))
		}
		if subs[i].ExpiresAt.IsZero() {
			continue
		}
		if earliestExpiry == nil || subs[i].ExpiresAt.Before(*earliestExpiry) {
			expiry := subs[i].ExpiresAt
			earliestExpiry = &expiry
		}
	}
	sort.Strings(names)

	var expiresAt *string
	if earliestExpiry != nil {
		formatted := earliestExpiry.Format(time.RFC3339)
		expiresAt = &formatted
	}

	return &AugmentPluginPlanSummary{
		ActiveCount: len(subs),
		GroupNames:  dedupeStrings(names),
		ExpiresAt:   expiresAt,
	}, nil
}

func (s *AugmentPluginService) listUserAPIKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	if s.apiKeys == nil {
		return nil, nil
	}
	keys, _, err := s.apiKeys.List(ctx, userID, pagination.PaginationParams{
		Page:      1,
		PageSize:  100,
		SortBy:    "created_at",
		SortOrder: "asc",
	}, APIKeyListFilters{})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *AugmentPluginService) publicSettings(ctx context.Context) (*PublicSettings, error) {
	if s.settings == nil {
		return nil, nil
	}
	settings, err := s.settings.GetPublicSettings(ctx)
	if err != nil {
		return nil, err
	}
	return settings, nil
}

func (s *AugmentPluginService) fallbackGatewayBaseURL() string {
	if s.cfg == nil {
		return ""
	}
	return strings.TrimSpace(s.cfg.Server.FrontendURL)
}

func (s *AugmentPluginService) apiKeyInvalidReason(apiKey *APIKey, user *User) string {
	switch {
	case apiKey == nil:
		return "invalid_api_key"
	case user == nil:
		return "user_not_found"
	case !user.IsActive():
		return "user_inactive"
	case apiKey.Status == StatusAPIKeyDisabled:
		return "api_key_disabled"
	case apiKey.Status == StatusAPIKeyExpired || apiKey.IsExpired():
		return "api_key_expired"
	case apiKey.Status == StatusAPIKeyQuotaExhausted || apiKey.IsQuotaExhausted():
		return "api_key_quota_exhausted"
	default:
		return "api_key_inactive"
	}
}

func (s *AugmentPluginService) ensurePluginAPIKey(ctx context.Context, userID int64) (*APIKey, error) {
	if s.apiKeys == nil {
		return nil, nil
	}
	key, err := s.apiKeys.Create(ctx, userID, CreateAPIKeyRequest{
		Name: "Augment Plugin",
	})
	if err != nil {
		return nil, err
	}
	return key, nil
}

func selectPrimaryActiveAPIKey(keys []APIKey) string {
	if len(keys) == 0 {
		return ""
	}
	copied := append([]APIKey(nil), keys...)
	sort.SliceStable(copied, func(i, j int) bool {
		if copied[i].CreatedAt.Equal(copied[j].CreatedAt) {
			return copied[i].ID < copied[j].ID
		}
		return copied[i].CreatedAt.Before(copied[j].CreatedAt)
	})
	for i := range copied {
		if copied[i].Status == StatusActive && !copied[i].IsExpired() && !copied[i].IsQuotaExhausted() {
			return copied[i].Key
		}
	}
	return ""
}

func buildModelRegistry(groups []Group) AugmentPluginModelRegistry {
	out := AugmentPluginModelRegistry{
		Groups: make([]AugmentPluginRegistryGroup, 0, len(groups)),
	}
	if len(groups) == 0 {
		for _, model := range openai.DefaultModels {
			out.Groups = append(out.Groups, AugmentPluginRegistryGroup{
				ID:           0,
				Name:         "OpenAI",
				Platform:     PlatformOpenAI,
				DefaultModel: model.ID,
			})
		}
		return out
	}
	scopeSet := make(map[string]struct{})

	for i := range groups {
		group := groups[i]
		defaultModel := strings.TrimSpace(group.DefaultMappedModel)
		if defaultModel == "" {
			defaultModel = fallbackDefaultModelForGroup(group)
		}
		registryGroup := AugmentPluginRegistryGroup{
			ID:                   group.ID,
			Name:                 group.Name,
			Platform:             group.Platform,
			DefaultModel:         defaultModel,
			SupportedModelScopes: append([]string(nil), group.SupportedModelScopes...),
		}
		out.Groups = append(out.Groups, registryGroup)
		for _, scope := range group.SupportedModelScopes {
			scope = strings.TrimSpace(scope)
			if scope == "" {
				continue
			}
			scopeSet[scope] = struct{}{}
		}
	}

	out.SupportedModelScopes = make([]string, 0, len(scopeSet))
	for scope := range scopeSet {
		out.SupportedModelScopes = append(out.SupportedModelScopes, scope)
	}
	sort.Strings(out.SupportedModelScopes)
	sort.SliceStable(out.Groups, func(i, j int) bool {
		if out.Groups[i].Platform == out.Groups[j].Platform {
			return out.Groups[i].Name < out.Groups[j].Name
		}
		return out.Groups[i].Platform < out.Groups[j].Platform
	})
	return out
}

func currentGroupForPrincipal(principal AugmentPluginPrincipal, groups []Group) *Group {
	if principal.APIKey != nil {
		if principal.APIKey.Group != nil && principal.APIKey.Group.ID > 0 {
			return principal.APIKey.Group
		}
		if principal.APIKey.GroupID != nil {
			for i := range groups {
				if groups[i].ID == *principal.APIKey.GroupID {
					return &groups[i]
				}
			}
		}
	}
	return nil
}

func selectDefaultModel(groups []Group, currentGroup *Group) string {
	if currentGroup != nil {
		if model := strings.TrimSpace(currentGroup.DefaultMappedModel); model != "" {
			return model
		}
		if model := fallbackDefaultModelForGroup(*currentGroup); model != "" {
			return model
		}
	}
	for i := range groups {
		if model := strings.TrimSpace(groups[i].DefaultMappedModel); model != "" {
			return model
		}
	}
	for i := range groups {
		if model := fallbackDefaultModelForGroup(groups[i]); model != "" {
			return model
		}
	}
	return "gpt-5.4"
}

func fallbackDefaultModelForGroup(group Group) string {
	switch group.Platform {
	case PlatformOpenAI:
		if strings.TrimSpace(group.DefaultMappedModel) != "" {
			return strings.TrimSpace(group.DefaultMappedModel)
		}
		return "gpt-5.4"
	case PlatformAnthropic, PlatformAntigravity:
		if hasString(group.SupportedModelScopes, "gemini_text") && !hasString(group.SupportedModelScopes, "claude") {
			return "gemini-2.5-pro"
		}
		return "claude-sonnet-4-5"
	case PlatformGemini:
		return "gemini-2.5-pro"
	default:
		if hasString(group.SupportedModelScopes, "claude") {
			return "claude-sonnet-4-5"
		}
		if hasString(group.SupportedModelScopes, "gemini_text") {
			return "gemini-2.5-pro"
		}
	}
	return ""
}

func pluginAPIKeyStatus(apiKey *APIKey) string {
	if apiKey == nil {
		return ""
	}
	return apiKey.Status
}

func hasString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func randomHexToken(byteLen int) (string, error) {
	if byteLen <= 0 {
		byteLen = 16
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
