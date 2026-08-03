package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAugmentBillingEndpointsRequireAuth(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	handler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil, &service.AugmentGatewayUsageService{})
	router := gin.New()
	router.GET("/api/v1/plugin/augment/billing/summary", handler.AugmentBillingSummary)
	router.GET("/api/v1/plugin/augment/billing/usage", handler.AugmentBillingUsage)
	router.GET("/api/v1/plugin/augment/billing/recent-errors", handler.AugmentBillingRecentErrors)

	for _, path := range []string{
		"/api/v1/plugin/augment/billing/summary",
		"/api/v1/plugin/augment/billing/usage",
		"/api/v1/plugin/augment/billing/recent-errors",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
}

func TestAugmentBillingEndpointsHideSensitiveFields(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	svc := &augmentGatewayUsageServiceStub{
		summary: &service.AugmentGatewayBillingSummary{
			EstimatedCost: 1.2,
			SettledCost:   1.1,
			Currency:      "USD",
		},
		rows: []service.AugmentGatewayBillingUsageRow{
			{
				Model:         "gpt-5.4",
				Endpoint:      "/chat",
				Status:        "success",
				Tokens:        123,
				EstimatedCost: 1.2,
				SettledCost:   1.1,
				RequestID:     "req-1",
				CreatedAt:     time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
			},
		},
		recentErrors: []service.AugmentGatewayRecentErrorRow{
			{
				Model:      "gpt-5.4",
				Endpoint:   "/chat",
				Status:     "error",
				ErrorClass: "billing_unsettled",
				RequestID:  "req-err-1",
				CreatedAt:  time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	handler := newAugmentBillingTestHandler(svc)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/api/v1/plugin/augment/billing/summary", handler.AugmentBillingSummary)
	router.GET("/api/v1/plugin/augment/billing/usage", handler.AugmentBillingUsage)
	router.GET("/api/v1/plugin/augment/billing/recent-errors", handler.AugmentBillingRecentErrors)

	for _, path := range []string{
		"/api/v1/plugin/augment/billing/summary",
		"/api/v1/plugin/augment/billing/usage",
		"/api/v1/plugin/augment/billing/recent-errors",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.NotContains(t, rec.Body.String(), "access_token")
		require.NotContains(t, rec.Body.String(), "refresh_token")
		require.NotContains(t, rec.Body.String(), "cookie")
		require.NotContains(t, rec.Body.String(), "prompt")
		require.NotContains(t, rec.Body.String(), "retrieval")
	}
}

type augmentGatewayUsageServiceStub struct {
	summary      *service.AugmentGatewayBillingSummary
	rows         []service.AugmentGatewayBillingUsageRow
	page         *pagination.PaginationResult
	recentErrors []service.AugmentGatewayRecentErrorRow
}

func (s *augmentGatewayUsageServiceStub) GetSummary(ctx context.Context, userID int64) (*service.AugmentGatewayBillingSummary, error) {
	return s.summary, nil
}

func (s *augmentGatewayUsageServiceStub) ListUsage(ctx context.Context, userID int64, params pagination.PaginationParams) ([]service.AugmentGatewayBillingUsageRow, *pagination.PaginationResult, error) {
	page := s.page
	if page == nil {
		page = &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize, Pages: 1, Total: int64(len(s.rows))}
	}
	return s.rows, page, nil
}

func (s *augmentGatewayUsageServiceStub) ListRecentErrors(ctx context.Context, userID int64, limit int) ([]service.AugmentGatewayRecentErrorRow, error) {
	return s.recentErrors, nil
}

func newAugmentBillingTestHandler(svc *augmentGatewayUsageServiceStub) *AuthHandler {
	authHandler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil)
	authHandler.augmentGatewayUsageService = (*service.AugmentGatewayUsageService)(nil)
	authHandler.augmentGatewayUsageService = service.NewAugmentGatewayUsageService(&augmentGatewayUsageRepoStubAdapter{stub: svc})
	return authHandler
}

type augmentGatewayUsageRepoStubAdapter struct {
	stub *augmentGatewayUsageServiceStub
}

func (a *augmentGatewayUsageRepoStubAdapter) Create(ctx context.Context, log *service.UsageLog) (bool, error) {
	return false, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetByID(ctx context.Context, id int64) (*service.UsageLog, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) Delete(ctx context.Context, id int64) error { return nil }
func (a *augmentGatewayUsageRepoStubAdapter) ListByUser(ctx context.Context, userID int64, params pagination.PaginationParams) ([]service.UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) ListByAPIKey(ctx context.Context, apiKeyID int64, params pagination.PaginationParams) ([]service.UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) ListByAccount(ctx context.Context, accountID int64, params pagination.PaginationParams) ([]service.UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) ListByUserAndTimeRange(ctx context.Context, userID int64, startTime, endTime time.Time) ([]service.UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) ListByAPIKeyAndTimeRange(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) ([]service.UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) ListByAccountAndTimeRange(ctx context.Context, accountID int64, startTime, endTime time.Time) ([]service.UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) ListByModelAndTimeRange(ctx context.Context, modelName string, startTime, endTime time.Time) ([]service.UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetAccountWindowStats(ctx context.Context, accountID int64, startTime time.Time) (*usagestats.AccountStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetAccountTodayStats(ctx context.Context, accountID int64) (*usagestats.AccountStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetDashboardStats(ctx context.Context) (*usagestats.DashboardStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetUsageTrendWithFilters(ctx context.Context, startTime, endTime time.Time, granularity string, userID, apiKeyID, accountID, groupID int64, model string, requestType *int16, stream *bool, billingType *int8) ([]usagestats.TrendDataPoint, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetModelStatsWithFilters(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, requestType *int16, stream *bool, billingType *int8) ([]usagestats.ModelStat, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetEndpointStatsWithFilters(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, model string, requestType *int16, stream *bool, billingType *int8) ([]usagestats.EndpointStat, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetUpstreamEndpointStatsWithFilters(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, model string, requestType *int16, stream *bool, billingType *int8) ([]usagestats.EndpointStat, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetGroupStatsWithFilters(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, requestType *int16, stream *bool, billingType *int8) ([]usagestats.GroupStat, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetUserBreakdownStats(ctx context.Context, startTime, endTime time.Time, dim usagestats.UserBreakdownDimension, limit int) ([]usagestats.UserBreakdownItem, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetAllGroupUsageSummary(ctx context.Context, todayStart time.Time) ([]usagestats.GroupUsageSummary, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetAPIKeyUsageTrend(ctx context.Context, startTime, endTime time.Time, granularity string, limit int) ([]usagestats.APIKeyUsageTrendPoint, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetUserUsageTrend(ctx context.Context, startTime, endTime time.Time, granularity string, limit int) ([]usagestats.UserUsageTrendPoint, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetUserSpendingRanking(ctx context.Context, startTime, endTime time.Time, limit int) (*usagestats.UserSpendingRankingResponse, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetBatchUserUsageStats(ctx context.Context, userIDs []int64, startTime, endTime time.Time) (map[int64]*usagestats.BatchUserUsageStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetBatchAPIKeyUsageStats(ctx context.Context, apiKeyIDs []int64, startTime, endTime time.Time) (map[int64]*usagestats.BatchAPIKeyUsageStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetUserDashboardStats(ctx context.Context, userID int64) (*usagestats.UserDashboardStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetAPIKeyDashboardStats(ctx context.Context, apiKeyID int64) (*usagestats.UserDashboardStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetUserUsageTrendByUserID(ctx context.Context, userID int64, startTime, endTime time.Time, granularity string) ([]usagestats.TrendDataPoint, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetUserModelStats(ctx context.Context, userID int64, startTime, endTime time.Time) ([]usagestats.ModelStat, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	logs := make([]service.UsageLog, 0, len(a.stub.rows))
	for _, row := range a.stub.rows {
		logs = append(logs, service.UsageLog{
			Model:          row.Model,
			RequestedModel: row.Model,
			RequestID:      row.RequestID,
			CreatedAt:      row.CreatedAt,
		})
	}
	return logs, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize, Pages: 1, Total: int64(len(logs))}, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetGlobalStats(ctx context.Context, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetStatsWithFilters(ctx context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetAccountUsageStats(ctx context.Context, accountID int64, startTime, endTime time.Time) (*usagestats.AccountUsageStatsResponse, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetUserStatsAggregated(ctx context.Context, userID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetAPIKeyStatsAggregated(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetAccountStatsAggregated(ctx context.Context, accountID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetModelStatsAggregated(ctx context.Context, modelName string, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (a *augmentGatewayUsageRepoStubAdapter) GetDailyStatsAggregated(ctx context.Context, userID int64, startTime, endTime time.Time) ([]map[string]any, error) {
	return nil, nil
}
