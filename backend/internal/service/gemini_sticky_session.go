package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type GeminiStickySessionSource string

const (
	GeminiStickySessionSourceNone              GeminiStickySessionSource = ""
	GeminiStickySessionSourceSessionHeader     GeminiStickySessionSource = "session_header"
	GeminiStickySessionSourceConversationID    GeminiStickySessionSource = "conversation_id"
	GeminiStickySessionSourceOpenCodeSession   GeminiStickySessionSource = "opencode_session"
	GeminiStickySessionSourceGeminiCLI         GeminiStickySessionSource = "gemini_cli"
	GeminiStickySessionSourceBodyFirstPartText GeminiStickySessionSource = "body_first_part_text"
	GeminiStickySessionSourceGatewayFallback   GeminiStickySessionSource = "gateway_fallback"

	GeminiSafetyResponseStateHeader  = "X-Sub2API-Gemini-Degraded"
	GeminiSafetyResponseReasonHeader = "X-Sub2API-Gemini-Warning"

	GeminiSafetyStateThoughtSignature = "thought-signature-safety"

	GeminiSafetyReasonStickySessionUntrusted       = "sticky-session-untrusted"
	GeminiSafetyReasonStickySessionBindingMissing  = "sticky-session-binding-missing"
	GeminiSafetyReasonStickySessionAccountSwitched = "sticky-session-account-switched"
	GeminiSafetyReasonThoughtSignatureScrubFailed  = "thought-signature-scrub-failed"
	GeminiSafetyReasonCompatSignatureRetry         = "compat-signature-retry"
)

type GeminiThoughtSignatureSessionPolicyDecision struct {
	VisibleDegraded      bool
	RequiresScrub        bool
	DisableStickySession bool
	Reason               string
}

type GeminiThoughtSignatureAccountSwitchDecision struct {
	VisibleDegraded bool
	RequiresScrub   bool
	Reason          string
}

// GenerateGeminiStickySessionHash generates a sticky-session hash for Gemini v1beta requests.
// Priority:
//  1. Header: session_id
//  2. Header: conversation_id
//  3. Header: x-opencode-session
//  4. Body:   contents[0].parts[0].text
func GenerateGeminiStickySessionHash(c *gin.Context, body []byte) string {
	hash, _ := GenerateGeminiStickySessionHashWithSource(c, body)
	return hash
}

func GenerateGeminiStickySessionHashWithSource(c *gin.Context, body []byte) (string, GeminiStickySessionSource) {
	if c == nil {
		return "", GeminiStickySessionSourceNone
	}

	sessionID := strings.TrimSpace(c.GetHeader("session_id"))
	source := GeminiStickySessionSourceSessionHeader
	if sessionID == "" {
		sessionID = strings.TrimSpace(c.GetHeader("conversation_id"))
		source = GeminiStickySessionSourceConversationID
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(c.GetHeader("x-opencode-session"))
		source = GeminiStickySessionSourceOpenCodeSession
	}
	if sessionID == "" && len(body) > 0 {
		sessionID = strings.TrimSpace(gjson.GetBytes(body, "contents.0.parts.0.text").String())
		source = GeminiStickySessionSourceBodyFirstPartText
	}
	if sessionID == "" {
		return "", GeminiStickySessionSourceNone
	}

	hash := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(hash[:]), source
}

func GeminiStickySessionSourceTrustedForThoughtSignatures(source GeminiStickySessionSource) bool {
	switch source {
	case GeminiStickySessionSourceSessionHeader,
		GeminiStickySessionSourceConversationID,
		GeminiStickySessionSourceOpenCodeSession,
		GeminiStickySessionSourceGeminiCLI:
		return true
	default:
		return false
	}
}

func GeminiRequestHasThoughtSignature(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	contents := gjson.GetBytes(body, "contents")
	if !contents.Exists() || !contents.IsArray() {
		return false
	}
	found := false
	contents.ForEach(func(_, content gjson.Result) bool {
		parts := content.Get("parts")
		if !parts.Exists() || !parts.IsArray() {
			return true
		}
		parts.ForEach(func(_, part gjson.Result) bool {
			if part.Get("thoughtSignature").Exists() {
				found = true
				return false
			}
			return true
		})
		return !found
	})
	return found
}

func EvaluateGeminiThoughtSignatureSessionPolicy(cfg *config.Config, source GeminiStickySessionSource, signaturePresent bool, sessionBoundAccountID int64) GeminiThoughtSignatureSessionPolicyDecision {
	if !signaturePresent || !geminiThoughtSignatureSessionSafetyEnabled(cfg) {
		return GeminiThoughtSignatureSessionPolicyDecision{}
	}

	if geminiProductionModeEnabled(cfg) && !GeminiStickySessionSourceTrustedForThoughtSignatures(source) {
		return GeminiThoughtSignatureSessionPolicyDecision{
			VisibleDegraded:      true,
			RequiresScrub:        true,
			DisableStickySession: true,
			Reason:               GeminiSafetyReasonStickySessionUntrusted,
		}
	}

	if sessionBoundAccountID <= 0 {
		return GeminiThoughtSignatureSessionPolicyDecision{
			VisibleDegraded: true,
			RequiresScrub:   true,
			Reason:          GeminiSafetyReasonStickySessionBindingMissing,
		}
	}

	return GeminiThoughtSignatureSessionPolicyDecision{}
}

func EvaluateGeminiThoughtSignatureAccountSwitchPolicy(cfg *config.Config, signaturePresent bool, sessionBoundAccountID, selectedAccountID int64) GeminiThoughtSignatureAccountSwitchDecision {
	if !signaturePresent || !geminiThoughtSignatureSessionSafetyEnabled(cfg) || sessionBoundAccountID <= 0 || selectedAccountID <= 0 || sessionBoundAccountID == selectedAccountID {
		return GeminiThoughtSignatureAccountSwitchDecision{}
	}
	return GeminiThoughtSignatureAccountSwitchDecision{
		VisibleDegraded: true,
		RequiresScrub:   true,
		Reason:          GeminiSafetyReasonStickySessionAccountSwitched,
	}
}

func MarkGeminiSafetyDegraded(c *gin.Context, reason string) {
	if c == nil {
		return
	}
	c.Header(GeminiSafetyResponseStateHeader, GeminiSafetyStateThoughtSignature)
	if reason == "" {
		return
	}
	c.Header(GeminiSafetyResponseReasonHeader, appendGeminiHeaderValue(c.Writer.Header().Get(GeminiSafetyResponseReasonHeader), reason))
}

func appendGeminiHeaderValue(existing, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return strings.TrimSpace(existing)
	}
	if existing == "" {
		return value
	}
	for _, part := range strings.Split(existing, ",") {
		if strings.TrimSpace(part) == value {
			return existing
		}
	}
	return existing + ", " + value
}
