package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestAugmentGatewayUsageServiceFiltersByClientProduct(t *testing.T) {
	t.Parallel()

	repo := &augmentGatewayUsageRepoStub{
		logs: []UsageLog{{RequestID: "req-1"}},
		page: &pagination.PaginationResult{Page: 1, PageSize: 20, Pages: 1, Total: 1},
	}
	svc := NewAugmentGatewayUsageService(repo)

	_, _, err := svc.ListUsage(context.Background(), 42, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, AugmentUsageClientProduct, repo.lastFilters.ClientProduct)
	require.Equal(t, int64(42), repo.lastFilters.UserID)
}

func TestAugmentGatewayUsageServiceAggregatesCacheHitRatio(t *testing.T) {
	t.Parallel()

	repo := &augmentGatewayUsageRepoStub{
		logs: []UsageLog{
			{InputTokens: 100, CacheReadTokens: 50},
			{InputTokens: 50, CacheReadTokens: 50},
		},
		page: &pagination.PaginationResult{Page: 1, PageSize: 500, Pages: 1, Total: 2},
	}
	svc := NewAugmentGatewayUsageService(repo)

	summary, err := svc.GetSummary(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(100), summary.TotalCacheReadTokens)
	require.Equal(t, 0.4, summary.CacheHitRatio)
}

func TestAugmentGatewayUsageServiceSeparatesEstimatedAndSettledCost(t *testing.T) {
	t.Parallel()

	estimated1 := 1.25
	estimated2 := 0.50
	settled1 := 1.10
	settled2 := 0.00
	repo := &augmentGatewayUsageRepoStub{
		logs: []UsageLog{
			{AugmentUsageFields: AugmentUsageFields{EstimatedCost: &estimated1, SettledCost: &settled1}},
			{AugmentUsageFields: AugmentUsageFields{EstimatedCost: &estimated2, SettledCost: &settled2}},
		},
		page: &pagination.PaginationResult{Page: 1, PageSize: 500, Pages: 1, Total: 2},
	}
	svc := NewAugmentGatewayUsageService(repo)

	summary, err := svc.GetSummary(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, 1.75, summary.EstimatedCost)
	require.Equal(t, 1.10, summary.SettledCost)
}

func TestAugmentGatewayUsageServiceAdminSummaryAggregatesAcrossAllAugmentRows(t *testing.T) {
	t.Parallel()

	estimated1 := 1.25
	estimated2 := 0.50
	settled1 := 1.10
	settled2 := 0.25
	freeQuota := 0.40
	paidBalance := 1.35
	repo := &augmentGatewayUsageRepoStub{
		logs: []UsageLog{
			{AugmentUsageFields: AugmentUsageFields{EstimatedCost: &estimated1, SettledCost: &settled1, FreeQuotaApplied: &freeQuota, PaidBalanceApplied: &paidBalance}},
			{AugmentUsageFields: AugmentUsageFields{EstimatedCost: &estimated2, SettledCost: &settled2}},
		},
		page: &pagination.PaginationResult{Page: 1, PageSize: 500, Pages: 1, Total: 2},
	}
	svc := NewAugmentGatewayUsageService(repo)

	summary, err := svc.GetSummaryAdmin(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1.75, summary.EstimatedCost)
	require.Equal(t, 1.35, summary.SettledCost)
	require.Equal(t, freeQuota, summary.FreeQuota)
	require.Equal(t, paidBalance, summary.PaidBalance)
	require.Equal(t, AugmentUsageClientProduct, repo.lastFilters.ClientProduct)
	require.Zero(t, repo.lastFilters.UserID)
}

func TestAugmentGatewayUsageServiceReturnsPriceSnapshotAndBalanceFields(t *testing.T) {
	t.Parallel()

	currency := AugmentUsageCurrencyUSD
	pricingVersion := AugmentUsagePricingVersionV1
	billable := true
	inputUnitPrice := 0.01
	outputUnitPrice := 0.02
	cacheReadUnitPrice := 0.003
	cacheCreationUnitPrice := 0.004
	reasoningUnitPrice := 0.0
	freeQuota := 0.0
	paidBalance := 1.10
	estimatedCost := 1.25
	settledCost := 1.10
	repo := &augmentGatewayUsageRepoStub{
		logs: []UsageLog{
			{
				Model:          "gpt-5.4",
				RequestedModel: "gpt-5.4",
				RequestID:      "req-price",
				CreatedAt:      time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
				AugmentUsageFields: AugmentUsageFields{
					Currency:               &currency,
					PricingVersion:         &pricingVersion,
					Billable:               &billable,
					InputUnitPrice:         &inputUnitPrice,
					OutputUnitPrice:        &outputUnitPrice,
					CacheReadUnitPrice:     &cacheReadUnitPrice,
					CacheCreationUnitPrice: &cacheCreationUnitPrice,
					ReasoningUnitPrice:     &reasoningUnitPrice,
					FreeQuotaApplied:       &freeQuota,
					PaidBalanceApplied:     &paidBalance,
					EstimatedCost:          &estimatedCost,
					SettledCost:            &settledCost,
					SettlementStatus:       stringPtr(AugmentUsageSettlementSettled),
				},
			},
		},
		page: &pagination.PaginationResult{Page: 1, PageSize: 20, Pages: 1, Total: 1},
	}
	svc := NewAugmentGatewayUsageService(repo)

	rows, _, err := svc.ListUsage(context.Background(), 42, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, pricingVersion, rows[0].PricingVersion)
	require.Equal(t, inputUnitPrice, rows[0].InputUnitPrice)
	require.Equal(t, outputUnitPrice, rows[0].OutputUnitPrice)
	require.Equal(t, cacheReadUnitPrice, rows[0].CacheReadUnitPrice)
	require.Equal(t, cacheCreationUnitPrice, rows[0].CacheCreationUnitPrice)
	require.Equal(t, currency, rows[0].Currency)

	summary, err := svc.GetSummary(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, paidBalance, summary.PaidBalance)
	require.Equal(t, currency, summary.Currency)
}

func TestAugmentGatewayUsageServiceUsesBackendPricingForFailuresPartialStreamsRetryCacheAndReasoning(t *testing.T) {
	t.Parallel()

	billable := false
	estimatedCost := 0.42
	settledCost := 0.0
	costSource := AugmentUsageCostSourceProviderUsage
	repo := &augmentGatewayUsageRepoStub{
		logs: []UsageLog{
			{
				Model:          "gpt-5.4",
				RequestedModel: "gpt-5.4",
				RequestID:      "req-failure",
				CreatedAt:      time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
				AugmentUsageFields: AugmentUsageFields{
					Billable:         &billable,
					EstimatedCost:    &estimatedCost,
					SettledCost:      &settledCost,
					CostSource:       &costSource,
					SettlementStatus: stringPtr(AugmentUsageSettlementSkipped),
				},
			},
		},
		page: &pagination.PaginationResult{Page: 1, PageSize: 20, Pages: 1, Total: 1},
	}
	svc := NewAugmentGatewayUsageService(repo)

	rows, _, err := svc.ListUsage(context.Background(), 42, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.False(t, rows[0].Billable)
	require.Equal(t, estimatedCost, rows[0].EstimatedCost)
	require.Equal(t, settledCost, rows[0].SettledCost)
	require.Equal(t, costSource, rows[0].CostSource)
}

func TestAugmentGatewayUsageServiceHidesPromptAndRetrievalBodies(t *testing.T) {
	t.Parallel()

	repo := &augmentGatewayUsageRepoStub{
		logs: []UsageLog{
			{
				Model:          "gpt-5.5",
				RequestedModel: "gpt-5.5",
				RequestID:      "req-hidden",
				CreatedAt:      time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
			},
		},
		page: &pagination.PaginationResult{Page: 1, PageSize: 20, Pages: 1, Total: 1},
	}
	svc := NewAugmentGatewayUsageService(repo)

	rows, _, err := svc.ListUsage(context.Background(), 42, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	data, err := json.Marshal(rows[0])
	require.NoError(t, err)
	require.NotContains(t, string(data), "prompt")
	require.NotContains(t, string(data), "retrieval")
}

func TestAugmentGatewayUsageServiceAdminRowsExposePersistedAugmentRoutingFields(t *testing.T) {
	t.Parallel()

	groupID := int64(301)
	upstreamModel := "gpt-5.4-mini"
	requestScope := AugmentUsageRequestScopeGateway
	featureScope := AugmentUsageFeatureScopeContextEngine
	augmentSessionID := "augment-session-1"
	routePolicyVersion := "2026-05-08"
	repo := &augmentGatewayUsageRepoStub{
		logs: []UsageLog{
			{
				RequestedModel: "gpt-5.4",
				UpstreamModel:  &upstreamModel,
				GroupID:        &groupID,
				RequestID:      "req-routing",
				AugmentUsageFields: AugmentUsageFields{
					RequestScope:       &requestScope,
					FeatureScope:       &featureScope,
					AugmentSessionID:   &augmentSessionID,
					RoutePolicyVersion: &routePolicyVersion,
				},
			},
		},
		page: &pagination.PaginationResult{Page: 1, PageSize: 20, Pages: 1, Total: 1},
	}
	svc := NewAugmentGatewayUsageService(repo)

	rows, _, err := svc.ListUsageAdmin(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, &groupID, rows[0].GroupID)
	require.Equal(t, upstreamModel, rows[0].UpstreamModel)
	require.Equal(t, requestScope, rows[0].RequestScope)
	require.Equal(t, featureScope, rows[0].FeatureScope)
	require.Equal(t, augmentSessionID, rows[0].AugmentSessionID)
	require.Equal(t, routePolicyVersion, rows[0].RoutePolicyVersion)
}

type augmentGatewayUsageRepoStub struct {
	logs        []UsageLog
	page        *pagination.PaginationResult
	lastFilters usagestats.UsageLogFilters
}

func (s *augmentGatewayUsageRepoStub) Create(ctx context.Context, log *UsageLog) (bool, error) {
	return false, nil
}
func (s *augmentGatewayUsageRepoStub) GetByID(ctx context.Context, id int64) (*UsageLog, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) Delete(ctx context.Context, id int64) error { return nil }
func (s *augmentGatewayUsageRepoStub) ListByUser(ctx context.Context, userID int64, params pagination.PaginationParams) ([]UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *augmentGatewayUsageRepoStub) ListByAPIKey(ctx context.Context, apiKeyID int64, params pagination.PaginationParams) ([]UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *augmentGatewayUsageRepoStub) ListByAccount(ctx context.Context, accountID int64, params pagination.PaginationParams) ([]UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *augmentGatewayUsageRepoStub) ListByUserAndTimeRange(ctx context.Context, userID int64, startTime, endTime time.Time) ([]UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *augmentGatewayUsageRepoStub) ListByAPIKeyAndTimeRange(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) ([]UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *augmentGatewayUsageRepoStub) ListByAccountAndTimeRange(ctx context.Context, accountID int64, startTime, endTime time.Time) ([]UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *augmentGatewayUsageRepoStub) ListByModelAndTimeRange(ctx context.Context, modelName string, startTime, endTime time.Time) ([]UsageLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetAccountWindowStats(ctx context.Context, accountID int64, startTime time.Time) (*usagestats.AccountStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetAccountTodayStats(ctx context.Context, accountID int64) (*usagestats.AccountStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetDashboardStats(ctx context.Context) (*usagestats.DashboardStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetUsageTrendWithFilters(ctx context.Context, startTime, endTime time.Time, granularity string, userID, apiKeyID, accountID, groupID int64, model string, requestType *int16, stream *bool, billingType *int8) ([]usagestats.TrendDataPoint, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetModelStatsWithFilters(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, requestType *int16, stream *bool, billingType *int8) ([]usagestats.ModelStat, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetEndpointStatsWithFilters(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, model string, requestType *int16, stream *bool, billingType *int8) ([]usagestats.EndpointStat, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetUpstreamEndpointStatsWithFilters(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, model string, requestType *int16, stream *bool, billingType *int8) ([]usagestats.EndpointStat, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetGroupStatsWithFilters(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, requestType *int16, stream *bool, billingType *int8) ([]usagestats.GroupStat, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetUserBreakdownStats(ctx context.Context, startTime, endTime time.Time, dim usagestats.UserBreakdownDimension, limit int) ([]usagestats.UserBreakdownItem, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetAllGroupUsageSummary(ctx context.Context, todayStart time.Time) ([]usagestats.GroupUsageSummary, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetAPIKeyUsageTrend(ctx context.Context, startTime, endTime time.Time, granularity string, limit int) ([]usagestats.APIKeyUsageTrendPoint, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetUserUsageTrend(ctx context.Context, startTime, endTime time.Time, granularity string, limit int) ([]usagestats.UserUsageTrendPoint, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetUserSpendingRanking(ctx context.Context, startTime, endTime time.Time, limit int) (*usagestats.UserSpendingRankingResponse, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetBatchUserUsageStats(ctx context.Context, userIDs []int64, startTime, endTime time.Time) (map[int64]*usagestats.BatchUserUsageStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetBatchAPIKeyUsageStats(ctx context.Context, apiKeyIDs []int64, startTime, endTime time.Time) (map[int64]*usagestats.BatchAPIKeyUsageStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetUserDashboardStats(ctx context.Context, userID int64) (*usagestats.UserDashboardStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetAPIKeyDashboardStats(ctx context.Context, apiKeyID int64) (*usagestats.UserDashboardStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetUserUsageTrendByUserID(ctx context.Context, userID int64, startTime, endTime time.Time, granularity string) ([]usagestats.TrendDataPoint, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetUserModelStats(ctx context.Context, userID int64, startTime, endTime time.Time) ([]usagestats.ModelStat, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]UsageLog, *pagination.PaginationResult, error) {
	s.lastFilters = filters
	return append([]UsageLog(nil), s.logs...), s.page, nil
}
func (s *augmentGatewayUsageRepoStub) GetGlobalStats(ctx context.Context, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetStatsWithFilters(ctx context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	s.lastFilters = filters
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetAccountUsageStats(ctx context.Context, accountID int64, startTime, endTime time.Time) (*usagestats.AccountUsageStatsResponse, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetUserStatsAggregated(ctx context.Context, userID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetAPIKeyStatsAggregated(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetAccountStatsAggregated(ctx context.Context, accountID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetModelStatsAggregated(ctx context.Context, modelName string, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	return nil, nil
}
func (s *augmentGatewayUsageRepoStub) GetDailyStatsAggregated(ctx context.Context, userID int64, startTime, endTime time.Time) ([]map[string]any, error) {
	return nil, nil
}

func stringPtr(value string) *string {
	return &value
}
