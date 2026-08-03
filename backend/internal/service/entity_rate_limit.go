package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	EntityRateLimitPolicyStatusActive   = "active"
	EntityRateLimitPolicyStatusDisabled = "disabled"
)

var (
	ErrEntityRPMExceeded         = infraerrors.TooManyRequests("ENTITY_RPM_EXCEEDED", "entity requests-per-minute limit exceeded")
	ErrEntityConcurrencyExceeded = infraerrors.TooManyRequests("ENTITY_CONCURRENCY_EXCEEDED", "entity concurrency limit exceeded")
	ErrEntityTPMExceeded         = infraerrors.TooManyRequests("ENTITY_TPM_EXCEEDED", "entity tokens-per-minute limit exceeded")
	ErrEntityCostExceeded        = infraerrors.TooManyRequests("ENTITY_COST_EXCEEDED", "entity cost-per-minute limit exceeded")
)

type entityRateLimitDecisionContextKey struct{}

type EntityRateLimitPolicy struct {
	ID               int64
	EntityID         int64
	Status           string
	RPMLimit         int
	TPMLimit         int
	ConcurrencyLimit int
	CostLimitUSD     float64
	Metadata         map[string]any
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type EntityRateLimitDecision struct {
	DecisionID              string
	PolicyID                int64
	EntityID                int64
	EntityKey               string
	EntityType              string
	RPMLimit                int
	TPMLimit                int
	ConcurrencyLimit        int
	CostLimitUSD            float64
	AdmittedAt              time.Time
	RedisFailureFailOpen    bool
	RedisFailureReason      string
	ConcurrencySlotAcquired bool
}

type EntityRateLimitPolicyRepository interface {
	GetActiveByEntityID(ctx context.Context, entityID int64) (*EntityRateLimitPolicy, error)
}

type EntityRateLimitCache interface {
	AcquireEntitySlot(ctx context.Context, entityID int64, maxConcurrency int, requestID string) (bool, error)
	ReleaseEntitySlot(ctx context.Context, entityID int64, requestID string) error
	IncrementEntityRPM(ctx context.Context, entityID int64) (count int, err error)
	AddEntityTPM(ctx context.Context, entityID int64, tokens int) (total int, err error)
	AddEntityCost(ctx context.Context, entityID int64, amount float64) (total float64, err error)
}

type EntityRateLimitService struct {
	policyRepo EntityRateLimitPolicyRepository
	cache      EntityRateLimitCache
}

func NewEntityRateLimitService(policyRepo EntityRateLimitPolicyRepository, cache EntityRateLimitCache) *EntityRateLimitService {
	return &EntityRateLimitService{policyRepo: policyRepo, cache: cache}
}

func WithEntityRateLimitDecision(ctx context.Context, decision *EntityRateLimitDecision) context.Context {
	if ctx == nil || decision == nil || decision.EntityID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, entityRateLimitDecisionContextKey{}, decision)
}

func EntityRateLimitDecisionFromContext(ctx context.Context) (*EntityRateLimitDecision, bool) {
	if ctx == nil {
		return nil, false
	}
	decision, ok := ctx.Value(entityRateLimitDecisionContextKey{}).(*EntityRateLimitDecision)
	if !ok || decision == nil || decision.EntityID <= 0 {
		return nil, false
	}
	return decision, true
}

func ContextWithEntityMetadataFrom(dst context.Context, src context.Context) context.Context {
	if dst == nil {
		dst = context.Background()
	}
	if src == nil {
		return dst
	}
	if resolved, ok := ResolvedEntityFromContext(src); ok {
		dst = WithResolvedEntity(dst, resolved)
	}
	if claimed := ClaimedEntityIDFromContext(src); claimed != "" {
		dst = WithClaimedEntityID(dst, claimed)
	}
	if decision, ok := EntityRateLimitDecisionFromContext(src); ok {
		dst = WithEntityRateLimitDecision(dst, decision)
	}
	return dst
}

func (s *EntityRateLimitService) Admit(ctx context.Context) (*EntityRateLimitDecision, func(), error) {
	if existing, ok := EntityRateLimitDecisionFromContext(ctx); ok {
		return existing, nil, nil
	}
	if s == nil || s.policyRepo == nil {
		return nil, nil, nil
	}
	resolved, ok := ResolvedEntityFromContext(ctx)
	if !ok || resolved == nil || resolved.Entity.ID <= 0 {
		return nil, nil, nil
	}
	policy, err := s.policyRepo.GetActiveByEntityID(ctx, resolved.Entity.ID)
	if err != nil {
		return nil, nil, err
	}
	if !entityRateLimitPolicyApplies(policy) {
		return nil, nil, nil
	}

	decision := &EntityRateLimitDecision{
		DecisionID:       generateRequestID(),
		PolicyID:         policy.ID,
		EntityID:         resolved.Entity.ID,
		EntityKey:        strings.TrimSpace(resolved.Entity.EntityKey),
		EntityType:       strings.TrimSpace(resolved.Entity.EntityType),
		RPMLimit:         policy.RPMLimit,
		TPMLimit:         policy.TPMLimit,
		ConcurrencyLimit: policy.ConcurrencyLimit,
		CostLimitUSD:     policy.CostLimitUSD,
		AdmittedAt:       time.Now(),
	}

	var release func()
	if policy.ConcurrencyLimit > 0 {
		if s.cache == nil {
			markEntityRateLimitRedisFailure(decision, "entity concurrency cache unavailable")
		} else {
			acquired, acquireErr := s.cache.AcquireEntitySlot(ctx, policy.EntityID, policy.ConcurrencyLimit, decision.DecisionID)
			if acquireErr != nil {
				markEntityRateLimitRedisFailure(decision, "entity concurrency acquire failed: "+acquireErr.Error())
			} else if !acquired {
				return nil, nil, ErrEntityConcurrencyExceeded
			} else {
				decision.ConcurrencySlotAcquired = true
				release = s.releaseFunc(policy.EntityID, decision.DecisionID)
			}
		}
	}

	if policy.RPMLimit > 0 {
		if s.cache == nil {
			markEntityRateLimitRedisFailure(decision, "entity rpm cache unavailable")
		} else {
			count, rpmErr := s.cache.IncrementEntityRPM(ctx, policy.EntityID)
			if rpmErr != nil {
				markEntityRateLimitRedisFailure(decision, "entity rpm increment failed: "+rpmErr.Error())
			} else if count > policy.RPMLimit {
				if release != nil {
					release()
				}
				return nil, nil, ErrEntityRPMExceeded
			}
		}
	}

	return decision, release, nil
}

func (s *EntityRateLimitService) releaseFunc(entityID int64, decisionID string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if s == nil || s.cache == nil || entityID <= 0 || strings.TrimSpace(decisionID) == "" {
				return
			}
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.cache.ReleaseEntitySlot(bgCtx, entityID, decisionID); err != nil {
				logger.LegacyPrintf("service.entity_rate_limit", "Warning: failed to release entity slot for %d (req=%s): %v", entityID, decisionID, err)
			}
		})
	}
}

func (s *EntityRateLimitService) ReconcileUsage(ctx context.Context, usageLog *UsageLog) error {
	if s == nil || usageLog == nil {
		return nil
	}
	decision, ok := EntityRateLimitDecisionFromContext(ctx)
	if !ok || decision.EntityID <= 0 {
		return nil
	}
	if decision.TPMLimit <= 0 && decision.CostLimitUSD <= 0 {
		return nil
	}
	if s.cache == nil {
		logger.LegacyPrintf("service.entity_rate_limit", "Warning: entity usage reconciliation skipped for entity=%d: cache unavailable", decision.EntityID)
		return nil
	}
	if decision.TPMLimit > 0 {
		tokens := usageLog.TotalTokens()
		if tokens > 0 {
			total, err := s.cache.AddEntityTPM(ctx, decision.EntityID, tokens)
			if err != nil {
				logger.LegacyPrintf("service.entity_rate_limit", "Warning: entity TPM reconciliation failed for entity=%d policy=%d: %v", decision.EntityID, decision.PolicyID, err)
			} else if total > decision.TPMLimit {
				return ErrEntityTPMExceeded
			}
		}
	}
	if decision.CostLimitUSD > 0 && usageLog.ActualCost > 0 {
		total, err := s.cache.AddEntityCost(ctx, decision.EntityID, usageLog.ActualCost)
		if err != nil {
			logger.LegacyPrintf("service.entity_rate_limit", "Warning: entity cost reconciliation failed for entity=%d policy=%d: %v", decision.EntityID, decision.PolicyID, err)
		} else if total > decision.CostLimitUSD {
			return ErrEntityCostExceeded
		}
	}
	return nil
}

func entityRateLimitPolicyApplies(policy *EntityRateLimitPolicy) bool {
	if policy == nil || policy.EntityID <= 0 {
		return false
	}
	status := strings.TrimSpace(policy.Status)
	if status == "" {
		status = EntityRateLimitPolicyStatusActive
	}
	if status != EntityRateLimitPolicyStatusActive {
		return false
	}
	return policy.RPMLimit > 0 || policy.TPMLimit > 0 || policy.ConcurrencyLimit > 0 || policy.CostLimitUSD > 0
}

func markEntityRateLimitRedisFailure(decision *EntityRateLimitDecision, reason string) {
	if decision == nil {
		return
	}
	decision.RedisFailureFailOpen = true
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "entity rate-limit cache unavailable"
	}
	if decision.RedisFailureReason == "" {
		decision.RedisFailureReason = reason
		return
	}
	decision.RedisFailureReason = fmt.Sprintf("%s; %s", decision.RedisFailureReason, reason)
}
