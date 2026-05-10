package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	EntityStatusActive   = "active"
	EntityStatusDisabled = "disabled"

	EntityTypeWorkspace = "workspace"
	EntityTypeTeam      = "team"
	EntityTypeProject   = "project"

	EntityResolutionSourceNone           = ""
	EntityResolutionSourceDefaultBinding = "default_binding"
	EntityResolutionSourceClaimedBinding = "claimed_binding"
)

var (
	ErrEntityNotFound      = infraerrors.NotFound("ENTITY_NOT_FOUND", "entity not found")
	ErrEntityNotAuthorized = infraerrors.Forbidden("ENTITY_NOT_AUTHORIZED", "entity is not authorized for this requester")
	ErrEntityInvalid       = infraerrors.BadRequest("ENTITY_INVALID", "invalid entity")
	ErrEntityAmbiguous     = infraerrors.Conflict("ENTITY_AMBIGUOUS", "multiple entity bindings match this requester")
)

type Entity struct {
	ID          int64
	EntityKey   string
	DisplayName string
	EntityType  string
	Status      string
	Metadata    map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type EntityBinding struct {
	ID        int64
	EntityID  int64
	APIKeyID  *int64
	UserID    *int64
	GroupID   *int64
	AccountID *int64
	IsDefault bool
	Status    string
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
	Entity    *Entity
}

type CreateEntityInput struct {
	EntityKey   string
	DisplayName string
	EntityType  string
	Status      string
	Metadata    map[string]any
}

type EntityListFilter struct {
	Status     string
	EntityType string
}

type CreateEntityBindingInput struct {
	EntityID  int64
	APIKeyID  *int64
	UserID    *int64
	GroupID   *int64
	AccountID *int64
	IsDefault bool
	Status    string
	Metadata  map[string]any
}

type EntityBindingListFilter struct {
	EntityID *int64
	APIKeyID *int64
	UserID   *int64
	GroupID  *int64
	Status   string
}

type EntityResolutionInput struct {
	APIKeyID         int64
	UserID           int64
	GroupID          *int64
	AccountID        *int64
	ClaimedEntityKey string
}

type ResolvedEntity struct {
	Entity  Entity
	Binding *EntityBinding
	Source  string
}

type EntityRegistryRepository interface {
	CreateEntity(ctx context.Context, input CreateEntityInput) (*Entity, error)
	GetEntityByID(ctx context.Context, id int64) (*Entity, error)
	GetEntityByKey(ctx context.Context, key string) (*Entity, error)
	ListEntities(ctx context.Context, filter EntityListFilter) ([]Entity, error)
	CreateBinding(ctx context.Context, input CreateEntityBindingInput) (*EntityBinding, error)
	ListBindings(ctx context.Context, filter EntityBindingListFilter) ([]EntityBinding, error)
	ResolveEntity(ctx context.Context, input EntityResolutionInput) (*ResolvedEntity, error)
}

func NormalizeEntityKey(value string) string {
	return strings.TrimSpace(value)
}

func normalizeEntityStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return EntityStatusActive
	}
	return status
}

func normalizeEntityType(entityType string) string {
	entityType = strings.TrimSpace(entityType)
	if entityType == "" {
		return EntityTypeWorkspace
	}
	return entityType
}

func normalizeEntityMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

func ValidateCreateEntityInput(input CreateEntityInput) (CreateEntityInput, error) {
	input.EntityKey = NormalizeEntityKey(input.EntityKey)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.EntityType = normalizeEntityType(input.EntityType)
	input.Status = normalizeEntityStatus(input.Status)
	input.Metadata = normalizeEntityMetadata(input.Metadata)
	if input.EntityKey == "" {
		return input, infraerrors.BadRequest("ENTITY_KEY_REQUIRED", "entity_key is required")
	}
	return input, nil
}

func ValidateCreateEntityBindingInput(input CreateEntityBindingInput) (CreateEntityBindingInput, error) {
	input.Status = normalizeEntityStatus(input.Status)
	input.Metadata = normalizeEntityMetadata(input.Metadata)
	if input.EntityID <= 0 {
		return input, infraerrors.BadRequest("ENTITY_ID_REQUIRED", "entity_id is required")
	}
	if input.AccountID != nil {
		return input, infraerrors.BadRequest("ENTITY_ACCOUNT_SCOPE_UNSUPPORTED", "account_id entity bindings are not supported yet")
	}
	if input.APIKeyID == nil && input.UserID == nil && input.GroupID == nil && input.AccountID == nil {
		return input, infraerrors.BadRequest("ENTITY_BINDING_SCOPE_REQUIRED", "at least one binding scope is required")
	}
	return input, nil
}

func IsEntityAuthorizationError(err error) bool {
	return infraerrors.Code(err) == http.StatusForbidden && infraerrors.Reason(err) == "ENTITY_NOT_AUTHORIZED"
}
