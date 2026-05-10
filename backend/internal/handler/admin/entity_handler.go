package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type entityAdminService interface {
	CreateEntity(ctx context.Context, input service.CreateEntityInput) (*service.Entity, error)
	ListEntities(ctx context.Context, filter service.EntityListFilter) ([]service.Entity, error)
	CreateEntityBinding(ctx context.Context, input service.CreateEntityBindingInput) (*service.EntityBinding, error)
	ListEntityBindings(ctx context.Context, filter service.EntityBindingListFilter) ([]service.EntityBinding, error)
}

type EntityHandler struct {
	adminService entityAdminService
}

func NewEntityHandler(adminService service.AdminService) *EntityHandler {
	entitySvc, _ := adminService.(entityAdminService)
	return &EntityHandler{adminService: entitySvc}
}

type createEntityRequest struct {
	EntityKey   string         `json:"entity_key"`
	DisplayName string         `json:"display_name"`
	EntityType  string         `json:"entity_type"`
	Status      string         `json:"status"`
	Metadata    map[string]any `json:"metadata"`
}

type createEntityBindingRequest struct {
	EntityID  int64          `json:"entity_id"`
	APIKeyID  *int64         `json:"api_key_id"`
	UserID    *int64         `json:"user_id"`
	GroupID   *int64         `json:"group_id"`
	AccountID *int64         `json:"account_id"`
	IsDefault bool           `json:"is_default"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata"`
}

func (h *EntityHandler) List(c *gin.Context) {
	if !h.ensureConfigured(c) {
		return
	}
	entities, err := h.adminService.ListEntities(c.Request.Context(), service.EntityListFilter{
		Status:     strings.TrimSpace(c.Query("status")),
		EntityType: strings.TrimSpace(c.Query("type")),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": entities, "total": len(entities)})
}

func (h *EntityHandler) Create(c *gin.Context) {
	if !h.ensureConfigured(c) {
		return
	}
	var req createEntityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_REQUEST", "invalid request body"))
		return
	}
	entity, err := h.adminService.CreateEntity(c.Request.Context(), service.CreateEntityInput{
		EntityKey:   req.EntityKey,
		DisplayName: req.DisplayName,
		EntityType:  req.EntityType,
		Status:      req.Status,
		Metadata:    req.Metadata,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, entity)
}

func (h *EntityHandler) ListBindings(c *gin.Context) {
	if !h.ensureConfigured(c) {
		return
	}
	filter, ok := parseEntityBindingFilter(c)
	if !ok {
		return
	}
	bindings, err := h.adminService.ListEntityBindings(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": bindings, "total": len(bindings)})
}

func (h *EntityHandler) CreateBinding(c *gin.Context) {
	if !h.ensureConfigured(c) {
		return
	}
	var req createEntityBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_REQUEST", "invalid request body"))
		return
	}
	binding, err := h.adminService.CreateEntityBinding(c.Request.Context(), service.CreateEntityBindingInput{
		EntityID:  req.EntityID,
		APIKeyID:  req.APIKeyID,
		UserID:    req.UserID,
		GroupID:   req.GroupID,
		AccountID: req.AccountID,
		IsDefault: req.IsDefault,
		Status:    req.Status,
		Metadata:  req.Metadata,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, binding)
}

func (h *EntityHandler) ensureConfigured(c *gin.Context) bool {
	if h != nil && h.adminService != nil {
		return true
	}
	response.Error(c, http.StatusNotImplemented, "entity registry is not configured")
	return false
}

func parseEntityBindingFilter(c *gin.Context) (service.EntityBindingListFilter, bool) {
	var filter service.EntityBindingListFilter
	var ok bool
	if filter.EntityID, ok = parseOptionalInt64Query(c, "entity_id"); !ok {
		return filter, false
	}
	if filter.APIKeyID, ok = parseOptionalInt64Query(c, "api_key_id"); !ok {
		return filter, false
	}
	if filter.UserID, ok = parseOptionalInt64Query(c, "user_id"); !ok {
		return filter, false
	}
	if filter.GroupID, ok = parseOptionalInt64Query(c, "group_id"); !ok {
		return filter, false
	}
	filter.Status = strings.TrimSpace(c.Query("status"))
	return filter, true
}

func parseOptionalInt64Query(c *gin.Context, key string) (*int64, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_QUERY", "invalid "+key))
		return nil, false
	}
	return &value, true
}
