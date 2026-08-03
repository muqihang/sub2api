package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type noAccountFakeDiagnoser struct {
	calls []noAccountFakeDiagnoseCall
	resp  service.ModelAvailabilityDiagnosis
}

type noAccountFakeDiagnoseCall struct {
	GroupID  *int64
	Model    string
	Platform string
}

func (f *noAccountFakeDiagnoser) DiagnoseModelAvailabilityForPlatform(_ context.Context, groupID *int64, model, platform string) service.ModelAvailabilityDiagnosis {
	f.calls = append(f.calls, noAccountFakeDiagnoseCall{GroupID: groupID, Model: model, Platform: platform})
	return f.resp
}

func noAccountPtrInt64(v int64) *int64 { return &v }

func newNoAccountTestGinContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
	return c
}

func TestClassifyNoAccountError_ModelNotSupportedReturns404(t *testing.T) {
	c := newNoAccountTestGinContext()
	fd := &noAccountFakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: noAccountPtrInt64(42)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5.1-codex-mini", "gpt-5.1-codex-mini", service.PlatformOpenAI)

	require.Equal(t, http.StatusNotFound, cls.Status)
	require.Equal(t, "model_not_found", cls.ErrType)
	require.True(t, cls.ModelNotFound)
	require.Contains(t, cls.Message, "gpt-5.1-codex-mini")
	require.Len(t, fd.calls, 1)
	require.Equal(t, int64(42), *fd.calls[0].GroupID)
	require.Equal(t, "gpt-5.1-codex-mini", fd.calls[0].Model)
	require.Equal(t, service.PlatformOpenAI, fd.calls[0].Platform)
}

func TestClassifyNoAccountError_HasModelSupportStays503(t *testing.T) {
	c := newNoAccountTestGinContext()
	fd := &noAccountFakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	apiKey := &service.APIKey{GroupID: noAccountPtrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "api_error", cls.ErrType)
	require.False(t, cls.ModelNotFound)
}

func TestClassifyNoAccountError_NoAccountsInPoolStays503(t *testing.T) {
	c := newNoAccountTestGinContext()
	fd := &noAccountFakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: false, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: noAccountPtrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.ModelNotFound)
}

func TestClassifyNoAccountError_InvalidInputsDoNotConsultDiagnoser(t *testing.T) {
	c := newNoAccountTestGinContext()
	fd := &noAccountFakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}

	tests := []struct {
		name   string
		apiKey *service.APIKey
		model  string
	}{
		{name: "nil_api_key", apiKey: nil, model: "gpt-5"},
		{name: "nil_group", apiKey: &service.APIKey{}, model: "gpt-5"},
		{name: "empty_model", apiKey: &service.APIKey{GroupID: noAccountPtrInt64(1)}, model: "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fd.calls = nil
			cls := classifyNoAccountErrorFromGin(c, fd, tt.apiKey, tt.model, tt.model, service.PlatformOpenAI)
			require.Equal(t, http.StatusServiceUnavailable, cls.Status)
			require.False(t, cls.ModelNotFound)
			require.Empty(t, fd.calls)
		})
	}
}

type noAccountCountTokensAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r *noAccountCountTokensAccountRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, _ int64, platforms []string) ([]service.Account, error) {
	return r.listByPlatforms(platforms), nil
}

func (r *noAccountCountTokensAccountRepo) ListSchedulableByPlatforms(_ context.Context, platforms []string) ([]service.Account, error) {
	return r.listByPlatforms(platforms), nil
}

func (r *noAccountCountTokensAccountRepo) listByPlatforms(platforms []string) []service.Account {
	allowed := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = struct{}{}
	}
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if _, ok := allowed[account.Platform]; ok {
			out = append(out, account)
		}
	}
	return out
}

type noAccountCountTokensGroupRepo struct {
	group *service.Group
}

func (r *noAccountCountTokensGroupRepo) Create(context.Context, *service.Group) error { return nil }
func (r *noAccountCountTokensGroupRepo) GetByID(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}
func (r *noAccountCountTokensGroupRepo) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}
func (r *noAccountCountTokensGroupRepo) Update(context.Context, *service.Group) error { return nil }
func (r *noAccountCountTokensGroupRepo) Delete(context.Context, int64) error          { return nil }
func (r *noAccountCountTokensGroupRepo) DeleteCascade(context.Context, int64) ([]int64, error) {
	return nil, nil
}
func (r *noAccountCountTokensGroupRepo) List(context.Context, pagination.PaginationParams) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *noAccountCountTokensGroupRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *noAccountCountTokensGroupRepo) ListActive(context.Context) ([]service.Group, error) {
	return nil, nil
}
func (r *noAccountCountTokensGroupRepo) ListActiveByPlatform(context.Context, string) ([]service.Group, error) {
	return nil, nil
}
func (r *noAccountCountTokensGroupRepo) ExistsByName(context.Context, string) (bool, error) {
	return false, nil
}
func (r *noAccountCountTokensGroupRepo) GetAccountCount(context.Context, int64) (int64, int64, error) {
	return 0, 0, nil
}
func (r *noAccountCountTokensGroupRepo) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (r *noAccountCountTokensGroupRepo) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return nil, nil
}
func (r *noAccountCountTokensGroupRepo) BindAccountsToGroup(context.Context, int64, []int64) error {
	return nil
}
func (r *noAccountCountTokensGroupRepo) UpdateSortOrders(context.Context, []service.GroupSortOrderUpdate) error {
	return nil
}

func TestGatewayHandlerCountTokensNoSupportedModelReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(78)
	user := &service.User{ID: 12, Role: service.RoleUser, Status: service.StatusActive}
	group := &service.Group{ID: groupID, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	accountRepo := &noAccountCountTokensAccountRepo{accounts: []service.Account{{
		ID:          501,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"claude-supported": "claude-supported"},
		},
	}}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	gatewaySvc := service.NewGatewayService(
		accountRepo,
		&noAccountCountTokensGroupRepo{group: group},
		nil, nil, nil, nil, nil, nil,
		cfg,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil)
	defer billingCache.Stop()

	body := `{"model":"claude-unsupported","messages":[{"role":"user","content":"safe test input"}]}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(body))
	c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{ID: 91, UserID: user.ID, User: user, GroupID: &groupID, Group: group, Status: service.StatusActive})
	c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: user.ID})

	(&GatewayHandler{gatewayService: gatewaySvc, billingCacheService: billingCache}).CountTokens(c)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "model_not_found", gjson.Get(rec.Body.String(), "error.type").String())
	require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), "claude-unsupported")
	require.NotContains(t, rec.Body.String(), "safe test input")
}
