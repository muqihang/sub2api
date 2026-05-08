package service

import (
	"context"
	"math"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type AugmentGatewayBillingSummary struct {
	EstimatedCost            float64 `json:"estimated_cost"`
	SettledCost              float64 `json:"settled_cost"`
	FreeQuota                float64 `json:"free_quota"`
	PaidBalance              float64 `json:"paid_balance"`
	Currency                 string  `json:"currency"`
	CacheHitRatio            float64 `json:"cache_hit_ratio"`
	TotalCacheReadTokens     int64   `json:"total_cache_read_tokens"`
	TotalCacheCreationTokens int64   `json:"total_cache_creation_tokens"`
}

type AugmentGatewayBillingUsageRow struct {
	Model                  string    `json:"model"`
	Endpoint               string    `json:"endpoint"`
	Status                 string    `json:"status"`
	Tokens                 int       `json:"tokens"`
	CacheReadTokens        int       `json:"cache_read_tokens"`
	CacheCreationTokens    int       `json:"cache_creation_tokens"`
	EstimatedCost          float64   `json:"estimated_cost"`
	SettledCost            float64   `json:"settled_cost"`
	PricingVersion         string    `json:"pricing_version"`
	InputUnitPrice         float64   `json:"input_unit_price"`
	OutputUnitPrice        float64   `json:"output_unit_price"`
	CacheReadUnitPrice     float64   `json:"cache_read_unit_price"`
	CacheCreationUnitPrice float64   `json:"cache_creation_unit_price"`
	ReasoningUnitPrice     float64   `json:"reasoning_unit_price"`
	Billable               bool      `json:"billable"`
	CostSource             string    `json:"cost_source"`
	Currency               string    `json:"currency"`
	ErrorClass             string    `json:"error_class"`
	RequestID              string    `json:"request_id"`
	CreatedAt              time.Time `json:"created_at"`
}

type AugmentGatewayRecentErrorRow struct {
	Model      string    `json:"model"`
	Endpoint   string    `json:"endpoint"`
	Status     string    `json:"status"`
	ErrorClass string    `json:"error_class"`
	RequestID  string    `json:"request_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type AugmentGatewayUsageService struct {
	usageRepo UsageLogRepository
}

func NewAugmentGatewayUsageService(usageRepo UsageLogRepository) *AugmentGatewayUsageService {
	return &AugmentGatewayUsageService{usageRepo: usageRepo}
}

func (s *AugmentGatewayUsageService) GetSummary(ctx context.Context, userID int64) (*AugmentGatewayBillingSummary, error) {
	rows, err := s.loadAllRows(ctx, userID)
	if err != nil {
		return nil, err
	}

	summary := &AugmentGatewayBillingSummary{}
	var totalInputLike int64
	for _, row := range rows {
		if row.EstimatedCost != nil {
			summary.EstimatedCost += *row.EstimatedCost
		}
		if row.SettledCost != nil {
			summary.SettledCost += *row.SettledCost
		}
		if row.FreeQuotaApplied != nil {
			summary.FreeQuota += *row.FreeQuotaApplied
		}
		if row.PaidBalanceApplied != nil {
			summary.PaidBalance += *row.PaidBalanceApplied
		}
		summary.TotalCacheReadTokens += int64(row.CacheReadTokens)
		summary.TotalCacheCreationTokens += int64(row.CacheCreationTokens)
		totalInputLike += int64(row.InputTokens + row.CacheReadTokens)
		if summary.Currency == "" && row.Currency != nil {
			summary.Currency = strings.TrimSpace(*row.Currency)
		}
	}
	if summary.Currency == "" {
		summary.Currency = AugmentUsageCurrencyUSD
	}
	if totalInputLike > 0 {
		summary.CacheHitRatio = roundAugmentBillingFloat(float64(summary.TotalCacheReadTokens) / float64(totalInputLike))
	}
	return summary, nil
}

func (s *AugmentGatewayUsageService) ListUsage(ctx context.Context, userID int64, params pagination.PaginationParams) ([]AugmentGatewayBillingUsageRow, *pagination.PaginationResult, error) {
	if s == nil || s.usageRepo == nil {
		return nil, nil, infraerrors.ServiceUnavailable("AUGMENT_BILLING_UNAVAILABLE", "augment gateway usage service is unavailable")
	}
	logs, page, err := s.usageRepo.ListWithFilters(ctx, params, usagestats.UsageLogFilters{
		ClientProduct: AugmentUsageClientProduct,
		UserID:        userID,
		ExactTotal:    true,
	})
	if err != nil {
		return nil, nil, err
	}
	rows := make([]AugmentGatewayBillingUsageRow, 0, len(logs))
	for _, log := range logs {
		rows = append(rows, augmentUsageLogToRow(log))
	}
	return rows, page, nil
}

func (s *AugmentGatewayUsageService) ListRecentErrors(ctx context.Context, userID int64, limit int) ([]AugmentGatewayRecentErrorRow, error) {
	rows, err := s.loadAllRows(ctx, userID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	out := make([]AugmentGatewayRecentErrorRow, 0, limit)
	for _, row := range rows {
		status := augmentUsageStatus(row)
		errorClass := augmentUsageErrorClass(row, status)
		if errorClass == "" {
			continue
		}
		out = append(out, AugmentGatewayRecentErrorRow{
			Model:      row.Model,
			Endpoint:   derefUsageString(row.InboundEndpoint),
			Status:     status,
			ErrorClass: errorClass,
			RequestID:  row.RequestID,
			CreatedAt:  row.CreatedAt,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *AugmentGatewayUsageService) loadAllRows(ctx context.Context, userID int64) ([]UsageLog, error) {
	if s == nil || s.usageRepo == nil {
		return nil, infraerrors.ServiceUnavailable("AUGMENT_BILLING_UNAVAILABLE", "augment gateway usage service is unavailable")
	}
	all := make([]UsageLog, 0)
	page := 1
	pageSize := 500
	for {
		rows, pageInfo, err := s.usageRepo.ListWithFilters(ctx, pagination.PaginationParams{Page: page, PageSize: pageSize}, usagestats.UsageLogFilters{
			ClientProduct: AugmentUsageClientProduct,
			UserID:        userID,
			ExactTotal:    true,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if pageInfo == nil || page >= pageInfo.Pages || len(rows) == 0 {
			break
		}
		page++
	}
	return all, nil
}

func augmentUsageLogToRow(log UsageLog) AugmentGatewayBillingUsageRow {
	status := augmentUsageStatus(log)
	return AugmentGatewayBillingUsageRow{
		Model:                  log.RequestedModel,
		Endpoint:               derefUsageString(log.InboundEndpoint),
		Status:                 status,
		Tokens:                 log.TotalTokens(),
		CacheReadTokens:        log.CacheReadTokens,
		CacheCreationTokens:    log.CacheCreationTokens,
		EstimatedCost:          derefUsageFloat(log.EstimatedCost),
		SettledCost:            derefUsageFloat(log.SettledCost),
		PricingVersion:         derefUsageString(log.PricingVersion),
		InputUnitPrice:         derefUsageFloat(log.InputUnitPrice),
		OutputUnitPrice:        derefUsageFloat(log.OutputUnitPrice),
		CacheReadUnitPrice:     derefUsageFloat(log.CacheReadUnitPrice),
		CacheCreationUnitPrice: derefUsageFloat(log.CacheCreationUnitPrice),
		ReasoningUnitPrice:     derefUsageFloat(log.ReasoningUnitPrice),
		Billable:               derefUsageBool(log.Billable),
		CostSource:             derefUsageString(log.CostSource),
		Currency:               derefUsageString(log.Currency),
		ErrorClass:             augmentUsageErrorClass(log, status),
		RequestID:              log.RequestID,
		CreatedAt:              log.CreatedAt,
	}
}

func augmentUsageStatus(log UsageLog) string {
	switch strings.TrimSpace(derefUsageString(log.SettlementStatus)) {
	case AugmentUsageSettlementSettled:
		return "success"
	case AugmentUsageSettlementSkipped:
		return "skipped"
	default:
		if derefUsageBool(log.Billable) && derefUsageFloat(log.SettledCost) == 0 && derefUsageFloat(log.EstimatedCost) > 0 {
			return "error"
		}
		return "unknown"
	}
}

func augmentUsageErrorClass(log UsageLog, status string) string {
	if status == "error" {
		return "billing_unsettled"
	}
	return ""
}

func derefUsageString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func derefUsageFloat(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func derefUsageBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func roundAugmentBillingFloat(value float64) float64 {
	return math.Round(value*10000) / 10000
}
