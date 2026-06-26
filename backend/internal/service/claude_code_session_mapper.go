package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	claudeCodeServerSessionScope = "claude_code_server_session"
	claudeCodeSessionBudgetScope = "session_budget_session"

	claudeCodeSessionBoundaryLedgerFileEnv = "SUB2API_CLAUDE_CODE_SESSION_BOUNDARY_LEDGER_FILE"
)

type claudeCodeSessionUserScopeContextKey struct{}

type ClaudeCodeSessionMapper struct {
	keyID   string
	version int
	secret  []byte
}

type ClaudeCodeSessionMapInput struct {
	UserScope              string
	BoundaryScope          string
	EnforceBoundary        bool
	FormalPoolProduction   bool
	AccountRef             string
	CredentialRef          string
	DeviceID               string
	AccountUUID            string
	EgressBucket           string
	ProxyIdentityRef       string
	PolicyVersion          string
	PersonaProfile         string
	EgressProfileRef       string
	ProfilePolicyVersion   string
	BillingShapePolicy     string
	RequestShapeProfileRef string
	CacheParityProfileRef  string
	ProviderFamily         string
	RawSessionID           string
}

type ClaudeCodeSessionMapping struct {
	SessionID  string                     `json:"session_id"`
	SessionRef *ControlPlaneScopedHMACRef `json:"session_ref,omitempty"`
}

type ClaudeCodeSessionBoundaryError struct {
	Code                            string `json:"code"`
	PreviousAccountRef              string `json:"previous_account_ref,omitempty"`
	AttemptedAccountRef             string `json:"attempted_account_ref,omitempty"`
	PreviousCredentialRef           string `json:"previous_credential_ref,omitempty"`
	AttemptedCredentialRef          string `json:"attempted_credential_ref,omitempty"`
	PreviousEgress                  string `json:"previous_egress,omitempty"`
	AttemptedEgress                 string `json:"attempted_egress,omitempty"`
	PreviousProxyIdentityRef        string `json:"previous_proxy_identity_ref,omitempty"`
	AttemptedProxyIdentityRef       string `json:"attempted_proxy_identity_ref,omitempty"`
	PreviousPolicyVersion           string `json:"previous_policy_version,omitempty"`
	AttemptedPolicyVersion          string `json:"attempted_policy_version,omitempty"`
	PreviousPersonaProfile          string `json:"previous_persona_profile,omitempty"`
	AttemptedPersonaProfile         string `json:"attempted_persona_profile,omitempty"`
	PreviousEgressProfileRef        string `json:"previous_egress_profile_ref,omitempty"`
	AttemptedEgressProfileRef       string `json:"attempted_egress_profile_ref,omitempty"`
	PreviousProfilePolicyVersion    string `json:"previous_profile_policy_version,omitempty"`
	AttemptedProfilePolicyVersion   string `json:"attempted_profile_policy_version,omitempty"`
	PreviousBillingShapePolicy      string `json:"previous_billing_shape_policy,omitempty"`
	AttemptedBillingShapePolicy     string `json:"attempted_billing_shape_policy,omitempty"`
	PreviousRequestShapeProfileRef  string `json:"previous_request_shape_profile_ref,omitempty"`
	AttemptedRequestShapeProfileRef string `json:"attempted_request_shape_profile_ref,omitempty"`
	PreviousCacheParityProfileRef   string `json:"previous_cache_parity_profile_ref,omitempty"`
	AttemptedCacheParityProfileRef  string `json:"attempted_cache_parity_profile_ref,omitempty"`
	PreviousProviderFamily          string `json:"previous_provider_family,omitempty"`
	AttemptedProviderFamily         string `json:"attempted_provider_family,omitempty"`
	PreviousServerSessionRef        string `json:"previous_server_session_ref,omitempty"`
	AttemptedServerSessionRef       string `json:"attempted_server_session_ref,omitempty"`
}

func (e *ClaudeCodeSessionBoundaryError) Error() string {
	if e == nil {
		return "claude native session boundary failed"
	}
	return e.Code
}

type claudeCodeSessionBoundaryBinding struct {
	AccountRef             string `json:"account_ref"`
	CredentialRef          string `json:"credential_ref,omitempty"`
	EgressBucket           string `json:"egress_bucket"`
	ProxyIdentityRef       string `json:"proxy_identity_ref,omitempty"`
	PolicyVersion          string `json:"policy_version,omitempty"`
	PersonaProfile         string `json:"persona_profile,omitempty"`
	EgressProfileRef       string `json:"egress_profile_ref,omitempty"`
	ProfilePolicyVersion   string `json:"profile_policy_version,omitempty"`
	BillingShapePolicy     string `json:"billing_shape_policy,omitempty"`
	RequestShapeProfileRef string `json:"request_shape_profile_ref,omitempty"`
	CacheParityProfileRef  string `json:"cache_parity_profile_ref,omitempty"`
	ProviderFamily         string `json:"provider_family"`
	DeviceRef              string `json:"device_ref,omitempty"`
	ServerSessionRef       string `json:"server_session_ref"`
}

var claudeCodeSessionBoundaryLedger sync.Map

type claudeCodeSessionBoundaryLedgerFile struct {
	Version int                                         `json:"version"`
	Entries map[string]claudeCodeSessionBoundaryBinding `json:"entries"`
}

func resetClaudeCodeSessionBoundaryLedgerForTest() {
	claudeCodeSessionBoundaryLedger = sync.Map{}
}

func WithClaudeCodeSessionUserScope(ctx context.Context, userScope string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, claudeCodeSessionUserScopeContextKey{}, strings.TrimSpace(userScope))
}

func ClaudeCodeSessionUserScopeFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	scope, ok := ctx.Value(claudeCodeSessionUserScopeContextKey{}).(string)
	scope = strings.TrimSpace(scope)
	return scope, ok && scope != ""
}

func NewClaudeCodeSessionMapperFromEnv() *ClaudeCodeSessionMapper {
	keyID := strings.TrimSpace(os.Getenv("SUB2API_SESSION_BUDGET_HMAC_KEY_ID"))
	if keyID == "" {
		keyID = "session_budget_v1"
	}
	version := 1
	if rawVersion := strings.TrimSpace(os.Getenv("SUB2API_SESSION_BUDGET_HMAC_VERSION")); rawVersion != "" {
		if parsed, err := strconv.Atoi(rawVersion); err == nil && parsed > 0 {
			version = parsed
		}
	}
	secret := strings.TrimSpace(os.Getenv("SUB2API_SESSION_BUDGET_HMAC_KEY"))
	if secret == "" {
		secret = "sub2api-session-budget-dev-key"
	}
	return &ClaudeCodeSessionMapper{
		keyID:   keyID,
		version: version,
		secret:  []byte(secret),
	}
}

func (m *ClaudeCodeSessionMapper) Map(input ClaudeCodeSessionMapInput) (*ClaudeCodeSessionMapping, error) {
	if m == nil {
		m = NewClaudeCodeSessionMapperFromEnv()
	}
	material, err := claudeCodeSessionMapMaterial(input)
	if err != nil {
		return nil, err
	}
	sessionID := claudeCodeUUIDLikeFromDigest(m.scopedHMACSum(claudeCodeServerSessionScope, material))
	mapping := &ClaudeCodeSessionMapping{
		SessionID: sessionID,
		SessionRef: &ControlPlaneScopedHMACRef{
			KeyID:   m.keyID,
			Scope:   claudeCodeSessionBudgetScope,
			Version: m.version,
			Value:   "hmac-sha256:" + hex.EncodeToString(m.scopedHMACSum(claudeCodeSessionBudgetScope, material)),
		},
	}
	if input.EnforceBoundary {
		if err := m.enforceBoundary(input, mapping); err != nil {
			return nil, err
		}
	}
	return mapping, nil
}

func claudeCodeSessionMapMaterial(input ClaudeCodeSessionMapInput) ([]byte, error) {
	rawSessionID := strings.TrimSpace(input.RawSessionID)
	if rawSessionID == "" {
		return nil, fmt.Errorf("claude code session mapper requires raw session id")
	}
	payload := struct {
		UserScope              string `json:"user_scope"`
		AccountRef             string `json:"account_ref,omitempty"`
		CredentialRef          string `json:"credential_ref,omitempty"`
		DeviceID               string `json:"device_id,omitempty"`
		AccountUUID            string `json:"account_uuid,omitempty"`
		EgressBucket           string `json:"egress_bucket,omitempty"`
		ProxyIdentityRef       string `json:"proxy_identity_ref,omitempty"`
		PolicyVersion          string `json:"policy_version,omitempty"`
		PersonaProfile         string `json:"persona_profile,omitempty"`
		EgressProfileRef       string `json:"egress_profile_ref,omitempty"`
		ProfilePolicyVersion   string `json:"profile_policy_version,omitempty"`
		BillingShapePolicy     string `json:"billing_shape_policy,omitempty"`
		RequestShapeProfileRef string `json:"request_shape_profile_ref,omitempty"`
		CacheParityProfileRef  string `json:"cache_parity_profile_ref,omitempty"`
		ProviderFamily         string `json:"provider_family,omitempty"`
		RawSessionID           string `json:"raw_session_id"`
	}{
		UserScope:              strings.TrimSpace(input.UserScope),
		AccountRef:             strings.TrimSpace(input.AccountRef),
		CredentialRef:          strings.TrimSpace(input.CredentialRef),
		DeviceID:               strings.TrimSpace(input.DeviceID),
		AccountUUID:            strings.TrimSpace(input.AccountUUID),
		EgressBucket:           strings.TrimSpace(input.EgressBucket),
		ProxyIdentityRef:       strings.TrimSpace(input.ProxyIdentityRef),
		PolicyVersion:          strings.TrimSpace(input.PolicyVersion),
		PersonaProfile:         strings.TrimSpace(input.PersonaProfile),
		EgressProfileRef:       strings.TrimSpace(input.EgressProfileRef),
		ProfilePolicyVersion:   strings.TrimSpace(input.ProfilePolicyVersion),
		BillingShapePolicy:     strings.TrimSpace(input.BillingShapePolicy),
		RequestShapeProfileRef: strings.TrimSpace(input.RequestShapeProfileRef),
		CacheParityProfileRef:  strings.TrimSpace(input.CacheParityProfileRef),
		ProviderFamily:         strings.TrimSpace(input.ProviderFamily),
		RawSessionID:           rawSessionID,
	}
	if payload.UserScope == "" {
		payload.UserScope = "claude_code_session_scope:anonymous"
	}
	return json.Marshal(payload)
}

func (m *ClaudeCodeSessionMapper) enforceBoundary(input ClaudeCodeSessionMapInput, mapping *ClaudeCodeSessionMapping) error {
	accountRef := strings.TrimSpace(input.AccountRef)
	egress := strings.TrimSpace(input.EgressBucket)
	provider := strings.TrimSpace(input.ProviderFamily)
	if accountRef == "" || egress == "" || provider == "" || mapping == nil || mapping.SessionRef == nil {
		return nil
	}
	enforceFormalPool := provider == "anthropic_formal_pool"
	keyPayload, err := json.Marshal(struct {
		BoundaryScope string `json:"boundary_scope"`
		RawSessionID  string `json:"raw_session_id"`
	}{
		BoundaryScope: claudeCodeSessionBoundaryScope(input),
		RawSessionID:  strings.TrimSpace(input.RawSessionID),
	})
	if err != nil {
		return err
	}
	key := "hmac-sha256:" + hex.EncodeToString(m.scopedHMACSum("claude_code_session_boundary_key", keyPayload))
	if enforceFormalPool && !claudeCodeSessionFormalPoolBoundaryContextComplete(input) {
		return &ClaudeCodeSessionBoundaryError{Code: "claude_native_session_boundary_incomplete"}
	}
	attempted := claudeCodeSessionBoundaryBinding{
		AccountRef:             safeBoundaryRef(accountRef),
		CredentialRef:          safeBoundaryRef(input.CredentialRef),
		EgressBucket:           sanitizeReasonCode(egress),
		ProxyIdentityRef:       safeBoundaryRef(input.ProxyIdentityRef),
		PolicyVersion:          sanitizeReasonCode(input.PolicyVersion),
		PersonaProfile:         sanitizeReasonCode(input.PersonaProfile),
		EgressProfileRef:       sanitizeReasonCode(input.EgressProfileRef),
		ProfilePolicyVersion:   sanitizeReasonCode(input.ProfilePolicyVersion),
		BillingShapePolicy:     sanitizeReasonCode(input.BillingShapePolicy),
		RequestShapeProfileRef: sanitizeReasonCode(input.RequestShapeProfileRef),
		CacheParityProfileRef:  sanitizeReasonCode(input.CacheParityProfileRef),
		ProviderFamily:         sanitizeReasonCode(provider),
		DeviceRef:              safeBoundaryRef(input.DeviceID),
		ServerSessionRef:       safeBoundaryRef(mapping.SessionRef.Value),
	}
	if input.FormalPoolProduction && enforceFormalPool && !claudeCodeSessionBoundaryLedgerPersistenceConfigured() {
		return &ClaudeCodeSessionBoundaryError{Code: "claude_native_session_boundary_ledger_unavailable"}
	}
	if enforceFormalPool {
		if err := loadClaudeCodeSessionBoundaryLedgerFromDisk(); err != nil {
			return &ClaudeCodeSessionBoundaryError{Code: "claude_native_session_boundary_ledger_unavailable"}
		}
	}
	prevAny, loaded := claudeCodeSessionBoundaryLedger.Load(key)
	if !loaded {
		if enforceFormalPool {
			if err := persistClaudeCodeSessionBoundaryLedgerEntryToDisk(key, attempted); err != nil {
				return &ClaudeCodeSessionBoundaryError{Code: "claude_native_session_boundary_ledger_unavailable"}
			}
			claudeCodeSessionBoundaryLedger.Store(key, attempted)
		}
		return nil
	}
	{
		prev, _ := prevAny.(claudeCodeSessionBoundaryBinding)
		if prev.AccountRef != attempted.AccountRef ||
			prev.CredentialRef != attempted.CredentialRef ||
			prev.EgressBucket != attempted.EgressBucket ||
			prev.ProxyIdentityRef != attempted.ProxyIdentityRef ||
			prev.PolicyVersion != attempted.PolicyVersion ||
			prev.PersonaProfile != attempted.PersonaProfile ||
			prev.EgressProfileRef != attempted.EgressProfileRef ||
			prev.ProfilePolicyVersion != attempted.ProfilePolicyVersion ||
			prev.BillingShapePolicy != attempted.BillingShapePolicy ||
			prev.RequestShapeProfileRef != attempted.RequestShapeProfileRef ||
			prev.CacheParityProfileRef != attempted.CacheParityProfileRef ||
			prev.ProviderFamily != attempted.ProviderFamily ||
			prev.DeviceRef != attempted.DeviceRef ||
			prev.ServerSessionRef != attempted.ServerSessionRef {
			return &ClaudeCodeSessionBoundaryError{
				Code:                      "claude_native_session_boundary_failed",
				PreviousAccountRef:        prev.AccountRef,
				AttemptedAccountRef:       attempted.AccountRef,
				PreviousCredentialRef:     prev.CredentialRef,
				AttemptedCredentialRef:    attempted.CredentialRef,
				PreviousEgress:            prev.EgressBucket,
				AttemptedEgress:           attempted.EgressBucket,
				PreviousProxyIdentityRef:  prev.ProxyIdentityRef,
				AttemptedProxyIdentityRef: attempted.ProxyIdentityRef,
				PreviousPolicyVersion:     prev.PolicyVersion,
				AttemptedPolicyVersion:    attempted.PolicyVersion,
				PreviousPersonaProfile:    prev.PersonaProfile,
				AttemptedPersonaProfile:   attempted.PersonaProfile,
				PreviousProviderFamily:    prev.ProviderFamily,
				AttemptedProviderFamily:   attempted.ProviderFamily,
				PreviousServerSessionRef:  prev.ServerSessionRef,
				AttemptedServerSessionRef: attempted.ServerSessionRef,
			}
		}
	}
	return nil
}

func claudeCodeSessionFormalPoolBoundaryContextComplete(input ClaudeCodeSessionMapInput) bool {
	for _, value := range []string{
		input.AccountRef,
		input.CredentialRef,
		input.DeviceID,
		input.EgressBucket,
		input.ProxyIdentityRef,
		input.PolicyVersion,
		input.PersonaProfile,
		input.EgressProfileRef,
		input.ProfilePolicyVersion,
		input.BillingShapePolicy,
		input.RequestShapeProfileRef,
		input.CacheParityProfileRef,
		input.ProviderFamily,
	} {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func claudeCodeSessionBoundaryLedgerFilePath() string {
	return strings.TrimSpace(os.Getenv(claudeCodeSessionBoundaryLedgerFileEnv))
}

func claudeCodeSessionBoundaryLedgerPersistenceConfigured() bool {
	return claudeCodeSessionBoundaryLedgerFilePath() != ""
}

func loadClaudeCodeSessionBoundaryLedgerFromDisk() error {
	path := claudeCodeSessionBoundaryLedgerFilePath()
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file claudeCodeSessionBoundaryLedgerFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return err
	}
	if file.Entries == nil {
		return nil
	}
	for key, binding := range file.Entries {
		if !isSafeLedgerRef(key) || !claudeCodeSessionBoundaryBindingSafe(binding) {
			return fmt.Errorf("unsafe claude code session boundary ledger")
		}
		claudeCodeSessionBoundaryLedger.Store(key, binding)
	}
	return nil
}

func persistClaudeCodeSessionBoundaryLedgerToDisk() error {
	return persistClaudeCodeSessionBoundaryLedgerEntryToDisk("", claudeCodeSessionBoundaryBinding{})
}

func persistClaudeCodeSessionBoundaryLedgerEntryToDisk(extraKey string, extraBinding claudeCodeSessionBoundaryBinding) error {
	path := claudeCodeSessionBoundaryLedgerFilePath()
	if path == "" {
		return nil
	}
	entries := map[string]claudeCodeSessionBoundaryBinding{}
	var snapshotErr error
	claudeCodeSessionBoundaryLedger.Range(func(k, v any) bool {
		key, ok := k.(string)
		if !ok || !isSafeLedgerRef(key) {
			snapshotErr = fmt.Errorf("unsafe claude code session boundary ledger key")
			return false
		}
		binding, ok := v.(claudeCodeSessionBoundaryBinding)
		if !ok || !claudeCodeSessionBoundaryBindingSafe(binding) {
			snapshotErr = fmt.Errorf("unsafe claude code session boundary ledger binding")
			return false
		}
		entries[key] = binding
		return true
	})
	if snapshotErr != nil {
		return snapshotErr
	}
	if extraKey != "" {
		if !isSafeLedgerRef(extraKey) || !claudeCodeSessionBoundaryBindingSafe(extraBinding) {
			return fmt.Errorf("unsafe claude code session boundary ledger entry")
		}
		entries[extraKey] = extraBinding
	}
	file := claudeCodeSessionBoundaryLedgerFile{Version: 1, Entries: entries}
	if err := ValidateNoRawSensitiveLedger(file); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func claudeCodeSessionBoundaryBindingSafe(binding claudeCodeSessionBoundaryBinding) bool {
	requiredRefs := []string{binding.AccountRef, binding.DeviceRef, binding.ServerSessionRef}
	for _, ref := range requiredRefs {
		if strings.TrimSpace(ref) == "" || !isSafeLedgerRef(ref) {
			return false
		}
	}
	for _, ref := range []string{binding.CredentialRef, binding.ProxyIdentityRef} {
		if strings.TrimSpace(ref) != "" && !isSafeLedgerRef(ref) {
			return false
		}
	}
	return true
}

func claudeCodeSessionBoundaryScope(input ClaudeCodeSessionMapInput) string {
	if scope := strings.TrimSpace(input.BoundaryScope); scope != "" {
		return scope
	}
	if scope := strings.TrimSpace(input.UserScope); scope != "" {
		return scope
	}
	return "claude_code_session_scope:anonymous"
}

func safeBoundaryRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if isSafeLedgerRef(ref) {
		return ref
	}
	if ref == "" {
		return ""
	}
	return scopedStickyHMAC("claude_code_session_boundary_ref", ref)
}

func (m *ClaudeCodeSessionMapper) scopedHMACSum(scope string, payload []byte) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(scope))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte("v"))
	_, _ = mac.Write([]byte(strconv.Itoa(m.version)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func claudeCodeUUIDLikeFromDigest(sum []byte) string {
	buf := make([]byte, 16)
	copy(buf, sum)
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func applyCCGatewayClaudeCodeSessionMapping(req *http.Request, account *Account) error {
	if req == nil {
		return nil
	}
	body, parsedUserID, rawSessionID := claudeCodeSessionPayloadFromRequest(req)
	if rawSessionID == "" {
		return nil
	}
	deviceID, err := claudeCodeSessionAccountDeviceID(account, parsedUserID)
	if err != nil {
		return err
	}
	accountRef := ccGatewayAccountRef(account)
	accountUUID := claudeCodeSessionAccountUUID(account, parsedUserID, accountRef)

	boundaryScope := claudeCodeSessionBoundaryScopeForAccount(req.Context(), account)
	mapper := NewClaudeCodeSessionMapperFromEnv()
	mapping, err := mapper.Map(ClaudeCodeSessionMapInput{
		UserScope:              claudeCodeSessionScopeFromContext(req.Context(), account, parsedUserID, accountUUID),
		BoundaryScope:          boundaryScope,
		EnforceBoundary:        boundaryScope != "",
		FormalPoolProduction:   claudeCodeSessionFormalPoolProduction(account),
		AccountRef:             accountRef,
		CredentialRef:          ccGatewayCredentialRef(account),
		DeviceID:               deviceID,
		AccountUUID:            accountUUID,
		EgressBucket:           resolveCCGatewayEgressBucket(account),
		ProxyIdentityRef:       ccGatewayProxyIdentityRef(account),
		PolicyVersion:          claudeCodeSessionPolicyVersion(req.Context(), account),
		PersonaProfile:         claudeCodeSessionPersonaProfile(req, account),
		EgressProfileRef:       ccGatewayTrustedEgressProfileRef(account),
		ProfilePolicyVersion:   ccGatewayProfilePolicyVersion(account),
		BillingShapePolicy:     ccGatewayBillingShapePolicy(account),
		RequestShapeProfileRef: ccGatewayRequestShapeProfileRef(account),
		CacheParityProfileRef:  ccGatewayCacheParityProfileRef(account),
		ProviderFamily:         claudeCodeSessionProviderFamily(account),
		RawSessionID:           rawSessionID,
	})
	if err != nil {
		return err
	}

	if len(body) > 0 && parsedUserID != nil {
		serverParsed := *parsedUserID
		serverParsed.DeviceID = deviceID
		serverParsed.AccountUUID = accountUUID
		if rewrittenBody, ok := rewriteMetadataUserIDSession(body, &serverParsed, mapping.SessionID); ok {
			claudeCodeReplaceRequestBody(req, rewrittenBody)
		} else {
			claudeCodeReplaceRequestBody(req, body)
		}
	}
	setHeaderRaw(req.Header, "X-Claude-Code-Session-Id", mapping.SessionID)
	return nil
}

func seedCCGatewayClaudeCodeSessionMappingInput(ctx context.Context, req *http.Request, clientHeaders http.Header) {
	if req == nil {
		return
	}
	if _, _, rawSessionID := claudeCodeSessionPayloadFromRequest(req); strings.TrimSpace(rawSessionID) != "" {
		return
	}
	rawSessionID := strings.TrimSpace(getHeaderRaw(clientHeaders, "X-Claude-Code-Session-Id"))
	if rawSessionID == "" {
		if native, ok := ClaudeCodeNativeAuditSummaryFromContext(ctx); ok && native.NativeAttested {
			rawSessionID = strings.TrimSpace(native.LocalSessionRef)
		}
	}
	if rawSessionID == "" {
		return
	}
	// This value is only input to the server-side session mapper. The mapper
	// replaces it with a server-canonical session before CC Gateway attestation.
	setHeaderRaw(req.Header, "X-Claude-Code-Session-Id", rawSessionID)
}

func claudeCodeSessionPayloadFromRequest(req *http.Request) ([]byte, *ParsedUserID, string) {
	if req == nil {
		return nil, nil, ""
	}
	body := claudeCodeReadRequestBody(req)
	if len(body) > 0 {
		if parsed := ParseMetadataUserID(strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())); parsed != nil {
			return body, parsed, strings.TrimSpace(parsed.SessionID)
		}
	}
	return body, nil, strings.TrimSpace(getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
}

func claudeCodeReadRequestBody(req *http.Request) []byte {
	if req == nil || req.Body == nil {
		return nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil
	}
	claudeCodeReplaceRequestBody(req, body)
	return body
}

func claudeCodeReplaceRequestBody(req *http.Request, body []byte) {
	if req == nil {
		return
	}
	reader := bytes.NewReader(body)
	req.Body = io.NopCloser(reader)
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	if len(body) > 0 {
		req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	} else {
		req.Header.Del("Content-Length")
	}
}

func rewriteMetadataUserIDSession(body []byte, parsed *ParsedUserID, sessionID string) ([]byte, bool) {
	if len(body) == 0 || parsed == nil || strings.TrimSpace(sessionID) == "" {
		return body, false
	}
	rewritten := parsedLegacyOrJSONMetadataUserID(parsed, sessionID)
	if rewritten == "" {
		return body, false
	}
	nextBody, err := sjson.SetBytes(body, "metadata.user_id", rewritten)
	if err != nil {
		return body, false
	}
	return nextBody, true
}

func parsedLegacyOrJSONMetadataUserID(parsed *ParsedUserID, sessionID string) string {
	if parsed == nil {
		return ""
	}
	if parsed.IsNewFormat {
		encoded, err := json.Marshal(jsonUserID{
			DeviceID:    parsed.DeviceID,
			AccountUUID: parsed.AccountUUID,
			SessionID:   sessionID,
		})
		if err != nil {
			return ""
		}
		return string(encoded)
	}
	return "user_" + parsed.DeviceID + "_account_" + parsed.AccountUUID + "_session_" + sessionID
}

func claudeCodeSessionScopeFromContext(ctx context.Context, account *Account, parsed *ParsedUserID, accountUUID string) string {
	if scope, ok := ClaudeCodeSessionUserScopeFromContext(ctx); ok {
		return scope
	}
	parts := make([]string, 0, 5)
	if group, ok := ctx.Value(ctxkey.Group).(*Group); ok && group != nil && group.ID > 0 {
		parts = append(parts, "group:"+strconv.FormatInt(group.ID, 10))
	}
	if account != nil && account.ID > 0 {
		parts = append(parts, "account:"+strconv.FormatInt(account.ID, 10))
	}
	if ref := strings.TrimSpace(ccGatewayAccountRef(account)); ref != "" {
		parts = append(parts, "account_ref:"+ref)
	}
	if len(parts) == 0 {
		return "claude_code_session_scope:anonymous"
	}
	return strings.Join(parts, "|")
}

func claudeCodeSessionBoundaryScopeFromContext(ctx context.Context) string {
	if scope, ok := ClaudeCodeSessionUserScopeFromContext(ctx); ok {
		return scope
	}
	if ctx != nil {
		if group, ok := ctx.Value(ctxkey.Group).(*Group); ok && group != nil && group.ID > 0 {
			return "group:" + strconv.FormatInt(group.ID, 10)
		}
	}
	return ""
}

func claudeCodeSessionBoundaryScopeForAccount(ctx context.Context, account *Account) string {
	if scope := claudeCodeSessionBoundaryScopeFromContext(ctx); scope != "" {
		return scope
	}
	if IsFormalPoolAccount(account) {
		return "formal_pool_session_scope:global"
	}
	return ""
}

var claudeCodeDeviceIDRe = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func claudeCodeSessionAccountDeviceID(account *Account, parsed *ParsedUserID) (string, error) {
	if IsFormalPoolAccount(account) {
		for _, key := range []string{"claude_code_device_id", "device_id"} {
			if deviceID := strings.TrimSpace(account.GetExtraString(key)); claudeCodeDeviceIDRe.MatchString(deviceID) {
				return strings.ToLower(deviceID), nil
			}
		}
		return "", fmt.Errorf("formal-pool claude native admission requires account-owned device identity")
	}
	if parsed == nil {
		return "", nil
	}
	return strings.TrimSpace(parsed.DeviceID), nil
}

func claudeCodeSessionAccountUUID(account *Account, parsed *ParsedUserID, accountRef string) string {
	if IsFormalPoolAccount(account) {
		return safeBoundaryRef(accountRef)
	}
	if parsed != nil && strings.TrimSpace(parsed.AccountUUID) != "" {
		return strings.TrimSpace(parsed.AccountUUID)
	}
	if account != nil {
		return strings.TrimSpace(account.GetExtraString("account_uuid"))
	}
	return ""
}

func claudeCodeSessionProviderFamily(account *Account) string {
	if IsFormalPoolAccount(account) {
		return "anthropic_formal_pool"
	}
	if account != nil {
		return sanitizeReasonCode(account.Platform)
	}
	return ""
}

func claudeCodeSessionPolicyVersion(ctx context.Context, account *Account) string {
	if ccGatewayTrustedPersonaContext(ctx) {
		if version := strings.TrimSpace(GetClaudeCodeVersion(ctx)); version != "" && ccGatewayPolicyVersionCompatible(version) {
			return version
		}
	}
	if version := strings.TrimSpace(account.GetExtraString(ccGatewayExtraPolicyVersion)); version != "" && ccGatewayPolicyVersionCompatible(version) {
		return ccGatewayAnthropicPolicyVersion
	}
	return ""
}

func claudeCodeSessionPersonaProfile(req *http.Request, account *Account) string {
	if req != nil {
		if profile := strings.TrimSpace(getHeaderRaw(req.Header, ccGatewayHealthcheckPersonaHeader)); profile != "" {
			return profile
		}
	}
	return ccGatewayPersonaProfile(account)
}

func claudeCodeSessionFormalPoolProduction(account *Account) bool {
	return IsFormalPoolAccount(account) && FormalPoolAccountStage(account) == FormalPoolStageProduction
}
