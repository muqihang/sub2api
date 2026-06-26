package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type defaultUpstream struct{ client *http.Client }

func (d defaultUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return d.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (d defaultUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return d.client.Do(req)
}

func freeListen() (net.Listener, string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	return ln, "http://" + ln.Addr().String()
}

func localCredentialBindingHMAC(secret, tokenType, rawCredential string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("formal_pool_credential_binding_v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(tokenType))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(rawCredential))
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	ccURL := strings.TrimRight(os.Getenv("CC_GATEWAY_URL"), "/")
	if ccURL == "" {
		log.Fatal("CC_GATEWAY_URL required")
	}
	summaryPath := os.Getenv("SUB2API_HARNESS_SUMMARY")
	if summaryPath == "" {
		log.Fatal("SUB2API_HARNESS_SUMMARY required")
	}

	cfg := &config.Config{}
	cfg.Gateway.MaxLineSize = 1024 * 1024
	cfg.Gateway.CCGateway.Enabled = true
	cfg.Gateway.CCGateway.BaseURL = ccURL
	cfg.Gateway.CCGateway.Token = "ccg-token"
	cfg.Gateway.CCGateway.InternalControlToken = "internal-control-material-v1-local-harness-ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	cfg.Gateway.CCGateway.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.CCGateway.ContextAttestationSecret = "scheduler-hmac-material-v1-local-harness-ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	cfg.Gateway.CCGateway.Providers.Anthropic = true

	selectedCredential := "selected-oauth-credential-fixture"
	credentialBinding := localCredentialBindingHMAC(cfg.Gateway.CCGateway.ContextAttestationSecret, "oauth", "Bearer "+selectedCredential)
	policyVersion := envOrDefault("SUB2API_HARNESS_POLICY_VERSION", "2.1.179")
	trustedEgressProfileRef := envOrDefault("SUB2API_HARNESS_EGRESS_PROFILE_REF", "strip_attribution")
	billingShapePolicy := envOrDefault("SUB2API_HARNESS_BILLING_SHAPE_POLICY", "strip")

	svc := service.NewGatewayService(nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, defaultUpstream{client: &http.Client{Timeout: 30 * time.Second}}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	account := &service.Account{
		ID: 1, Name: "explicit-local-canary", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"access_token": selectedCredential,
			"scope":        "user:profile user:inference user:sessions:claude_code user:mcp_servers",
		},
		Extra: map[string]any{
			"claude_code_device_id":                            strings.Repeat("b", 64),
			"cc_gateway_enabled":                               "true",
			"cc_gateway_account_ref":                           "hmac-sha256:" + strings.Repeat("a", 64),
			"cc_gateway_credential_ref":                        "opaque:credential-ref:v1:local-harness-credential",
			"cc_gateway_credential_binding_hmac":               credentialBinding,
			service.FormalPoolExtraCredentialGeneration:        "localhost-full-chain-generation-v1",
			"cc_gateway_proxy_identity_ref":                    "opaque:proxy-ref:v1:harness",
				"cc_gateway_persona_profile":                       "claude_code_2_1_179_native_degraded",
			"cc_gateway_trusted_egress_profile_ref":            trustedEgressProfileRef,
			"cc_gateway_profile_policy_version":                "claude_code_2_1_179_cp1_degraded_v1",
			"cc_gateway_billing_shape_policy":                  billingShapePolicy,
			"cc_gateway_request_shape_profile_ref":             "claude_code_2_1_179_messages_streaming_tooldefs_degraded_v1",
			"cc_gateway_cache_parity_profile_ref":              "claude_code_2_1_179_cache_parity_degraded_v1",
			"cc_gateway_canary_only":                           "false",
			"cc_gateway_policy_version":                        policyVersion,
			"cc_gateway_routes":                                "native_messages",
			"cc_gateway_egress_bucket_enabled":                 "true",
			"cc_gateway_egress_bucket":                         "bucket-a",
			"billing_cch_mode":                                 billingShapePolicy,
			service.FormalPoolExtraOnboardingStage:             service.FormalPoolStageProduction,
			service.FormalPoolExtraPoolProfileEffective:        service.PoolProfileNormal,
			service.FormalPoolExtraRuntimeRegistered:           "true",
			service.FormalPoolExtraRuntimeRegisteredAt:         "2026-06-24T00:00:00Z",
			service.FormalPoolExtraHealthcheckStatus:           "passed",
			service.FormalPoolExtraHealthcheckStatusCodeBucket: "status_2xx",
			service.FormalPoolExtraHealthcheckCCGatewaySeen:    "true",
			service.FormalPoolExtraHealthcheckFallbackDetected: "false",
			service.FormalPoolExtraHealthcheckProxyMismatch:    "false",
			service.FormalPoolExtraHealthcheckRiskTextDetected: "false",
			service.FormalPoolExtraHealthcheckRawRef:           "opaque:healthcheck-ref:v1:local-harness",
		},
	}

	router := gin.New()
	router.POST("/v1/messages", func(c *gin.Context) {
		if c.Request.URL.RawQuery != "beta=true" {
			c.JSON(403, gin.H{"error": "query_not_allowed"})
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		if err != nil || len(body) == 0 {
			c.JSON(400, gin.H{"error": "bad_body"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(body), service.PlatformAnthropic)
		if err != nil {
			c.JSON(400, gin.H{"error": "parse_failed"})
			return
		}
		ctx := c.Request.Context()
		if service.IsClaudeCodeNativeMarkerPresent(c.Request.Header) {
			audit, err := service.NewClaudeCodeNativeAttestationService().VerifyMessagesRequest(c.Request.Method, c.Request.URL.RequestURI(), c.Request.Header, body)
			if err != nil {
				c.JSON(403, gin.H{"error": "invalid_native_attestation"})
				return
			}
			if audit.LocalSessionRef == "" {
				c.JSON(403, gin.H{"error": "invalid_native_attestation"})
				return
			}
			ctx = service.WithClaudeCodeNativeAuditSummary(ctx, audit)
			ctx = service.SetClaudeCodeClient(ctx, true)
			if audit.ClaudeCodeVersion != "" {
				ctx = service.SetClaudeCodeVersion(ctx, audit.ClaudeCodeVersion)
			}
		} else {
			ctx = service.SetClaudeCodeClient(ctx, true)
			ctx = service.SetClaudeCodeVersion(ctx, policyVersion)
		}
		c.Request = c.Request.WithContext(ctx)
		rec := map[string]any{"ts": time.Now().Format(time.RFC3339Nano), "event": "sub2api_selected", "model": parsed.Model, "body_size": len(body), "stream": parsed.Stream}
		appendJSON(summaryPath, rec)
		_, err = svc.Forward(ctx, c, account, parsed)
		if err != nil {
			appendJSON(summaryPath, safeForwardErrorEvent(err, c.Writer.Written(), c.Writer.Status()))
			if !c.Writer.Written() {
				c.JSON(502, gin.H{"error": "forward_failed"})
			}
		}
	})
	router.NoRoute(func(c *gin.Context) { c.JSON(403, gin.H{"error": "route_blocked"}) })

	ln, url := freeListen()
	fmt.Println(mustJSON(map[string]any{"listen": url, "summary": summaryPath}))
	if err := http.Serve(ln, router); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func appendJSON(path string, obj any) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(obj)
	f.Write(append(b, '\n'))
}
func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func safeForwardErrorEvent(err error, responseWritten bool, responseStatus int) map[string]any {
	event := map[string]any{
		"ts":                 time.Now().Format(time.RFC3339Nano),
		"event":              "sub2api_forward_error",
		"safe_error_class":   classifyForwardError(err),
		"response_written":   responseWritten,
		"raw_error_retained": false,
	}
	if responseWritten && responseStatus > 0 {
		event["response_status"] = responseStatus
	}
	if code := safeCCGatewayControlPlaneCode(err); code != "" {
		event["cc_gateway_error_code"] = code
	}
	return event
}

func safeCCGatewayControlPlaneCode(err error) string {
	if err == nil {
		return ""
	}
	const prefix = "cc gateway control-plane error:"
	msg := strings.ToLower(err.Error())
	idx := strings.Index(msg, prefix)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(msg[idx+len(prefix):])
	if rest == "" {
		return ""
	}
	code := strings.Fields(rest)[0]
	for _, r := range code {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return ""
		}
	}
	return code
}

func classifyForwardError(err error) string {
	if err == nil {
		return "none"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "account-owned device identity"):
		return "formal_pool_missing_account_device_identity"
	case strings.Contains(msg, "claude_native_session_boundary"):
		return "formal_pool_session_boundary"
	case strings.Contains(msg, "cc gateway control-plane error") || strings.Contains(msg, "cc gateway"):
		return "cc_gateway_contract"
	case strings.Contains(msg, "access_token not found") || strings.Contains(msg, "api_key not found"):
		return "selected_credential_missing"
	case strings.Contains(msg, "native admission") || strings.Contains(msg, "formal pool"):
		return "formal_pool_admission"
	case strings.Contains(msg, "upstream request failed"):
		return "upstream_request_error"
	default:
		return "other"
	}
}
