package service

import (
	"encoding/hex"
	"net"
	"testing"
	"time"
)

func TestFormalPoolConfigDefaults(t *testing.T) {
	cfg := DefaultFormalPoolConfig()
	again := DefaultFormalPoolConfig()

	if cfg.NonceTTL != 5*time.Minute {
		t.Fatalf("NonceTTL = %v, want %v", cfg.NonceTTL, 5*time.Minute)
	}
	if len(cfg.EgressMatchCIDRWhitelist) != 0 {
		t.Fatalf("EgressMatchCIDRWhitelist = %v, want empty", cfg.EgressMatchCIDRWhitelist)
	}
	if cfg.ProxyEgressCacheSuccessTTL != 60*time.Second {
		t.Fatalf("ProxyEgressCacheSuccessTTL = %v, want %v", cfg.ProxyEgressCacheSuccessTTL, 60*time.Second)
	}
	if cfg.ProxyEgressCacheFailureTTL != 15*time.Second {
		t.Fatalf("ProxyEgressCacheFailureTTL = %v, want %v", cfg.ProxyEgressCacheFailureTTL, 15*time.Second)
	}
	if cfg.ProxyEgressProbeTimeout != 3*time.Second {
		t.Fatalf("ProxyEgressProbeTimeout = %v, want %v", cfg.ProxyEgressProbeTimeout, 3*time.Second)
	}
	if cfg.PublicRouteRatePerNonce != 10 {
		t.Fatalf("PublicRouteRatePerNonce = %d, want 10", cfg.PublicRouteRatePerNonce)
	}
	if cfg.PublicRouteRatePerIP != 30 {
		t.Fatalf("PublicRouteRatePerIP = %d, want 30", cfg.PublicRouteRatePerIP)
	}
	if cfg.PublicRouteTotalPerNonce != 20 {
		t.Fatalf("PublicRouteTotalPerNonce = %d, want 20", cfg.PublicRouteTotalPerNonce)
	}
	if cfg.PublicRouteFallbackPerIP != 3 {
		t.Fatalf("PublicRouteFallbackPerIP = %d, want 3", cfg.PublicRouteFallbackPerIP)
	}
	if cfg.PublicRouteConstantDelayMin != 80*time.Millisecond {
		t.Fatalf("PublicRouteConstantDelayMin = %v, want %v", cfg.PublicRouteConstantDelayMin, 80*time.Millisecond)
	}
	if cfg.PublicRouteConstantDelayMax != 150*time.Millisecond {
		t.Fatalf("PublicRouteConstantDelayMax = %v, want %v", cfg.PublicRouteConstantDelayMax, 150*time.Millisecond)
	}
	if len(cfg.RateLimitHMACSecret) != 0 {
		t.Fatalf("RateLimitHMACSecret length = %d, want default empty stable secret", len(cfg.RateLimitHMACSecret))
	}
	if string(cfg.RateLimitHMACSecret) != string(again.RateLimitHMACSecret) {
		t.Fatal("DefaultFormalPoolConfig must be stable across calls")
	}
}

func TestParseFormalPoolCIDRAllowlist(t *testing.T) {
	got, err := ParseFormalPoolCIDRAllowlist([]string{"", "  ", "192.0.2.0/24", " 2001:db8::/32 "})
	if err != nil {
		t.Fatalf("ParseFormalPoolCIDRAllowlist returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed CIDR count = %d, want 2", len(got))
	}
	if !got[0].Contains(net.ParseIP("192.0.2.9")) || got[0].Contains(net.ParseIP("192.0.3.1")) {
		t.Fatalf("first CIDR not parsed exactly as 192.0.2.0/24: %v", got[0])
	}
	if !got[1].Contains(net.ParseIP("2001:db8::1")) {
		t.Fatalf("second CIDR not parsed as 2001:db8::/32: %v", got[1])
	}

	if got, err := ParseFormalPoolCIDRAllowlist([]string{"203.0.113.7"}); err == nil {
		t.Fatalf("ParseFormalPoolCIDRAllowlist accepted non-CIDR address as %v", got)
	}
	if _, err := ParseFormalPoolCIDRAllowlist([]string{"192.0.2.0/33"}); err == nil {
		t.Fatal("ParseFormalPoolCIDRAllowlist accepted invalid CIDR")
	}
}

func TestGenerateFormalPoolHMACSecretHex(t *testing.T) {
	first, err := GenerateFormalPoolHMACSecretHex()
	if err != nil {
		t.Fatalf("GenerateFormalPoolHMACSecretHex returned error: %v", err)
	}
	second, err := GenerateFormalPoolHMACSecretHex()
	if err != nil {
		t.Fatalf("GenerateFormalPoolHMACSecretHex returned error: %v", err)
	}
	if len(first) != 64 {
		t.Fatalf("secret length = %d, want 64 hex chars", len(first))
	}
	decoded, err := hex.DecodeString(first)
	if err != nil {
		t.Fatalf("secret should be hex: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded secret length = %d, want 32 bytes", len(decoded))
	}
	if first == second {
		t.Fatal("generated secrets should not repeat")
	}
}

func TestParseFormalPoolHMACSecretHex(t *testing.T) {
	raw := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	got, err := ParseFormalPoolHMACSecretHex(raw)
	if err != nil {
		t.Fatalf("ParseFormalPoolHMACSecretHex returned error: %v", err)
	}
	if len(got) != 32 {
		t.Fatalf("decoded secret length = %d, want 32", len(got))
	}
	if hex.EncodeToString(got) != raw {
		t.Fatalf("decoded secret = %s, want %s", hex.EncodeToString(got), raw)
	}

	for _, bad := range []string{
		"",
		"001122",
		"00112233445566778899aabbccddeeff00112233445566778899aabbccddee",
		"00112233445566778899aabbccddeeff00112233445566778899aabbccddeezz",
	} {
		if got, err := ParseFormalPoolHMACSecretHex(bad); err == nil {
			t.Fatalf("ParseFormalPoolHMACSecretHex(%q) = %x, want error", bad, got)
		}
	}
}
