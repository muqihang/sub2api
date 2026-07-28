package service

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeAccountTestModeWebSearch(t *testing.T) {
	require.Equal(t, AccountTestModeWebSearch, normalizeAccountTestMode(" web_search "))
	require.Equal(t, AccountTestModeWebSearch, normalizeAccountTestMode("WEB_SEARCH"))
}

func TestCreateOpenAIWebSearchProbePayloadRequiresHostedSearch(t *testing.T) {
	payload := createOpenAIWebSearchProbePayload("gpt-5.6-sol")
	require.Equal(t, "gpt-5.6-sol", payload["model"])
	require.Equal(t, false, payload["stream"])
	require.Equal(t, false, payload["store"])
	require.Equal(t, "required", payload["tool_choice"])

	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Equal(t, "web_search", gjson.GetBytes(encoded, "tools.0.type").String())
}

func TestOpenAIWebSearchProbeRequiresSemanticProof(t *testing.T) {
	require.True(t, isOpenAIWebSearchProbeSuccess(http.StatusOK, []byte(`{"output":[{"type":"web_search_call"},{"type":"message"}]}`)))
	require.False(t, isOpenAIWebSearchProbeSuccess(http.StatusOK, []byte(`{"output":[{"type":"message"}]}`)))
	require.False(t, isOpenAIWebSearchProbeSuccess(http.StatusBadGateway, []byte(`{"output":[{"type":"web_search_call"}]}`)))
	require.False(t, isOpenAIWebSearchProbeSuccess(http.StatusOK, []byte(`not-json`)))
}

func TestBuildOpenAIWebSearchProbeExtraUpdates(t *testing.T) {
	now := time.Date(2026, 7, 27, 22, 0, 0, 0, time.FixedZone("CST", 8*60*60))

	supported := buildOpenAIWebSearchProbeExtraUpdates(
		&http.Response{StatusCode: http.StatusOK},
		[]byte(`{"output":[{"type":"web_search_call"}]}`),
		nil,
		now,
	)
	require.Equal(t, true, supported["openai_web_search_supported"])
	require.Equal(t, "", supported["openai_web_search_last_error"])

	ignored := buildOpenAIWebSearchProbeExtraUpdates(
		&http.Response{StatusCode: http.StatusOK},
		[]byte(`{"output":[{"type":"message"}]}`),
		nil,
		now,
	)
	require.Equal(t, false, ignored["openai_web_search_supported"])
	require.Contains(t, ignored["openai_web_search_last_error"], "web_search_call")
}
