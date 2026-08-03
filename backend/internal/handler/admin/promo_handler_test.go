package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestPromoHandlerUpdateClearsExpiryWhenExpiresAtIsZero(t *testing.T) {
	gin.SetMode(gin.TestMode)

	expiresAt := time.Now().Add(24 * time.Hour).UTC()
	repo := &adminPromoCodeRepoStub{
		code: &service.PromoCode{
			ID:          9,
			Code:        "PROMO",
			BonusAmount: 10,
			MaxUses:     1,
			Status:      service.PromoCodeStatusActive,
			ExpiresAt:   &expiresAt,
		},
	}
	svc := service.NewPromoService(repo, nil, nil, nil, nil)
	handler := NewPromoHandler(svc)

	body, err := json.Marshal(map[string]any{"expires_at": 0})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/promo-codes/9", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "9"}}

	handler.Update(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", w.Code, w.Body.String())
	}
	if repo.updated == nil || repo.updated.ExpiresAt != nil {
		t.Fatalf("persisted ExpiresAt = %v, want nil", repo.updatedExpiresAt())
	}
}

type adminPromoCodeRepoStub struct {
	code    *service.PromoCode
	updated *service.PromoCode
}

func (r *adminPromoCodeRepoStub) updatedExpiresAt() *time.Time {
	if r.updated == nil {
		return nil
	}
	return r.updated.ExpiresAt
}

func (r *adminPromoCodeRepoStub) Create(context.Context, *service.PromoCode) error { return nil }
func (r *adminPromoCodeRepoStub) GetByID(context.Context, int64) (*service.PromoCode, error) {
	copy := *r.code
	return &copy, nil
}
func (r *adminPromoCodeRepoStub) GetByCode(context.Context, string) (*service.PromoCode, error) {
	return nil, service.ErrPromoCodeNotFound
}
func (r *adminPromoCodeRepoStub) GetByCodeForUpdate(context.Context, string) (*service.PromoCode, error) {
	return nil, service.ErrPromoCodeNotFound
}
func (r *adminPromoCodeRepoStub) Update(_ context.Context, code *service.PromoCode) error {
	copy := *code
	r.updated = &copy
	return nil
}
func (r *adminPromoCodeRepoStub) Delete(context.Context, int64) error { return nil }
func (r *adminPromoCodeRepoStub) List(context.Context, pagination.PaginationParams) ([]service.PromoCode, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *adminPromoCodeRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string) ([]service.PromoCode, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *adminPromoCodeRepoStub) CreateUsage(context.Context, *service.PromoCodeUsage) error {
	return nil
}
func (r *adminPromoCodeRepoStub) GetUsageByPromoCodeAndUser(context.Context, int64, int64) (*service.PromoCodeUsage, error) {
	return nil, nil
}
func (r *adminPromoCodeRepoStub) ListUsagesByPromoCode(context.Context, int64, pagination.PaginationParams) ([]service.PromoCodeUsage, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *adminPromoCodeRepoStub) IncrementUsedCount(context.Context, int64) error { return nil }
