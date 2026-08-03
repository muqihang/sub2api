package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type formalPoolGuardResolver struct {
	principal service.FormalPoolOnboardingPrincipal
	err       error
	calls     int
}

func (r *formalPoolGuardResolver) Resolve(*gin.Context) (service.FormalPoolOnboardingPrincipal, error) {
	r.calls++
	return r.principal, r.err
}

func TestFormalPoolOnboardingPrincipalGuardResolvesOnceAndStoresTypedPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	principal := service.FormalPoolOnboardingPrincipal{SubjectID: 7, Role: service.RoleAdmin}
	resolver := &formalPoolGuardResolver{principal: principal}
	router := gin.New()
	router.Use(adminhandler.FormalPoolOnboardingPrincipalGuard(resolver))
	router.GET("/", func(c *gin.Context) {
		stored, ok := adminhandler.FormalPoolOnboardingPrincipalFromGin(c)
		require.True(t, ok)
		require.Equal(t, principal, stored)
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, 1, resolver.calls)
}

func TestFormalPoolOnboardingPrincipalGuardUsesCommonErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantReason string
	}{
		{name: "authentication", err: errors.New("untrusted resolver detail"), wantStatus: http.StatusUnauthorized, wantReason: "FORMAL_POOL_AUTH_REQUIRED"},
		{name: "forbidden", err: service.ErrFormalPoolOnboardingForbidden, wantStatus: http.StatusForbidden, wantReason: "FORMAL_POOL_FORBIDDEN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &formalPoolGuardResolver{err: tc.err}
			router := gin.New()
			router.Use(adminhandler.FormalPoolOnboardingPrincipalGuard(resolver))
			router.GET("/", func(c *gin.Context) { t.Fatal("denied principal reached handler") })
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			require.Equal(t, tc.wantStatus, rec.Code)
			require.Contains(t, rec.Body.String(), tc.wantReason)
			require.NotContains(t, rec.Body.String(), "untrusted resolver detail")
			require.Equal(t, 1, resolver.calls)
		})
	}
}
