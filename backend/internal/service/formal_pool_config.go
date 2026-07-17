package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type FormalPoolConfig struct {
	PublicOrigin                      string
	NonceTTL                          time.Duration
	EgressMatchCIDRWhitelist          []net.IPNet
	ProxyEgressCacheSuccessTTL        time.Duration
	ProxyEgressCacheFailureTTL        time.Duration
	ProxyEgressProbeTimeout           time.Duration
	PublicRouteRatePerNonce           int
	PublicRouteRatePerIP              int
	PublicRouteTotalPerNonce          int
	PublicRouteFallbackPerIP          int
	PublicRouteConstantDelayMin       time.Duration
	PublicRouteConstantDelayMax       time.Duration
	RateLimitHMACSecret               []byte
	CCGatewayContextAttestationSecret string
}

func NormalizeFormalPoolPublicOrigin(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" ||
		parsed.RawPath != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("invalid formal_pool public_origin")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", fmt.Errorf("invalid formal_pool public_origin")
	}
	hostname, err := validateFormalPoolOriginHost(parsed.Host)
	if err != nil {
		return "", err
	}
	ip := net.ParseIP(hostname)
	loopback := strings.EqualFold(hostname, "localhost") || (ip != nil && ip.IsLoopback())
	if scheme == "http" && !loopback {
		return "", fmt.Errorf("invalid formal_pool public_origin")
	}
	return scheme + "://" + parsed.Host, nil
}

func validateFormalPoolOriginHost(host string) (string, error) {
	hostname := ""
	port := ""
	explicitPort := false
	if strings.HasPrefix(host, "[") {
		closeBracket := strings.LastIndex(host, "]")
		if closeBracket < 0 {
			return "", fmt.Errorf("invalid formal_pool public_origin")
		}
		hostname = host[1:closeBracket]
		if hostname == "" || strings.Contains(hostname, "%") || net.ParseIP(hostname) == nil {
			return "", fmt.Errorf("invalid formal_pool public_origin")
		}
		suffix := host[closeBracket+1:]
		if suffix != "" {
			if !strings.HasPrefix(suffix, ":") {
				return "", fmt.Errorf("invalid formal_pool public_origin")
			}
			explicitPort = true
			port = strings.TrimPrefix(suffix, ":")
		}
	} else {
		if strings.ContainsAny(host, "[]") || strings.Count(host, ":") > 1 {
			return "", fmt.Errorf("invalid formal_pool public_origin")
		}
		hostname = host
		if colon := strings.LastIndexByte(host, ':'); colon >= 0 {
			explicitPort = true
			hostname = host[:colon]
			port = host[colon+1:]
		}
	}
	if hostname == "" || strings.TrimSpace(hostname) != hostname || strings.ContainsAny(hostname, "@/\\") {
		return "", fmt.Errorf("invalid formal_pool public_origin")
	}
	if explicitPort {
		if port == "" || (len(port) > 1 && port[0] == '0') {
			return "", fmt.Errorf("invalid formal_pool public_origin")
		}
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", fmt.Errorf("invalid formal_pool public_origin")
		}
	}
	return hostname, nil
}

func ValidateFormalPoolBrowserEgressURL(raw string) error {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return fmt.Errorf("invalid formal pool browser egress URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.RawFragment != "" || parsed.RawPath != "" {
		return fmt.Errorf("invalid formal pool browser egress URL")
	}
	if parsed.IsAbs() {
		if _, err := NormalizeFormalPoolPublicOrigin(parsed.Scheme + "://" + parsed.Host); err != nil {
			return fmt.Errorf("invalid formal pool browser egress URL")
		}
	} else if parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return fmt.Errorf("invalid formal pool browser egress URL")
	}
	if !strings.HasPrefix(parsed.Path, formalPoolBrowserEgressPublicPathPrefix) ||
		strings.Contains(strings.TrimPrefix(parsed.Path, formalPoolBrowserEgressPublicPathPrefix), "/") ||
		strings.TrimPrefix(parsed.Path, formalPoolBrowserEgressPublicPathPrefix) == "" {
		return fmt.Errorf("invalid formal pool browser egress URL")
	}
	return nil
}

func DefaultFormalPoolConfig() FormalPoolConfig {
	return FormalPoolConfig{
		NonceTTL:                    5 * time.Minute,
		ProxyEgressCacheSuccessTTL:  60 * time.Second,
		ProxyEgressCacheFailureTTL:  15 * time.Second,
		ProxyEgressProbeTimeout:     3 * time.Second,
		PublicRouteRatePerNonce:     10,
		PublicRouteRatePerIP:        30,
		PublicRouteTotalPerNonce:    20,
		PublicRouteFallbackPerIP:    3,
		PublicRouteConstantDelayMin: 80 * time.Millisecond,
		PublicRouteConstantDelayMax: 150 * time.Millisecond,
	}
}

func ParseFormalPoolCIDRAllowlist(values []string) ([]net.IPNet, error) {
	out := make([]net.IPNet, 0, len(values))
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		_, network, err := net.ParseCIDR(trimmed)
		if err != nil {
			return nil, fmt.Errorf("parse formal pool cidr %q: %w", trimmed, err)
		}
		out = append(out, *network)
	}
	return out, nil
}

// GenerateFormalPoolHMACSecret returns 32 raw random bytes.
func GenerateFormalPoolHMACSecret() ([]byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// GenerateFormalPoolHMACSecretHex returns a 64-character hex string encoding 32 random bytes.
func GenerateFormalPoolHMACSecretHex() (string, error) {
	buf, err := GenerateFormalPoolHMACSecret()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func ParseFormalPoolHMACSecretHex(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) != 64 {
		return nil, fmt.Errorf("formal pool hmac secret must be 64 hex chars, got %d", len(trimmed))
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse formal pool hmac secret hex: %w", err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("formal pool hmac secret must decode to 32 bytes, got %d", len(decoded))
	}
	return decoded, nil
}
