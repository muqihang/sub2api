//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGeminiPolicy_ProjectIDAutodetectFallbackAllowedOutsideProduction(t *testing.T) {
	t.Parallel()

	require.True(t, geminiAllowsProjectIDFallbackToAIStudio(&config.Config{}))
}

func TestGeminiPolicy_ProjectIDAutodetectFallbackRejectedInProduction(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.AllowProjectIDFallbackToAIStudio = false

	require.False(t, geminiAllowsProjectIDFallbackToAIStudio(cfg))
}

func TestGeminiPolicy_UnauthorizedClientRetryAllowedOutsideProduction(t *testing.T) {
	t.Parallel()

	require.True(t, geminiAllowsUnauthorizedClientRetry(&config.Config{}))
}

func TestGeminiPolicy_UnauthorizedClientRetryRejectedInProduction(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.AllowUnauthorizedClientRetryFallback = false

	require.False(t, geminiAllowsUnauthorizedClientRetry(cfg))
}

func TestGeminiPolicy_GoogleOneDefaultTierFallbackAllowedAsVisibleDegradedStateInProduction(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.AllowGoogleOneDefaultTierFallback = true

	decision := geminiGoogleOneDefaultTierFallbackPolicy(cfg)
	require.True(t, decision.Allow)
	require.True(t, decision.VisibleDegraded)
}

func TestGeminiPolicy_GoogleOneDefaultTierFallbackDisabledInProduction(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true
	cfg.Gemini.AllowGoogleOneDefaultTierFallback = false

	decision := geminiGoogleOneDefaultTierFallbackPolicy(cfg)
	require.False(t, decision.Allow)
	require.False(t, decision.VisibleDegraded)
}

func TestGeminiPolicy_ThoughtSignatureSafetyDisabledSkipsRuntimeGuard(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Gemini.RequireThoughtSignatureSessionSafety = false

	decision := EvaluateGeminiThoughtSignatureSessionPolicy(cfg, GeminiStickySessionSourceBodyFirstPartText, true, 0)
	require.False(t, decision.VisibleDegraded)
	require.False(t, decision.RequiresScrub)
}
