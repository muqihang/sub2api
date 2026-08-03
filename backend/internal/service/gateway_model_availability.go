package service

import (
	"context"
	"strings"
)

// ModelAvailabilityDiagnosis reports whether a requested model is configured
// for any account in a pool, ignoring transient capacity state.
type ModelAvailabilityDiagnosis struct {
	HasAccountsInPool bool
	HasModelSupport   bool
}

// ModelAvailabilityDiagnoser is implemented by gateway services that can
// distinguish unsupported-model configuration from transient no-capacity errors.
type ModelAvailabilityDiagnoser interface {
	DiagnoseModelAvailabilityForPlatform(ctx context.Context, groupID *int64, requestedModel string, platform string) ModelAvailabilityDiagnosis
}

func (s *GatewayService) DiagnoseModelAvailabilityForPlatform(ctx context.Context, groupID *int64, requestedModel string, platform string) ModelAvailabilityDiagnosis {
	if s == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" || strings.TrimSpace(platform) == "" {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	accounts, _, err := s.listSchedulableAccounts(ctx, groupID, platform, false)
	if err != nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	diag := ModelAvailabilityDiagnosis{}
	for i := range accounts {
		diag.HasAccountsInPool = true
		if s.isModelSupportedByAccountWithContext(ctx, &accounts[i], requestedModel) {
			diag.HasModelSupport = true
			return diag
		}
	}
	return diag
}
