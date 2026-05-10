package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	augmentOfficialPoolCaptureTarget = "pool_session"

	AugmentOfficialPoolSessionStatusActive          = "active"
	AugmentOfficialPoolSessionStatusRevoked         = "revoked"
	AugmentOfficialPoolSessionStatusDisabled        = "disabled"
	AugmentOfficialPoolSessionStatusReloginRequired = "relogin_required"

	augmentOfficialPoolSessionDefaultHealthScore = 100
	augmentOfficialPoolSessionLeaseTTL           = 90 * time.Second
	augmentOfficialPoolSessionFailureCooldown    = 2 * time.Minute
)

var (
	ErrAugmentOfficialPoolSessionUnavailable = infraerrors.ServiceUnavailable(
		"AUGMENT_OFFICIAL_POOL_SESSION_UNAVAILABLE",
		"no available official pool session",
	)
	ErrAugmentOfficialPoolBindIntentInvalid = infraerrors.Unauthorized(
		"AUGMENT_OFFICIAL_POOL_BIND_INTENT_INVALID",
		"augment official pool bind intent is invalid",
	)
)

type AugmentOfficialPoolSessionStore interface {
	CreateBindIntent(ctx context.Context, input AugmentOfficialPoolBindIntentStoreCreateInput) (*AugmentOfficialPoolBindIntentStoreRecord, error)
	ConsumeBindIntent(ctx context.Context, bindIntentID string, adminUserID int64) (*AugmentOfficialPoolBindIntentStoreRecord, error)
	UpsertPoolSession(ctx context.Context, input AugmentOfficialPoolStoredSessionInput) (*AugmentOfficialPoolStoredAdminView, error)
	ListAdminSessions(ctx context.Context) ([]AugmentOfficialPoolStoredAdminView, error)
	GetAdminSession(ctx context.Context, sessionID int64) (*AugmentOfficialPoolStoredAdminView, error)
	AcquireUsableSession(ctx context.Context, source string, now, leaseUntil time.Time) (*AugmentOfficialPoolStoredCredentialRow, error)
	AcquireUsableSessionByID(ctx context.Context, sessionID int64, now, leaseUntil time.Time) (*AugmentOfficialPoolStoredCredentialRow, error)
	ReleaseLease(ctx context.Context, sessionID int64, input AugmentOfficialPoolLeaseReleaseInput) (*AugmentOfficialPoolStoredAdminView, error)
	RevokePoolSession(ctx context.Context, sessionID int64, status string, now time.Time) (*AugmentOfficialPoolStoredAdminView, error)
}

type AugmentOfficialPoolBindIntentStoreCreateInput struct {
	AdminUserID     int64
	StateHash       string
	Mode            string
	Source          string
	TenantAllowlist []string
}

type AugmentOfficialPoolBindIntentStoreRecord struct {
	ID              int64
	AdminUserID     int64
	BindIntentID    string
	StateHash       string
	Mode            string
	Source          string
	TenantAllowlist []string
	ExpiresAt       time.Time
	ConsumedAt      *time.Time
	CreatedAt       time.Time
}

type AugmentOfficialPoolStoredSessionInput struct {
	Source                     string
	TenantOrigin               string
	PortalOrigin               *string
	Scopes                     []string
	ExpiresAt                  *time.Time
	LastRefreshAt              *time.Time
	LastSuccessAt              *time.Time
	LastErrorAt                *time.Time
	LastErrorCode              *string
	Status                     string
	EncryptedCredentialPayload []byte
	CredentialSchemaVersion    int
	KeyVersion                 string
	Fingerprint                string
	CreatedByAdminID           int64
	HealthScore                int
}

type AugmentOfficialPoolStoredAdminView struct {
	ID                      int64
	Source                  string
	TenantOrigin            string
	PortalOrigin            *string
	Scopes                  []string
	ExpiresAt               *time.Time
	LastRefreshAt           *time.Time
	LastSuccessAt           *time.Time
	LastErrorAt             *time.Time
	LastErrorCode           *string
	Status                  string
	CredentialSchemaVersion int
	KeyVersion              string
	Fingerprint             string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	LastUsedAt              *time.Time
	CooldownUntil           *time.Time
	LeasedAt                *time.Time
	LeasedUntil             *time.Time
	HealthScore             int
	CreatedByAdminID        int64
	HasCredentialPayload    bool
}

type AugmentOfficialPoolStoredCredentialRow struct {
	ID                         int64
	Source                     string
	TenantOrigin               string
	PortalOrigin               *string
	Scopes                     []string
	ExpiresAt                  *time.Time
	Status                     string
	EncryptedCredentialPayload []byte
	CredentialSchemaVersion    int
	KeyVersion                 string
	Fingerprint                string
	HealthScore                int
}

type AugmentOfficialPoolLeaseReleaseInput struct {
	Now           time.Time
	Success       bool
	ErrorCode     string
	CooldownUntil *time.Time
}

type AugmentOfficialPoolBindIntentRequest struct {
	Mode            string
	Source          string
	TenantAllowlist []string
}

type AugmentOfficialPoolBindIntentResponse struct {
	BindIntentID string    `json:"bind_intent_id"`
	State        string    `json:"state"`
	ExpiresAt    time.Time `json:"expires_at"`
	BindToken    string    `json:"bind_token"`
}

type AugmentOfficialPoolBindRequest struct {
	BindIntentID string         `json:"bind_intent_id"`
	State        string         `json:"state"`
	Mode         string         `json:"mode"`
	Source       string         `json:"source"`
	Payload      map[string]any `json:"payload"`
	RequestID    string         `json:"request_id,omitempty"`
}

type AugmentOfficialPoolSessionAdminView struct {
	ID                   int64      `json:"id"`
	Source               string     `json:"source"`
	TenantOrigin         string     `json:"tenant_origin"`
	PortalOrigin         *string    `json:"portal_origin,omitempty"`
	Scopes               []string   `json:"scopes"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	LastRefreshAt        *time.Time `json:"last_refresh_at,omitempty"`
	LastSuccessAt        *time.Time `json:"last_success_at,omitempty"`
	LastErrorAt          *time.Time `json:"last_error_at,omitempty"`
	LastErrorCode        *string    `json:"last_error_code,omitempty"`
	Status               string     `json:"status"`
	FingerprintPrefix    string     `json:"fingerprint_prefix"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	LastUsedAt           *time.Time `json:"last_used_at,omitempty"`
	CooldownUntil        *time.Time `json:"cooldown_until,omitempty"`
	LeasedAt             *time.Time `json:"leased_at,omitempty"`
	LeasedUntil          *time.Time `json:"leased_until,omitempty"`
	HealthScore          int        `json:"health_score"`
	CreatedByAdminID     int64      `json:"created_by_admin_id"`
	HasCredentialPayload bool       `json:"has_credential_payload"`
}

type AugmentOfficialPoolSessionDiagnostics struct {
	ID                   int64      `json:"id"`
	Source               string     `json:"source"`
	TenantHost           string     `json:"tenant_host"`
	Status               string     `json:"status"`
	FingerprintPrefix    string     `json:"fingerprint_prefix"`
	LastRefreshAt        *time.Time `json:"last_refresh_at,omitempty"`
	LastSuccessAt        *time.Time `json:"last_success_at,omitempty"`
	LastErrorAt          *time.Time `json:"last_error_at,omitempty"`
	LastErrorCode        *string    `json:"last_error_code,omitempty"`
	LastUsedAt           *time.Time `json:"last_used_at,omitempty"`
	CooldownUntil        *time.Time `json:"cooldown_until,omitempty"`
	LeasedAt             *time.Time `json:"leased_at,omitempty"`
	LeasedUntil          *time.Time `json:"leased_until,omitempty"`
	HealthScore          int        `json:"health_score"`
	HasCredentialPayload bool       `json:"has_credential_payload"`
}

type AugmentOfficialPoolRouteSessionLookupInput struct {
	PresentedSource       string
	PresentedTenantOrigin string
	PresentedFingerprint  string
}

type AugmentOfficialPoolSessionLease struct {
	SessionID int64
	Bundle    *AugmentSessionBundle

	service *AugmentOfficialPoolSessionService
}

type augmentOfficialPoolSourcePriorityProvider interface {
	GetSourcePriority(ctx context.Context) ([]string, error)
}

func (l *AugmentOfficialPoolSessionLease) Release(ctx context.Context, success bool, errorCode string) error {
	if l == nil || l.service == nil || l.SessionID <= 0 {
		return nil
	}
	return l.service.releaseLease(ctx, l.SessionID, success, errorCode)
}

type AugmentOfficialPoolSessionService struct {
	store                  AugmentOfficialPoolSessionStore
	cipher                 AugmentOfficialSessionCipher
	bindTokenSecret        []byte
	now                    func() time.Time
	sourcePriorityProvider augmentOfficialPoolSourcePriorityProvider
	localCursorReader      augmentLocalCursorSessionReader
}

func NewAugmentOfficialPoolSessionService(store AugmentOfficialPoolSessionStore, cipher AugmentOfficialSessionCipher, bindTokenSecret string) *AugmentOfficialPoolSessionService {
	return &AugmentOfficialPoolSessionService{
		store:           store,
		cipher:          cipher,
		bindTokenSecret: []byte(strings.TrimSpace(bindTokenSecret)),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *AugmentOfficialPoolSessionService) SetSourcePriorityProvider(provider augmentOfficialPoolSourcePriorityProvider) {
	s.sourcePriorityProvider = provider
}

func (s *AugmentOfficialPoolSessionService) CreateBindIntent(ctx context.Context, adminUserID int64, input AugmentOfficialPoolBindIntentRequest) (*AugmentOfficialPoolBindIntentResponse, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_POOL_UNAVAILABLE", "augment official pool service is unavailable")
	}
	mode, err := normalizeAugmentOfficialSessionMode(input.Mode)
	if err != nil {
		return nil, err
	}
	source := strings.TrimSpace(input.Source)
	if err := ValidateAugmentOfficialSource(source, AugmentOfficialSourcePolicy{}); err != nil {
		return nil, err
	}
	tenantAllowlist, err := normalizeTenantAllowlist(input.TenantAllowlist)
	if err != nil {
		return nil, err
	}

	state, err := augmentOfficialRandomHexToken(16)
	if err != nil {
		return nil, fmt.Errorf("generate augment official pool bind state: %w", err)
	}
	stateHash := sha256HexString(state)
	record, err := s.store.CreateBindIntent(ctx, AugmentOfficialPoolBindIntentStoreCreateInput{
		AdminUserID:     adminUserID,
		StateHash:       stateHash,
		Mode:            mode,
		Source:          source,
		TenantAllowlist: tenantAllowlist,
	})
	if err != nil {
		return nil, err
	}

	bindToken, err := s.signBindToken(augmentOfficialBindTokenClaims{
		BindIntentID: record.BindIntentID,
		UserID:       adminUserID,
		ExpiresAt:    record.ExpiresAt.UTC().Unix(),
		StateHash:    stateHash,
		Mode:         mode,
		Source:       source,
	})
	if err != nil {
		return nil, err
	}
	return &AugmentOfficialPoolBindIntentResponse{
		BindIntentID: record.BindIntentID,
		State:        state,
		ExpiresAt:    record.ExpiresAt.UTC(),
		BindToken:    bindToken,
	}, nil
}

func (s *AugmentOfficialPoolSessionService) BindSession(ctx context.Context, adminUserID int64, bindToken string, input AugmentOfficialPoolBindRequest) (*AugmentOfficialPoolSessionAdminView, error) {
	if s == nil || s.store == nil || s.cipher == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_POOL_UNAVAILABLE", "augment official pool service is unavailable")
	}
	claims, err := s.parseBindToken(bindToken)
	if err != nil {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	if claims.UserID != adminUserID || claims.BindIntentID != strings.TrimSpace(input.BindIntentID) || claims.StateHash != sha256HexString(strings.TrimSpace(input.State)) {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	mode, err := normalizeAugmentOfficialSessionMode(input.Mode)
	if err != nil {
		return nil, err
	}
	source := strings.TrimSpace(input.Source)
	if claims.Mode != mode || claims.Source != source {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	normalizedPayload, err := normalizeAugmentOfficialCredentialPayload(input.Payload, s.now())
	if err != nil {
		return nil, err
	}
	intentRecord, err := s.store.ConsumeBindIntent(ctx, claims.BindIntentID, adminUserID)
	if err != nil {
		return nil, err
	}
	if intentRecord.Mode != mode || intentRecord.Source != source || !originAllowed(intentRecord.TenantAllowlist, normalizedPayload.TenantOrigin) {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	secretPayload := augmentOfficialEncryptedCredentialPayload{
		AccessToken:       normalizedPayload.AccessToken,
		RefreshToken:      normalizedPayload.RefreshToken,
		OfficialSessionID: normalizedPayload.OfficialSessionID,
	}
	secretData, err := json.Marshal(secretPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal augment official pool credential payload: %w", err)
	}
	encryptedPayload, err := s.cipher.Encrypt(secretData)
	if err != nil {
		return nil, err
	}
	keyVersion, err := encryptedPayloadKeyID(encryptedPayload)
	if err != nil {
		return nil, err
	}
	fingerprint := buildAugmentOfficialFingerprint(source, normalizedPayload.TenantOrigin, normalizedPayload.AccessToken, normalizedPayload.RefreshToken, normalizedPayload.OfficialSessionID)
	now := s.now()
	stored, err := s.store.UpsertPoolSession(ctx, AugmentOfficialPoolStoredSessionInput{
		Source:                     source,
		TenantOrigin:               normalizedPayload.TenantOrigin,
		PortalOrigin:               normalizedPayload.PortalOrigin,
		Scopes:                     normalizedPayload.Scopes,
		ExpiresAt:                  ptrTimeValue(normalizedPayload.ExpiresAt),
		LastSuccessAt:              ptrTimeValue(now),
		Status:                     AugmentOfficialPoolSessionStatusActive,
		EncryptedCredentialPayload: encryptedPayload,
		CredentialSchemaVersion:    normalizedPayload.CredentialSchemaVersion,
		KeyVersion:                 keyVersion,
		Fingerprint:                fingerprint,
		CreatedByAdminID:           adminUserID,
		HealthScore:                augmentOfficialPoolSessionDefaultHealthScore,
	})
	if err != nil {
		return nil, err
	}
	view := storedPoolAdminViewToAdminView(stored)
	return &view, nil
}

func (s *AugmentOfficialPoolSessionService) ListAdminSessions(ctx context.Context) ([]AugmentOfficialPoolSessionAdminView, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_POOL_UNAVAILABLE", "augment official pool service is unavailable")
	}
	stored, err := s.store.ListAdminSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AugmentOfficialPoolSessionAdminView, 0, len(stored))
	for i := range stored {
		out = append(out, storedPoolAdminViewToAdminView(&stored[i]))
	}
	return out, nil
}

func (s *AugmentOfficialPoolSessionService) GetAdminSessionDiagnostics(ctx context.Context, sessionID int64) (*AugmentOfficialPoolSessionDiagnostics, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_POOL_UNAVAILABLE", "augment official pool service is unavailable")
	}
	stored, err := s.store.GetAdminSession(ctx, sessionID)
	if err != nil || stored == nil {
		return nil, err
	}
	return &AugmentOfficialPoolSessionDiagnostics{
		ID:                   stored.ID,
		Source:               stored.Source,
		TenantHost:           hostFromOrigin(stored.TenantOrigin),
		Status:               stored.Status,
		FingerprintPrefix:    fingerprintPrefix(stored.Fingerprint),
		LastRefreshAt:        cloneAdminTime(stored.LastRefreshAt),
		LastSuccessAt:        cloneAdminTime(stored.LastSuccessAt),
		LastErrorAt:          cloneAdminTime(stored.LastErrorAt),
		LastErrorCode:        cloneAdminString(stored.LastErrorCode),
		LastUsedAt:           cloneAdminTime(stored.LastUsedAt),
		CooldownUntil:        cloneAdminTime(stored.CooldownUntil),
		LeasedAt:             cloneAdminTime(stored.LeasedAt),
		LeasedUntil:          cloneAdminTime(stored.LeasedUntil),
		HealthScore:          stored.HealthScore,
		HasCredentialPayload: stored.HasCredentialPayload,
	}, nil
}

func (s *AugmentOfficialPoolSessionService) ResolveRouteSession(ctx context.Context, input AugmentOfficialPoolRouteSessionLookupInput) (*AugmentOfficialPoolSessionAdminView, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_POOL_UNAVAILABLE", "augment official pool service is unavailable")
	}
	sessions, err := s.ListAdminSessions(ctx)
	if err != nil {
		return nil, err
	}

	sourcePriority := []string(nil)
	if s.sourcePriorityProvider != nil {
		if loaded, err := s.sourcePriorityProvider.GetSourcePriority(ctx); err == nil {
			sourcePriority = loaded
		}
	}
	sourceOrder := normalizePoolSourcePriority(sourcePriority)
	sourceRank := make(map[string]int, len(sourceOrder))
	for index, source := range sourceOrder {
		sourceRank[source] = index
	}

	now := s.now()
	var best *AugmentOfficialPoolSessionAdminView
	for i := range sessions {
		session := sessions[i]
		if !augmentOfficialPoolSessionUsableForRoutePolicy(session, input, now) {
			continue
		}
		if best == nil || augmentOfficialPoolSessionPreferred(session, *best, sourceRank) {
			copy := session
			best = &copy
		}
	}
	return best, nil
}

func (s *AugmentOfficialPoolSessionService) RevokeSessionForAdmin(ctx context.Context, sessionID int64) (*AugmentOfficialPoolSessionAdminView, error) {
	return s.updateSessionStatus(ctx, sessionID, AugmentOfficialPoolSessionStatusRevoked)
}

func (s *AugmentOfficialPoolSessionService) DisableSessionForAdmin(ctx context.Context, sessionID int64) (*AugmentOfficialPoolSessionAdminView, error) {
	return s.updateSessionStatus(ctx, sessionID, AugmentOfficialPoolSessionStatusDisabled)
}

func (s *AugmentOfficialPoolSessionService) RequireSessionReloginForAdmin(ctx context.Context, sessionID int64) (*AugmentOfficialPoolSessionAdminView, error) {
	return s.updateSessionStatus(ctx, sessionID, AugmentOfficialPoolSessionStatusReloginRequired)
}

func (s *AugmentOfficialPoolSessionService) updateSessionStatus(ctx context.Context, sessionID int64, status string) (*AugmentOfficialPoolSessionAdminView, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_POOL_UNAVAILABLE", "augment official pool service is unavailable")
	}
	stored, err := s.store.RevokePoolSession(ctx, sessionID, status, s.now())
	if err != nil || stored == nil {
		return nil, err
	}
	view := storedPoolAdminViewToAdminView(stored)
	return &view, nil
}

func (s *AugmentOfficialPoolSessionService) AcquireSessionBundle(ctx context.Context, sourcePriority []string) (*AugmentOfficialPoolSessionLease, error) {
	if s == nil || s.store == nil || s.cipher == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_POOL_UNAVAILABLE", "augment official pool service is unavailable")
	}
	now := s.now()
	leaseUntil := now.Add(augmentOfficialPoolSessionLeaseTTL)
	if len(sourcePriority) == 0 && s.sourcePriorityProvider != nil {
		if loaded, err := s.sourcePriorityProvider.GetSourcePriority(ctx); err == nil {
			sourcePriority = loaded
		}
	}
	for _, source := range normalizePoolSourcePriority(sourcePriority) {
		row, err := s.store.AcquireUsableSession(ctx, source, now, leaseUntil)
		if err != nil {
			return nil, err
		}
		if row == nil {
			continue
		}
		lease, err := s.leaseFromStoredCredentialRow(ctx, row)
		if err == nil && lease != nil {
			return lease, nil
		}
	}
	return nil, ErrAugmentOfficialPoolSessionUnavailable
}

func (s *AugmentOfficialPoolSessionService) AcquireSessionBundleByID(ctx context.Context, sessionID int64) (*AugmentOfficialPoolSessionLease, error) {
	if s == nil || s.store == nil || s.cipher == nil || sessionID <= 0 {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_OFFICIAL_POOL_UNAVAILABLE", "augment official pool service is unavailable")
	}
	now := s.now()
	leaseUntil := now.Add(augmentOfficialPoolSessionLeaseTTL)
	row, err := s.store.AcquireUsableSessionByID(ctx, sessionID, now, leaseUntil)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrAugmentOfficialPoolSessionUnavailable
	}
	lease, err := s.leaseFromStoredCredentialRow(ctx, row)
	if err != nil || lease == nil {
		return nil, ErrAugmentOfficialPoolSessionUnavailable
	}
	return lease, nil
}

func (s *AugmentOfficialPoolSessionService) leaseFromStoredCredentialRow(ctx context.Context, row *AugmentOfficialPoolStoredCredentialRow) (*AugmentOfficialPoolSessionLease, error) {
	if row == nil {
		return nil, ErrAugmentOfficialPoolSessionUnavailable
	}
	plaintext, err := s.cipher.Decrypt(row.EncryptedCredentialPayload)
	if err != nil {
		_ = s.releaseLease(ctx, row.ID, false, "decrypt_failed")
		return nil, err
	}
	var payload augmentOfficialEncryptedCredentialPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		_ = s.releaseLease(ctx, row.ID, false, "payload_invalid")
		return nil, err
	}
	bundle := &AugmentSessionBundle{
		AccessToken:   payload.AccessToken,
		RefreshToken:  payload.RefreshToken,
		TenantURL:     row.TenantOrigin,
		Scopes:        append([]string(nil), row.Scopes...),
		SessionSource: AugmentSessionSourceOfficial,
	}
	if row.PortalOrigin != nil {
		bundle.PortalURL = *row.PortalOrigin
	}
	if row.ExpiresAt != nil {
		bundle.ExpiresAt = row.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if _, err := normalizeOfficialAugmentSessionBundle(bundle); err != nil {
		_ = s.releaseLease(ctx, row.ID, false, "bundle_invalid")
		return nil, err
	}
	return &AugmentOfficialPoolSessionLease{
		SessionID: row.ID,
		Bundle:    bundle,
		service:   s,
	}, nil
}

func (s *AugmentOfficialPoolSessionService) releaseLease(ctx context.Context, sessionID int64, success bool, errorCode string) error {
	now := s.now()
	var cooldownUntil *time.Time
	if !success {
		cooldown := now.Add(augmentOfficialPoolSessionFailureCooldown)
		cooldownUntil = &cooldown
	}
	_, err := s.store.ReleaseLease(ctx, sessionID, AugmentOfficialPoolLeaseReleaseInput{
		Now:           now,
		Success:       success,
		ErrorCode:     strings.TrimSpace(errorCode),
		CooldownUntil: cooldownUntil,
	})
	return err
}

func (s *AugmentOfficialPoolSessionService) signBindToken(claims augmentOfficialBindTokenClaims) (string, error) {
	if len(s.bindTokenSecret) == 0 {
		return "", infraerrors.InternalServer("AUGMENT_OFFICIAL_POOL_BIND_TOKEN_CONFIG_INVALID", "augment official pool bind token secret is not configured")
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal augment official pool bind token: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.bindTokenSecret)
	_, _ = mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedPayload + "." + signature, nil
}

func (s *AugmentOfficialPoolSessionService) parseBindToken(token string) (*augmentOfficialBindTokenClaims, error) {
	if len(s.bindTokenSecret) == 0 {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	payload, signature, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payload == "" || signature == "" {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	mac := hmac.New(sha256.New, s.bindTokenSecret)
	_, _ = mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	var claims augmentOfficialBindTokenClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	if claims.UserID <= 0 || claims.BindIntentID == "" || claims.ExpiresAt <= 0 {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	if time.Unix(claims.ExpiresAt, 0).UTC().Before(s.now()) {
		return nil, ErrAugmentOfficialPoolBindIntentInvalid
	}
	return &claims, nil
}

func storedPoolAdminViewToAdminView(stored *AugmentOfficialPoolStoredAdminView) AugmentOfficialPoolSessionAdminView {
	return AugmentOfficialPoolSessionAdminView{
		ID:                   stored.ID,
		Source:               stored.Source,
		TenantOrigin:         stored.TenantOrigin,
		PortalOrigin:         cloneAdminString(stored.PortalOrigin),
		Scopes:               append([]string(nil), stored.Scopes...),
		ExpiresAt:            cloneAdminTime(stored.ExpiresAt),
		LastRefreshAt:        cloneAdminTime(stored.LastRefreshAt),
		LastSuccessAt:        cloneAdminTime(stored.LastSuccessAt),
		LastErrorAt:          cloneAdminTime(stored.LastErrorAt),
		LastErrorCode:        cloneAdminString(stored.LastErrorCode),
		Status:               stored.Status,
		FingerprintPrefix:    fingerprintPrefix(stored.Fingerprint),
		CreatedAt:            stored.CreatedAt.UTC(),
		UpdatedAt:            stored.UpdatedAt.UTC(),
		LastUsedAt:           cloneAdminTime(stored.LastUsedAt),
		CooldownUntil:        cloneAdminTime(stored.CooldownUntil),
		LeasedAt:             cloneAdminTime(stored.LeasedAt),
		LeasedUntil:          cloneAdminTime(stored.LeasedUntil),
		HealthScore:          stored.HealthScore,
		CreatedByAdminID:     stored.CreatedByAdminID,
		HasCredentialPayload: stored.HasCredentialPayload,
	}
}

func normalizePoolSourcePriority(sourcePriority []string) []string {
	if len(sourcePriority) == 0 {
		return []string{augmentOfficialSessionSourceOfficialQuickLogin, augmentOfficialSessionSourceWukongQuickLogin}
	}
	out := make([]string, 0, len(sourcePriority))
	seen := map[string]struct{}{}
	for _, source := range sourcePriority {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if err := ValidateAugmentOfficialSource(source, AugmentOfficialSourcePolicy{}); err != nil {
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		out = append(out, source)
	}
	if len(out) == 0 {
		return []string{augmentOfficialSessionSourceOfficialQuickLogin, augmentOfficialSessionSourceWukongQuickLogin}
	}
	return out
}

func augmentOfficialPoolSessionUsableForRoutePolicy(
	session AugmentOfficialPoolSessionAdminView,
	input AugmentOfficialPoolRouteSessionLookupInput,
	now time.Time,
) bool {
	if session.Status != AugmentOfficialPoolSessionStatusActive || !session.HasCredentialPayload {
		return false
	}
	if session.ExpiresAt != nil && !session.ExpiresAt.After(now) {
		return false
	}
	if session.CooldownUntil != nil && session.CooldownUntil.After(now) {
		return false
	}
	if presented := strings.TrimSpace(input.PresentedSource); presented != "" && !strings.EqualFold(presented, session.Source) {
		return false
	}
	if presented := strings.TrimSpace(input.PresentedTenantOrigin); presented != "" && presented != strings.TrimSpace(session.TenantOrigin) {
		return false
	}
	if presented := strings.TrimSpace(input.PresentedFingerprint); presented != "" {
		prefix := strings.TrimSpace(session.FingerprintPrefix)
		if prefix == "" || !strings.HasPrefix(presented, prefix) {
			return false
		}
	}
	return true
}

func augmentOfficialPoolSessionPreferred(
	candidate AugmentOfficialPoolSessionAdminView,
	current AugmentOfficialPoolSessionAdminView,
	sourceRank map[string]int,
) bool {
	candidateRank := augmentOfficialPoolSourceRank(candidate.Source, sourceRank)
	currentRank := augmentOfficialPoolSourceRank(current.Source, sourceRank)
	if candidateRank != currentRank {
		return candidateRank < currentRank
	}
	if candidate.HealthScore != current.HealthScore {
		return candidate.HealthScore > current.HealthScore
	}
	return augmentOfficialPoolSessionSortTime(candidate).After(augmentOfficialPoolSessionSortTime(current))
}

func augmentOfficialPoolSourceRank(source string, sourceRank map[string]int) int {
	if rank, ok := sourceRank[strings.TrimSpace(source)]; ok {
		return rank
	}
	return len(sourceRank) + 1
}

func augmentOfficialPoolSessionSortTime(session AugmentOfficialPoolSessionAdminView) time.Time {
	if session.LastSuccessAt != nil {
		return session.LastSuccessAt.UTC()
	}
	return session.CreatedAt.UTC()
}
