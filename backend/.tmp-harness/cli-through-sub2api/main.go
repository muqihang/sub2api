package main

import (
	"bytes"
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
	cfg.Gateway.CCGateway.DefaultEgressBucket = "bucket-a"
	cfg.Gateway.CCGateway.Providers.Anthropic = true

	svc := service.NewGatewayService(nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, defaultUpstream{client: &http.Client{Timeout: 30 * time.Second}}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	account := &service.Account{
		ID: 1, Name: "explicit-local-canary", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: false, Concurrency: 0,
		Credentials: map[string]any{
			"access_token": "synthetic-selected-token",
			"scope":        "user:profile user:inference user:sessions:claude_code user:mcp_servers",
		},
		Extra: map[string]any{
			"cc_gateway_enabled":               "true",
			"cc_gateway_account_ref":           "hmac-sha256:local-harness-account",
			"cc_gateway_canary_only":           "true",
			"cc_gateway_policy_version":        "2.1.150",
			"cc_gateway_routes":                "native_messages",
			"cc_gateway_egress_bucket_enabled": "true",
			"cc_gateway_egress_bucket":         "bucket-a",
			"billing_cch_mode":                 "sign",
			"account_uuid":                     "redacted-session-id",
			"organization_uuid":                "redacted-session-id",
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
		parsed, err := service.ParseGatewayRequest(body, service.PlatformAnthropic)
		if err != nil {
			c.JSON(400, gin.H{"error": "parse_failed"})
			return
		}
		ctx := c.Request.Context()
		ctx = service.SetClaudeCodeClient(ctx, true)
		ctx = service.SetClaudeCodeVersion(ctx, "2.1.150")
		ctx = service.WithCCGatewayExplicitCanaryRequest(ctx, service.CCGatewayAnthropicCanaryRequest{
			AccountID:      account.ID,
			EgressBucket:   "bucket-a",
			BillingCCHMode: "sign",
			Method:         http.MethodPost,
			Route:          "/v1/messages",
		})
		ctx = service.WithCCGatewayExplicitCanaryLocalOnly(ctx)
		c.Request = c.Request.WithContext(ctx)
		rec := map[string]any{"ts": time.Now().Format(time.RFC3339Nano), "event": "sub2api_selected", "model": parsed.Model, "body_size": len(body), "stream": parsed.Stream}
		appendJSON(summaryPath, rec)
		_, err = svc.Forward(ctx, c, account, parsed)
		if err != nil && !c.Writer.Written() {
			c.JSON(502, gin.H{"error": "forward_failed"})
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
