package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr       = ":8080"
	defaultMaxRequestBytes  = int64(8 * 1024 * 1024)
	defaultMaxResponseBytes = int64(32 * 1024 * 1024)
	defaultUpstreamTimeout  = 60 * time.Second
	maxImportAccounts       = 100
	maxImportProxies        = 100
)

type guardConfig struct {
	UpstreamURL      string
	UpstreamAPIKey   string
	GuardAPIKey      string
	MaxRequestBytes  int64
	MaxResponseBytes int64
	UpstreamTimeout  time.Duration
}

type guard struct {
	config   guardConfig
	upstream *url.URL
	client   *http.Client
}

func main() {
	config, listenAddr, err := configFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           newGuard(config, &http.Client{}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      70 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 * 1024,
	}
	log.Printf("nexus-agent-guard listening on %s", listenAddr)
	log.Fatal(server.ListenAndServe())
}

func configFromEnv() (guardConfig, string, error) {
	config := guardConfig{
		UpstreamURL:      strings.TrimSpace(os.Getenv("UPSTREAM_URL")),
		UpstreamAPIKey:   strings.TrimSpace(os.Getenv("UPSTREAM_API_KEY")),
		GuardAPIKey:      strings.TrimSpace(os.Getenv("GUARD_API_KEY")),
		MaxRequestBytes:  envInt64("MAX_REQUEST_BYTES", defaultMaxRequestBytes),
		MaxResponseBytes: envInt64("MAX_RESPONSE_BYTES", defaultMaxResponseBytes),
		UpstreamTimeout:  time.Duration(envInt64("UPSTREAM_TIMEOUT_SECONDS", int64(defaultUpstreamTimeout/time.Second))) * time.Second,
	}
	if err := validateConfig(config); err != nil {
		return guardConfig{}, "", err
	}
	listenAddr := strings.TrimSpace(os.Getenv("LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	return config, listenAddr, nil
}

func validateConfig(config guardConfig) error {
	upstream, err := url.Parse(config.UpstreamURL)
	if err != nil || upstream.Host == "" || (upstream.Scheme != "http" && upstream.Scheme != "https") {
		return errors.New("UPSTREAM_URL must be an absolute http/https origin")
	}
	if upstream.User != nil || (upstream.Path != "" && upstream.Path != "/") || upstream.RawQuery != "" || upstream.Fragment != "" {
		return errors.New("UPSTREAM_URL must not contain credentials, path, query, or fragment")
	}
	if len(config.UpstreamAPIKey) < 16 {
		return errors.New("UPSTREAM_API_KEY must contain at least 16 characters")
	}
	if len(config.GuardAPIKey) < 32 {
		return errors.New("GUARD_API_KEY must contain at least 32 characters")
	}
	if config.MaxRequestBytes < 1024 || config.MaxResponseBytes < 1024 {
		return errors.New("request and response byte limits must be at least 1024")
	}
	if config.UpstreamTimeout < time.Second || config.UpstreamTimeout > 5*time.Minute {
		return errors.New("UPSTREAM_TIMEOUT_SECONDS must be between 1 and 300")
	}
	return nil
}

func envInt64(name string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func newGuard(config guardConfig, client *http.Client) http.Handler {
	upstream, err := url.Parse(config.UpstreamURL)
	if err != nil {
		panic(err)
	}
	if client == nil {
		client = &http.Client{}
	}
	return &guard{config: config, upstream: upstream, client: client}
}

func (g *guard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
		return
	}

	if !g.authenticated(r) {
		writeError(w, http.StatusUnauthorized, "invalid guard key")
		return
	}
	if r.URL.RawPath != "" {
		writeError(w, http.StatusBadRequest, "encoded paths are not allowed")
		return
	}
	if !allowedPath(r.URL.Path) {
		writeError(w, http.StatusForbidden, "path is not allowed")
		return
	}
	if !allowedMethod(r.Method, r.URL.Path) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := validateQuery(r.Method, r.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var body []byte
	if r.Method == http.MethodPost {
		var err error
		body, err = g.readAndValidateImport(r)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errBodyTooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			writeError(w, status, err.Error())
			return
		}
	} else if r.ContentLength > 0 {
		writeError(w, http.StatusBadRequest, "GET requests must not contain a body")
		return
	}

	started := time.Now()
	status, responseBody, err := g.forward(r, body)
	if err != nil {
		log.Printf("request method=%s path=%s status=502 duration_ms=%d error=%q", r.Method, r.URL.Path, time.Since(started).Milliseconds(), err.Error())
		writeError(w, http.StatusBadGateway, "upstream request failed")
		return
	}
	log.Printf("request method=%s path=%s status=%d duration_ms=%d request_bytes=%d response_bytes=%d", r.Method, r.URL.Path, status, time.Since(started).Milliseconds(), len(body), len(responseBody))
	w.WriteHeader(status)
	_, _ = w.Write(responseBody)
}

func (g *guard) authenticated(r *http.Request) bool {
	values := r.Header.Values("X-Api-Key")
	if len(values) != 1 {
		return false
	}
	provided := strings.TrimSpace(values[0])
	return len(provided) == len(g.config.GuardAPIKey) && subtle.ConstantTimeCompare([]byte(provided), []byte(g.config.GuardAPIKey)) == 1
}

func allowedPath(path string) bool {
	switch path {
	case "/api/v1/admin/accounts", "/api/v1/admin/accounts/data", "/api/v1/admin/accounts/batch":
		return true
	default:
		return false
	}
}

func allowedMethod(method, path string) bool {
	switch path {
	case "/api/v1/admin/accounts":
		return method == http.MethodGet
	case "/api/v1/admin/accounts/data":
		return method == http.MethodGet || method == http.MethodPost
	case "/api/v1/admin/accounts/batch":
		return method == http.MethodPost
	default:
		return false
	}
}

func validateQuery(method string, requestURL *url.URL) error {
	query := requestURL.Query()
	if method == http.MethodPost && len(query) != 0 {
		return errors.New("query parameters are not allowed for imports")
	}
	allowed := map[string]bool{
		"group": true, "ids": true, "include_proxies": true, "page": true, "page_size": true,
		"platform": true, "privacy_mode": true, "search": true, "sort_by": true, "sort_order": true,
		"status": true, "type": true,
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 || len(values[0]) > 256 || strings.ContainsAny(values[0], "\r\n\x00") {
			return fmt.Errorf("query parameter %q is not allowed", key)
		}
	}
	if err := validateBoundedInteger(query, "page", 1, 10000); err != nil {
		return err
	}
	if err := validateBoundedInteger(query, "page_size", 1, 500); err != nil {
		return err
	}
	if value := query.Get("include_proxies"); value != "" && value != "true" && value != "false" {
		return errors.New("include_proxies must be true or false")
	}
	if value := query.Get("sort_order"); value != "" && value != "asc" && value != "desc" {
		return errors.New("sort_order must be asc or desc")
	}
	if value := query.Get("ids"); value != "" {
		for _, part := range strings.Split(value, ",") {
			if _, err := strconv.ParseInt(part, 10, 64); err != nil {
				return errors.New("ids must be comma-separated integers")
			}
		}
	}
	return nil
}

func validateBoundedInteger(query url.Values, name string, min, max int64) error {
	value := query.Get(name)
	if value == "" {
		return nil
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < min || number > max {
		return fmt.Errorf("%s must be between %d and %d", name, min, max)
	}
	return nil
}

var errBodyTooLarge = errors.New("request body exceeds configured limit")

func (g *guard) readAndValidateImport(r *http.Request) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, errors.New("content-type must be application/json")
	}
	if r.ContentLength > g.config.MaxRequestBytes {
		return nil, errBodyTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, g.config.MaxRequestBytes+1))
	if err != nil {
		return nil, errors.New("failed to read request body")
	}
	if int64(len(body)) > g.config.MaxRequestBytes {
		return nil, errBodyTooLarge
	}
	if r.URL.Path == "/api/v1/admin/accounts/data" {
		if err := validateDataImport(body); err != nil {
			return nil, err
		}
	} else {
		if err := validateBatchImport(body); err != nil {
			return nil, err
		}
	}
	return body, nil
}

func validateDataImport(body []byte) error {
	root, err := decodeObject(body)
	if err != nil {
		return errors.New("invalid JSON import body")
	}
	if err := exactKeys(root, "data", "skip_default_group_bind"); err != nil {
		return err
	}
	rawData, ok := root["data"]
	if !ok {
		return errors.New("data import requires a data object")
	}
	data, err := decodeObject(rawData)
	if err != nil {
		return errors.New("data import requires a data object")
	}
	if err := exactKeys(data, "type", "version", "exported_at", "proxies", "accounts", "skipped_shadows"); err != nil {
		return err
	}
	if rawType, ok := data["type"]; ok {
		var dataType string
		if json.Unmarshal(rawType, &dataType) != nil || (dataType != "" && dataType != "sub2api-data" && dataType != "sub2api-bundle") {
			return errors.New("unsupported data import type")
		}
	}
	if err := validateObjectArray(data["accounts"], "accounts", 1, maxImportAccounts); err != nil {
		return err
	}
	if rawProxies, ok := data["proxies"]; ok {
		if err := validateObjectArray(rawProxies, "proxies", 0, maxImportProxies); err != nil {
			return err
		}
	}
	return nil
}

func validateBatchImport(body []byte) error {
	root, err := decodeObject(body)
	if err != nil {
		return errors.New("invalid JSON import body")
	}
	if err := exactKeys(root, "accounts"); err != nil {
		return err
	}
	return validateObjectArray(root["accounts"], "accounts", 1, maxImportAccounts)
}

func decodeObject(body []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	var value map[string]json.RawMessage
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, errors.New("expected JSON object")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("unexpected trailing JSON")
	}
	return value, nil
}

func exactKeys(value map[string]json.RawMessage, allowed ...string) error {
	allowedSet := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = true
	}
	for key := range value {
		if !allowedSet[key] {
			return fmt.Errorf("field %q is not allowed", key)
		}
	}
	return nil
}

func validateObjectArray(raw json.RawMessage, name string, min, max int) error {
	if len(raw) == 0 {
		return fmt.Errorf("%s is required", name)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || len(values) < min || len(values) > max {
		return fmt.Errorf("%s must contain between %d and %d objects", name, min, max)
	}
	for _, value := range values {
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) != nil || object == nil {
			return fmt.Errorf("%s must contain only objects", name)
		}
	}
	return nil
}

func (g *guard) forward(r *http.Request, body []byte) (int, []byte, error) {
	target := *g.upstream
	target.Path = r.URL.Path
	target.RawPath = ""
	target.RawQuery = r.URL.RawQuery
	target.Fragment = ""

	ctx, cancel := context.WithTimeout(r.Context(), g.config.UpstreamTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, r.Method, target.String(), bytesReader(body))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Api-Key", g.config.UpstreamAPIKey)
	if r.Method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := g.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, g.config.MaxResponseBytes+1))
	if err != nil {
		return 0, nil, err
	}
	if int64(len(raw)) > g.config.MaxResponseBytes {
		return 0, nil, errors.New("upstream response exceeds configured limit")
	}
	sanitized, err := sanitizeJSON(raw)
	if err != nil {
		return 0, nil, errors.New("upstream returned invalid JSON")
	}
	return response.StatusCode, sanitized, nil
}

func bytesReader(body []byte) io.Reader {
	if len(body) == 0 {
		return nil
	}
	return strings.NewReader(string(body))
}

func sanitizeJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	value = redactJSON(value)
	return json.Marshal(value)
}

func redactJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for key, item := range typed {
			if sensitiveResponseKey(key) {
				continue
			}
			clean[key] = redactJSON(item)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for index, item := range typed {
			clean[index] = redactJSON(item)
		}
		return clean
	default:
		return value
	}
}

func sensitiveResponseKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(strings.ToLower(key))
	switch normalized {
	case "tokensource", "tokenexpiresat", "tokenexpiry", "tokentype", "tokenstatus", "tokenvalidationoutcome":
		return false
	}
	if normalized == "credentials" {
		return false
	}
	return strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "passwd") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "cookie") ||
		strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "sessionkey") ||
		normalized == "cch" ||
		strings.Contains(normalized, "credentialref")
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    status,
		"message": message,
	})
}
