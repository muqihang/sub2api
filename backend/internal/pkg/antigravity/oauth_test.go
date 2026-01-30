package antigravity

import "testing"

func TestEffectiveUserAgent_PrefersUserAgentEnv(t *testing.T) {
	t.Setenv("ANTIGRAVITY_USER_AGENT", "antigravity/9.9.9 windows/amd64")
	t.Setenv("ANTIGRAVITY_VERSION", "1.2.3")

	got := EffectiveUserAgent()
	if got != "antigravity/9.9.9 windows/amd64" {
		t.Fatalf("EffectiveUserAgent() = %q, want %q", got, "antigravity/9.9.9 windows/amd64")
	}
}

func TestEffectiveUserAgent_UsesVersionEnvWhenNoUserAgentEnv(t *testing.T) {
	t.Setenv("ANTIGRAVITY_USER_AGENT", "")
	t.Setenv("ANTIGRAVITY_VERSION", "1.15.8")

	got := EffectiveUserAgent()
	want := "antigravity/1.15.8 windows/amd64"
	if got != want {
		t.Fatalf("EffectiveUserAgent() = %q, want %q", got, want)
	}
}

func TestEffectiveUserAgent_FallsBackToDefault(t *testing.T) {
	t.Setenv("ANTIGRAVITY_USER_AGENT", "")
	t.Setenv("ANTIGRAVITY_VERSION", "")

	got := EffectiveUserAgent()
	if got == "" {
		t.Fatalf("EffectiveUserAgent() returned empty string")
	}
}

func TestEffectiveV1InternalUserAgent_PrefersDedicatedEnv(t *testing.T) {
	t.Setenv("ANTIGRAVITY_V1INTERNAL_USER_AGENT", "antigravity/9.9.9")
	t.Setenv("ANTIGRAVITY_VERSION", "1.2.3")
	t.Setenv("ANTIGRAVITY_USER_AGENT", "antigravity/8.8.8 windows/amd64")

	got := EffectiveV1InternalUserAgent()
	if got != "antigravity/9.9.9" {
		t.Fatalf("EffectiveV1InternalUserAgent() = %q, want %q", got, "antigravity/9.9.9")
	}
}

func TestEffectiveV1InternalUserAgent_UsesVersionEnv(t *testing.T) {
	t.Setenv("ANTIGRAVITY_V1INTERNAL_USER_AGENT", "")
	t.Setenv("ANTIGRAVITY_VERSION", "1.15.8")
	t.Setenv("ANTIGRAVITY_USER_AGENT", "antigravity/8.8.8 windows/amd64")

	got := EffectiveV1InternalUserAgent()
	if got != "antigravity/1.15.8" {
		t.Fatalf("EffectiveV1InternalUserAgent() = %q, want %q", got, "antigravity/1.15.8")
	}
}

func TestEffectiveV1InternalUserAgent_UsesFirstTokenFromUserAgentEnv(t *testing.T) {
	t.Setenv("ANTIGRAVITY_V1INTERNAL_USER_AGENT", "")
	t.Setenv("ANTIGRAVITY_VERSION", "")
	t.Setenv("ANTIGRAVITY_USER_AGENT", "antigravity/1.15.8 windows/amd64")

	got := EffectiveV1InternalUserAgent()
	if got != "antigravity/1.15.8" {
		t.Fatalf("EffectiveV1InternalUserAgent() = %q, want %q", got, "antigravity/1.15.8")
	}
}
