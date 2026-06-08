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
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	claudeCodeServerSessionScope = "claude_code_server_session"
	claudeCodeSessionBudgetScope = "session_budget_session"
)

type claudeCodeSessionUserScopeContextKey struct{}

type ClaudeCodeSessionMapper struct {
	keyID   string
	version int
	secret  []byte
}

type ClaudeCodeSessionMapInput struct {
	UserScope    string
	AccountRef   string
	DeviceID     string
	AccountUUID  string
	RawSessionID string
}

type ClaudeCodeSessionMapping struct {
	SessionID  string                     `json:"session_id"`
	SessionRef *ControlPlaneScopedHMACRef `json:"session_ref,omitempty"`
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
	return &ClaudeCodeSessionMapping{
		SessionID: sessionID,
		SessionRef: &ControlPlaneScopedHMACRef{
			KeyID:   m.keyID,
			Scope:   claudeCodeSessionBudgetScope,
			Version: m.version,
			Value:   "hmac-sha256:" + hex.EncodeToString(m.scopedHMACSum(claudeCodeSessionBudgetScope, material)),
		},
	}, nil
}

func claudeCodeSessionMapMaterial(input ClaudeCodeSessionMapInput) ([]byte, error) {
	rawSessionID := strings.TrimSpace(input.RawSessionID)
	if rawSessionID == "" {
		return nil, fmt.Errorf("claude code session mapper requires raw session id")
	}
	payload := struct {
		UserScope    string `json:"user_scope"`
		AccountRef   string `json:"account_ref,omitempty"`
		DeviceID     string `json:"device_id,omitempty"`
		AccountUUID  string `json:"account_uuid,omitempty"`
		RawSessionID string `json:"raw_session_id"`
	}{
		UserScope:    strings.TrimSpace(input.UserScope),
		AccountRef:   strings.TrimSpace(input.AccountRef),
		DeviceID:     strings.TrimSpace(input.DeviceID),
		AccountUUID:  strings.TrimSpace(input.AccountUUID),
		RawSessionID: rawSessionID,
	}
	if payload.UserScope == "" {
		payload.UserScope = "claude_code_session_scope:anonymous"
	}
	return json.Marshal(payload)
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

func applyCCGatewayClaudeCodeSessionMapping(req *http.Request, account *Account) {
	if req == nil {
		return
	}
	body, parsedUserID, rawSessionID := claudeCodeSessionPayloadFromRequest(req)
	if rawSessionID == "" {
		return
	}
	accountUUID := ""
	if parsedUserID != nil {
		accountUUID = parsedUserID.AccountUUID
	}
	if accountUUID == "" && account != nil {
		accountUUID = strings.TrimSpace(account.GetExtraString("account_uuid"))
	}

	mapper := NewClaudeCodeSessionMapperFromEnv()
	mapping, err := mapper.Map(ClaudeCodeSessionMapInput{
		UserScope:    claudeCodeSessionScopeFromContext(req.Context(), account, parsedUserID, accountUUID),
		AccountRef:   ccGatewayAccountRef(account),
		DeviceID:     claudeCodeSessionDeviceID(parsedUserID),
		AccountUUID:  accountUUID,
		RawSessionID: rawSessionID,
	})
	if err != nil {
		return
	}

	if len(body) > 0 && parsedUserID != nil {
		if rewrittenBody, ok := rewriteMetadataUserIDSession(body, parsedUserID, mapping.SessionID); ok {
			claudeCodeReplaceRequestBody(req, rewrittenBody)
		} else {
			claudeCodeReplaceRequestBody(req, body)
		}
	}
	setHeaderRaw(req.Header, "X-Claude-Code-Session-Id", mapping.SessionID)
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
	if accountUUID != "" {
		parts = append(parts, "account_uuid:"+accountUUID)
	}
	if deviceID := claudeCodeSessionDeviceID(parsed); deviceID != "" {
		parts = append(parts, "device:"+deviceID)
	}
	if len(parts) == 0 {
		return "claude_code_session_scope:anonymous"
	}
	return strings.Join(parts, "|")
}

func claudeCodeSessionDeviceID(parsed *ParsedUserID) string {
	if parsed == nil {
		return ""
	}
	return strings.TrimSpace(parsed.DeviceID)
}
