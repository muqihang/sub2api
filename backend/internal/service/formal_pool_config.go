package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"
)

type FormalPoolConfig struct {
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
