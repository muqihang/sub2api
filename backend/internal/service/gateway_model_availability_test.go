package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type modelAvailabilityAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (r *modelAvailabilityAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	return r.listByPlatforms([]string{platform}), nil
}

func (r *modelAvailabilityAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]Account, error) {
	return r.listByPlatforms([]string{platform}), nil
}

func (r *modelAvailabilityAccountRepo) ListSchedulableByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	return r.listByPlatforms(platforms), nil
}

func (r *modelAvailabilityAccountRepo) ListSchedulableUngroupedByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	return r.listByPlatforms(platforms), nil
}

func (r *modelAvailabilityAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]Account, error) {
	return r.listByPlatforms([]string{platform}), nil
}

func (r *modelAvailabilityAccountRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, _ int64, platforms []string) ([]Account, error) {
	return r.listByPlatforms(platforms), nil
}

func (r *modelAvailabilityAccountRepo) listByPlatforms(platforms []string) []Account {
	allowed := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = struct{}{}
	}
	out := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if _, ok := allowed[account.Platform]; ok && account.IsSchedulable() {
			out = append(out, account)
		}
	}
	return out
}

func TestDiagnoseModelAvailabilityForPlatform_NoModelFallsBackToAvailable(t *testing.T) {
	repo := &modelAvailabilityAccountRepo{}
	svc := &GatewayService{accountRepo: repo}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_NoAccountsInPool(t *testing.T) {
	repo := &modelAvailabilityAccountRepo{}
	svc := &GatewayService{accountRepo: repo}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5", PlatformOpenAI)

	require.False(t, diag.HasAccountsInPool)
	require.False(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_ExplicitMappingMatches(t *testing.T) {
	repo := &modelAvailabilityAccountRepo{
		accounts: []Account{{
			ID:          1,
			Platform:    PlatformOpenAI,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.1-codex-mini": "gpt-5.1-codex-mini"}},
		}},
	}
	svc := &GatewayService{accountRepo: repo}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.1-codex-mini", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_NoMatchingModel(t *testing.T) {
	repo := &modelAvailabilityAccountRepo{
		accounts: []Account{
			{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}}},
			{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5-mini": "gpt-5-mini"}}},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.1-codex-mini", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.False(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_WrongPlatformFiltersOut(t *testing.T) {
	repo := &modelAvailabilityAccountRepo{
		accounts: []Account{{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true}},
	}
	svc := &GatewayService{accountRepo: repo}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5", PlatformOpenAI)

	require.False(t, diag.HasAccountsInPool)
	require.False(t, diag.HasModelSupport)
}
