package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ihandler "github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type subscriptionRevokeRouteRepoStub struct {
	service.UserSubscriptionRepository
	sub     *service.UserSubscription
	deleted bool
}

func (s *subscriptionRevokeRouteRepoStub) GetByID(_ context.Context, id int64) (*service.UserSubscription, error) {
	if s.sub == nil || s.sub.ID != id || s.deleted {
		return nil, service.ErrSubscriptionNotFound
	}
	cp := *s.sub
	return &cp, nil
}

func (s *subscriptionRevokeRouteRepoStub) Delete(_ context.Context, id int64) error {
	if s.sub == nil || s.sub.ID != id || s.deleted {
		return service.ErrSubscriptionNotFound
	}
	s.deleted = true
	return nil
}

func TestRegisterSubscriptionRoutes_PostRevokeAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &subscriptionRevokeRouteRepoStub{sub: &service.UserSubscription{ID: 77, UserID: 1, GroupID: 2, Status: service.SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}}
	svc := service.NewSubscriptionService(nil, repo, nil, nil, nil)
	t.Cleanup(svc.Stop)

	router := gin.New()
	admin := router.Group("/api/v1/admin")
	registerSubscriptionRoutes(admin, &ihandler.Handlers{
		Admin: &ihandler.AdminHandlers{
			Subscription: adminhandler.NewSubscriptionHandler(svc),
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/subscriptions/77/revoke", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.True(t, repo.deleted)
}
