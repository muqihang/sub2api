package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryCreateSyncRequestTypeAndLegacyFields(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-1",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		InputTokens:    10,
		OutputTokens:   20,
		TotalCost:      1,
		ActualCost:     1,
		BillingType:    service.BillingTypeBalance,
		RequestType:    service.RequestTypeWSV2,
		Stream:         false,
		OpenAIWSMode:   false,
		CreatedAt:      createdAt,
	}
	prepared := prepareUsageLogInsert(log)

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(99), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Equal(t, int64(99), log.ID)
	require.Nil(t, log.ServiceTier)
	require.Equal(t, service.RequestTypeWSV2, log.RequestType)
	require.True(t, log.Stream)
	require.True(t, log.OpenAIWSMode)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryCreate_PersistsServiceTier(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	serviceTier := "priority"
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-service-tier",
		Model:          "gpt-5.4",
		RequestedModel: "gpt-5.4",
		ServiceTier:    &serviceTier,
		CreatedAt:      createdAt,
	}
	prepared := prepareUsageLogInsert(log)

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(100), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildUsageLogBestEffortInsertQuery_IncludesRequestedModelColumn(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-best-effort-query",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC),
	})

	query, args := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})

	require.Contains(t, query, "INSERT INTO usage_logs (")
	require.Contains(t, query, "\n\t\t\tmodel,\n\t\t\trequested_model,\n\t\t\tupstream_model,")
	require.Contains(t, query, "\n\t\t\trequest_id,\n\t\t\tmodel,\n\t\t\trequested_model,\n\t\t\tupstream_model,")
	require.Len(t, args, len(prepared.args))
	require.Equal(t, prepared.args[5], args[5])
}

func TestExecUsageLogInsertNoResult_PersistsRequestedModel(t *testing.T) {
	db, mock := newSQLMock(t)
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-best-effort-exec",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 4, 12, 0, 0, 0, time.UTC),
	})

	mock.ExpectExec("INSERT INTO usage_logs").
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := execUsageLogInsertNoResult(context.Background(), db, prepared)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareUsageLogInsert_ArgCountMatchesTypes(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-arg-count",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 5, 12, 0, 0, 0, time.UTC),
	})

	require.Len(t, prepared.args, len(usageLogInsertArgTypes))
}

func TestScanUsageLog_AllowsLegacyNullEntityAuditFields(t *testing.T) {
	db, mock := newSQLMock(t)
	createdAt := time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(strings.Split(usageLogSelectColumns, ", ")).
		AddRow(
			int64(99),                // id
			int64(1),                 // user_id
			int64(2),                 // api_key_id
			int64(3),                 // account_id
			"req-legacy-entity-null", // request_id
			"gpt-5",                  // model
			nil,                      // requested_model
			nil,                      // upstream_model
			nil,                      // entity_id
			nil,                      // entity_type
			nil,                      // claimed_entity_id
			nil,                      // group_id
			nil,                      // subscription_id
			10,                       // input_tokens
			5,                        // output_tokens
			0,                        // cache_creation_tokens
			0,                        // cache_read_tokens
			0,                        // cache_creation_5m_tokens
			0,                        // cache_creation_1h_tokens
			0,                        // image_output_tokens
			float64(0),               // image_output_cost
			float64(0),               // input_cost
			float64(0),               // output_cost
			float64(0),               // cache_creation_cost
			float64(0),               // cache_read_cost
			float64(0),               // total_cost
			float64(0),               // actual_cost
			float64(1),               // rate_multiplier
			nil,                      // account_rate_multiplier
			int16(0),                 // billing_type
			int16(service.RequestTypeSync),
			false,     // stream
			false,     // openai_ws_mode
			nil,       // duration_ms
			nil,       // first_token_ms
			nil,       // user_agent
			nil,       // ip_address
			0,         // image_count
			nil,       // image_size
			nil,       // image_input_size
			nil,       // image_output_size
			nil,       // image_size_source
			nil,       // image_size_breakdown
			nil,       // service_tier
			nil,       // reasoning_effort
			nil,       // inbound_endpoint
			nil,       // upstream_endpoint
			false,     // cache_ttl_overridden
			nil,       // channel_id
			nil,       // model_mapping_chain
			nil,       // billing_tier
			nil,       // billing_mode
			nil,       // account_stats_cost
			nil,       // client_product
			nil,       // request_scope
			nil,       // feature_scope
			nil,       // augment_session_id
			nil,       // route_policy_version
			nil,       // pricing_version
			nil,       // billable
			nil,       // cost_source
			nil,       // currency
			nil,       // upstream_attempt_id
			nil,       // settlement_status
			nil,       // input_unit_price
			nil,       // output_unit_price
			nil,       // cache_read_unit_price
			nil,       // cache_creation_unit_price
			nil,       // reasoning_unit_price
			nil,       // estimated_cost
			nil,       // settled_cost
			nil,       // free_quota_applied
			nil,       // paid_balance_applied
			createdAt, // created_at
		)
	mock.ExpectQuery("SELECT").
		WillReturnRows(rows)

	queryRows, err := db.QueryContext(context.Background(), "SELECT")
	require.NoError(t, err)
	defer queryRows.Close()
	require.True(t, queryRows.Next())

	log, err := scanUsageLog(queryRows)
	require.NoError(t, err)
	require.Equal(t, int64(99), log.ID)
	require.Nil(t, log.EntityID)
	require.Nil(t, log.EntityType)
	require.Nil(t, log.ClaimedEntityID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareUsageLogInsert_PersistsImageSizeMetadata(t *testing.T) {
	imageSize := "4K"
	inputSize := "1024x1024"
	outputSize := "3840x2160"
	source := "output"
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:             1,
		APIKeyID:           2,
		AccountID:          3,
		RequestID:          "req-image-metadata",
		Model:              "gpt-image-2",
		RequestedModel:     "gpt-image-2",
		ImageCount:         2,
		ImageSize:          &imageSize,
		ImageInputSize:     &inputSize,
		ImageOutputSize:    &outputSize,
		ImageSizeSource:    &source,
		ImageSizeBreakdown: map[string]int{"1K": 1, "4K": 1},
		CreatedAt:          time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC),
	})

	require.Equal(t, sql.NullString{String: imageSize, Valid: true}, prepared.args[37])
	require.Equal(t, sql.NullString{String: inputSize, Valid: true}, prepared.args[38])
	require.Equal(t, sql.NullString{String: outputSize, Valid: true}, prepared.args[39])
	require.Equal(t, sql.NullString{String: source, Valid: true}, prepared.args[40])
	breakdownJSON, ok := prepared.args[41].(string)
	require.True(t, ok)
	require.JSONEq(t, `{"1K":1,"4K":1}`, breakdownJSON)
}

func TestCoalesceTrimmedString(t *testing.T) {
	require.Equal(t, "fallback", coalesceTrimmedString(sql.NullString{}, "fallback"))
	require.Equal(t, "fallback", coalesceTrimmedString(sql.NullString{Valid: true, String: "   "}, "fallback"))
	require.Equal(t, "value", coalesceTrimmedString(sql.NullString{Valid: true, String: "value"}, "fallback"))
}

func TestUsageLogInsertStoresAugmentScopeFields(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	clientProduct := service.AugmentUsageClientProduct
	requestScope := service.AugmentUsageRequestScopeGateway
	featureScope := service.AugmentUsageFeatureScopeChat
	routePolicyVersion := service.AugmentOfficialRoutePolicyVersion
	pricingVersion := service.AugmentUsagePricingVersionV1
	costSource := service.AugmentUsageCostSourceProviderUsage
	currency := service.AugmentUsageCurrencyUSD
	upstreamAttemptID := "upstream-attempt-1"
	settlementStatus := service.AugmentUsageSettlementSettled
	augmentSessionID := "conv-123"
	billable := true
	estimatedCost := 1.25
	settledCost := 1.10
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-augment-scope",
		Model:          "gpt-5.4",
		RequestedModel: "gpt-5.4",
		CreatedAt:      createdAt,
		AugmentUsageFields: service.AugmentUsageFields{
			ClientProduct:      &clientProduct,
			RequestScope:       &requestScope,
			FeatureScope:       &featureScope,
			AugmentSessionID:   &augmentSessionID,
			RoutePolicyVersion: &routePolicyVersion,
			PricingVersion:     &pricingVersion,
			Billable:           &billable,
			CostSource:         &costSource,
			Currency:           &currency,
			UpstreamAttemptID:  &upstreamAttemptID,
			SettlementStatus:   &settlementStatus,
			EstimatedCost:      &estimatedCost,
			SettledCost:        &settledCost,
		},
	}
	prepared := prepareUsageLogInsert(log)

	require.Equal(t, clientProduct, prepared.args[52].(sql.NullString).String)
	require.Equal(t, requestScope, prepared.args[53].(sql.NullString).String)
	require.Equal(t, featureScope, prepared.args[54].(sql.NullString).String)
	require.Equal(t, augmentSessionID, prepared.args[55].(sql.NullString).String)
	require.Equal(t, routePolicyVersion, prepared.args[56].(sql.NullString).String)
	require.Equal(t, pricingVersion, prepared.args[57].(sql.NullString).String)
	require.Equal(t, billable, prepared.args[58].(sql.NullBool).Bool)
	require.Equal(t, upstreamAttemptID, prepared.args[61].(sql.NullString).String)
	require.Equal(t, estimatedCost, prepared.args[68].(sql.NullFloat64).Float64)
	require.Equal(t, settledCost, prepared.args[69].(sql.NullFloat64).Float64)

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(101), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareUsageLogInsert_PreservesEntityAndAugmentColumnOrder(t *testing.T) {
	clientProduct := service.AugmentUsageClientProduct
	requestScope := service.AugmentUsageRequestScopeGateway
	featureScope := service.AugmentUsageFeatureScopeChat
	entityType := service.EntityTypeWorkspace
	claimedEntityID := "workspace-alpha"
	log := &service.UsageLog{
		UserID:          1,
		APIKeyID:        2,
		AccountID:       3,
		RequestID:       "req-union-order",
		Model:           "gpt-5.4",
		RequestedModel:  "gpt-5.4",
		EntityID:        ptrInt64(99),
		EntityType:      &entityType,
		ClaimedEntityID: &claimedEntityID,
		CreatedAt:       time.Date(2026, 5, 8, 12, 30, 0, 0, time.UTC),
		AugmentUsageFields: service.AugmentUsageFields{
			ClientProduct: &clientProduct,
			RequestScope:  &requestScope,
			FeatureScope:  &featureScope,
		},
	}

	prepared := prepareUsageLogInsert(log)
	require.Equal(t, int64(99), prepared.args[7].(sql.NullInt64).Int64)
	require.Equal(t, entityType, prepared.args[8].(sql.NullString).String)
	require.Equal(t, claimedEntityID, prepared.args[9].(sql.NullString).String)
	require.Equal(t, clientProduct, prepared.args[52].(sql.NullString).String)
	require.Equal(t, requestScope, prepared.args[53].(sql.NullString).String)
	require.Equal(t, featureScope, prepared.args[54].(sql.NullString).String)

	query, _ := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
	require.Contains(t, query, "upstream_model,\n\t\t\tentity_id,\n\t\t\tentity_type,\n\t\t\tclaimed_entity_id,\n\t\t\tgroup_id,")
	require.Contains(t, query, "account_stats_cost,\n\t\t\t\tclient_product,\n\t\t\t\trequest_scope,\n\t\t\t\tfeature_scope,")
}

func TestUsageLogListFiltersClientProductZhumengAugment(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_logs WHERE client_product = \\$1").
		WithArgs(service.AugmentUsageClientProduct).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectQuery("SELECT .* FROM usage_logs WHERE client_product = \\$1 ORDER BY id DESC LIMIT \\$2 OFFSET \\$3").
		WithArgs(service.AugmentUsageClientProduct, 20, 0).
		WillReturnRows(sqlmock.NewRows(splitUsageLogSelectColumns()))

	logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, usagestats.UsageLogFilters{
		ClientProduct: service.AugmentUsageClientProduct,
		ExactTotal:    true,
	})
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NotNil(t, page)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogPricingVersionDoesNotChangeHistoricalCost(t *testing.T) {
	clientProduct := service.AugmentUsageClientProduct
	pricingVersion := service.AugmentUsagePricingVersionV1
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-pricing-version",
		Model:          "gpt-5.4",
		RequestedModel: "gpt-5.4",
		TotalCost:      2.5,
		ActualCost:     2.0,
		CreatedAt:      time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		AugmentUsageFields: service.AugmentUsageFields{
			ClientProduct:  &clientProduct,
			PricingVersion: &pricingVersion,
		},
	}
	prepared := prepareUsageLogInsert(log)
	require.Equal(t, 2.5, prepared.args[24])
	require.Equal(t, 2.0, prepared.args[25])
	require.Equal(t, pricingVersion, prepared.args[57].(sql.NullString).String)
}

func TestUsageLogProviderRetryDedupUsesRequestIDAndAttempt(t *testing.T) {
	clientProduct := service.AugmentUsageClientProduct
	upstreamAttemptID := "upstream-attempt-2"
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-provider-retry",
		Model:          "gpt-5.5",
		RequestedModel: "gpt-5.5",
		CreatedAt:      time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		AugmentUsageFields: service.AugmentUsageFields{
			ClientProduct:     &clientProduct,
			UpstreamAttemptID: &upstreamAttemptID,
		},
	}
	prepared := prepareUsageLogInsert(log)
	require.Equal(t, "req-provider-retry", prepared.requestID)
	require.Equal(t, upstreamAttemptID, prepared.args[61].(sql.NullString).String)
}

func TestUsageLogStoresPriceSnapshotForAugmentBilling(t *testing.T) {
	clientProduct := service.AugmentUsageClientProduct
	inputUnitPrice := 0.01
	outputUnitPrice := 0.02
	cacheReadUnitPrice := 0.003
	cacheCreationUnitPrice := 0.004
	reasoningUnitPrice := 0.005
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-price-snapshot",
		Model:          "gpt-5.5",
		RequestedModel: "gpt-5.5",
		CreatedAt:      time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		AugmentUsageFields: service.AugmentUsageFields{
			ClientProduct:          &clientProduct,
			InputUnitPrice:         &inputUnitPrice,
			OutputUnitPrice:        &outputUnitPrice,
			CacheReadUnitPrice:     &cacheReadUnitPrice,
			CacheCreationUnitPrice: &cacheCreationUnitPrice,
			ReasoningUnitPrice:     &reasoningUnitPrice,
		},
	}
	prepared := prepareUsageLogInsert(log)
	require.Equal(t, inputUnitPrice, prepared.args[63].(sql.NullFloat64).Float64)
	require.Equal(t, outputUnitPrice, prepared.args[64].(sql.NullFloat64).Float64)
	require.Equal(t, cacheReadUnitPrice, prepared.args[65].(sql.NullFloat64).Float64)
	require.Equal(t, cacheCreationUnitPrice, prepared.args[66].(sql.NullFloat64).Float64)
	require.Equal(t, reasoningUnitPrice, prepared.args[67].(sql.NullFloat64).Float64)
}

func TestUsageLogBillingRulesForFailuresPartialStreamsCacheAndReasoning(t *testing.T) {
	clientProduct := service.AugmentUsageClientProduct
	billable := false
	settlementStatus := service.AugmentUsageSettlementSkipped
	estimatedCost := 0.42
	settledCost := 0.0
	log := &service.UsageLog{
		UserID:              1,
		APIKeyID:            2,
		AccountID:           3,
		RequestID:           "req-billing-rules",
		Model:               "gpt-5.4-mini",
		RequestedModel:      "gpt-5.4-mini",
		CacheReadTokens:     12,
		CacheCreationTokens: 4,
		CreatedAt:           time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		AugmentUsageFields: service.AugmentUsageFields{
			ClientProduct:    &clientProduct,
			Billable:         &billable,
			SettlementStatus: &settlementStatus,
			EstimatedCost:    &estimatedCost,
			SettledCost:      &settledCost,
		},
	}
	prepared := prepareUsageLogInsert(log)
	require.Equal(t, false, prepared.args[58].(sql.NullBool).Bool)
	require.Equal(t, settlementStatus, prepared.args[62].(sql.NullString).String)
	require.Equal(t, estimatedCost, prepared.args[68].(sql.NullFloat64).Float64)
	require.Equal(t, settledCost, prepared.args[69].(sql.NullFloat64).Float64)
}

func TestAppendUsageLogBillingModeWhereCondition(t *testing.T) {
	tests := []struct {
		name          string
		billingMode   string
		wantCondition string
	}{
		{
			name:          "image includes legacy image rows",
			billingMode:   string(service.BillingModeImage),
			wantCondition: "(billing_mode = $1 OR COALESCE(image_count, 0) > 0)",
		},
		{
			name:          "token includes legacy non-image rows",
			billingMode:   string(service.BillingModeToken),
			wantCondition: "(billing_mode = $1 OR ((billing_mode IS NULL OR billing_mode = '') AND COALESCE(image_count, 0) <= 0))",
		},
		{
			name:          "per request remains exact",
			billingMode:   string(service.BillingModePerRequest),
			wantCondition: "billing_mode = $1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conditions, args := appendUsageLogBillingModeWhereCondition(nil, nil, tt.billingMode)
			require.Equal(t, []string{tt.wantCondition}, conditions)
			require.Equal(t, []any{tt.billingMode}, args)
		})
	}
}

func anySliceToDriverValues(values []any) []driver.Value {
	out := make([]driver.Value, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func splitUsageLogSelectColumns() []string {
	return strings.Split(usageLogSelectColumns, ", ")
}

func TestUsageLogRepositoryListWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	requestType := int16(service.RequestTypeWSV2)
	stream := false
	filters := usagestats.UsageLogFilters{
		RequestType: &requestType,
		Stream:      &stream,
		ExactTotal:  true,
	}

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_logs WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\)").
		WithArgs(requestType).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery("SELECT .* FROM usage_logs WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\) ORDER BY id DESC LIMIT \\$2 OFFSET \\$3").
		WithArgs(requestType, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NotNil(t, page)
	require.Equal(t, int64(0), page.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryListWithFiltersEntityPredicates(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	filters := usagestats.UsageLogFilters{
		EntityID:        123,
		EntityType:      service.EntityTypeWorkspace,
		ClaimedEntityID: "workspace-alpha",
		ExactTotal:      true,
	}

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_logs WHERE entity_id = \\$1 AND entity_type = \\$2 AND claimed_entity_id = \\$3").
		WithArgs(int64(123), service.EntityTypeWorkspace, "workspace-alpha").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery("SELECT .* FROM usage_logs WHERE entity_id = \\$1 AND entity_type = \\$2 AND claimed_entity_id = \\$3 ORDER BY id DESC LIMIT \\$4 OFFSET \\$5").
		WithArgs(int64(123), service.EntityTypeWorkspace, "workspace-alpha", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NotNil(t, page)
	require.Equal(t, int64(0), page.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetUsageTrendWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(service.RequestTypeStream)
	stream := true

	mock.ExpectQuery("AND \\(request_type = \\$3 OR \\(request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE\\)\\)").
		WithArgs(start, end, requestType).
		WillReturnRows(sqlmock.NewRows([]string{"date", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost"}))

	trend, err := repo.GetUsageTrendWithFilters(context.Background(), start, end, "day", 0, 0, 0, 0, "", &requestType, &stream, nil)
	require.NoError(t, err)
	require.Empty(t, trend)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetModelStatsWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(service.RequestTypeWSV2)
	stream := false

	mock.ExpectQuery("AND \\(request_type = \\$3 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\)").
		WithArgs(start, end, requestType).
		WillReturnRows(sqlmock.NewRows([]string{"model", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost", "account_cost"}))

	stats, err := repo.GetModelStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, &requestType, &stream, nil)
	require.NoError(t, err)
	require.Empty(t, stats)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetStatsWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	requestType := int16(service.RequestTypeSync)
	stream := true
	filters := usagestats.UsageLogFilters{
		RequestType: &requestType,
		Stream:      &stream,
	}

	mock.ExpectQuery("FROM usage_logs\\s+WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE\\)\\)").
		WithArgs(requestType).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests",
			"total_input_tokens",
			"total_output_tokens",
			"total_cache_tokens",
			"total_cache_creation_tokens",
			"total_cache_read_tokens",
			"total_cost",
			"total_actual_cost",
			"total_account_cost",
			"avg_duration_ms",
		}).AddRow(int64(1), int64(2), int64(3), int64(4), int64(1), int64(3), 1.2, 1.0, 1.2, 20.0))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(inbound_endpoint\\), ''\\), 'unknown'\\) AS endpoint").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), requestType).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(upstream_endpoint\\), ''\\), 'unknown'\\) AS endpoint").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), requestType).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))
	mock.ExpectQuery("SELECT CONCAT\\(").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), requestType).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))

	stats, err := repo.GetStatsWithFilters(context.Background(), filters)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.Equal(t, int64(9), stats.TotalTokens)
	require.Equal(t, int64(1), stats.TotalCacheCreationTokens)
	require.Equal(t, int64(3), stats.TotalCacheReadTokens)
	require.NotNil(t, stats.TotalAccountCost, "TotalAccountCost should always be returned")
	require.Equal(t, 1.2, *stats.TotalAccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetModelStatsAccountCostColumn(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("FROM usage_logs").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"model", "requests", "input_tokens", "output_tokens",
			"cache_creation_tokens", "cache_read_tokens", "total_tokens",
			"cost", "actual_cost", "account_cost",
		}).
			AddRow("claude-opus-4-6", int64(10), int64(100), int64(200), int64(5), int64(3), int64(308), 2.5, 2.0, 1.8).
			AddRow("claude-sonnet-4-6", int64(5), int64(50), int64(100), int64(0), int64(0), int64(150), 1.0, 0.8, 0.7))

	results, err := repo.GetModelStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "claude-opus-4-6", results[0].Model)
	require.Equal(t, 2.5, results[0].Cost)
	require.Equal(t, 2.0, results[0].ActualCost)
	require.Equal(t, 1.8, results[0].AccountCost)
	require.Equal(t, "claude-sonnet-4-6", results[1].Model)
	require.Equal(t, 0.7, results[1].AccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetGroupStatsAccountCostColumn(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("FROM usage_logs").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"group_id", "group_name", "requests", "total_tokens",
			"cost", "actual_cost", "account_cost",
		}).
			AddRow(int64(1), "azure-cc", int64(100), int64(5000), 10.0, 8.5, 7.2).
			AddRow(int64(2), "max", int64(50), int64(2000), 5.0, 4.0, 3.5))

	results, err := repo.GetGroupStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, int64(1), results[0].GroupID)
	require.Equal(t, "azure-cc", results[0].GroupName)
	require.Equal(t, 10.0, results[0].Cost)
	require.Equal(t, 8.5, results[0].ActualCost)
	require.Equal(t, 7.2, results[0].AccountCost)
	require.Equal(t, int64(2), results[1].GroupID)
	require.Equal(t, 3.5, results[1].AccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetStatsWithFiltersAlwaysReturnsAccountCost(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	// No AccountID filter set - TotalAccountCost should still be returned
	filters := usagestats.UsageLogFilters{}

	mock.ExpectQuery("FROM usage_logs").
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests", "total_input_tokens", "total_output_tokens",
			"total_cache_tokens", "total_cache_creation_tokens", "total_cache_read_tokens",
			"total_cost", "total_actual_cost",
			"total_account_cost", "avg_duration_ms",
		}).AddRow(int64(50), int64(1000), int64(2000), int64(100), int64(60), int64(40), 15.0, 12.5, 11.0, 100.0))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(inbound_endpoint\\)").
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(upstream_endpoint\\)").
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))
	mock.ExpectQuery("SELECT CONCAT\\(").
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))

	stats, err := repo.GetStatsWithFilters(context.Background(), filters)
	require.NoError(t, err)
	require.Equal(t, int64(60), stats.TotalCacheCreationTokens)
	require.Equal(t, int64(40), stats.TotalCacheReadTokens)
	require.NotNil(t, stats.TotalAccountCost, "TotalAccountCost must always be returned, even without AccountID filter")
	require.Equal(t, 11.0, *stats.TotalAccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetUserSpendingRanking(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	rows := sqlmock.NewRows([]string{"user_id", "email", "actual_cost", "requests", "tokens", "total_actual_cost", "total_requests", "total_tokens"}).
		AddRow(int64(2), "beta@example.com", 12.5, int64(9), int64(900), 40.0, int64(30), int64(2600)).
		AddRow(int64(1), "alpha@example.com", 12.5, int64(8), int64(800), 40.0, int64(30), int64(2600)).
		AddRow(int64(3), "gamma@example.com", 4.25, int64(5), int64(300), 40.0, int64(30), int64(2600))

	mock.ExpectQuery("WITH user_spend AS \\(").
		WithArgs(start, end, 12).
		WillReturnRows(rows)

	got, err := repo.GetUserSpendingRanking(context.Background(), start, end, 12)
	require.NoError(t, err)
	require.Equal(t, &usagestats.UserSpendingRankingResponse{
		Ranking: []usagestats.UserSpendingRankingItem{
			{UserID: 2, Email: "beta@example.com", ActualCost: 12.5, Requests: 9, Tokens: 900},
			{UserID: 1, Email: "alpha@example.com", ActualCost: 12.5, Requests: 8, Tokens: 800},
			{UserID: 3, Email: "gamma@example.com", ActualCost: 4.25, Requests: 5, Tokens: 300},
		},
		TotalActualCost: 40.0,
		TotalRequests:   30,
		TotalTokens:     2600,
	}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildRequestTypeFilterConditionLegacyFallback(t *testing.T) {
	tests := []struct {
		name      string
		request   int16
		wantWhere string
		wantArg   int16
	}{
		{
			name:      "sync_with_legacy_fallback",
			request:   int16(service.RequestTypeSync),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE))",
			wantArg:   int16(service.RequestTypeSync),
		},
		{
			name:      "stream_with_legacy_fallback",
			request:   int16(service.RequestTypeStream),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE))",
			wantArg:   int16(service.RequestTypeStream),
		},
		{
			name:      "ws_v2_with_legacy_fallback",
			request:   int16(service.RequestTypeWSV2),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND openai_ws_mode = TRUE))",
			wantArg:   int16(service.RequestTypeWSV2),
		},
		{
			name:      "invalid_request_type_normalized_to_unknown",
			request:   int16(99),
			wantWhere: "request_type = $3",
			wantArg:   int16(service.RequestTypeUnknown),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildRequestTypeFilterCondition(3, tt.request)
			require.Equal(t, tt.wantWhere, where)
			require.Equal(t, []any{tt.wantArg}, args)
		})
	}
}

type usageLogScannerStub struct {
	values []any
}

func (s usageLogScannerStub) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return fmt.Errorf("scan arg count mismatch: got %d want %d", len(dest), len(s.values))
	}
	for i := range dest {
		dv := reflect.ValueOf(dest[i])
		if dv.Kind() != reflect.Ptr {
			return fmt.Errorf("dest[%d] is not pointer", i)
		}
		dv.Elem().Set(reflect.ValueOf(s.values[i]))
	}
	return nil
}

func TestScanUsageLogRequestTypeAndLegacyFallback(t *testing.T) {
	t.Run("image_size_metadata_is_scanned", func(t *testing.T) {
		now := time.Now().UTC()
		values := []any{
			int64(4),
			int64(13),
			int64(23),
			int64(33),
			sql.NullString{Valid: true, String: "req-image-metadata"},
			"gpt-image-2",
			sql.NullString{Valid: true, String: "gpt-image-2"},
			sql.NullString{},
			sql.NullInt64{},  // entity_id
			sql.NullString{}, // entity_type
			sql.NullString{}, // claimed_entity_id
			sql.NullInt64{},
			sql.NullInt64{},
			0, 0, 0, 0, 0, 0,
			0, 0.0, // image_output_tokens, image_output_cost
			0.0, 0.0, 0.0, 0.0, 0.8, 0.8,
			1.0,
			sql.NullFloat64{},
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeSync),
			false,
			false,
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			2,
			sql.NullString{Valid: true, String: "4K"},
			sql.NullString{Valid: true, String: "1024x1024"},
			sql.NullString{Valid: true, String: "3840x2160"},
			sql.NullString{Valid: true, String: "output"},
			sql.NullString{Valid: true, String: `{"4K":2}`},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			false,
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			sql.NullFloat64{},
		}
		values = append(values, usageLogAugmentScanTail(now)...)
		log, err := scanUsageLog(usageLogScannerStub{values: values})
		require.NoError(t, err)
		require.Equal(t, 2, log.ImageCount)
		require.NotNil(t, log.ImageSize)
		require.Equal(t, "4K", *log.ImageSize)
		require.NotNil(t, log.ImageInputSize)
		require.Equal(t, "1024x1024", *log.ImageInputSize)
		require.NotNil(t, log.ImageOutputSize)
		require.Equal(t, "3840x2160", *log.ImageOutputSize)
		require.NotNil(t, log.ImageSizeSource)
		require.Equal(t, "output", *log.ImageSizeSource)
		require.Equal(t, map[string]int{"4K": 2}, log.ImageSizeBreakdown)
	})

	t.Run("request_type_ws_v2_overrides_legacy", func(t *testing.T) {
		now := time.Now().UTC()
		values := []any{
			int64(1),  // id
			int64(10), // user_id
			int64(20), // api_key_id
			int64(30), // account_id
			sql.NullString{Valid: true, String: "req-1"},
			"gpt-5", // model
			sql.NullString{Valid: true, String: "gpt-5"}, // requested_model
			sql.NullString{},  // upstream_model
			sql.NullInt64{},   // entity_id
			sql.NullString{},  // entity_type
			sql.NullString{},  // claimed_entity_id
			sql.NullInt64{},   // group_id
			sql.NullInt64{},   // subscription_id
			1,                 // input_tokens
			2,                 // output_tokens
			3,                 // cache_creation_tokens
			4,                 // cache_read_tokens
			5,                 // cache_creation_5m_tokens
			6,                 // cache_creation_1h_tokens
			0,                 // image_output_tokens
			0.0,               // image_output_cost
			0.1,               // input_cost
			0.2,               // output_cost
			0.3,               // cache_creation_cost
			0.4,               // cache_read_cost
			1.0,               // total_cost
			0.9,               // actual_cost
			1.0,               // rate_multiplier
			sql.NullFloat64{}, // account_rate_multiplier
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeWSV2),
			false, // legacy stream
			false, // legacy openai ws
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			0,
			sql.NullString{},
			sql.NullString{}, // image_input_size
			sql.NullString{}, // image_output_size
			sql.NullString{}, // image_size_source
			sql.NullString{}, // image_size_breakdown
			sql.NullString{Valid: true, String: "priority"},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			false,
			sql.NullInt64{},   // channel_id
			sql.NullString{},  // model_mapping_chain
			sql.NullString{},  // billing_tier
			sql.NullString{},  // billing_mode
			sql.NullFloat64{}, // account_stats_cost
		}
		values = append(values, usageLogAugmentScanTail(now)...)
		log, err := scanUsageLog(usageLogScannerStub{values: values})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "priority", *log.ServiceTier)
		require.Equal(t, service.RequestTypeWSV2, log.RequestType)
		require.True(t, log.Stream)
		require.True(t, log.OpenAIWSMode)
	})

	t.Run("request_type_unknown_falls_back_to_legacy", func(t *testing.T) {
		now := time.Now().UTC()
		values := []any{
			int64(2),
			int64(11),
			int64(21),
			int64(31),
			sql.NullString{Valid: true, String: "req-2"},
			"gpt-5",
			sql.NullString{Valid: true, String: "gpt-5"},
			sql.NullString{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			sql.NullInt64{},
			sql.NullInt64{},
			1, 2, 3, 4, 5, 6,
			0, 0.0, // image_output_tokens, image_output_cost
			0.1, 0.2, 0.3, 0.4, 1.0, 0.9,
			1.0,
			sql.NullFloat64{},
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeUnknown),
			true,
			false,
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			0,
			sql.NullString{},
			sql.NullString{}, // image_input_size
			sql.NullString{}, // image_output_size
			sql.NullString{}, // image_size_source
			sql.NullString{}, // image_size_breakdown
			sql.NullString{Valid: true, String: "flex"},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			false,
			sql.NullInt64{},   // channel_id
			sql.NullString{},  // model_mapping_chain
			sql.NullString{},  // billing_tier
			sql.NullString{},  // billing_mode
			sql.NullFloat64{}, // account_stats_cost
		}
		values = append(values, usageLogAugmentScanTail(now)...)
		log, err := scanUsageLog(usageLogScannerStub{values: values})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "flex", *log.ServiceTier)
		require.Equal(t, service.RequestTypeStream, log.RequestType)
		require.True(t, log.Stream)
		require.False(t, log.OpenAIWSMode)
	})

	t.Run("service_tier_is_scanned", func(t *testing.T) {
		now := time.Now().UTC()
		values := []any{
			int64(3),
			int64(12),
			int64(22),
			int64(32),
			sql.NullString{Valid: true, String: "req-3"},
			"gpt-5.4",
			sql.NullString{Valid: true, String: "gpt-5.4"},
			sql.NullString{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			sql.NullInt64{},
			sql.NullInt64{},
			1, 2, 3, 4, 5, 6,
			0, 0.0, // image_output_tokens, image_output_cost
			0.1, 0.2, 0.3, 0.4, 1.0, 0.9,
			1.0,
			sql.NullFloat64{},
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeSync),
			false,
			false,
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			0,
			sql.NullString{},
			sql.NullString{}, // image_input_size
			sql.NullString{}, // image_output_size
			sql.NullString{}, // image_size_source
			sql.NullString{}, // image_size_breakdown
			sql.NullString{Valid: true, String: "priority"},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			false,
			sql.NullInt64{},   // channel_id
			sql.NullString{},  // model_mapping_chain
			sql.NullString{},  // billing_tier
			sql.NullString{},  // billing_mode
			sql.NullFloat64{}, // account_stats_cost
		}
		values = append(values, usageLogAugmentScanTail(now)...)
		log, err := scanUsageLog(usageLogScannerStub{values: values})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "priority", *log.ServiceTier)
	})

}

func usageLogAugmentScanTail(now time.Time) []any {
	return []any{
		sql.NullString{},  // client_product
		sql.NullString{},  // request_scope
		sql.NullString{},  // feature_scope
		sql.NullString{},  // augment_session_id
		sql.NullString{},  // route_policy_version
		sql.NullString{},  // pricing_version
		sql.NullBool{},    // billable
		sql.NullString{},  // cost_source
		sql.NullString{},  // currency
		sql.NullString{},  // upstream_attempt_id
		sql.NullString{},  // settlement_status
		sql.NullFloat64{}, // input_unit_price
		sql.NullFloat64{}, // output_unit_price
		sql.NullFloat64{}, // cache_read_unit_price
		sql.NullFloat64{}, // cache_creation_unit_price
		sql.NullFloat64{}, // reasoning_unit_price
		sql.NullFloat64{}, // estimated_cost
		sql.NullFloat64{}, // settled_cost
		sql.NullFloat64{}, // free_quota_applied
		sql.NullFloat64{}, // paid_balance_applied
		now,
	}
}
