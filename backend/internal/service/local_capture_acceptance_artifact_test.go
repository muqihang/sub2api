package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const jointLocalCaptureArtifactSlug = "sub2api-cc-gateway-joint-local-capture"
const jointExpectedGatewayUserAgent = "claude-cli/2.1.170 (external, sdk-cli)"
const jointExpectedGatewayPersonaVariant = "claude-code-2.1.170-macos-local"

type jointRedactionRule struct {
	Label  string
	Needle string
}

var jointLocalCaptureRedactionRules = []jointRedactionRule{
	{Label: "forbidden_bearer_fixture", Needle: "Bearer selected-token"},
	{Label: "forbidden_selected_api_key_fixture", Needle: "selected-api-key"},
	{Label: "forbidden_oauth_fixture", Needle: "oauth-token"},
	{Label: "forbidden_setup_token_fixture", Needle: "setup-token"},
	{Label: "forbidden_client_device_fixture", Needle: "client-device"},
	{Label: "forbidden_fake_device_fixture", Needle: "fake-device"},
	{Label: "forbidden_account_uuid_fixture", Needle: "acct-uuid"},
	{Label: "forbidden_org_uuid_fixture", Needle: "org-uuid"},
	{Label: "forbidden_user_email_fixture", Needle: "user@example.com"},
	{Label: "forbidden_selected_email_fixture", Needle: "selected@example.com"},
	{Label: "forbidden_session_uuid_fixture", Needle: "99999999-8888-4777-8666-555555555555"},
	{Label: "forbidden_alt_session_uuid_fixture", Needle: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"},
	{Label: "forbidden_cch_fixture", Needle: "cch=12345"},
	{Label: "forbidden_email_at_sign", Needle: "@"},
	{Label: "forbidden_bearer_prefix", Needle: "Bearer "},
}

type jointCaptureMetadataSummary struct {
	FieldNames       []string          `json:"field_names,omitempty"`
	FieldRefs        map[string]string `json:"field_refs,omitempty"`
	UserIDValueRef   string            `json:"user_id_value_ref,omitempty"`
	SessionHeaderRef string            `json:"session_header_ref,omitempty"`
}

type jointCaptureBodySummary struct {
	BodyRef              string                       `json:"body_ref"`
	SizeBytes            int                          `json:"size_bytes"`
	TopLevelKeys         []string                     `json:"top_level_keys,omitempty"`
	MessageCount         int                          `json:"message_count,omitempty"`
	SystemCount          int                          `json:"system_count,omitempty"`
	BillingHeaderPresent bool                         `json:"billing_header_present"`
	CCHPresent           bool                         `json:"cch_present"`
	Metadata             *jointCaptureMetadataSummary `json:"metadata,omitempty"`
}

type jointCaptureHopSummary struct {
	URLHost                 string                   `json:"url_host,omitempty"`
	Route                   string                   `json:"route,omitempty"`
	HeaderKeyOrder          []string                 `json:"header_key_order,omitempty"`
	HeaderValuesSummary     map[string]string        `json:"header_values_summary,omitempty"`
	Body                    *jointCaptureBodySummary `json:"body,omitempty"`
	RequestCount            int                      `json:"request_count"`
	ProxyURLUsed            string                   `json:"proxy_url_used,omitempty"`
	TLSProfileUsed          bool                     `json:"tls_profile_used"`
	BodyUnchangedFromClient bool                     `json:"body_unchanged_from_client,omitempty"`
}

type jointCaptureScenario struct {
	Name                     string                  `json:"name"`
	Category                 string                  `json:"category"`
	Route                    string                  `json:"route"`
	PolicyDecision           string                  `json:"policy_decision"`
	SelectedAccountIDRef     string                  `json:"selected_account_id_ref,omitempty"`
	EgressBucketID           string                  `json:"egress_bucket_id,omitempty"`
	PolicyVersion            string                  `json:"policy_version,omitempty"`
	ResponseStatus           int                     `json:"response_status"`
	ResponseErrorKind        string                  `json:"response_error_kind,omitempty"`
	ResponseErrorCode        string                  `json:"response_error_code,omitempty"`
	ClientHeaderOrder        []string                `json:"client_header_order,omitempty"`
	ClientBodyRef            string                  `json:"client_body_ref,omitempty"`
	Sub2APIToGateway         *jointCaptureHopSummary `json:"sub2api_to_gateway,omitempty"`
	GatewayToUpstream        *jointCaptureHopSummary `json:"gateway_to_upstream,omitempty"`
	RequestCount             int                     `json:"request_count"`
	FailClosed               bool                    `json:"fail_closed"`
	NoRealUpstream           bool                    `json:"no_real_upstream"`
	NoNativeFallback         bool                    `json:"no_native_fallback"`
	Sub2APIFinalMutation     bool                    `json:"sub2api_final_mutation"`
	CCGatewayOwnsFinalOutput bool                    `json:"cc_gateway_owns_final_output"`
	Passed                   bool                    `json:"passed"`
	Notes                    []string                `json:"notes,omitempty"`
}

type jointCaptureRedactionScan struct {
	Passed   bool     `json:"passed"`
	Patterns []string `json:"patterns"`
	Hits     []string `json:"hits,omitempty"`
}

type jointCaptureReport struct {
	ExecutedAt              string                    `json:"executed_at"`
	ArtifactDir             string                    `json:"artifact_dir"`
	GatewayMode             string                    `json:"gateway_mode"`
	NoRealUpstream          bool                      `json:"no_real_upstream"`
	NoRawSecrets            bool                      `json:"no_raw_secrets"`
	NoNativeFallback        bool                      `json:"no_native_fallback"`
	Sub2APINotFinalMutating bool                      `json:"sub2api_not_final_mutating"`
	CCGatewayFinalOwner     bool                      `json:"cc_gateway_final_owner"`
	NegativeCasesFailClosed bool                      `json:"negative_cases_fail_closed"`
	Scenarios               []jointCaptureScenario    `json:"scenarios"`
	RedactionScan           jointCaptureRedactionScan `json:"redaction_scan"`
}

type recordingGatewayRequest struct {
	URL            string
	Host           string
	HeaderKeyOrder []string
	Headers        http.Header
	Body           []byte
	ProxyURL       string
	TLSProfileUsed bool
}

type jointGatewayRecordingUpstream struct {
	client   *http.Client
	mu       sync.Mutex
	requests []recordingGatewayRequest
}

func (u *jointGatewayRecordingUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *jointGatewayRecordingUpstream) DoWithTLS(req *http.Request, proxyURL string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))

	serializedOrder := serializeRequestHeaderOrder(req, body)
	clonedHeaders := jointCloneHeaders(req.Header)

	u.mu.Lock()
	u.requests = append(u.requests, recordingGatewayRequest{
		URL:            req.URL.String(),
		Host:           req.URL.Host,
		HeaderKeyOrder: serializedOrder,
		Headers:        clonedHeaders,
		Body:           append([]byte(nil), body...),
		ProxyURL:       proxyURL,
		TLSProfileUsed: profile != nil,
	})
	u.mu.Unlock()

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (u *jointGatewayRecordingUpstream) popSingle(t *testing.T) recordingGatewayRequest {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	require.Len(t, u.requests, 1)
	captured := u.requests[0]
	u.requests = nil
	return captured
}

func (u *jointGatewayRecordingUpstream) reset() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests = nil
}

type rawCaptureRequest struct {
	Method         string
	Path           string
	HeaderKeyOrder []string
	Headers        http.Header
	Body           []byte
}

type rawCaptureResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

type rawCaptureServer struct {
	listener net.Listener
	mu       sync.Mutex
	requests []rawCaptureRequest
	closed   chan struct{}
}

func startRawCaptureServer(t *testing.T) *rawCaptureServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &rawCaptureServer{listener: ln, closed: make(chan struct{})}
	go server.serve(t)
	t.Cleanup(func() {
		_ = server.Close()
	})
	return server
}

func (s *rawCaptureServer) serve(t *testing.T) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			t.Logf("raw capture accept error: %v", err)
			return
		}
		go s.handleConn(t, conn)
	}
}

func (s *rawCaptureServer) handleConn(t *testing.T, conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	requestLine, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	requestLine = strings.TrimSpace(requestLine)
	parts := strings.Split(requestLine, " ")
	if len(parts) < 2 {
		return
	}
	method := parts[0]
	path := parts[1]
	headers := http.Header{}
	order := make([]string, 0, 16)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		idx := strings.Index(trimmed, ":")
		if idx <= 0 {
			continue
		}
		key := trimmed[:idx]
		value := strings.TrimSpace(trimmed[idx+1:])
		order = append(order, key)
		headers.Add(key, value)
	}
	contentLength, _ := strconv.Atoi(headers.Get("Content-Length"))
	body := make([]byte, contentLength)
	if contentLength > 0 {
		_, err = io.ReadFull(reader, body)
		if err != nil {
			return
		}
	}
	request := rawCaptureRequest{
		Method:         method,
		Path:           path,
		HeaderKeyOrder: append([]string(nil), order...),
		Headers:        jointCloneHeaders(headers),
		Body:           append([]byte(nil), body...),
	}
	s.mu.Lock()
	s.requests = append(s.requests, request)
	s.mu.Unlock()

	response := defaultRawCaptureResponse(path, body)
	if response.Status == 0 {
		response.Status = http.StatusOK
	}
	if response.Headers == nil {
		response.Headers = map[string]string{}
	}
	if _, ok := response.Headers["Content-Type"]; !ok {
		response.Headers["Content-Type"] = "application/json"
	}
	response.Headers["Content-Length"] = strconv.Itoa(len(response.Body))
	response.Headers["Connection"] = "close"
	statusText := http.StatusText(response.Status)
	if statusText == "" {
		statusText = "Status"
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "HTTP/1.1 %d %s\r\n", response.Status, statusText)
	for key, value := range response.Headers {
		fmt.Fprintf(&out, "%s: %s\r\n", key, value)
	}
	out.WriteString("\r\n")
	out.Write(response.Body)
	_, _ = conn.Write(out.Bytes())
}

func (s *rawCaptureServer) popSingle(t *testing.T) rawCaptureRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Len(t, s.requests, 1)
	captured := s.requests[0]
	s.requests = nil
	return captured
}

func (s *rawCaptureServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *rawCaptureServer) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
}

func (s *rawCaptureServer) URL() string {
	return "http://" + s.listener.Addr().String()
}

func (s *rawCaptureServer) Close() error {
	select {
	case <-s.closed:
		return nil
	default:
		close(s.closed)
	}
	return s.listener.Close()
}

type connectProxyServer struct {
	listener net.Listener
	mu       sync.Mutex
	targets  []string
	closed   chan struct{}
}

func startConnectProxyServer(t *testing.T) *connectProxyServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &connectProxyServer{listener: ln, closed: make(chan struct{})}
	go server.serve(t)
	t.Cleanup(func() {
		_ = server.Close()
	})
	return server
}

func (s *connectProxyServer) serve(t *testing.T) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			t.Logf("connect proxy accept error: %v", err)
			return
		}
		go s.handleConn(conn)
	}
}

func (s *connectProxyServer) handleConn(client net.Conn) {
	defer client.Close()
	reader := bufio.NewReader(client)
	requestLine, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	requestLine = strings.TrimSpace(requestLine)
	parts := strings.Split(requestLine, " ")
	if len(parts) < 3 || !strings.EqualFold(parts[0], http.MethodConnect) {
		_, _ = client.Write([]byte("HTTP/1.1 405 Method Not Allowed\r\nConnection: close\r\n\r\n"))
		return
	}
	target := parts[1]
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		_, _ = client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n"))
		return
	}
	defer upstream.Close()

	s.mu.Lock()
	s.targets = append(s.targets, target)
	s.mu.Unlock()

	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(upstream, reader)
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(client, upstream)
		errCh <- err
	}()
	<-errCh
}

func (s *connectProxyServer) URL() string {
	return "http://" + s.listener.Addr().String()
}

func (s *connectProxyServer) Close() error {
	select {
	case <-s.closed:
		return nil
	default:
		close(s.closed)
	}
	return s.listener.Close()
}

type ccGatewayProcess struct {
	baseURL    string
	configPath string
	cmd        *exec.Cmd
	stdout     bytes.Buffer
	stderr     bytes.Buffer
}

func startCCGatewayProcess(t *testing.T, configYAML string) *ccGatewayProcess {
	t.Helper()
	port := reserveFreePort(t)
	configYAML = strings.ReplaceAll(configYAML, "{{PORT}}", strconv.Itoa(port))
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o644))

	cmd := exec.Command(filepath.Join(ccGatewayRepoRoot(), "node_modules", ".bin", "tsx"), "src/index.ts", configPath)
	cmd.Dir = ccGatewayRepoRoot()
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &bytes.Buffer{}
	process := &ccGatewayProcess{
		baseURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		configPath: configPath,
		cmd:        cmd,
	}
	cmd.Stdout = &process.stdout
	cmd.Stderr = &process.stderr
	require.NoError(t, cmd.Start())

	t.Cleanup(func() {
		if process.cmd.Process != nil {
			_ = process.cmd.Process.Kill()
			_, _ = process.cmd.Process.Wait()
		}
	})

	deadline := time.Now().Add(10 * time.Second)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(process.baseURL + "/_health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return process
			}
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("cc-gateway did not become healthy\nstdout:\n%s\nstderr:\n%s", process.stdout.String(), process.stderr.String())
	return nil
}

func reserveFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func ccGatewayRepoRoot() string {
	if root := strings.TrimSpace(os.Getenv("CC_GATEWAY_REPO_ROOT")); root != "" {
		return root
	}
	worktree := "/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main"
	if _, err := os.Stat(filepath.Join(worktree, "package.json")); err == nil {
		return worktree
	}
	return "/Users/muqihang/chelingxi_workspace/cc-gateway"
}

func defaultRawCaptureResponse(path string, body []byte) rawCaptureResponse {
	if path == "/v1/messages/count_tokens?beta=true" {
		return rawCaptureResponse{
			Status: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": "application/json",
				"x-request-id": "upstream-count-tokens",
			},
			Body: []byte(`{"input_tokens":7}`),
		}
	}
	if path == "/v1/messages?beta=true" {
		if bytes.Contains(body, []byte(`"stream":true`)) {
			sse := strings.Join([]string{
				`event: message_start`,
				`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","stop_reason":"","usage":{"input_tokens":12}}}`,
				``,
				`event: content_block_start`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"ok"}}`,
				``,
				`event: message_delta`,
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
				``,
				`event: message_stop`,
				`data: {"type":"message_stop"}`,
				``,
			}, "\n")
			return rawCaptureResponse{
				Status: http.StatusOK,
				Headers: map[string]string{
					"Content-Type": "text/event-stream",
					"x-request-id": "upstream-sse",
				},
				Body: []byte(sse),
			}
		}
		return rawCaptureResponse{
			Status: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": "application/json",
				"x-request-id": "upstream-message",
			},
			Body: []byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`),
		}
	}
	return rawCaptureResponse{
		Status:  http.StatusNotFound,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    []byte(`{"error":{"message":"unexpected local path"}}`),
	}
}

func jointGatewayConfigYAML(upstreamURL, proxyURL string) string {
	return fmt.Sprintf(`mode: sub2api
server:
  port: {{PORT}}
  tls:
    cert: ""
    key: ""
upstream:
  url: %q
providers:
  anthropic: true
auth:
  gateway_token: gateway-token
  tokens: []
identity:
  device_id: "%s"
  email: canonical@example.com
env:
  platform: darwin
  platform_raw: darwin
  arch: arm64
  node_version: v24.3.0
  terminal: iTerm2.app
  package_managers: npm,pnpm
  runtimes: node
  is_running_with_bun: false
  is_ci: false
  is_claude_ai_auth: true
  version: "%s"
  version_base: "%s"
  build_time: "2026-05-21T00:00:00Z"
  deployment_environment: unknown-darwin
  vcs: git
prompt_env:
  platform: darwin
  shell: zsh
  os_version: "Darwin 24.4.0"
  working_dir: "/Users/test/project"
process:
  constrained_memory: 34359738368
  rss_range: [300000000, 500000000]
  heap_total_range: [40000000, 80000000]
  heap_used_range: [100000000, 200000000]
shared_pool:
  max_body_bytes: 2097152
  billing_cch_mode: strip
  message_beta_profile: claude_code_2_1_170_subscription_1m
account_identities:
  "301":
    device_id: "%s"
    account_uuid_hash: "scoped_hmac_ref:key_id=fixture;scope=account-ref;version=1;value=acct301"
    email_hash: "scoped_hmac_ref:key_id=fixture;scope=email-ref;version=1;value=email301"
    account_hash: "scoped_hmac_ref:key_id=fixture;scope=account-partition;version=1;value=account301"
    persona_variant: "%s"
    session_policy: preserve_downstream_session_id
    policy_version: "%s"
  "201":
    device_id: "%s"
    account_uuid_hash: "scoped_hmac_ref:key_id=fixture;scope=account-ref;version=1;value=acct201"
    email_hash: "scoped_hmac_ref:key_id=fixture;scope=email-ref;version=1;value=email201"
    account_hash: "scoped_hmac_ref:key_id=fixture;scope=account-partition;version=1;value=account201"
    persona_variant: "%s"
    session_policy: preserve_downstream_session_id
    policy_version: "%s"
egress_buckets:
  bucket-a:
    enabled: true
    proxy_url: %q
    proxy_identity_hash: "opaque:proxy-ref:v1:bucket-a"
    allowed_account_ids: ["301", "201"]
logging:
  level: error
  audit: false
`, upstreamURL, strings.Repeat("a", 64), ccGatewayAnthropicPolicyVersion, ccGatewayAnthropicPolicyVersion, strings.Repeat("b", 64), jointExpectedGatewayPersonaVariant, ccGatewayAnthropicPolicyVersion, strings.Repeat("c", 64), jointExpectedGatewayPersonaVariant, ccGatewayAnthropicPolicyVersion, proxyURL)
}

func jointGatewaySigningConfigYAML(upstreamURL, proxyURL string) string {
	return fmt.Sprintf(`mode: sub2api
server:
  port: {{PORT}}
  tls:
    cert: ""
    key: ""
upstream:
  url: %q
providers:
  anthropic: true
auth:
  gateway_token: gateway-token
  tokens: []
identity:
  device_id: "%s"
  email: canonical@example.com
env:
  platform: darwin
  platform_raw: darwin
  arch: arm64
  node_version: v24.3.0
  terminal: iTerm2.app
  package_managers: npm,pnpm
  runtimes: node
  is_running_with_bun: false
  is_ci: false
  is_claude_ai_auth: true
  version: "%s"
  version_base: "%s"
  build_time: "2026-05-21T00:00:00Z"
  deployment_environment: unknown-darwin
  vcs: git
prompt_env:
  platform: darwin
  shell: zsh
  os_version: "Darwin 24.4.0"
  working_dir: "/Users/test/project"
process:
  constrained_memory: 34359738368
  rss_range: [300000000, 500000000]
  heap_total_range: [40000000, 80000000]
  heap_used_range: [100000000, 200000000]
shared_pool:
  max_body_bytes: 2097152
  billing_cch_mode: sign
  signing_enabled: true
  signing_evidence_gates_approved: true
  message_beta_profile: claude_code_2_1_170_subscription_1m
account_identities:
  "301":
    device_id: "%s"
    account_uuid_hash: "scoped_hmac_ref:key_id=fixture;scope=account-ref;version=1;value=acct301"
    email_hash: "scoped_hmac_ref:key_id=fixture;scope=email-ref;version=1;value=email301"
    account_hash: "scoped_hmac_ref:key_id=fixture;scope=account-partition;version=1;value=account301"
    persona_variant: "%s"
    session_policy: preserve_downstream_session_id
    policy_version: "%s"
egress_buckets:
  bucket-a:
    enabled: true
    proxy_url: %q
    proxy_identity_hash: "opaque:proxy-ref:v1:bucket-a"
    allowed_account_ids: ["301"]
logging:
  level: error
  audit: false
`, upstreamURL, strings.Repeat("d", 64), ccGatewayAnthropicPolicyVersion, ccGatewayAnthropicPolicyVersion, strings.Repeat("e", 64), jointExpectedGatewayPersonaVariant, ccGatewayAnthropicPolicyVersion, proxyURL)
}

func jointGatewayDisabledConfigYAML(upstreamURL, proxyURL string) string {
	return fmt.Sprintf(`mode: sub2api
server:
  port: {{PORT}}
  tls:
    cert: ""
    key: ""
upstream:
  url: %q
providers:
  anthropic: true
auth:
  gateway_token: gateway-token
  tokens: []
identity:
  device_id: "%s"
  email: canonical@example.com
env:
  platform: darwin
  platform_raw: darwin
  arch: arm64
  node_version: v24.3.0
  terminal: iTerm2.app
  package_managers: npm,pnpm
  runtimes: node
  is_running_with_bun: false
  is_ci: false
  is_claude_ai_auth: true
  version: "%s"
  version_base: "%s"
  build_time: "2026-05-21T00:00:00Z"
  deployment_environment: unknown-darwin
  vcs: git
prompt_env:
  platform: darwin
  shell: zsh
  os_version: "Darwin 24.4.0"
  working_dir: "/Users/test/project"
process:
  constrained_memory: 34359738368
  rss_range: [300000000, 500000000]
  heap_total_range: [40000000, 80000000]
  heap_used_range: [100000000, 200000000]
shared_pool:
  max_body_bytes: 2097152
  billing_cch_mode: disabled
  message_beta_profile: claude_code_2_1_170_subscription_1m
account_identities:
  "301":
    device_id: "%s"
    account_uuid_hash: "scoped_hmac_ref:key_id=fixture;scope=account-ref;version=1;value=acct301"
    email_hash: "scoped_hmac_ref:key_id=fixture;scope=email-ref;version=1;value=email301"
    account_hash: "scoped_hmac_ref:key_id=fixture;scope=account-partition;version=1;value=account301"
    persona_variant: "%s"
    session_policy: preserve_downstream_session_id
    policy_version: "%s"
egress_buckets:
  bucket-a:
    enabled: true
    proxy_url: %q
    proxy_identity_hash: "opaque:proxy-ref:v1:bucket-a"
    allowed_account_ids: ["301"]
logging:
  level: error
  audit: false
`, upstreamURL, strings.Repeat("f", 64), ccGatewayAnthropicPolicyVersion, ccGatewayAnthropicPolicyVersion, strings.Repeat("g", 64), jointExpectedGatewayPersonaVariant, ccGatewayAnthropicPolicyVersion, proxyURL)
}

func newJointCaptureService(baseURL string, upstream *jointGatewayRecordingUpstream) *GatewayService {
	seedGatewayForwardingSettingsForTest()
	cfg := ccGatewayTestConfig(PlatformAnthropic)
	cfg.Gateway.MaxLineSize = defaultMaxLineSize
	cfg.Gateway.CCGateway.BaseURL = baseURL
	cfg.Gateway.CCGateway.Token = "gateway-token"
	cfg.Gateway.CCGateway.DefaultEgressBucket = ""
	return &GatewayService{
		cfg:             cfg,
		identityService: NewIdentityService(&identityCacheStub{}),
		httpUpstream:    upstream,
	}
}

func newJointOAuthAccount() *Account {
	account := newCCGatewayBoundaryAccount()
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	return account
}

func newJointAPIKeyAccount() *Account {
	account := newAnthropicAPIKeyAccountForTest()
	account.Extra["cc_gateway_enabled"] = "true"
	account.Extra["cc_gateway_canary_only"] = "false"
	account.Extra["cc_gateway_policy_version"] = ccGatewayAnthropicPolicyVersion
	account.Extra["cc_gateway_routes"] = "native_messages,native_count_tokens"
	account.Extra["cc_gateway_egress_bucket_enabled"] = "true"
	account.Extra["cc_gateway_egress_bucket"] = "bucket-a"
	return account
}

func newJointContext(path string) (*gin.Context, context.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx := context.Background()
	c.Request = httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
	c.Request.Header.Set("User-Agent", "claude-cli/99.9.9 (external, sdk-cli)")
	c.Request.Header.Set("Anthropic-Beta", "client-beta")
	c.Request.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	c.Request.Header.Set("X-Claude-Code-Session-Id", "99999999-8888-4777-8666-555555555555")
	return c, ctx, rec
}

func TestJointLocalCaptureAcceptanceArtifact(t *testing.T) {
	captureServer := startRawCaptureServer(t)
	proxyServer := startConnectProxyServer(t)
	stripGateway := startCCGatewayProcess(t, jointGatewayConfigYAML(captureServer.URL(), proxyServer.URL()))
	signingGateway := startCCGatewayProcess(t, jointGatewaySigningConfigYAML(captureServer.URL(), proxyServer.URL()))
	disabledGateway := startCCGatewayProcess(t, jointGatewayDisabledConfigYAML(captureServer.URL(), proxyServer.URL()))
	gatewayUpstream := &jointGatewayRecordingUpstream{client: &http.Client{Timeout: 10 * time.Second}}
	svc := newJointCaptureService(stripGateway.baseURL, gatewayUpstream)
	signingSvc := newJointCaptureService(signingGateway.baseURL, gatewayUpstream)

	report := jointCaptureReport{
		ExecutedAt:  time.Now().Format(time.RFC3339),
		ArtifactDir: jointLocalCaptureSafeDeliverableDir(t),
		GatewayMode: "sub2api",
	}

	run := func(name string, fn func() jointCaptureScenario) {
		scenario := fn()
		scenario.Name = name
		report.Scenarios = append(report.Scenarios, scenario)
	}

	run("oauth_native_messages_strip", func() jointCaptureScenario {
		captureServer.reset()
		gatewayUpstream.reset()
		account := newJointOAuthAccount()
		c, ctx, rec := newJointContext("/v1/messages")
		body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.146.abc; cch=12345;"}],"metadata":{"user_id":"{\"device_id\":\"client-device\",\"account_uuid\":\"acct-client\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
		result, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, http.StatusOK, rec.Code)
		hop1 := gatewayUpstream.popSingle(t)
		hop2 := captureServer.popSingle(t)
		sub2apiSummary := summarizeGatewayHop(hop1, body)
		upstreamSummary := summarizeRawCaptureHop(hop2)
		passed := !sub2apiSummary.BodyUnchangedFromClient &&
			sub2apiSummary.Body.BillingHeaderPresent &&
			sub2apiSummary.Body.CCHPresent &&
			!upstreamSummary.Body.BillingHeaderPresent &&
			!upstreamSummary.Body.CCHPresent &&
			upstreamSummary.HeaderValuesSummary["User-Agent"] == jointExpectedGatewayUserAgent
		return jointCaptureScenario{
			Category:                 "sub2api_joint",
			Route:                    "/v1/messages?beta=true",
			PolicyDecision:           "forward_strip",
			SelectedAccountIDRef:     jointHashText(strconv.FormatInt(account.ID, 10)),
			EgressBucketID:           "bucket-a",
			PolicyVersion:            ccGatewayAnthropicPolicyVersion,
			ResponseStatus:           rec.Code,
			ClientHeaderOrder:        []string{"User-Agent", "Anthropic-Beta", "Accept-Encoding", "X-Claude-Code-Session-Id"},
			ClientBodyRef:            jointBodyRef(body),
			Sub2APIToGateway:         &sub2apiSummary,
			GatewayToUpstream:        &upstreamSummary,
			RequestCount:             hop2Count(hop1, hop2),
			FailClosed:               false,
			NoRealUpstream:           isLoopbackHost(hop1.Host) && isLoopbackHost(rawCaptureHost(hop2.Headers.Get("Host"))),
			NoNativeFallback:         hop1.ProxyURL == "" && !hop1.TLSProfileUsed,
			Sub2APIFinalMutation:     !sub2apiSummary.BodyUnchangedFromClient,
			CCGatewayOwnsFinalOutput: !upstreamSummary.Body.BillingHeaderPresent && !upstreamSummary.Body.CCHPresent && upstreamSummary.HeaderValuesSummary["User-Agent"] == jointExpectedGatewayUserAgent,
			Passed:                   passed,
			Notes: []string{
				"sub2api->gateway rewrites metadata.user_id session to a server-issued UUID-like value before CC Gateway final-output handling",
				"gateway final persona is canonical Claude Code 2.1.170 subscription profile",
			},
		}
	})

	run("oauth_native_count_tokens_deferred", func() jointCaptureScenario {
		captureServer.reset()
		gatewayUpstream.reset()
		account := newJointOAuthAccount()
		c, ctx, rec := newJointContext("/v1/messages/count_tokens")
		body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
		err := svc.ForwardCountTokens(ctx, c, account, parseAnthropicRequestForTest(t, body))
		require.Error(t, err)
		hop1 := gatewayUpstream.popSingle(t)
		require.Zero(t, captureServer.count())
		sub2apiSummary := summarizeGatewayHop(hop1, body)
		return jointCaptureScenario{
			Category:                 "sub2api_joint",
			Route:                    "/v1/messages/count_tokens?beta=true",
			PolicyDecision:           "defer_block",
			SelectedAccountIDRef:     jointHashText(strconv.FormatInt(account.ID, 10)),
			EgressBucketID:           "bucket-a",
			PolicyVersion:            ccGatewayAnthropicPolicyVersion,
			ResponseStatus:           rec.Code,
			ResponseErrorKind:        extractErrorTypeFromBody(rec.Body.Bytes()),
			ResponseErrorCode:        extractErrorCodeFromBody(rec.Body.Bytes()),
			ClientHeaderOrder:        []string{"User-Agent", "Anthropic-Beta", "Accept-Encoding", "X-Claude-Code-Session-Id"},
			ClientBodyRef:            jointBodyRef(body),
			Sub2APIToGateway:         &sub2apiSummary,
			RequestCount:             1,
			FailClosed:               rec.Code == http.StatusForbidden,
			NoRealUpstream:           isLoopbackHost(hop1.Host),
			NoNativeFallback:         captureServer.count() == 0,
			Sub2APIFinalMutation:     sub2apiSummary.BodyUnchangedFromClient,
			CCGatewayOwnsFinalOutput: true,
			Passed:                   rec.Code == http.StatusForbidden && extractErrorCodeFromBody(rec.Body.Bytes()) == "count_tokens_deferred",
			Notes:                    []string{"route is explicitly deferred in first wave; no upstream request observed"},
		}
	})

	run("oauth_native_messages_sign_primary", func() jointCaptureScenario {
		captureServer.reset()
		gatewayUpstream.reset()
		account := newJointOAuthAccount()
		c, ctx, rec := newJointContext("/v1/messages")
		body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"metadata":{"user_id":"{\"device_id\":\"client-device\",\"account_uuid\":\"acct-client\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello sign lane"}]}]}`)
		result, err := signingSvc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, http.StatusOK, rec.Code)
		hop1 := gatewayUpstream.popSingle(t)
		hop2 := captureServer.popSingle(t)
		sub2apiSummary := summarizeGatewayHop(hop1, body)
		upstreamSummary := summarizeRawCaptureHop(hop2)
		upstreamBody := string(hop2.Body)
		passed := !sub2apiSummary.BodyUnchangedFromClient &&
			!sub2apiSummary.Body.BillingHeaderPresent &&
			!sub2apiSummary.Body.CCHPresent &&
			upstreamSummary.Body.BillingHeaderPresent &&
			upstreamSummary.Body.CCHPresent &&
			!strings.Contains(upstreamBody, "cch=00000;") &&
			regexp.MustCompile(`cc_version=2\.1\.170\.[a-f0-9]{3}`).MatchString(upstreamBody) &&
			upstreamSummary.HeaderValuesSummary["User-Agent"] == jointExpectedGatewayUserAgent
		return jointCaptureScenario{
			Category:                 "sub2api_joint",
			Route:                    "/v1/messages?beta=true",
			PolicyDecision:           "forward_sign_primary",
			SelectedAccountIDRef:     jointHashText(strconv.FormatInt(account.ID, 10)),
			EgressBucketID:           "bucket-a",
			PolicyVersion:            ccGatewayAnthropicPolicyVersion,
			ResponseStatus:           rec.Code,
			ClientHeaderOrder:        []string{"User-Agent", "Anthropic-Beta", "Accept-Encoding", "X-Claude-Code-Session-Id"},
			ClientBodyRef:            jointBodyRef(body),
			Sub2APIToGateway:         &sub2apiSummary,
			GatewayToUpstream:        &upstreamSummary,
			RequestCount:             hop2Count(hop1, hop2),
			FailClosed:               false,
			NoRealUpstream:           isLoopbackHost(hop1.Host) && isLoopbackHost(rawCaptureHost(hop2.Headers.Get("Host"))),
			NoNativeFallback:         hop1.ProxyURL == "" && !hop1.TLSProfileUsed,
			Sub2APIFinalMutation:     !sub2apiSummary.BodyUnchangedFromClient,
			CCGatewayOwnsFinalOutput: upstreamSummary.Body.BillingHeaderPresent && upstreamSummary.Body.CCHPresent && upstreamSummary.HeaderValuesSummary["User-Agent"] == jointExpectedGatewayUserAgent,
			Passed:                   passed,
			Notes: []string{
				"sub2api->gateway body is pre-final with no billing/CCH material",
				"cc gateway generated billing block, cc_version suffix, CCH, canonical persona, and post-sign verifier passed before localhost upstream capture",
			},
		}
	})

	run("apikey_native_messages_strip", func() jointCaptureScenario {
		captureServer.reset()
		gatewayUpstream.reset()
		account := newJointAPIKeyAccount()
		c, ctx, rec := newJointContext("/v1/messages")
		body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.146.abc; cch=12345;"}],"metadata":{"user_id":"{\"device_id\":\"client-device\",\"account_uuid\":\"acct-client\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
		result, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, http.StatusOK, rec.Code)
		hop1 := gatewayUpstream.popSingle(t)
		hop2 := captureServer.popSingle(t)
		sub2apiSummary := summarizeGatewayHop(hop1, body)
		upstreamSummary := summarizeRawCaptureHop(hop2)
		passed := !sub2apiSummary.BodyUnchangedFromClient && !upstreamSummary.Body.BillingHeaderPresent && !upstreamSummary.Body.CCHPresent && upstreamSummary.HeaderValuesSummary["x-api-key"] != ""
		return jointCaptureScenario{
			Category:                 "sub2api_joint",
			Route:                    "/v1/messages?beta=true",
			PolicyDecision:           "forward_strip",
			SelectedAccountIDRef:     jointHashText(strconv.FormatInt(account.ID, 10)),
			EgressBucketID:           "bucket-a",
			PolicyVersion:            ccGatewayAnthropicPolicyVersion,
			ResponseStatus:           rec.Code,
			ClientHeaderOrder:        []string{"User-Agent", "Anthropic-Beta", "Accept-Encoding", "X-Claude-Code-Session-Id"},
			ClientBodyRef:            jointBodyRef(body),
			Sub2APIToGateway:         &sub2apiSummary,
			GatewayToUpstream:        &upstreamSummary,
			RequestCount:             hop2Count(hop1, hop2),
			FailClosed:               false,
			NoRealUpstream:           isLoopbackHost(hop1.Host) && isLoopbackHost(rawCaptureHost(hop2.Headers.Get("Host"))),
			NoNativeFallback:         hop1.ProxyURL == "" && !hop1.TLSProfileUsed,
			Sub2APIFinalMutation:     !sub2apiSummary.BodyUnchangedFromClient,
			CCGatewayOwnsFinalOutput: !upstreamSummary.Body.BillingHeaderPresent && !upstreamSummary.Body.CCHPresent,
			Passed:                   passed,
			Notes: []string{
				"anthropic api-key passthrough is included for /v1/messages in first wave",
				"server-issued session mapping happens before gateway strips billing markers",
			},
		}
	})

	run("apikey_native_count_tokens_deferred", func() jointCaptureScenario {
		captureServer.reset()
		gatewayUpstream.reset()
		account := newJointAPIKeyAccount()
		c, ctx, rec := newJointContext("/v1/messages/count_tokens")
		body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
		err := svc.ForwardCountTokens(ctx, c, account, parseAnthropicRequestForTest(t, body))
		require.Error(t, err)
		hop1 := gatewayUpstream.popSingle(t)
		require.Zero(t, captureServer.count())
		sub2apiSummary := summarizeGatewayHop(hop1, body)
		return jointCaptureScenario{
			Category:                 "sub2api_joint",
			Route:                    "/v1/messages/count_tokens?beta=true",
			PolicyDecision:           "defer_block",
			SelectedAccountIDRef:     jointHashText(strconv.FormatInt(account.ID, 10)),
			EgressBucketID:           "bucket-a",
			PolicyVersion:            ccGatewayAnthropicPolicyVersion,
			ResponseStatus:           rec.Code,
			ResponseErrorKind:        extractErrorTypeFromBody(rec.Body.Bytes()),
			ResponseErrorCode:        extractErrorCodeFromBody(rec.Body.Bytes()),
			ClientHeaderOrder:        []string{"User-Agent", "Anthropic-Beta", "Accept-Encoding", "X-Claude-Code-Session-Id"},
			ClientBodyRef:            jointBodyRef(body),
			Sub2APIToGateway:         &sub2apiSummary,
			RequestCount:             1,
			FailClosed:               rec.Code == http.StatusForbidden,
			NoRealUpstream:           isLoopbackHost(hop1.Host),
			NoNativeFallback:         captureServer.count() == 0,
			Sub2APIFinalMutation:     sub2apiSummary.BodyUnchangedFromClient,
			CCGatewayOwnsFinalOutput: true,
			Passed:                   rec.Code == http.StatusForbidden && extractErrorCodeFromBody(rec.Body.Bytes()) == "count_tokens_deferred",
			Notes:                    []string{"anthropic api-key count_tokens remains deferred; no native fallback observed"},
		}
	})

	run("openai_chat_completions_to_anthropic", func() jointCaptureScenario {
		captureServer.reset()
		gatewayUpstream.reset()
		account := newJointOAuthAccount()
		c, ctx, rec := newJointContext("/v1/chat/completions")
		body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"messages":[{"role":"user","content":"hello"}]}`)
		parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-sonnet-4-6", Stream: false}
		result, err := svc.ForwardAsChatCompletions(ctx, c, account, body, parsed)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, http.StatusOK, rec.Code)
		hop1 := gatewayUpstream.popSingle(t)
		hop2 := captureServer.popSingle(t)
		sub2apiSummary := summarizeGatewayHop(hop1, body)
		upstreamSummary := summarizeRawCaptureHop(hop2)
		passed := !sub2apiSummary.Body.BillingHeaderPresent && !sub2apiSummary.Body.CCHPresent && sub2apiSummary.Body.Metadata == nil && upstreamSummary.Body.Metadata != nil && !upstreamSummary.Body.BillingHeaderPresent
		return jointCaptureScenario{
			Category:                 "sub2api_joint",
			Route:                    "/v1/chat/completions -> /v1/messages?beta=true",
			PolicyDecision:           "forward_strip",
			SelectedAccountIDRef:     jointHashText(strconv.FormatInt(account.ID, 10)),
			EgressBucketID:           "bucket-a",
			PolicyVersion:            ccGatewayAnthropicPolicyVersion,
			ResponseStatus:           rec.Code,
			ClientHeaderOrder:        []string{"User-Agent", "Anthropic-Beta", "Accept-Encoding", "X-Claude-Code-Session-Id"},
			ClientBodyRef:            jointBodyRef(body),
			Sub2APIToGateway:         &sub2apiSummary,
			GatewayToUpstream:        &upstreamSummary,
			RequestCount:             hop2Count(hop1, hop2),
			FailClosed:               false,
			NoRealUpstream:           isLoopbackHost(hop1.Host) && isLoopbackHost(rawCaptureHost(hop2.Headers.Get("Host"))),
			NoNativeFallback:         hop1.ProxyURL == "" && !hop1.TLSProfileUsed,
			Sub2APIFinalMutation:     sub2apiSummary.Body.Metadata == nil,
			CCGatewayOwnsFinalOutput: upstreamSummary.Body.Metadata != nil,
			Passed:                   passed,
			Notes:                    []string{"Sub2API performs protocol conversion only; CC Gateway injects final metadata/session binding"},
		}
	})

	run("openai_responses_to_anthropic", func() jointCaptureScenario {
		captureServer.reset()
		gatewayUpstream.reset()
		account := newJointOAuthAccount()
		c, ctx, rec := newJointContext("/v1/responses")
		body := []byte(`{"model":"claude-sonnet-4-6","stream":false,"input":"hello"}`)
		parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-sonnet-4-6", Stream: false}
		result, err := svc.ForwardAsResponses(ctx, c, account, body, parsed)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, http.StatusOK, rec.Code)
		hop1 := gatewayUpstream.popSingle(t)
		hop2 := captureServer.popSingle(t)
		sub2apiSummary := summarizeGatewayHop(hop1, body)
		upstreamSummary := summarizeRawCaptureHop(hop2)
		passed := !sub2apiSummary.Body.BillingHeaderPresent && !sub2apiSummary.Body.CCHPresent && sub2apiSummary.Body.Metadata == nil && upstreamSummary.Body.Metadata != nil && !upstreamSummary.Body.BillingHeaderPresent
		return jointCaptureScenario{
			Category:                 "sub2api_joint",
			Route:                    "/v1/responses -> /v1/messages?beta=true",
			PolicyDecision:           "forward_strip",
			SelectedAccountIDRef:     jointHashText(strconv.FormatInt(account.ID, 10)),
			EgressBucketID:           "bucket-a",
			PolicyVersion:            ccGatewayAnthropicPolicyVersion,
			ResponseStatus:           rec.Code,
			ClientHeaderOrder:        []string{"User-Agent", "Anthropic-Beta", "Accept-Encoding", "X-Claude-Code-Session-Id"},
			ClientBodyRef:            jointBodyRef(body),
			Sub2APIToGateway:         &sub2apiSummary,
			GatewayToUpstream:        &upstreamSummary,
			RequestCount:             hop2Count(hop1, hop2),
			FailClosed:               false,
			NoRealUpstream:           isLoopbackHost(hop1.Host) && isLoopbackHost(rawCaptureHost(hop2.Headers.Get("Host"))),
			NoNativeFallback:         hop1.ProxyURL == "" && !hop1.TLSProfileUsed,
			Sub2APIFinalMutation:     sub2apiSummary.Body.Metadata == nil,
			CCGatewayOwnsFinalOutput: upstreamSummary.Body.Metadata != nil,
			Passed:                   passed,
			Notes:                    []string{"Sub2API responses conversion path leaves final metadata/session ownership to CC Gateway"},
		}
	})

	run("event_logging_v2_suppressed_local", func() jointCaptureScenario {
		response := serveLocalEventRoute(http.MethodPost, "/api/event_logging/v2/batch")
		return jointCaptureScenario{
			Category:                 "sub2api_local_policy",
			Route:                    "/api/event_logging/v2/batch",
			PolicyDecision:           "suppress_local",
			ResponseStatus:           response.Code,
			RequestCount:             0,
			FailClosed:               false,
			NoRealUpstream:           true,
			NoNativeFallback:         true,
			Sub2APIFinalMutation:     true,
			CCGatewayOwnsFinalOutput: false,
			Passed:                   response.Code == http.StatusOK,
			Notes:                    []string{"legacy telemetry is suppressed before any CC Gateway routing"},
		}
	})

	run("event_logging_legacy_suppressed_local", func() jointCaptureScenario {
		response := serveLocalEventRoute(http.MethodPost, "/api/event_logging/batch")
		return jointCaptureScenario{
			Category:                 "sub2api_local_policy",
			Route:                    "/api/event_logging/batch",
			PolicyDecision:           "suppress_local",
			ResponseStatus:           response.Code,
			RequestCount:             0,
			FailClosed:               false,
			NoRealUpstream:           true,
			NoNativeFallback:         true,
			Sub2APIFinalMutation:     true,
			CCGatewayOwnsFinalOutput: false,
			Passed:                   response.Code == http.StatusOK,
			Notes:                    []string{"legacy telemetry is suppressed before any CC Gateway routing"},
		}
	})

	run("unknown_event_endpoint_blocked", func() jointCaptureScenario {
		response := serveLocalEventRoute(http.MethodPost, "/api/event_logging/v3/batch")
		return jointCaptureScenario{
			Category:                 "sub2api_local_policy",
			Route:                    "/api/event_logging/v3/batch",
			PolicyDecision:           "block",
			ResponseStatus:           response.Code,
			RequestCount:             0,
			FailClosed:               response.Code == http.StatusNotFound,
			NoRealUpstream:           true,
			NoNativeFallback:         true,
			Sub2APIFinalMutation:     true,
			CCGatewayOwnsFinalOutput: false,
			Passed:                   response.Code == http.StatusNotFound,
			Notes:                    []string{"unknown event route is blocked and never reaches CC Gateway"},
		}
	})

	run("gateway_control_plane_invalid_token_401", func() jointCaptureScenario {
		captureServer.reset()
		resp := doGatewayJSON(t, stripGateway.baseURL, "/v1/messages?beta=true", directGatewayHeaders("301", "oauth", true, false), map[string]any{
			"metadata": map[string]any{"user_id": `{"session_id":"99999999-8888-4777-8666-555555555555"}`},
			"messages": []map[string]any{{"role": "user", "content": "hello"}},
		})
		return directGatewayScenario("gateway_control_plane_invalid_token_401", "/v1/messages?beta=true", "control_plane_401", "301", resp, captureServer.count() == 0)
	})

	run("gateway_control_plane_missing_identity_403", func() jointCaptureScenario {
		captureServer.reset()
		resp := doGatewayJSON(t, stripGateway.baseURL, "/v1/messages?beta=true", directGatewayHeaders("999", "oauth", false, false), map[string]any{
			"metadata": map[string]any{"user_id": `{"session_id":"99999999-8888-4777-8666-555555555555"}`},
			"messages": []map[string]any{{"role": "user", "content": "hello"}},
		})
		return directGatewayScenario("gateway_control_plane_missing_identity_403", "/v1/messages?beta=true", "control_plane_403", "999", resp, captureServer.count() == 0)
	})

	run("gateway_control_plane_missing_egress_bucket_400", func() jointCaptureScenario {
		captureServer.reset()
		resp := doGatewayJSON(t, stripGateway.baseURL, "/v1/messages?beta=true", directGatewayHeadersWithoutBucket("301", "oauth"), map[string]any{
			"metadata": map[string]any{"user_id": `{"session_id":"99999999-8888-4777-8666-555555555555"}`},
			"messages": []map[string]any{{"role": "user", "content": "hello"}},
		})
		return directGatewayScenario("gateway_control_plane_missing_egress_bucket_400", "/v1/messages?beta=true", "control_plane_400", "301", resp, captureServer.count() == 0)
	})

	run("gateway_unknown_endpoint_404", func() jointCaptureScenario {
		captureServer.reset()
		resp := doGatewayJSON(t, stripGateway.baseURL, "/v1/unknown?beta=true", directGatewayHeaders("301", "oauth", false, false), map[string]any{
			"metadata": map[string]any{"user_id": `{"session_id":"99999999-8888-4777-8666-555555555555"}`},
			"messages": []map[string]any{{"role": "user", "content": "hello"}},
		})
		return directGatewayScenario("gateway_unknown_endpoint_404", "/v1/unknown?beta=true", "block_404", "301", resp, captureServer.count() == 0)
	})

	run("gateway_strip_verifier_failure_400", func() jointCaptureScenario {
		captureServer.reset()
		resp := doGatewayJSON(t, stripGateway.baseURL, "/v1/messages?beta=true", directGatewayHeaders("301", "oauth", false, false), map[string]any{
			"metadata": map[string]any{"user_id": `{"session_id":"99999999-8888-4777-8666-555555555555"}`},
			"messages": []map[string]any{{"role": "user", "content": "literal marker retained"}},
			"system":   []map[string]any{{"type": "text", "text": "literal cch=12345 must fail verifier"}},
		})
		return directGatewayScenario("gateway_strip_verifier_failure_400", "/v1/messages?beta=true", "control_plane_400", "301", resp, captureServer.count() == 0)
	})

	run("gateway_signing_untrusted_cch_fail_closed_403", func() jointCaptureScenario {
		captureServer.reset()
		resp := doGatewayJSON(t, signingGateway.baseURL, "/v1/messages?beta=true", directGatewayHeaders("301", "oauth", false, false), map[string]any{
			"metadata": map[string]any{"user_id": `{"session_id":"99999999-8888-4777-8666-555555555555"}`},
			"messages": []map[string]any{{"role": "user", "content": "literal cch=12345 must fail closed"}},
		})
		return directGatewayScenario("gateway_signing_untrusted_cch_fail_closed_403", "/v1/messages?beta=true", "control_plane_403", "301", resp, captureServer.count() == 0)
	})

	run("gateway_billing_mode_disabled_403", func() jointCaptureScenario {
		captureServer.reset()
		resp := doGatewayJSON(t, disabledGateway.baseURL, "/v1/messages?beta=true", directGatewayHeaders("301", "oauth", false, false), map[string]any{
			"metadata": map[string]any{"user_id": `{"session_id":"99999999-8888-4777-8666-555555555555"}`},
			"messages": []map[string]any{{"role": "user", "content": "hello"}},
		})
		return directGatewayScenario("gateway_billing_mode_disabled_403", "/v1/messages?beta=true", "control_plane_403", "301", resp, captureServer.count() == 0)
	})

	report.NoRealUpstream = true
	report.NoRawSecrets = true
	report.NoNativeFallback = true
	report.Sub2APINotFinalMutating = true
	report.CCGatewayFinalOwner = true
	report.NegativeCasesFailClosed = true
	for _, scenario := range report.Scenarios {
		report.NoRealUpstream = report.NoRealUpstream && scenario.NoRealUpstream
		report.NoNativeFallback = report.NoNativeFallback && scenario.NoNativeFallback
		if strings.HasPrefix(scenario.Category, "sub2api_joint") {
			report.Sub2APINotFinalMutating = report.Sub2APINotFinalMutating && scenario.Sub2APIFinalMutation
			report.CCGatewayFinalOwner = report.CCGatewayFinalOwner && scenario.CCGatewayOwnsFinalOutput
		}
		if strings.Contains(scenario.PolicyDecision, "control_plane") || scenario.PolicyDecision == "block" || scenario.PolicyDecision == "defer_block" {
			report.NegativeCasesFailClosed = report.NegativeCasesFailClosed && scenario.FailClosed
		}
	}

	jsonBytes, markdownBytes := writeJointLocalCaptureArtifacts(t, &report)
	report.RedactionScan = scanJointLocalCaptureArtifacts(jsonBytes, markdownBytes)
	report.NoRawSecrets = report.RedactionScan.Passed
	jsonBytes, markdownBytes = rewriteJointLocalCaptureArtifacts(t, &report)
	report.RedactionScan = scanJointLocalCaptureArtifacts(jsonBytes, markdownBytes)
	report.NoRawSecrets = report.RedactionScan.Passed
	_, _ = rewriteJointLocalCaptureArtifacts(t, &report)

	for _, scenario := range report.Scenarios {
		if !scenario.Passed {
			t.Fatalf("joint local capture scenario failed: %s", scenario.Name)
		}
	}
	if !report.RedactionScan.Passed {
		t.Fatalf("redaction scan failed: %+v", report.RedactionScan)
	}
}

func directGatewayScenario(name, route, decision, accountID string, resp gatewayHTTPResponse, noUpstream bool) jointCaptureScenario {
	statusOK := resp.Status >= 400
	return jointCaptureScenario{
		Name:                     name,
		Category:                 "gateway_direct",
		Route:                    route,
		PolicyDecision:           decision,
		SelectedAccountIDRef:     jointHashText(accountID),
		EgressBucketID:           resp.EgressBucket,
		PolicyVersion:            ccGatewayAnthropicPolicyVersion,
		ResponseStatus:           resp.Status,
		ResponseErrorKind:        resp.Headers.Get(ccGatewayErrorKindHeader),
		ResponseErrorCode:        resp.Headers.Get(ccGatewayErrorCodeHeader),
		ClientHeaderOrder:        resp.HeaderOrder,
		RequestCount:             0,
		FailClosed:               statusOK,
		NoRealUpstream:           true,
		NoNativeFallback:         noUpstream,
		Sub2APIFinalMutation:     true,
		CCGatewayOwnsFinalOutput: false,
		Passed:                   statusOK && resp.Headers.Get(ccGatewayErrorKindHeader) == "control-plane" && resp.Headers.Get(ccGatewayErrorCodeHeader) != "",
		Notes:                    []string{"direct gateway control-plane probe; no upstream request observed"},
	}
}

type gatewayHTTPResponse struct {
	Status       int
	Headers      http.Header
	Body         []byte
	HeaderOrder  []string
	EgressBucket string
}

func doGatewayJSON(t *testing.T, baseURL, path string, headerPairs [][2]string, body any) gatewayHTTPResponse {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	headerOrder := []string{"Content-Type"}
	var egressBucket string
	for _, pair := range headerPairs {
		req.Header.Set(pair[0], pair[1])
		headerOrder = append(headerOrder, pair[0])
		if strings.EqualFold(pair[0], ccGatewayEgressBucketHeader) {
			egressBucket = pair[1]
		}
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return gatewayHTTPResponse{Status: resp.StatusCode, Headers: jointCloneHeaders(resp.Header), Body: bodyBytes, HeaderOrder: headerOrder, EgressBucket: egressBucket}
}

func directGatewayHeaders(accountID, tokenType string, invalidGatewayToken, useAPIKey bool) [][2]string {
	headers := [][2]string{
		{"X-CC-Gateway-Token", "gateway-token"},
		{"X-CC-Provider", PlatformAnthropic},
		{"X-CC-Account-Id", accountID},
		{"X-CC-Egress-Bucket", "bucket-a"},
		{"X-CC-Policy-Version", ccGatewayAnthropicPolicyVersion},
		{"X-Claude-Code-Session-Id", "99999999-8888-4777-8666-555555555555"},
		{"X-CC-Token-Type", tokenType},
	}
	if invalidGatewayToken {
		headers[0][1] = "wrong-token"
	}
	if useAPIKey {
		headers = append(headers, [2]string{"X-API-Key", "selected-api-key"})
	} else {
		headers = append(headers, [2]string{"Authorization", "Bearer selected-token"})
	}
	return headers
}

func directGatewayHeadersWithoutBucket(accountID, tokenType string) [][2]string {
	headers := directGatewayHeaders(accountID, tokenType, false, false)
	filtered := make([][2]string, 0, len(headers))
	for _, pair := range headers {
		if strings.EqualFold(pair[0], ccGatewayEgressBucketHeader) {
			continue
		}
		filtered = append(filtered, pair)
	}
	return filtered
}

func serveLocalEventRoute(method, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/event_logging/batch", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/api/event_logging/v2/batch", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func serializeRequestHeaderOrder(req *http.Request, body []byte) []string {
	clone := req.Clone(req.Context())
	clone.Header = jointCloneHeaders(req.Header)
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.ContentLength = int64(len(body))
	var buf bytes.Buffer
	_ = clone.Write(&buf)
	return parseSerializedHeaderOrder(buf.Bytes())
}

func parseSerializedHeaderOrder(raw []byte) []string {
	lines := strings.Split(string(raw), "\r\n")
	order := make([]string, 0, len(lines))
	for i, line := range lines {
		if i == 0 {
			continue
		}
		if line == "" {
			break
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			order = append(order, line[:idx])
		}
	}
	return order
}

func summarizeGatewayHop(captured recordingGatewayRequest, clientBody []byte) jointCaptureHopSummary {
	bodySummary := summarizeBody(captured.Body, headerFirst(captured.Headers, "X-Claude-Code-Session-Id"))
	return jointCaptureHopSummary{
		URLHost:                 captured.Host,
		Route:                   routePath(captured.URL),
		HeaderKeyOrder:          captured.HeaderKeyOrder,
		HeaderValuesSummary:     summarizeHeaders(captured.Headers, captured.HeaderKeyOrder),
		Body:                    &bodySummary,
		RequestCount:            1,
		ProxyURLUsed:            captured.ProxyURL,
		TLSProfileUsed:          captured.TLSProfileUsed,
		BodyUnchangedFromClient: bytes.Equal(clientBody, captured.Body),
	}
}

func summarizeRawCaptureHop(captured rawCaptureRequest) jointCaptureHopSummary {
	bodySummary := summarizeBody(captured.Body, headerFirst(captured.Headers, "X-Claude-Code-Session-Id"))
	return jointCaptureHopSummary{
		URLHost:             rawCaptureHost(captured.Headers.Get("Host")),
		Route:               captured.Path,
		HeaderKeyOrder:      captured.HeaderKeyOrder,
		HeaderValuesSummary: summarizeHeaders(captured.Headers, captured.HeaderKeyOrder),
		Body:                &bodySummary,
		RequestCount:        1,
	}
}

func summarizeBody(body []byte, sessionHeader string) jointCaptureBodySummary {
	summary := jointCaptureBodySummary{
		BodyRef:              jointBodyRef(body),
		SizeBytes:            len(body),
		BillingHeaderPresent: bytes.Contains(bytes.ToLower(body), []byte("x-anthropic-billing-header")),
		CCHPresent:           bytes.Contains(bytes.ToLower(body), []byte("cch=")),
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return summary
	}
	summary.TopLevelKeys = jointSortedKeys(parsed)
	if messages, ok := parsed["messages"].([]any); ok {
		summary.MessageCount = len(messages)
	}
	switch system := parsed["system"].(type) {
	case []any:
		summary.SystemCount = len(system)
	case string:
		if strings.TrimSpace(system) != "" {
			summary.SystemCount = 1
		}
	}
	metadata, ok := parsed["metadata"].(map[string]any)
	if !ok {
		return summary
	}
	userIDRaw, _ := metadata["user_id"].(string)
	if userIDRaw == "" {
		return summary
	}
	var parsedUserID map[string]any
	if err := json.Unmarshal([]byte(userIDRaw), &parsedUserID); err != nil {
		return summary
	}
	fieldNames := jointSortedKeys(parsedUserID)
	fieldHashes := make(map[string]string, len(parsedUserID))
	for _, key := range fieldNames {
		fieldHashes[key] = jointHashText(fmt.Sprintf("%v", parsedUserID[key]))
	}
	summary.Metadata = &jointCaptureMetadataSummary{
		FieldNames:       fieldNames,
		FieldRefs:        fieldHashes,
		UserIDValueRef:   jointHashText(userIDRaw),
		SessionHeaderRef: jointHashText(sessionHeader),
	}
	return summary
}

func summarizeHeaders(headers http.Header, order []string) map[string]string {
	seen := map[string]struct{}{}
	summary := map[string]string{}
	for _, key := range order {
		values := headerValuesAnyCase(headers, key)
		summary[key] = summarizeHeaderValue(key, values)
		seen[strings.ToLower(key)] = struct{}{}
	}
	for key, values := range headers {
		if _, ok := seen[strings.ToLower(key)]; ok {
			continue
		}
		summary[key] = summarizeHeaderValue(key, values)
	}
	return summary
}

func headerValuesAnyCase(headers http.Header, key string) []string {
	for existingKey, values := range headers {
		if strings.EqualFold(existingKey, key) {
			return append([]string(nil), values...)
		}
	}
	return nil
}

func extractErrorCodeFromBody(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	errorObj, _ := payload["error"].(map[string]any)
	code, _ := errorObj["code"].(string)
	return code
}

func extractErrorTypeFromBody(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	errorObj, _ := payload["error"].(map[string]any)
	errType, _ := errorObj["type"].(string)
	return errType
}

func summarizeHeaderValue(key string, values []string) string {
	joined := strings.Join(values, ",")
	lower := strings.ToLower(key)
	switch lower {
	case "authorization", "x-api-key", "x-cc-gateway-token", "x-cc-account-id", "x-cc-account-email", "x-cc-account-uuid", "x-cc-organization-uuid", "x-claude-code-session-id", "x-request-id":
		if joined == "" {
			return ""
		}
		return jointHashText(joined)
	case "host":
		return redactLoopback(joined)
	default:
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
			return jointHashText(joined)
		}
		return joined
	}
}

func rewriteJointLocalCaptureArtifacts(t *testing.T, report *jointCaptureReport) ([]byte, []byte) {
	return writeJointLocalCaptureArtifacts(t, report)
}

func writeJointLocalCaptureArtifacts(t *testing.T, report *jointCaptureReport) ([]byte, []byte) {
	t.Helper()
	outDir := report.ArtifactDir
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	jsonBytes, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	jsonPath := filepath.Join(outDir, "joint_local_capture_summary.redacted.json")
	require.NoError(t, os.WriteFile(jsonPath, jsonBytes, 0o644))

	var md strings.Builder
	md.WriteString("# Joint local capture acceptance\n\n")
	md.WriteString("- Executed at: `" + report.ExecutedAt + "`\n")
	md.WriteString("- Gateway mode: `" + report.GatewayMode + "`\n")
	md.WriteString("- No real upstream: `" + strconv.FormatBool(report.NoRealUpstream) + "`\n")
	md.WriteString("- No raw secrets in safe deliverable: `" + strconv.FormatBool(report.NoRawSecrets) + "`\n")
	md.WriteString("- No native fallback: `" + strconv.FormatBool(report.NoNativeFallback) + "`\n")
	md.WriteString("- Sub2API not final-mutating on CC Gateway routes: `" + strconv.FormatBool(report.Sub2APINotFinalMutating) + "`\n")
	md.WriteString("- CC Gateway final-output owner: `" + strconv.FormatBool(report.CCGatewayFinalOwner) + "`\n")
	md.WriteString("- Negative cases fail closed: `" + strconv.FormatBool(report.NegativeCasesFailClosed) + "`\n\n")
	for _, scenario := range report.Scenarios {
		status := "FAIL"
		if scenario.Passed {
			status = "PASS"
		}
		md.WriteString("## " + scenario.Name + " - " + status + "\n")
		md.WriteString("- route: `" + scenario.Route + "`\n")
		md.WriteString("- decision: `" + scenario.PolicyDecision + "`\n")
		if scenario.SelectedAccountIDRef != "" {
			md.WriteString("- selected account id ref: `" + scenario.SelectedAccountIDRef + "`\n")
		}
		if scenario.EgressBucketID != "" {
			md.WriteString("- egress bucket: `" + scenario.EgressBucketID + "`\n")
		}
		if scenario.PolicyVersion != "" {
			md.WriteString("- policy version: `" + scenario.PolicyVersion + "`\n")
		}
		md.WriteString("- response status: `" + strconv.Itoa(scenario.ResponseStatus) + "`\n")
		if scenario.ResponseErrorCode != "" {
			md.WriteString("- control-plane: `" + scenario.ResponseErrorKind + "/" + scenario.ResponseErrorCode + "`\n")
		}
		md.WriteString("- request count: `" + strconv.Itoa(scenario.RequestCount) + "`\n")
		md.WriteString("- no real upstream: `" + strconv.FormatBool(scenario.NoRealUpstream) + "`\n")
		md.WriteString("- no native fallback: `" + strconv.FormatBool(scenario.NoNativeFallback) + "`\n")
		if scenario.Sub2APIToGateway != nil {
			md.WriteString("- sub2api->gateway route: `" + scenario.Sub2APIToGateway.Route + "`\n")
			md.WriteString("- sub2api->gateway body ref: `" + scenario.Sub2APIToGateway.Body.BodyRef + "`\n")
			md.WriteString("- sub2api->gateway billing/cch: `" + strconv.FormatBool(scenario.Sub2APIToGateway.Body.BillingHeaderPresent) + "/" + strconv.FormatBool(scenario.Sub2APIToGateway.Body.CCHPresent) + "`\n")
		}
		if scenario.GatewayToUpstream != nil {
			md.WriteString("- gateway->upstream route: `" + scenario.GatewayToUpstream.Route + "`\n")
			md.WriteString("- gateway->upstream body ref: `" + scenario.GatewayToUpstream.Body.BodyRef + "`\n")
			md.WriteString("- gateway->upstream billing/cch: `" + strconv.FormatBool(scenario.GatewayToUpstream.Body.BillingHeaderPresent) + "/" + strconv.FormatBool(scenario.GatewayToUpstream.Body.CCHPresent) + "`\n")
		}
		for _, note := range scenario.Notes {
			md.WriteString("- note: `" + note + "`\n")
		}
		md.WriteString("\n")
	}
	md.WriteString("## Redaction scan\n")
	md.WriteString("- passed: `" + strconv.FormatBool(report.RedactionScan.Passed) + "`\n")
	if len(report.RedactionScan.Hits) > 0 {
		md.WriteString("- hits: `" + strings.Join(report.RedactionScan.Hits, ", ") + "`\n")
	}
	markdownBytes := []byte(md.String())
	mdPath := filepath.Join(outDir, "README.md")
	require.NoError(t, os.WriteFile(mdPath, markdownBytes, 0o644))
	return jsonBytes, markdownBytes
}

func scanJointLocalCaptureArtifacts(jsonBytes, markdownBytes []byte) jointCaptureRedactionScan {
	combined := string(jsonBytes) + "\n" + string(markdownBytes)
	hits := make([]string, 0)
	patterns := make([]string, 0, len(jointLocalCaptureRedactionRules))
	for _, rule := range jointLocalCaptureRedactionRules {
		patterns = append(patterns, rule.Label)
		if strings.Contains(combined, rule.Needle) {
			hits = append(hits, rule.Label)
		}
	}
	return jointCaptureRedactionScan{Passed: len(hits) == 0, Patterns: patterns, Hits: hits}
}

func jointLocalCaptureSafeDeliverableDir(t *testing.T) string {
	t.Helper()
	backendRoot := repoBackendRootForCapture(t)
	date := time.Now().Format("2006-01-02")
	return filepath.Join(backendRoot, "..", "docs", "anti-ban", "captures", "real-baseline", date+"-"+jointLocalCaptureArtifactSlug, "safe-deliverable")
}

func repoBackendRootForCapture(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func jointBodyRef(b []byte) string {
	_ = b
	return "scoped_hmac_ref:key_id=joint_artifact_test;scope=joint_body_ref;version=1;value=redacted"
}

func jointHashText(s string) string {
	_ = s
	return "scoped_hmac_ref:key_id=joint_artifact_test;scope=joint_text_ref;version=1;value=redacted"
}

func jointSortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func jointCloneHeaders(h http.Header) http.Header {
	cloned := make(http.Header, len(h))
	for key, values := range h {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func headerFirst(headers http.Header, key string) string {
	values := headers.Values(key)
	if len(values) == 0 {
		return headers.Get(key)
	}
	return values[0]
}

func routePath(rawURL string) string {
	parsed, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil || parsed.URL == nil {
		return rawURL
	}
	return parsed.URL.RequestURI()
}

func rawCaptureHost(host string) string {
	if host == "" {
		return ""
	}
	return host
}

func isLoopbackHost(host string) bool {
	if host == "" {
		return true
	}
	hostOnly := host
	if strings.Contains(hostOnly, ":") {
		if parsedHost, _, err := net.SplitHostPort(hostOnly); err == nil {
			hostOnly = parsedHost
		}
	}
	if hostOnly == "localhost" || hostOnly == "127.0.0.1" || hostOnly == "::1" {
		return true
	}
	ip := net.ParseIP(hostOnly)
	return ip != nil && ip.IsLoopback()
}

func redactLoopback(host string) string {
	if isLoopbackHost(host) {
		return "loopback"
	}
	return host
}

func hop2Count(_ recordingGatewayRequest, _ rawCaptureRequest) int {
	return 1
}
