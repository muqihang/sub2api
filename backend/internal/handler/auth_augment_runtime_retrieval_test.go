package handler

import (
	"bytes"
	"encoding/json"
	"io"
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
	workspaceRoot := prepareAugmentContextBundleGitWorkspace(t)
	t.Setenv("AUGMENT_LEGACY_WORKSPACE_ROOT", workspaceRoot)
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
	restrictedClientProduct := service.AugmentClientProductZhumeng
	apiKey.RestrictedClientProduct = &restrictedClientProduct
	group := service.Group{
		ID:                     107,
		Name:                   "OpenAI",
		Platform:               service.PlatformOpenAI,
		Status:                 service.StatusActive,
		Hydrated:               true,
		AugmentGatewayEntitled: true,
		DefaultMappedModel:     "gpt-5.4",
	}
	apiKey.GroupID = &group.ID
	apiKey.Group = &group

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
	require.Contains(t, formatted, "Augment context metadata:")
	require.Contains(t, formatted, "workspace_root: "+workspaceRoot)
	require.Contains(t, formatted, "branch: feature/context-bundle")
	require.Contains(t, formatted, "worktree: "+workspaceRoot)
	require.Contains(t, formatted, "checkpoint_id: "+checkpointID)
	require.Contains(t, formatted, "active_blob_count: 1")
	require.NotContains(t, formatted, "unknown")
}

func TestAugmentLegacyCodebaseRetrievalOfficialPassthroughUsesPoolSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	officialCalls := 0
	officialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialCalls++
		require.Equal(t, "/agents/codebase-retrieval", r.URL.Path)
		require.Equal(t, "Bearer official-access-token", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), `"information_request":"find the official retrieval route"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"formatted_retrieval":"official retrieval result"}`))
	}))
	defer officialServer.Close()

	now := time.Now().UTC()
	user := &service.User{ID: 7, Email: "retrieval@example.com", Role: service.RoleAdmin, Status: service.StatusActive}
	apiKey := &service.APIKey{
		ID:        17,
		UserID:    user.ID,
		Key:       "sk-compat-retrieval-official",
		Name:      "compat-retrieval-official",
		Status:    service.StatusActive,
		CreatedAt: now,
		User:      user,
	}
	restrictedClientProduct := service.AugmentClientProductZhumeng
	apiKey.RestrictedClientProduct = &restrictedClientProduct
	group := service.Group{ID: 107, Name: "OpenAI", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, AugmentGatewayEntitled: true, DefaultMappedModel: "gpt-5.4"}
	apiKey.GroupID = &group.ID
	apiKey.Group = &group

	pluginService := service.NewAugmentPluginService(
		&config.Config{
			Server:  config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
			Gateway: config.GatewayConfig{Augment: config.GatewayAugmentConfig{Enabled: true}},
		},
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
			public: &service.PublicSettings{SiteName: "逐梦站", APIBaseURL: "http://127.0.0.1:18081"},
		},
	)

	cipher := newHandlerTestAugmentSessionVaultCipher(t)
	encryptedPayload := mustEncryptHandlerOfficialPayload(t, cipher, map[string]string{
		"access_token": "official-access-token",
	})
	poolService := service.NewAugmentOfficialPoolSessionService(
		&handlerOfficialPoolSessionStoreStub{
			credentialRow: &service.AugmentOfficialPoolStoredCredentialRow{
				ID:                         1,
				Source:                     "official_quick_login",
				TenantOrigin:               officialServer.URL,
				Scopes:                     []string{"email"},
				Status:                     service.AugmentOfficialPoolSessionStatusActive,
				EncryptedCredentialPayload: encryptedPayload,
				CredentialSchemaVersion:    1,
				KeyVersion:                 "local",
				Fingerprint:                "fingerprint-1",
				HealthScore:                100,
			},
			adminViews: []service.AugmentOfficialPoolStoredAdminView{
				{
					ID:                   1,
					Source:               "official_quick_login",
					TenantOrigin:         officialServer.URL,
					Scopes:               []string{"email"},
					Status:               service.AugmentOfficialPoolSessionStatusActive,
					Fingerprint:          "fingerprint-1",
					CreatedAt:            now,
					UpdatedAt:            now,
					LastSuccessAt:        handlerTimePtr(now),
					HealthScore:          100,
					HasCredentialPayload: true,
				},
			},
		},
		cipher,
		"bind-secret",
	)

	authHandler := NewAuthHandler(
		&config.Config{
			Server:  config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
			Gateway: config.GatewayConfig{Augment: config.GatewayAugmentConfig{Enabled: true}},
		},
		nil, nil, nil, nil, nil, nil,
		pluginService,
		poolService,
	)

	router := gin.New()
	router.POST("/agents/codebase-retrieval", authHandler.AugmentLegacyCodebaseRetrieval)

	req := httptest.NewRequest(http.MethodPost, "/agents/codebase-retrieval", strings.NewReader(`{"information_request":"find the official retrieval route","blobs":{"checkpoint_id":"","added_blobs":[],"deleted_blobs":[]},"dialog":[],"max_output_length":2000}`))
	req.Header.Set("Authorization", "Bearer "+apiKey.Key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, officialCalls)
	require.JSONEq(t, `{"formatted_retrieval":"official retrieval result"}`, rec.Body.String())
}

func TestAugmentLegacyPromptEnhancerOfficialPassthroughUsesPoolSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	officialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/prompt-enhancer", r.URL.Path)
		require.Equal(t, "Bearer official-access-token", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), `"model":"gpt-5.4"`)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"text\":\"official enhanced prompt\",\"workspace_file_chunks\":[],\"incorporated_external_sources\":[],\"nodes\":[]}\n"))
	}))
	defer officialServer.Close()

	now := time.Now().UTC()
	user := &service.User{ID: 8, Email: "prompt@example.com", Role: service.RoleAdmin, Status: service.StatusActive}
	apiKey := &service.APIKey{
		ID:        18,
		UserID:    user.ID,
		Key:       "sk-compat-prompt-official",
		Name:      "compat-prompt-official",
		Status:    service.StatusActive,
		CreatedAt: now,
		User:      user,
	}
	restrictedClientProduct := service.AugmentClientProductZhumeng
	apiKey.RestrictedClientProduct = &restrictedClientProduct
	group := service.Group{ID: 108, Name: "OpenAI", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, AugmentGatewayEntitled: true, DefaultMappedModel: "gpt-5.4"}
	apiKey.GroupID = &group.ID
	apiKey.Group = &group

	pluginService := service.NewAugmentPluginService(
		&config.Config{
			Server:  config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
			Gateway: config.GatewayConfig{Augment: config.GatewayAugmentConfig{Enabled: true}},
		},
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
			public: &service.PublicSettings{SiteName: "逐梦站", APIBaseURL: "http://127.0.0.1:18081"},
		},
	)

	cipher := newHandlerTestAugmentSessionVaultCipher(t)
	encryptedPayload := mustEncryptHandlerOfficialPayload(t, cipher, map[string]string{
		"access_token": "official-access-token",
	})
	poolService := service.NewAugmentOfficialPoolSessionService(
		&handlerOfficialPoolSessionStoreStub{
			credentialRow: &service.AugmentOfficialPoolStoredCredentialRow{
				ID:                         1,
				Source:                     "official_quick_login",
				TenantOrigin:               officialServer.URL,
				Scopes:                     []string{"email"},
				Status:                     service.AugmentOfficialPoolSessionStatusActive,
				EncryptedCredentialPayload: encryptedPayload,
				CredentialSchemaVersion:    1,
				KeyVersion:                 "local",
				Fingerprint:                "fingerprint-1",
				HealthScore:                100,
			},
			adminViews: []service.AugmentOfficialPoolStoredAdminView{
				{
					ID:                   1,
					Source:               "official_quick_login",
					TenantOrigin:         officialServer.URL,
					Scopes:               []string{"email"},
					Status:               service.AugmentOfficialPoolSessionStatusActive,
					Fingerprint:          "fingerprint-1",
					CreatedAt:            now,
					UpdatedAt:            now,
					LastSuccessAt:        handlerTimePtr(now),
					HealthScore:          100,
					HasCredentialPayload: true,
				},
			},
		},
		cipher,
		"bind-secret",
	)

	authHandler := NewAuthHandler(
		&config.Config{
			Server:  config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
			Gateway: config.GatewayConfig{Augment: config.GatewayAugmentConfig{Enabled: true}},
		},
		nil, nil, nil, nil, nil, nil,
		pluginService,
		poolService,
	)

	router := gin.New()
	router.POST("/prompt-enhancer", authHandler.AugmentLegacyPromptEnhancer)

	req := httptest.NewRequest(http.MethodPost, "/prompt-enhancer", bytes.NewReader([]byte(`{"model":"gpt-5.4","nodes":[{"id":1,"type":0,"text_node":{"content":"enhance this prompt"}}],"chat_history":[],"workspace_file_chunks":[],"incorporated_external_sources":[]}`)))
	req.Header.Set("Authorization", "Bearer "+apiKey.Key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "official enhanced prompt")
	require.Equal(t, "application/x-ndjson", rec.Header().Get("Content-Type"))
}

func TestAugmentLegacyPromptEnhancerExecutesWithThePoolSessionChosenByRoutePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	officialCalls := 0
	officialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialCalls++
		require.Equal(t, "/prompt-enhancer", r.URL.Path)
		require.Equal(t, "Bearer selected-access-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"text\":\"selected pool session\",\"workspace_file_chunks\":[],\"incorporated_external_sources\":[],\"nodes\":[]}\n"))
	}))
	defer officialServer.Close()

	now := time.Now().UTC()
	user := &service.User{ID: 9, Email: "pool-match@example.com", Role: service.RoleAdmin, Status: service.StatusActive}
	apiKey := &service.APIKey{
		ID:        19,
		UserID:    user.ID,
		Key:       "sk-compat-prompt-pool-match",
		Name:      "compat-prompt-pool-match",
		Status:    service.StatusActive,
		CreatedAt: now,
		User:      user,
	}
	restrictedClientProduct := service.AugmentClientProductZhumeng
	apiKey.RestrictedClientProduct = &restrictedClientProduct
	group := service.Group{ID: 109, Name: "OpenAI", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, AugmentGatewayEntitled: true, DefaultMappedModel: "gpt-5.4"}
	apiKey.GroupID = &group.ID
	apiKey.Group = &group

	pluginService := service.NewAugmentPluginService(
		&config.Config{
			Server:  config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
			Gateway: config.GatewayConfig{Augment: config.GatewayAugmentConfig{Enabled: true}},
		},
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
			public: &service.PublicSettings{SiteName: "逐梦站", APIBaseURL: "http://127.0.0.1:18081"},
		},
	)

	cipher := newHandlerTestAugmentSessionVaultCipher(t)
	selectedPayload := mustEncryptHandlerOfficialPayload(t, cipher, map[string]string{"access_token": "selected-access-token"})
	otherPayload := mustEncryptHandlerOfficialPayload(t, cipher, map[string]string{"access_token": "other-access-token"})
	store := &handlerOfficialPoolSessionStoreStub{
		credentialRow: &service.AugmentOfficialPoolStoredCredentialRow{
			ID:                         2,
			Source:                     "official_quick_login",
			TenantOrigin:               officialServer.URL,
			Scopes:                     []string{"email"},
			Status:                     service.AugmentOfficialPoolSessionStatusActive,
			EncryptedCredentialPayload: otherPayload,
			CredentialSchemaVersion:    1,
			KeyVersion:                 "local",
			Fingerprint:                "other-fingerprint",
			HealthScore:                200,
		},
		credentialRowsByID: map[int64]*service.AugmentOfficialPoolStoredCredentialRow{
			1: {
				ID:                         1,
				Source:                     "official_quick_login",
				TenantOrigin:               officialServer.URL,
				Scopes:                     []string{"email"},
				Status:                     service.AugmentOfficialPoolSessionStatusActive,
				EncryptedCredentialPayload: selectedPayload,
				CredentialSchemaVersion:    1,
				KeyVersion:                 "local",
				Fingerprint:                "selected-fingerprint",
				HealthScore:                100,
			},
			2: {
				ID:                         2,
				Source:                     "official_quick_login",
				TenantOrigin:               officialServer.URL,
				Scopes:                     []string{"email"},
				Status:                     service.AugmentOfficialPoolSessionStatusActive,
				EncryptedCredentialPayload: otherPayload,
				CredentialSchemaVersion:    1,
				KeyVersion:                 "local",
				Fingerprint:                "other-fingerprint",
				HealthScore:                200,
			},
		},
		adminViews: []service.AugmentOfficialPoolStoredAdminView{
			{
				ID:                   2,
				Source:               "official_quick_login",
				TenantOrigin:         officialServer.URL,
				Scopes:               []string{"email"},
				ExpiresAt:            handlerTimePtr(now.Add(time.Hour)),
				Status:               service.AugmentOfficialPoolSessionStatusActive,
				Fingerprint:          "other-fingerprint",
				CreatedAt:            now.Add(-time.Hour),
				UpdatedAt:            now,
				LastSuccessAt:        handlerTimePtr(now),
				HealthScore:          200,
				HasCredentialPayload: true,
			},
			{
				ID:                   1,
				Source:               "official_quick_login",
				TenantOrigin:         officialServer.URL,
				Scopes:               []string{"email"},
				ExpiresAt:            handlerTimePtr(now.Add(time.Hour)),
				Status:               service.AugmentOfficialPoolSessionStatusActive,
				Fingerprint:          "selected-fingerprint",
				CreatedAt:            now.Add(-2 * time.Hour),
				UpdatedAt:            now,
				LastSuccessAt:        handlerTimePtr(now.Add(-time.Minute)),
				HealthScore:          100,
				HasCredentialPayload: true,
			},
		},
	}
	poolService := service.NewAugmentOfficialPoolSessionService(store, cipher, "bind-secret")

	authHandler := NewAuthHandler(
		&config.Config{
			Server:  config.ServerConfig{FrontendURL: "http://127.0.0.1:18082"},
			Gateway: config.GatewayConfig{Augment: config.GatewayAugmentConfig{Enabled: true}},
		},
		nil, nil, nil, nil, nil, nil,
		pluginService,
		poolService,
	)

	router := gin.New()
	router.POST("/prompt-enhancer", authHandler.AugmentLegacyPromptEnhancer)

	req := httptest.NewRequest(http.MethodPost, "/prompt-enhancer", strings.NewReader(`{"model":"gpt-5.4","nodes":[{"id":1,"type":0,"text_node":{"content":"rewrite"}}],"chat_history":[],"workspace_file_chunks":[],"incorporated_external_sources":[]}`))
	req.Header.Set("Authorization", "Bearer "+apiKey.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-augment-official-fingerprint", "selected-fin")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, officialCalls)
	require.Equal(t, []int64{1}, store.acquiredSessionIDs)
}
