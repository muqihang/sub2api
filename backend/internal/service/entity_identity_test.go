package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type entityIdentityRegistryRepoStub struct {
	EntityRegistryRepository

	calls    int
	resolved *ResolvedEntity
	err      error
}

func (s *entityIdentityRegistryRepoStub) ResolveEntity(ctx context.Context, input EntityResolutionInput) (*ResolvedEntity, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.resolved, nil
}

func TestOpenAIGatewayServiceResolveTrustedEntityNoOpsWhenEntityOrchestrationDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EntityOrchestration.Enabled = false
	repo := &entityIdentityRegistryRepoStub{resolved: resolvedEntityForRateLimitTest()}
	svc := &OpenAIGatewayService{cfg: cfg, entityRegistryRepo: repo}
	c := &gin.Context{}

	resolved, err := svc.ResolveTrustedEntityForRequest(c, &APIKey{ID: 10, UserID: 20}, "workspace-alpha")

	require.NoError(t, err)
	require.Nil(t, resolved)
	require.Zero(t, repo.calls)
}

func TestOpenAIGatewayServiceAdmitEntityQuotaNoOpsWhenEntityOrchestrationDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.EntityOrchestration.Enabled = false
	repo := &entityRateLimitPolicyRepoStub{policy: &EntityRateLimitPolicy{
		ID:       12,
		EntityID: 123,
		Status:   EntityRateLimitPolicyStatusActive,
		RPMLimit: 1,
	}}
	cache := &entityRateLimitCacheStub{rpmCount: 2}
	svc := &OpenAIGatewayService{
		cfg:                cfg,
		entityRateLimitSvc: NewEntityRateLimitService(repo, cache),
	}
	ctx := WithResolvedEntity(context.Background(), resolvedEntityForRateLimitTest())

	nextCtx, release, err := svc.AdmitEntityQuotaForRequest(ctx)

	require.NoError(t, err)
	require.Nil(t, release)
	require.Same(t, ctx, nextCtx)
	require.Zero(t, repo.calls)
	require.Zero(t, cache.rpmCalls)
}
