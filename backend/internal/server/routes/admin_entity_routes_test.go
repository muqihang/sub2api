package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type entityRouteRegistryStub struct {
	listCalls int
}

func (s *entityRouteRegistryStub) CreateEntity(context.Context, service.CreateEntityInput) (*service.Entity, error) {
	return nil, service.ErrEntityNotFound
}

func (s *entityRouteRegistryStub) GetEntityByID(context.Context, int64) (*service.Entity, error) {
	return nil, service.ErrEntityNotFound
}

func (s *entityRouteRegistryStub) GetEntityByKey(context.Context, string) (*service.Entity, error) {
	return nil, service.ErrEntityNotFound
}

func (s *entityRouteRegistryStub) ListEntities(context.Context, service.EntityListFilter) ([]service.Entity, error) {
	s.listCalls++
	return []service.Entity{{ID: 1, EntityKey: "team-alpha", Status: service.EntityStatusActive}}, nil
}

func (s *entityRouteRegistryStub) CreateBinding(context.Context, service.CreateEntityBindingInput) (*service.EntityBinding, error) {
	return nil, service.ErrEntityNotFound
}

func (s *entityRouteRegistryStub) ListBindings(context.Context, service.EntityBindingListFilter) ([]service.EntityBinding, error) {
	return nil, nil
}

func (s *entityRouteRegistryStub) ResolveEntity(context.Context, service.EntityResolutionInput) (*service.ResolvedEntity, error) {
	return nil, nil
}

func TestRegisterEntityRoutesRegistersAdminEntitiesWithLiveDependency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1/admin")
	registry := &entityRouteRegistryStub{}
	adminSvc := service.NewAdminService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, registry)

	registerEntityRoutes(group, &handler.Handlers{
		Admin: &handler.AdminHandlers{
			Entity: admin.NewEntityHandler(adminSvc),
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/entities", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, registry.listCalls)
}
