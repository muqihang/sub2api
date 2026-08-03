package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexGatewayAdminServiceStub struct {
	summary           *service.CodexGatewayAdminSummary
	providerGroups    []service.CodexGatewayProviderRuntime
	providerGroup     *service.CodexGatewayProviderRuntime
	models            []service.CodexGatewayManagedModel
	model             *service.CodexGatewayManagedModel
	smokeResult       *service.CodexGatewaySmokeResult
	stateStoreSummary *service.CodexGatewayStateStoreSummary
}

func (s *codexGatewayAdminServiceStub) GetSummary(context.Context) (*service.CodexGatewayAdminSummary, error) {
	return s.summary, nil
}

func (s *codexGatewayAdminServiceStub) ListProviderGroups(context.Context) ([]service.CodexGatewayProviderRuntime, error) {
	return s.providerGroups, nil
}

func (s *codexGatewayAdminServiceStub) UpdateProviderGroup(_ context.Context, provider service.CodexGatewayProvider, groupID int64) (*service.CodexGatewayProviderRuntime, error) {
	if s.providerGroup != nil {
		return s.providerGroup, nil
	}
	return &service.CodexGatewayProviderRuntime{
		Provider: provider,
		GroupID:  groupID,
	}, nil
}

func (s *codexGatewayAdminServiceStub) ListModels(context.Context) ([]service.CodexGatewayManagedModel, error) {
	return s.models, nil
}

func (s *codexGatewayAdminServiceStub) UpdateModel(_ context.Context, modelID string, input service.CodexGatewayModelMutation) (*service.CodexGatewayManagedModel, error) {
	if s.model != nil {
		return s.model, nil
	}
	return &service.CodexGatewayManagedModel{
		Model:       service.CodexGatewayModel{Slug: modelID},
		Enabled:     input.Enabled,
		Visible:     input.Enabled,
		SmokeStatus: input.SmokeStatus,
	}, nil
}

func (s *codexGatewayAdminServiceStub) Smoke(_ context.Context, input service.CodexGatewaySmokeRequest) (*service.CodexGatewaySmokeResult, error) {
	if s.smokeResult != nil {
		return s.smokeResult, nil
	}
	return &service.CodexGatewaySmokeResult{
		Status:  "accepted",
		ModelID: input.ModelID,
	}, nil
}

func (s *codexGatewayAdminServiceStub) GetStateStoreSummary(context.Context) (*service.CodexGatewayStateStoreSummary, error) {
	return s.stateStoreSummary, nil
}

func newCodexGatewayAdminHandlerTestRouter(t *testing.T, svc *codexGatewayAdminServiceStub) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewCodexGatewayHandler(svc)
	router.GET("/summary", h.Summary)
	router.GET("/provider-groups", h.ProviderGroups)
	router.PUT("/provider-groups", h.UpdateProviderGroups)
	router.GET("/models", h.Models)
	router.PUT("/models/:id", h.UpdateModel)
	router.POST("/smoke", h.Smoke)
	router.GET("/state-store/summary", h.StateStoreSummary)
	return router
}

func TestCodexGatewayAdminHandler_SummaryAndCollections(t *testing.T) {
	router := newCodexGatewayAdminHandlerTestRouter(t, &codexGatewayAdminServiceStub{
		summary: &service.CodexGatewayAdminSummary{
			ProviderGroups: []service.CodexGatewayProviderRuntime{
				{Provider: service.CodexGatewayProviderOpenAI, GroupID: 1001, Healthy: true},
			},
			Models: []service.CodexGatewayManagedModel{
				{
					Model:           service.CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai"},
					Enabled:         true,
					Visible:         true,
					SmokeStatus:     service.CodexGatewaySmokeStatusPassed,
					ProviderHealthy: true,
				},
			},
			StateStore: service.CodexGatewayStateStoreSummary{
				EntryCount:  2,
				TTLSeconds:  86400,
				MaxItems:    200,
				OldestEntry: "resp_old",
				NewestEntry: "resp_new",
			},
		},
		providerGroups: []service.CodexGatewayProviderRuntime{
			{Provider: service.CodexGatewayProviderOpenAI, GroupID: 1001, Healthy: true},
		},
		models: []service.CodexGatewayManagedModel{
			{
				Model:           service.CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai"},
				Enabled:         true,
				Visible:         true,
				SmokeStatus:     service.CodexGatewaySmokeStatusPassed,
				ProviderHealthy: true,
			},
		},
		stateStoreSummary: &service.CodexGatewayStateStoreSummary{
			EntryCount: 2,
			TTLSeconds: 86400,
			MaxItems:   200,
		},
	})

	for _, tc := range []struct {
		name         string
		method       string
		path         string
		body         string
		wantStatus   int
		wantContains []string
	}{
		{
			name:       "summary",
			method:     http.MethodGet,
			path:       "/summary",
			wantStatus: http.StatusOK,
			wantContains: []string{
				`"provider_groups"`,
				`"models_section"`,
				`"state_store"`,
				`"provider_group_count":1`,
				`"enabled_model_count":1`,
				`"state_entry_count":2`,
			},
		},
		{
			name:       "provider groups",
			method:     http.MethodGet,
			path:       "/provider-groups",
			wantStatus: http.StatusOK,
			wantContains: []string{
				`"rows"`,
				`"provider":"openai"`,
			},
		},
		{
			name:       "models",
			method:     http.MethodGet,
			path:       "/models",
			wantStatus: http.StatusOK,
			wantContains: []string{
				`"rows"`,
				`"slug":"gpt-5.5"`,
				`"smoke_status":"passed"`,
			},
		},
		{
			name:       "state store summary",
			method:     http.MethodGet,
			path:       "/state-store/summary",
			wantStatus: http.StatusOK,
			wantContains: []string{
				`"entry_count":2`,
				`"ttl_seconds":86400`,
				`"max_items":200`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code)
			for _, fragment := range tc.wantContains {
				require.Contains(t, rec.Body.String(), fragment)
			}
		})
	}
}

func TestCodexGatewayAdminHandler_MutationsAndSmoke(t *testing.T) {
	router := newCodexGatewayAdminHandlerTestRouter(t, &codexGatewayAdminServiceStub{
		providerGroup: &service.CodexGatewayProviderRuntime{
			Provider: service.CodexGatewayProviderDeepSeek,
			GroupID:  2002,
			Healthy:  true,
		},
		model: &service.CodexGatewayManagedModel{
			Model:           service.CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek"},
			Enabled:         true,
			Visible:         false,
			SmokeStatus:     service.CodexGatewaySmokeStatusPassed,
			ProviderHealthy: true,
		},
		smokeResult: &service.CodexGatewaySmokeResult{
			Status:    "accepted",
			ModelID:   "deepseek-v4-pro",
			RequestID: "smoke-1",
			Message:   "smoke execution is not wired in MVP",
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/provider-groups", bytes.NewBufferString(`{"provider":"deepseek","group_id":2002}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"provider":"deepseek"`)
	require.Contains(t, rec.Body.String(), `"group_id":2002`)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/models/deepseek-v4-pro", bytes.NewBufferString(`{"enabled":true,"smoke_status":"passed"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"slug":"deepseek-v4-pro"`)
	require.Contains(t, rec.Body.String(), `"smoke_status":"passed"`)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/smoke", bytes.NewBufferString(`{"model_id":"deepseek-v4-pro","request_id":"smoke-1"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"status":"accepted"`)
	require.Contains(t, rec.Body.String(), `"model_id":"deepseek-v4-pro"`)
	require.Contains(t, rec.Body.String(), `"request_id":"smoke-1"`)
}
