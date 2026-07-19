package admin

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type formalPoolPrincipalCountingUserRepo struct {
	service.UserRepository
	calls atomic.Int64
}

func (r *formalPoolPrincipalCountingUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	r.calls.Add(1)
	return nil, nil
}

func TestFormalPoolPrincipalResolverEmptyConfiguredTenantIsForbiddenWithoutUserFetch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &formalPoolPrincipalCountingUserRepo{}
	users := service.NewUserService(repo, nil, nil, nil)
	resolver := NewFormalPoolOnboardingPrincipalResolver(users, "", time.Now)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{
		UserID: 1, AuthMethod: "jwt", TokenVersion: 1, ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
	})

	_, err := resolver.Resolve(c)
	require.ErrorIs(t, err, service.ErrFormalPoolOnboardingForbidden)
	require.Zero(t, repo.calls.Load())
}

func TestFormalPoolPrincipalRevalidatorEmptyPrincipalTenantIsForbiddenWithoutUserFetch(t *testing.T) {
	repo := &formalPoolPrincipalCountingUserRepo{}
	users := service.NewUserService(repo, nil, nil, nil)
	revalidator := NewFormalPoolOnboardingPrincipalRevalidator(users, "tenant-one", time.Now)
	principal := service.FormalPoolOnboardingPrincipal{
		SubjectID: 1, AdministratorID: 1, CreatorID: 1, Role: service.RoleAdmin,
		CallerKind: service.CallerKindHumanJWT, AuthorityRevision: 1,
		ExpiresAtUnix: time.Now().Add(time.Hour).Unix(), Active: true, SystemAdmin: true,
	}

	err := revalidator.Revalidate(context.Background(), principal)
	require.ErrorIs(t, err, service.ErrFormalPoolOnboardingForbidden)
	require.Zero(t, repo.calls.Load())
}
