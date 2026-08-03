package admin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type FormalPoolOnboardingPrincipalResolver interface {
	Resolve(c *gin.Context) (service.FormalPoolOnboardingPrincipal, error)
}

type formalPoolOnboardingPrincipalResolver struct {
	users    *service.UserService
	tenantID string
	now      func() time.Time
}

func NewFormalPoolOnboardingPrincipalResolver(users *service.UserService, tenantID string, now func() time.Time) FormalPoolOnboardingPrincipalResolver {
	return &formalPoolOnboardingPrincipalResolver{users: users, tenantID: strings.TrimSpace(tenantID), now: now}
}

func NewFormalPoolOnboardingPrincipalRevalidator(users *service.UserService, tenantID string, now func() time.Time) service.FormalPoolOnboardingPrincipalRevalidator {
	return &formalPoolOnboardingPrincipalResolver{users: users, tenantID: strings.TrimSpace(tenantID), now: now}
}

func (r *formalPoolOnboardingPrincipalResolver) Resolve(c *gin.Context) (service.FormalPoolOnboardingPrincipal, error) {
	if r == nil || r.users == nil || r.now == nil || c == nil || c.Request == nil {
		return service.FormalPoolOnboardingPrincipal{}, service.ErrFormalPoolOnboardingAuthenticationRequired
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 || subject.AuthMethod != "jwt" || subject.ExpiresAtUnix <= r.now().Unix() {
		return service.FormalPoolOnboardingPrincipal{}, service.ErrFormalPoolOnboardingAuthenticationRequired
	}
	if r.tenantID == "" {
		return service.FormalPoolOnboardingPrincipal{}, service.ErrFormalPoolOnboardingForbidden
	}
	user, err := r.currentUser(c.Request.Context(), subject.UserID, subject.TokenVersion)
	if err != nil {
		return service.FormalPoolOnboardingPrincipal{}, err
	}
	if !user.IsAdmin() {
		return service.FormalPoolOnboardingPrincipal{}, service.ErrFormalPoolOnboardingForbidden
	}
	return service.FormalPoolOnboardingPrincipal{
		SubjectID: user.ID, AdministratorID: user.ID, TenantID: r.tenantID,
		CreatorID: user.ID, Role: user.Role, CallerKind: service.CallerKindHumanJWT,
		AuthorityRevision: user.TokenVersion, ExpiresAtUnix: subject.ExpiresAtUnix,
		Active: true, SystemAdmin: true,
	}, nil
}

func (r *formalPoolOnboardingPrincipalResolver) Revalidate(ctx context.Context, principal service.FormalPoolOnboardingPrincipal) error {
	if r == nil || r.users == nil || r.now == nil || ctx == nil {
		return service.ErrFormalPoolOnboardingAuthenticationRequired
	}
	if principal.SubjectID <= 0 || principal.AdministratorID <= 0 || principal.CreatorID <= 0 || principal.AuthorityRevision <= 0 ||
		strings.TrimSpace(principal.Role) == "" ||
		principal.CallerKind != service.CallerKindHumanJWT || !principal.Active || !principal.SystemAdmin || principal.ExpiresAtUnix <= r.now().Unix() {
		return service.ErrFormalPoolOnboardingAuthenticationRequired
	}
	if r.tenantID == "" || strings.TrimSpace(principal.TenantID) == "" {
		return service.ErrFormalPoolOnboardingForbidden
	}
	user, err := r.currentUser(ctx, principal.SubjectID, principal.AuthorityRevision)
	if err != nil {
		return err
	}
	if !user.IsAdmin() || user.Role != principal.Role || principal.AdministratorID != user.ID || principal.CreatorID != user.ID || principal.TenantID != r.tenantID {
		return service.ErrFormalPoolOnboardingForbidden
	}
	return nil
}

func (r *formalPoolOnboardingPrincipalResolver) currentUser(ctx context.Context, userID, tokenVersion int64) (*service.User, error) {
	user, err := r.users.GetByID(ctx, userID)
	if err != nil || user == nil || user.DeletedAt != nil || !user.IsActive() || user.TokenVersion != tokenVersion {
		return nil, service.ErrFormalPoolOnboardingAuthenticationRequired
	}
	return user, nil
}

const formalPoolOnboardingPrincipalKey = "formal_pool_onboarding_principal"

func FormalPoolOnboardingPrincipalGuard(resolver FormalPoolOnboardingPrincipalResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		if resolver == nil {
			_ = writeFormalPoolPrincipalError(c, service.ErrFormalPoolOnboardingAuthenticationRequired)
			return
		}
		principal, err := resolver.Resolve(c)
		if err != nil {
			_ = writeFormalPoolPrincipalError(c, err)
			return
		}
		c.Set(formalPoolOnboardingPrincipalKey, principal)
		c.Next()
	}
}

func FormalPoolOnboardingPrincipalFromGin(c *gin.Context) (service.FormalPoolOnboardingPrincipal, bool) {
	if c == nil {
		return service.FormalPoolOnboardingPrincipal{}, false
	}
	value, ok := c.Get(formalPoolOnboardingPrincipalKey)
	if !ok {
		return service.FormalPoolOnboardingPrincipal{}, false
	}
	principal, ok := value.(service.FormalPoolOnboardingPrincipal)
	return principal, ok
}

func writeFormalPoolPrincipalError(c *gin.Context, err error) bool {
	if c == nil {
		return false
	}
	if !errors.Is(err, service.ErrFormalPoolOnboardingForbidden) {
		err = service.ErrFormalPoolOnboardingAuthenticationRequired
	}
	response.ErrorFrom(c, err)
	c.Abort()
	return true
}
