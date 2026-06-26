package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func loadNativeFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "claude_code_native", name))
	require.NoError(t, err)
	return body
}

func TestClaudeCodeNativeShapeHealthcheckFixtureSuiteCoversNativeTakeoverSignals(t *testing.T) {
	messagesSonnet := loadNativeFixture(t, "messages_toolsearch_sonnet.json")
	messagesOpus := loadNativeFixture(t, "messages_opus.json")
	messagesRich := loadNativeFixture(t, "messages_rich_native_shape.json")
	countTokens := loadNativeFixture(t, "count_tokens_sonnet.json")
	controlPlaneSafe := loadNativeFixture(t, "control_plane_safe_intent_summary.json")
	netwatchSafe := loadNativeFixture(t, "netwatch_summary.json")
	promptCache := loadNativeFixture(t, "messages_prompt_cache_sonnet.json")

	fixtures := []ClaudeCodeNativeShapeFixture{
		{
			Name:  "messages_toolsearch_sonnet",
			Route: ClaudeCodeNativeInboundMessages,
			Body:  messagesSonnet,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundMessages + "?beta=true",
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("a", 64),
				ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
			}, messagesSonnet),
		},
		{
			Name:  "messages_opus",
			Route: ClaudeCodeNativeInboundMessages,
			Body:  messagesOpus,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundMessages,
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("b", 64),
				ShapeHealthcheckProfile: ClaudeCodeNativeToolSearchHealthProfile,
			}, messagesOpus),
		},
		{
			Name:  "messages_rich_native_shape",
			Route: ClaudeCodeNativeInboundMessages,
			Body:  messagesRich,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundMessages,
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("e", 64),
				ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
			}, messagesRich),
		},

		{
			Name:  "messages_prompt_cache_sonnet",
			Route: ClaudeCodeNativeInboundMessages,
			Body:  promptCache,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundMessages,
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("f", 64),
				ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
			}, promptCache),
		},
		{
			Name:  "count_tokens_sonnet",
			Route: ClaudeCodeNativeInboundCountTokens,
			Body:  countTokens,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundCountTokens,
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("c", 64),
				ShapeHealthcheckProfile: ClaudeCodeNativeControlPlaneHealthProfile,
			}, countTokens),
		},
		{
			Name:  "count_tokens_netwatch_profile",
			Route: ClaudeCodeNativeInboundCountTokens,
			Body:  countTokens,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundCountTokens,
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("d", 64),
				ShapeHealthcheckProfile: ClaudeCodeNativeNetwatchHealthProfile,
			}, countTokens),
		},
	}

	health := EvaluateClaudeCodeNativeShapeHealthcheckSuite(fixtures, ClaudeCodeNativeShapeHealthcheckEvidence{
		LocalhostOnly:               true,
		MockUpstreamOnly:            true,
		ControlPlaneSafeSummary:     controlPlaneSafe,
		NetwatchSafeSummary:         netwatchSafe,
		RawBodiesOmittedFromAudit:   true,
		PromptCacheSafeUsageSummary: []byte(`{"provider_cache_mechanism":"anthropic_cache_control","cache_control_present":true,"cache_control_locations":["history","system","tools"],"prompt_caching_beta_present":true,"context_management_beta_present":true,"cache_usage_fields":["cache_creation_input_tokens","cache_read_input_tokens"],"cache_creation_input_tokens":3,"cache_read_input_tokens":7,"stores_raw":false,"body_omitted":true,"response_omitted":true}`),
		FgtsSafeTraceSummary:        []byte(`{"mode":"observe_only","requested_mode":"enabled","env_value":"unset","eager_input_streaming_present":true,"direct_official_egress":false,"stores_raw":false,"body_omitted":true}`),
	})

	require.Equal(t, ClaudeCodeNativeShapeHealthcheckPass, health.Status)
	require.Equal(t, health.Denominator, health.Passed)
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("tool_search_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("system_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("context_management_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("prompt_caching_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("prompt_cache_usage_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("eager_input_streaming_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("fgts_trace_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("output_config_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("adaptive_thinking_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("count_tokens_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("control_plane_safe_intent_fixture"))
	require.True(t, HasClaudeCodeNativeShapeHealthcheckField("netwatch_fixture"))
	require.ElementsMatch(t, []string{
		ClaudeCodeNativeTakeoverHealthProfile,
		ClaudeCodeNativeToolSearchHealthProfile,
		ClaudeCodeNativeControlPlaneHealthProfile,
		ClaudeCodeNativeNetwatchHealthProfile,
	}, health.Profiles)

	require.JSONEq(t, `{"control_plane":"safe_summary_present","netwatch":"safe_summary_present","prompt_cache":"safe_usage_summary_present","fgts":"safe_trace_summary_present"}`, string(health.SafeEvidence))

	safe, err := json.Marshal(health)
	require.NoError(t, err)
	for _, forbidden := range []string{"synthetic native healthcheck content", "synthetic opus healthcheck content", "synthetic count tokens content", "synthetic native rich shape content", "synthetic native system identity block", "synthetic native cache stable system block", "synthetic native cache history block", "api.anthropic.com", "authorization", "cookie", "raw_"} {
		require.NotContains(t, string(safe), forbidden)
	}
}

func TestClaudeCodeNativeShapeHealthcheckFailsClosedOnBypassOrRealUpstream(t *testing.T) {
	body := loadNativeFixture(t, "messages_toolsearch_sonnet.json")
	fixture := ClaudeCodeNativeShapeFixture{
		Name:  "messages_toolsearch_sonnet",
		Route: ClaudeCodeNativeInboundMessages,
		Body:  body,
		Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
			RequestURI:              ClaudeCodeNativeInboundMessages,
			GuardVersion:            "guard_v1",
			ClaudeCodeVersion:       "2.1.175",
			LocalSessionRef:         "hmac-sha256:" + strings.Repeat("d", 64),
			ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
		}, body),
	}

	health := EvaluateClaudeCodeNativeShapeHealthcheckSuite([]ClaudeCodeNativeShapeFixture{fixture}, ClaudeCodeNativeShapeHealthcheckEvidence{
		LocalhostOnly:             false,
		MockUpstreamOnly:          false,
		NetwatchSafeSummary:       []byte(`{"potential_guard_bypass_count":1,"official_or_public_bypass_count":1,"stores_payload":false,"stores_headers":false}`),
		RawBodiesOmittedFromAudit: true,
	})

	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, health.Status)
	require.Less(t, health.Passed, health.Denominator)
	require.Contains(t, health.FailedFields, "localhost_only")
	require.Contains(t, health.FailedFields, "mock_upstream_only")
	require.Contains(t, health.FailedFields, "netwatch_fixture")
}

func TestClaudeCodeNativeShapeHealthcheckRequiresExplicitToolSearchMarkers(t *testing.T) {
	plainTools := []byte(`{"model":"claude-sonnet-4-6","stream":true,"thinking":{"type":"enabled","budget_tokens":1024},"tools":[{"name":"Read","input_schema":{"type":"object","properties":{"tool_reference":{"type":"string"},"defer_loading":{"type":"boolean"}}}}],"messages":[{"role":"user","content":"plain tool fixture content"}]}`)
	countTokens := loadNativeFixture(t, "count_tokens_sonnet.json")
	controlPlaneSafe := loadNativeFixture(t, "control_plane_safe_intent_summary.json")
	netwatchSafe := loadNativeFixture(t, "netwatch_summary.json")

	health := EvaluateClaudeCodeNativeShapeHealthcheckSuite([]ClaudeCodeNativeShapeFixture{
		{
			Name:  "plain_tools_without_toolsearch",
			Route: ClaudeCodeNativeInboundMessages,
			Body:  plainTools,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundMessages,
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("e", 64),
				ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
			}, plainTools),
		},
		{
			Name:  "count_tokens_sonnet",
			Route: ClaudeCodeNativeInboundCountTokens,
			Body:  countTokens,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundCountTokens,
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("f", 64),
				ShapeHealthcheckProfile: ClaudeCodeNativeToolSearchHealthProfile,
			}, countTokens),
		},
	}, ClaudeCodeNativeShapeHealthcheckEvidence{
		LocalhostOnly:             true,
		MockUpstreamOnly:          true,
		ControlPlaneSafeSummary:   controlPlaneSafe,
		NetwatchSafeSummary:       netwatchSafe,
		RawBodiesOmittedFromAudit: true,
	})

	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, health.Status)
	require.Contains(t, health.FailedFields, "tool_search_fixture")
}

func TestClaudeCodeNativeShapeHealthcheckSafeSummariesRejectSensitiveOrBypassFields(t *testing.T) {
	body := loadNativeFixture(t, "messages_toolsearch_sonnet.json")
	fixture := ClaudeCodeNativeShapeFixture{
		Name:  "messages_toolsearch_sonnet",
		Route: ClaudeCodeNativeInboundMessages,
		Body:  body,
		Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
			RequestURI:              ClaudeCodeNativeInboundMessages,
			GuardVersion:            "guard_v1",
			ClaudeCodeVersion:       "2.1.175",
			LocalSessionRef:         "hmac-sha256:" + strings.Repeat("a", 64),
			ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
		}, body),
	}

	health := EvaluateClaudeCodeNativeShapeHealthcheckSuite([]ClaudeCodeNativeShapeFixture{fixture}, ClaudeCodeNativeShapeHealthcheckEvidence{
		LocalhostOnly:             true,
		MockUpstreamOnly:          true,
		ControlPlaneSafeSummary:   []byte(`{"safe_intent":true,"method":"GET","path_template":"/api/claude_cli/bootstrap","decision":"stub_json","status":200,"stores_raw":false,"messages_signing_reused":true,"response_schema_keys":["ok"],"authorization":"present"}`),
		NetwatchSafeSummary:       []byte(`{"connection_count":1,"potential_guard_bypass_count":0,"official_or_public_bypass_count":0,"loopback_guard_connection_count":1,"remote_host_buckets":{"api.anthropic.com":1},"stores_payload":false,"stores_headers":false}`),
		RawBodiesOmittedFromAudit: true,
	})

	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, health.Status)
	require.Contains(t, health.FailedFields, "control_plane_safe_intent_fixture")
	require.Contains(t, health.FailedFields, "netwatch_fixture")
	require.Empty(t, health.SafeEvidence)
}

func TestClaudeCodeNativeShapeHealthcheckSafeSummariesRejectTypeCoercionAndBroadSensitiveKeys(t *testing.T) {
	body := loadNativeFixture(t, "messages_toolsearch_sonnet.json")
	fixture := ClaudeCodeNativeShapeFixture{
		Name:  "messages_toolsearch_sonnet",
		Route: ClaudeCodeNativeInboundMessages,
		Body:  body,
		Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
			RequestURI:              ClaudeCodeNativeInboundMessages,
			GuardVersion:            "guard_v1",
			ClaudeCodeVersion:       "2.1.175",
			LocalSessionRef:         "hmac-sha256:" + strings.Repeat("a", 64),
			ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
		}, body),
	}

	health := EvaluateClaudeCodeNativeShapeHealthcheckSuite([]ClaudeCodeNativeShapeFixture{fixture}, ClaudeCodeNativeShapeHealthcheckEvidence{
		LocalhostOnly:             true,
		MockUpstreamOnly:          true,
		ControlPlaneSafeSummary:   []byte(`{"safe_intent":"true","method":"GET","path_template":"/api/claude_cli/bootstrap","decision":"stub_json","status":"200","stores_raw":false,"messages_signing_reused":false,"response_schema_keys":["raw_token"]}`),
		NetwatchSafeSummary:       []byte(`{"connection_count":"1","potential_guard_bypass_count":"0","official_or_public_bypass_count":0,"loopback_guard_connection_count":1,"remote_host_buckets":{"loopback":1},"stores_payload":false,"stores_headers":false}`),
		RawBodiesOmittedFromAudit: true,
	})

	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, health.Status)
	require.Contains(t, health.FailedFields, "control_plane_safe_intent_fixture")
	require.Contains(t, health.FailedFields, "netwatch_fixture")
}

func TestClaudeCodeNativeShapeHealthcheckAllowsBlockAndShadowControlPlaneDecisions(t *testing.T) {
	for _, decision := range []string{"block_403", "shadow_stub", "shadow_block"} {
		summary := []byte(`{"safe_intent":true,"method":"GET","path_template":"/api/claude_cli/bootstrap","decision":"` + decision + `","status":403,"stores_raw":false,"messages_signing_reused":false,"response_schema_keys":["ok"]}`)
		require.True(t, validClaudeCodeNativeControlPlaneSafeIntent(summary), decision)
	}
}

func TestClaudeCodeNativeShapeHealthcheckRequiresRichFixtureObjectShapes(t *testing.T) {
	malformed := gjson.Parse(`{"system":[{}],"context_management":null,"output_config":"effort","thinking":{"type":"adaptive"}}`)
	require.False(t, claudeCodeNativeHasSystemFixture(malformed))
	require.False(t, claudeCodeNativeHasContextManagementFixture(malformed))
	require.False(t, claudeCodeNativeHasOutputConfigFixture(malformed))

	wellFormed := gjson.Parse(`{"system":[{"type":"text","text":"synthetic"}],"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]},"output_config":{"effort":"high"}}`)
	require.True(t, claudeCodeNativeHasSystemFixture(wellFormed))
	require.True(t, claudeCodeNativeHasContextManagementFixture(wellFormed))
	require.True(t, claudeCodeNativeHasOutputConfigFixture(wellFormed))
}

func TestClaudeCodeNativeShapeHealthcheckRequiresEagerInputStreamingTrace(t *testing.T) {
	bodyWithoutEager := []byte(`{"model":"claude-sonnet-4-6","stream":true,"thinking":{"type":"adaptive"},"system":[{"type":"text","text":"safe synthetic system","cache_control":{"type":"ephemeral"}}],"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]},"output_config":{"effort":"high"},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"defer_loading":true,"cache_control":{"type":"ephemeral"},"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"safe synthetic user","cache_control":{"type":"ephemeral"}}]}]}`)
	countTokens := loadNativeFixture(t, "count_tokens_sonnet.json")
	controlPlaneSafe := loadNativeFixture(t, "control_plane_safe_intent_summary.json")
	netwatchSafe := loadNativeFixture(t, "netwatch_summary.json")

	fixture := ClaudeCodeNativeShapeFixture{
		Name:  "without_eager",
		Route: ClaudeCodeNativeInboundMessages,
		Body:  bodyWithoutEager,
		Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
			RequestURI:              ClaudeCodeNativeInboundMessages,
			GuardVersion:            "guard_v1",
			ClaudeCodeVersion:       "2.1.175",
			LocalSessionRef:         "hmac-sha256:" + strings.Repeat("a", 64),
			ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
		}, bodyWithoutEager),
	}
	countFixture := ClaudeCodeNativeShapeFixture{
		Name:  "count_tokens_sonnet",
		Route: ClaudeCodeNativeInboundCountTokens,
		Body:  countTokens,
		Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
			RequestURI:              ClaudeCodeNativeInboundCountTokens,
			GuardVersion:            "guard_v1",
			ClaudeCodeVersion:       "2.1.175",
			LocalSessionRef:         "hmac-sha256:" + strings.Repeat("b", 64),
			ShapeHealthcheckProfile: ClaudeCodeNativeControlPlaneHealthProfile,
		}, countTokens),
	}
	baseEvidence := ClaudeCodeNativeShapeHealthcheckEvidence{
		LocalhostOnly:               true,
		MockUpstreamOnly:            true,
		ControlPlaneSafeSummary:     controlPlaneSafe,
		NetwatchSafeSummary:         netwatchSafe,
		RawBodiesOmittedFromAudit:   true,
		PromptCacheSafeUsageSummary: []byte(`{"provider_cache_mechanism":"anthropic_cache_control","cache_control_present":true,"cache_control_locations":["history","system","tools"],"prompt_caching_beta_present":true,"context_management_beta_present":true,"cache_usage_fields":["cache_creation_input_tokens","cache_read_input_tokens"],"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"stores_raw":false,"body_omitted":true,"response_omitted":true}`),
		FgtsSafeTraceSummary:        []byte(`{"mode":"observe_only","requested_mode":"enabled","env_value":"unset","eager_input_streaming_present":true,"direct_official_egress":false,"stores_raw":false,"body_omitted":true}`),
	}

	missingEager := EvaluateClaudeCodeNativeShapeHealthcheckSuite([]ClaudeCodeNativeShapeFixture{fixture, countFixture}, baseEvidence)
	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, missingEager.Status)
	require.Contains(t, missingEager.FailedFields, "eager_input_streaming_fixture")

	missingTraceEvidence := baseEvidence
	missingTraceEvidence.FgtsSafeTraceSummary = nil
	withEagerBody := []byte(strings.Replace(string(bodyWithoutEager), `"messages"`, `"eager_input_streaming":true,"messages"`, 1))
	withEager := fixture
	withEager.Name = "with_eager"
	withEager.Body = withEagerBody
	withEager.Audit = buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
		RequestURI:              ClaudeCodeNativeInboundMessages,
		GuardVersion:            "guard_v1",
		ClaudeCodeVersion:       "2.1.175",
		LocalSessionRef:         "hmac-sha256:" + strings.Repeat("c", 64),
		ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
	}, withEagerBody)
	missingTrace := EvaluateClaudeCodeNativeShapeHealthcheckSuite([]ClaudeCodeNativeShapeFixture{withEager, countFixture}, missingTraceEvidence)
	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, missingTrace.Status)
	require.Contains(t, missingTrace.FailedFields, "fgts_trace_fixture")

	unsafeTrace := baseEvidence
	unsafeTrace.FgtsSafeTraceSummary = []byte(`{"mode":"enabled","requested_mode":"enabled","env_value":"1","eager_input_streaming_present":true,"direct_official_egress":true,"stores_raw":false,"body_omitted":true}`)
	unsafe := EvaluateClaudeCodeNativeShapeHealthcheckSuite([]ClaudeCodeNativeShapeFixture{withEager, countFixture}, unsafeTrace)
	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, unsafe.Status)
	require.Contains(t, unsafe.FailedFields, "fgts_trace_fixture")
}

func TestClaudeCodeNativeShapeHealthcheckRequiresPromptCachingShapeAndSafeUsageExpectation(t *testing.T) {
	messagesSonnet := loadNativeFixture(t, "messages_toolsearch_sonnet.json")
	messagesOpus := loadNativeFixture(t, "messages_opus.json")
	messagesRich := loadNativeFixture(t, "messages_rich_native_shape.json")
	promptCache := loadNativeFixture(t, "messages_prompt_cache_sonnet.json")
	countTokens := loadNativeFixture(t, "count_tokens_sonnet.json")
	controlPlaneSafe := loadNativeFixture(t, "control_plane_safe_intent_summary.json")
	netwatchSafe := loadNativeFixture(t, "netwatch_summary.json")
	promptCacheUsage := []byte(`{"provider_cache_mechanism":"anthropic_cache_control","cache_control_present":true,"cache_control_locations":["history","system","tools"],"prompt_caching_beta_present":true,"context_management_beta_present":true,"cache_usage_fields":["cache_creation_input_tokens","cache_read_input_tokens"],"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"stores_raw":false,"body_omitted":true,"response_omitted":true}`)

	fixtureFor := func(name string, body []byte, profile string) ClaudeCodeNativeShapeFixture {
		return ClaudeCodeNativeShapeFixture{
			Name:  name,
			Route: ClaudeCodeNativeInboundMessages,
			Body:  body,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundMessages,
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("a", 64),
				ShapeHealthcheckProfile: profile,
			}, body),
		}
	}
	countFixture := ClaudeCodeNativeShapeFixture{
		Name:  "count_tokens_sonnet",
		Route: ClaudeCodeNativeInboundCountTokens,
		Body:  countTokens,
		Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
			RequestURI:              ClaudeCodeNativeInboundCountTokens,
			GuardVersion:            "guard_v1",
			ClaudeCodeVersion:       "2.1.175",
			LocalSessionRef:         "hmac-sha256:" + strings.Repeat("b", 64),
			ShapeHealthcheckProfile: ClaudeCodeNativeControlPlaneHealthProfile,
		}, countTokens),
	}

	baseEvidence := ClaudeCodeNativeShapeHealthcheckEvidence{
		LocalhostOnly:               true,
		MockUpstreamOnly:            true,
		ControlPlaneSafeSummary:     controlPlaneSafe,
		NetwatchSafeSummary:         netwatchSafe,
		RawBodiesOmittedFromAudit:   true,
		PromptCacheSafeUsageSummary: promptCacheUsage,
	}

	missingCacheShape := EvaluateClaudeCodeNativeShapeHealthcheckSuite([]ClaudeCodeNativeShapeFixture{
		fixtureFor("messages_toolsearch_sonnet", messagesSonnet, ClaudeCodeNativeTakeoverHealthProfile),
		fixtureFor("messages_opus", messagesOpus, ClaudeCodeNativeToolSearchHealthProfile),
		fixtureFor("messages_rich_native_shape", messagesRich, ClaudeCodeNativeTakeoverHealthProfile),
		countFixture,
	}, baseEvidence)
	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, missingCacheShape.Status)
	require.Contains(t, missingCacheShape.FailedFields, "prompt_caching_fixture")

	missingSafeUsage := baseEvidence
	missingSafeUsage.PromptCacheSafeUsageSummary = nil
	missingUsage := EvaluateClaudeCodeNativeShapeHealthcheckSuite([]ClaudeCodeNativeShapeFixture{
		fixtureFor("messages_toolsearch_sonnet", messagesSonnet, ClaudeCodeNativeTakeoverHealthProfile),
		fixtureFor("messages_opus", messagesOpus, ClaudeCodeNativeToolSearchHealthProfile),
		fixtureFor("messages_rich_native_shape", messagesRich, ClaudeCodeNativeTakeoverHealthProfile),
		fixtureFor("messages_prompt_cache_sonnet", promptCache, ClaudeCodeNativeTakeoverHealthProfile),
		countFixture,
	}, missingSafeUsage)
	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, missingUsage.Status)
	require.Contains(t, missingUsage.FailedFields, "prompt_cache_usage_fixture")

	unsafeUsage := baseEvidence
	unsafeUsage.PromptCacheSafeUsageSummary = []byte(`{"provider_cache_mechanism":"anthropic_cache_control","cache_control_present":true,"cache_control_locations":["history","system","raw prompt body"],"prompt_caching_beta_present":true,"context_management_beta_present":true,"cache_usage_fields":["cache_creation_input_tokens","cache_read_input_tokens"],"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"stores_raw":false,"body_omitted":true,"response_omitted":true}`)
	unsafe := EvaluateClaudeCodeNativeShapeHealthcheckSuite([]ClaudeCodeNativeShapeFixture{
		fixtureFor("messages_prompt_cache_sonnet", promptCache, ClaudeCodeNativeTakeoverHealthProfile),
		countFixture,
	}, unsafeUsage)
	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, unsafe.Status)
	require.Contains(t, unsafe.FailedFields, "prompt_cache_usage_fixture")
}

func TestClaudeCodeNativePromptCacheUsageLocationsMustMatchObservedShape(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","stream":true,"system":[{"type":"text","text":"safe synthetic system","cache_control":{"type":"ephemeral"}}],"thinking":{"type":"adaptive"},"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]},"output_config":{"effort":"high"},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"defer_loading":true,"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"safe synthetic user"}]}`)
	countTokens := loadNativeFixture(t, "count_tokens_sonnet.json")
	controlPlaneSafe := loadNativeFixture(t, "control_plane_safe_intent_summary.json")
	netwatchSafe := loadNativeFixture(t, "netwatch_summary.json")

	fixtures := []ClaudeCodeNativeShapeFixture{
		{
			Name:  "system_cache_only",
			Route: ClaudeCodeNativeInboundMessages,
			Body:  body,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundMessages,
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("a", 64),
				ShapeHealthcheckProfile: ClaudeCodeNativeTakeoverHealthProfile,
			}, body),
		},
		{
			Name:  "count_tokens_sonnet",
			Route: ClaudeCodeNativeInboundCountTokens,
			Body:  countTokens,
			Audit: buildClaudeCodeNativeAuditSummary(&ClaudeCodeNativeAttestationPayload{
				RequestURI:              ClaudeCodeNativeInboundCountTokens,
				GuardVersion:            "guard_v1",
				ClaudeCodeVersion:       "2.1.175",
				LocalSessionRef:         "hmac-sha256:" + strings.Repeat("b", 64),
				ShapeHealthcheckProfile: ClaudeCodeNativeControlPlaneHealthProfile,
			}, countTokens),
		},
	}

	health := EvaluateClaudeCodeNativeShapeHealthcheckSuite(fixtures, ClaudeCodeNativeShapeHealthcheckEvidence{
		LocalhostOnly:               true,
		MockUpstreamOnly:            true,
		ControlPlaneSafeSummary:     controlPlaneSafe,
		NetwatchSafeSummary:         netwatchSafe,
		RawBodiesOmittedFromAudit:   true,
		PromptCacheSafeUsageSummary: []byte(`{"provider_cache_mechanism":"anthropic_cache_control","cache_control_present":true,"cache_control_locations":["history","system","tools"],"prompt_caching_beta_present":true,"context_management_beta_present":true,"cache_usage_fields":["cache_creation_input_tokens","cache_read_input_tokens"],"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"stores_raw":false,"body_omitted":true,"response_omitted":true}`),
	})

	require.Equal(t, ClaudeCodeNativeShapeHealthcheckFail, health.Status)
	require.Contains(t, health.FailedFields, "prompt_cache_usage_fixture")
}

func TestClaudeCodeNativePromptCachingFixtureIgnoresSchemaFalsePositive(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","system":[{"type":"text","text":"safe synthetic system"}],"tools":[{"name":"Read","input_schema":{"type":"object","properties":{"cache_control":{"type":"object"}}}}],"messages":[{"role":"user","content":[{"type":"text","text":"safe synthetic user"}]}]}`)
	root := gjson.ParseBytes(body)
	require.False(t, claudeCodeNativeHasPromptCachingFixture(root))
}
