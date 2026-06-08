package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultProxyEgressProbeEndpoint = "https://api.ipify.org?format=json"

// ProxyEgressProbeOptions configures a side-effect-free proxy egress IP probe.
type ProxyEgressProbeOptions struct {
	Timeout   time.Duration
	Endpoint  string
	Transport http.RoundTripper
}

// SideEffectFreeProxyEgressProbe resolves the exit IP for a normalized proxy URL
// without writing latency or cache state.
type SideEffectFreeProxyEgressProbe struct {
	timeout   time.Duration
	endpoint  string
	transport http.RoundTripper
}

func NewSideEffectFreeProxyEgressProbe(opts ProxyEgressProbeOptions) *SideEffectFreeProxyEgressProbe {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultFormalPoolConfig().ProxyEgressProbeTimeout
	}
	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint == "" {
		endpoint = defaultProxyEgressProbeEndpoint
	}
	return &SideEffectFreeProxyEgressProbe{
		timeout:   timeout,
		endpoint:  endpoint,
		transport: opts.Transport,
	}
}

func (p *SideEffectFreeProxyEgressProbe) Probe(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
	if p == nil {
		return "", errors.New("proxy egress probe is nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	proxyURL, err := parseProxyEgressProbeProxyURL(normalizedProxyURL)
	if err != nil {
		return "", err
	}
	endpointURL, err := parseProxyEgressProbeEndpoint(p.endpoint)
	if err != nil {
		return "", err
	}

	timeout := p.timeout
	if timeout <= 0 {
		timeout = DefaultFormalPoolConfig().ProxyEgressProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := probeCtx.Err(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, endpointURL.String(), nil)
	if err != nil {
		return "", errors.New("invalid proxy egress endpoint")
	}
	client := &http.Client{Transport: p.roundTripper(proxyURL)}
	res, err := client.Do(req)
	if err != nil {
		if ctxErr := probeCtx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", errors.New("proxy egress probe request failed")
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("proxy egress endpoint returned status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return "", errors.New("read proxy egress response failed")
	}
	rawIP, err := parseProxyEgressProbeIP(body)
	if err != nil {
		return "", err
	}
	return rawIP, nil
}

func (p *SideEffectFreeProxyEgressProbe) roundTripper(proxyURL *url.URL) http.RoundTripper {
	if p.transport != nil {
		return p.transport
	}
	return &http.Transport{Proxy: http.ProxyURL(proxyURL)}
}

func parseProxyEgressProbeProxyURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("invalid proxy url")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("invalid proxy url")
	}
	return parsed, nil
}

func parseProxyEgressProbeEndpoint(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("invalid proxy egress endpoint")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("invalid proxy egress endpoint")
	}
	return parsed, nil
}

func parseProxyEgressProbeIP(body []byte) (string, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", errors.New("proxy egress endpoint returned invalid ip")
	}

	var payload struct {
		IP string `json:"ip"`
	}
	candidate := trimmed
	if json.Unmarshal(body, &payload) == nil && payload.IP != "" {
		candidate = strings.TrimSpace(payload.IP)
	}
	if ip := net.ParseIP(candidate); ip != nil {
		return candidate, nil
	}
	return "", errors.New("proxy egress endpoint returned invalid ip")
}
