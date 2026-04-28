package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAugmentLegacyBatchUploadCheckpointAndRetrievalReturnRealBlobRecord(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:       7,
		Email:    "retrieval@example.com",
		Username: "retrieval-user",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	apiKey := &service.APIKey{
		ID:        17,
		UserID:    user.ID,
		Key:       "sk-compat-retrieval",
		Name:      "compat-retrieval",
		Status:    service.StatusActive,
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
		User:      user,
	}
	group := service.Group{
		ID:                 107,
		Name:               "OpenAI",
		Platform:           service.PlatformOpenAI,
		Status:             service.StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-5.4",
	}

	pluginService := service.NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		augmentPluginAuthStub{},
		augmentPluginUserStub{user: user},
		augmentPluginAPIKeyStub{
			apiKeyByValue: map[string]*service.APIKey{apiKey.Key: apiKey},
			keysByUser:    map[int64][]service.APIKey{user.ID: {*apiKey}},
			availableByUser: map[int64][]service.Group{
				user.ID: {group},
			},
		},
		augmentPluginSubscriptionStub{},
		augmentPluginSettingStub{
			public: &service.PublicSettings{
				SiteName:   "逐梦站",
				APIBaseURL: "http://127.0.0.1:18081",
			},
		},
	)

	authHandler := NewAuthHandler(
		&config.Config{Server: config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pluginService,
	)

	router := gin.New()
	router.POST("/batch-upload", authHandler.AugmentLegacyBatchUpload)
	router.POST("/checkpoint-blobs", authHandler.AugmentLegacyCheckpointBlobs)
	router.POST("/agents/codebase-retrieval", authHandler.AugmentLegacyCodebaseRetrieval)

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	postJSON := func(path string, body string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+apiKey.Key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		require.NoError(t, err)
		return resp
	}

	uploadResp := postJSON("/batch-upload", `{"blobs":[{"blob_name":"blob-gateway","path":"backend/internal/server/routes/gateway.go","content":"r.POST(\"/agents/codebase-retrieval\", h.Auth.AugmentLegacyCodebaseRetrieval)\n"}]}`)
	defer uploadResp.Body.Close()
	require.Equal(t, http.StatusOK, uploadResp.StatusCode)

	checkpointResp := postJSON("/checkpoint-blobs", `{"blobs":{"checkpoint_id":"","added_blobs":["blob-gateway"],"deleted_blobs":[]}}`)
	defer checkpointResp.Body.Close()
	require.Equal(t, http.StatusOK, checkpointResp.StatusCode)

	var checkpointBody map[string]any
	require.NoError(t, json.NewDecoder(checkpointResp.Body).Decode(&checkpointBody))
	checkpointID, ok := checkpointBody["new_checkpoint_id"].(string)
	require.True(t, ok)
	require.NotEmpty(t, checkpointID)

	retrievalResp := postJSON("/agents/codebase-retrieval", `{"information_request":"find the retrieval route","blobs":{"checkpoint_id":"`+checkpointID+`","added_blobs":[],"deleted_blobs":[]},"dialog":[],"max_output_length":2000}`)
	defer retrievalResp.Body.Close()
	require.Equal(t, http.StatusOK, retrievalResp.StatusCode)

	var retrievalBody map[string]string
	require.NoError(t, json.NewDecoder(retrievalResp.Body).Decode(&retrievalBody))
	formatted := retrievalBody["formatted_retrieval"]
	require.NotEmpty(t, formatted)
	require.Contains(t, formatted, "[CODEBASE_RETRIEVAL]")
	require.Contains(t, formatted, "request: find the retrieval route")
	require.Contains(t, formatted, "backend/internal/server/routes/gateway.go")
	require.Contains(t, formatted, "r.POST(\"/agents/codebase-retrieval\", h.Auth.AugmentLegacyCodebaseRetrieval)")
	require.NotContains(t, formatted, "unknown")
}
