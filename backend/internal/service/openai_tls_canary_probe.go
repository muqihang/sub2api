package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const openAIGatewayTLSCanaryProbeTimeout = 10 * time.Second

type OpenAIGatewayTLSCanaryProbeInput struct {
	AccountID    int64
	Bucket       string
	TLSProfileID int64
	Transport    OpenAIClientTransport
	Route        string
	Headers      http.Header
}

func (s *OpenAIGatewayService) RunOpenAITLSCanaryProbe(ctx context.Context, input OpenAIGatewayTLSCanaryProbeInput) (*OpenAIGatewayTLSCanarySnapshot, error) {
	if s == nil || s.gatewayCoreService == nil {
		return nil, errors.New("openai tls canary live probe runtime unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if input.TLSProfileID > 0 {
		ctx = WithOpenAIGatewayTLSCanaryProfileID(ctx, input.TLSProfileID)
	}
	probeCtx, cancel := context.WithTimeout(ctx, openAIGatewayTLSCanaryProbeTimeout)
	defer cancel()

	transport := normalizeOpenAIClientTransport(input.Transport)
	if transport == OpenAIClientTransportUnknown {
		transport = OpenAIClientTransportHTTP
	}
	snapshot, err := s.gatewayCoreService.BuildTLSCanarySnapshotWithProfileOverride(probeCtx, input.AccountID, input.Bucket, input.Route, input.Headers, transport, input.TLSProfileID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, errors.New("openai tls canary snapshot unavailable")
	}
	switch transport {
	case OpenAIClientTransportHTTP:
		return s.runOpenAITLSCanaryHTTPProbe(probeCtx, input, snapshot)
	case OpenAIClientTransportWS:
		return s.runOpenAITLSCanaryWSProbe(probeCtx, input, snapshot)
	default:
		markOpenAITLSCanaryProbeFailure(snapshot, "unsupported_transport", "")
		return snapshot, nil
	}
}

func (s *OpenAIGatewayService) runOpenAITLSCanaryHTTPProbe(ctx context.Context, input OpenAIGatewayTLSCanaryProbeInput, snapshot *OpenAIGatewayTLSCanarySnapshot) (*OpenAIGatewayTLSCanarySnapshot, error) {
	route := normalizeOpenAITLSCanaryRoute(input.Route)
	probe := &OpenAIGatewayTLSCanaryProbe{
		Mode:      "live",
		Transport: string(OpenAIClientTransportHTTP),
		Route:     route,
	}
	snapshot.Probe = probe
	if !isOpenAITLSCanaryHTTPLiveRoute(route) {
		markOpenAITLSCanaryProbeFailure(snapshot, "unsupported_route", "")
		return snapshot, nil
	}
	account, err := s.openAITLSCanaryAccount(ctx, input)
	if err != nil {
		return nil, err
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		markOpenAITLSCanaryProbeFailure(snapshot, "token_unavailable", err.Error())
		return snapshot, nil
	}
	targetURL, err := s.openAITLSCanaryHTTPURL(account, route)
	if err != nil {
		markOpenAITLSCanaryProbeFailure(snapshot, "invalid_route", err.Error())
		return snapshot, nil
	}
	body := []byte(`{"model":"gpt-4o-mini","input":"tls canary","max_output_tokens":1}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("content-type", "application/json")
	startedAt := time.Now()
	resp, err := s.sendOpenAIHTTPRequest(ctx, nil, req, account)
	probe.HandshakeMS = time.Since(startedAt).Milliseconds()
	if err != nil {
		markOpenAITLSCanaryProbeFailure(snapshot, classifyOpenAITLSCanaryError(err), err.Error())
		return snapshot, nil
	}
	if resp != nil {
		probe.HTTPStatus = resp.StatusCode
		if resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}
	snapshot.Success = resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 500
	if !snapshot.Success {
		markOpenAITLSCanaryProbeFailure(snapshot, "http_status_"+fmt.Sprint(probe.HTTPStatus), "")
	} else {
		snapshot.FailureReason = ""
	}
	return snapshot, nil
}

func (s *OpenAIGatewayService) runOpenAITLSCanaryWSProbe(ctx context.Context, input OpenAIGatewayTLSCanaryProbeInput, snapshot *OpenAIGatewayTLSCanarySnapshot) (*OpenAIGatewayTLSCanarySnapshot, error) {
	route := normalizeOpenAITLSCanaryRoute(input.Route)
	probe := &OpenAIGatewayTLSCanaryProbe{
		Mode:      "live",
		Transport: string(OpenAIClientTransportWS),
		Route:     route,
	}
	snapshot.Probe = probe
	if !isOpenAITLSCanaryWSLiveRoute(route) {
		markOpenAITLSCanaryProbeFailure(snapshot, "unsupported_route", "")
		return snapshot, nil
	}
	account, err := s.openAITLSCanaryAccount(ctx, input)
	if err != nil {
		return nil, err
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		markOpenAITLSCanaryProbeFailure(snapshot, "token_unavailable", err.Error())
		return snapshot, nil
	}
	wsURL, err := s.buildOpenAIResponsesWSURL(account)
	if err != nil {
		markOpenAITLSCanaryProbeFailure(snapshot, "build_ws_url_failed", err.Error())
		return snapshot, nil
	}
	headers, _, err := s.buildOpenAIWSHeaders(nil, account, token, OpenAIWSProtocolDecision{}, false, "", "", "")
	if err != nil {
		markOpenAITLSCanaryProbeFailure(snapshot, "build_ws_headers_failed", err.Error())
		return snapshot, nil
	}
	egress, err := s.resolveOpenAIEgress(ctx, account)
	if err != nil {
		markOpenAITLSCanaryProbeFailure(snapshot, classifyOpenAITLSCanaryError(err), err.Error())
		return snapshot, nil
	}
	effectiveTLS, err := s.resolveOpenAIWSEffectiveTLS(ctx, account, egress)
	if err != nil {
		markOpenAITLSCanaryProbeFailure(snapshot, classifyOpenAITLSCanaryError(err), err.Error())
		return snapshot, nil
	}
	diagnostics := openAIWSDialerStrategyDiagnostics(headers, egress.ProxyURL, effectiveTLS)
	probe.WSDialerStrategy = diagnostics["ws_dialer_strategy"]
	if diagnostics["ws_transport_supported"] == "false" {
		markOpenAITLSCanaryProbeFailure(snapshot, strings.TrimSpace(diagnostics["ws_transport_unsupported_reason"]), "")
		return snapshot, nil
	}
	dialer := s.getOpenAIWSPassthroughDialer()
	if dialer == nil {
		markOpenAITLSCanaryProbeFailure(snapshot, "ws_dialer_unavailable", "")
		return snapshot, nil
	}
	startedAt := time.Now()
	conn, status, _, err := dialer.Dial(ctx, wsURL, headers, egress.ProxyURL, effectiveTLS)
	probe.HandshakeMS = time.Since(startedAt).Milliseconds()
	probe.WSHandshakeStatus = status
	if err != nil {
		markOpenAITLSCanaryProbeFailure(snapshot, classifyOpenAITLSCanaryError(err), err.Error())
		return snapshot, nil
	}
	if conn != nil {
		_ = conn.Close()
	}
	snapshot.Success = true
	snapshot.FailureReason = ""
	return snapshot, nil
}

func (s *OpenAIGatewayService) openAITLSCanaryAccount(ctx context.Context, input OpenAIGatewayTLSCanaryProbeInput) (*Account, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrAccountNotFound
	}
	account, err := s.accountRepo.GetByID(ctx, input.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || !account.IsOpenAI() {
		return nil, ErrAccountNotFound
	}
	copied := *account
	if bucket := strings.TrimSpace(input.Bucket); bucket != "" {
		copied.Extra = mergeMap(copied.Extra, map[string]any{"openai_gateway_egress_bucket": bucket})
	}
	return &copied, nil
}

func (s *OpenAIGatewayService) openAITLSCanaryHTTPURL(account *Account, route string) (string, error) {
	route = normalizeOpenAITLSCanaryRoute(route)
	if route == "" {
		return "", errors.New("route is empty")
	}
	base := openaiPlatformAPIURL
	if account != nil && account.Type == AccountTypeOAuth {
		base = chatgptCodexURL
	}
	if account != nil && account.Type == AccountTypeAPIKey {
		if configuredBase := account.GetOpenAIBaseURL(); configuredBase != "" {
			validated, err := s.validateUpstreamBaseURL(configuredBase)
			if err != nil {
				return "", err
			}
			base = validated
		}
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/") + route, nil
}

func normalizeOpenAITLSCanaryRoute(route string) string {
	route = "/" + strings.Trim(strings.TrimSpace(route), "/")
	if route == "/" {
		return ""
	}
	return route
}

func isOpenAITLSCanaryHTTPLiveRoute(route string) bool {
	switch normalizeOpenAITLSCanaryRoute(route) {
	case "/v1/responses", "/responses", "/openai/v1/responses", "/backend-api/codex/responses":
		return true
	default:
		return false
	}
}

func isOpenAITLSCanaryWSLiveRoute(route string) bool {
	switch normalizeOpenAITLSCanaryRoute(route) {
	case "/v1/responses", "/responses", "/openai/v1/responses", "/backend-api/codex/responses":
		return true
	default:
		return false
	}
}

func markOpenAITLSCanaryProbeFailure(snapshot *OpenAIGatewayTLSCanarySnapshot, reason string, message string) {
	if snapshot == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "probe_failed"
	}
	snapshot.Success = false
	snapshot.FailureReason = reason
	if snapshot.Probe != nil {
		snapshot.Probe.FailureReason = reason
		snapshot.Probe.Error = strings.TrimSpace(message)
	}
}

func classifyOpenAITLSCanaryError(err error) string {
	if err == nil {
		return ""
	}
	var policyErr *OpenAIEgressPolicyError
	if errors.As(err, &policyErr) && policyErr != nil {
		return strings.TrimSpace(policyErr.Code)
	}
	return "probe_failed"
}
