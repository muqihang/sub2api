package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const EntityHeader = "X-Entity-ID"

type resolvedEntityContextKey struct{}
type claimedEntityIDContextKey struct{}

func WithResolvedEntity(ctx context.Context, resolved *ResolvedEntity) context.Context {
	if ctx == nil || resolved == nil || strings.TrimSpace(resolved.Entity.EntityKey) == "" {
		return ctx
	}
	return context.WithValue(ctx, resolvedEntityContextKey{}, resolved)
}

func WithClaimedEntityID(ctx context.Context, claimed string) context.Context {
	if ctx == nil {
		return ctx
	}
	claimed = strings.TrimSpace(claimed)
	if claimed == "" {
		return ctx
	}
	return context.WithValue(ctx, claimedEntityIDContextKey{}, claimed)
}

func ClaimedEntityIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(claimedEntityIDContextKey{}).(string)
	return strings.TrimSpace(value)
}

func ResolvedEntityFromContext(ctx context.Context) (*ResolvedEntity, bool) {
	if ctx == nil {
		return nil, false
	}
	resolved, ok := ctx.Value(resolvedEntityContextKey{}).(*ResolvedEntity)
	if !ok || resolved == nil || strings.TrimSpace(resolved.Entity.EntityKey) == "" {
		return nil, false
	}
	return resolved, true
}

func EntityScopeFromContext(ctx context.Context) string {
	resolved, ok := ResolvedEntityFromContext(ctx)
	if !ok {
		return ""
	}
	return strings.TrimSpace(resolved.Entity.EntityKey)
}

func EntityScopedSeed(entityKey, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	entityKey = strings.TrimSpace(entityKey)
	if entityKey == "" {
		return raw
	}
	if strings.HasPrefix(raw, "entity:"+entityKey+":") {
		return raw
	}
	return fmt.Sprintf("entity:%s:%s", entityKey, raw)
}

func EntityScopedSeedFromContext(ctx context.Context, raw string) string {
	return EntityScopedSeed(EntityScopeFromContext(ctx), raw)
}

func DeriveEntityScopedSessionHash(entityKey, sessionID string) string {
	currentHash, _ := deriveOpenAISessionHashes(EntityScopedSeed(entityKey, sessionID))
	return currentHash
}

func EntityScopedStateKey(entityKey, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	entityKey = strings.TrimSpace(entityKey)
	if entityKey == "" {
		return raw
	}
	sum := sha256.Sum256([]byte(raw))
	return "entity:" + entityKey + ":" + hex.EncodeToString(sum[:])
}

func EntityScopedStateKeyFromContext(ctx context.Context, raw string) string {
	return EntityScopedStateKey(EntityScopeFromContext(ctx), raw)
}

func (s *OpenAIGatewayService) ResolveTrustedEntityForRequest(c *gin.Context, apiKey *APIKey, claimedEntityKey string) (*ResolvedEntity, error) {
	if !s.entityOrchestrationEnabled() {
		return nil, nil
	}
	if s == nil || s.entityRegistryRepo == nil || apiKey == nil {
		if strings.TrimSpace(claimedEntityKey) != "" {
			return nil, ErrEntityNotAuthorized
		}
		return nil, nil
	}
	resolved, err := s.entityRegistryRepo.ResolveEntity(requestContext(c), EntityResolutionInput{
		APIKeyID:         apiKey.ID,
		UserID:           apiKey.UserID,
		GroupID:          apiKey.GroupID,
		ClaimedEntityKey: claimedEntityKey,
	})
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, nil
	}
	if c != nil {
		c.Set("resolved_entity", resolved)
		if c.Request != nil {
			resolvedCtx := WithResolvedEntity(c.Request.Context(), resolved)
			resolvedCtx = WithClaimedEntityID(resolvedCtx, claimedEntityKey)
			c.Request = c.Request.WithContext(resolvedCtx)
		}
	}
	return resolved, nil
}

func (s *OpenAIGatewayService) AdmitEntityQuotaForRequest(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.entityOrchestrationEnabled() {
		return ctx, nil, nil
	}
	if s == nil || s.entityRateLimitSvc == nil {
		return ctx, nil, nil
	}
	decision, release, err := s.entityRateLimitSvc.Admit(ctx)
	if err != nil || decision == nil {
		return ctx, release, err
	}
	return WithEntityRateLimitDecision(ctx, decision), release, nil
}

func (s *OpenAIGatewayService) entityOrchestrationEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.OpenAICore.EntityOrchestration.Enabled
}

func requestContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}
	return context.Background()
}

func EntityResolutionHTTPStatus(err error) (status int, code string, message string, ok bool) {
	if err == nil {
		return 0, "", "", false
	}
	if IsEntityAuthorizationError(err) {
		return http.StatusForbidden, "permission_error", "entity is not authorized for this requester", true
	}
	if strings.Contains(strings.ToLower(err.Error()), "entity") {
		return http.StatusBadRequest, "invalid_request_error", err.Error(), true
	}
	return 0, "", "", false
}
