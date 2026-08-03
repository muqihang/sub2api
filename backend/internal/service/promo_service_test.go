package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

func TestPromoServiceUpdateZeroExpiresAtClearsExpiry(t *testing.T) {
	expiresAt := time.Now().Add(24 * time.Hour).UTC()
	repo := &promoCodeRepoStub{
		code: &PromoCode{
			ID:          7,
			Code:        "PROMO",
			BonusAmount: 10,
			MaxUses:     1,
			Status:      PromoCodeStatusActive,
			ExpiresAt:   &expiresAt,
		},
	}
	svc := NewPromoService(repo, nil, nil, nil, nil)

	zero := time.Time{}
	got, err := svc.Update(context.Background(), 7, &UpdatePromoCodeInput{ExpiresAt: &zero})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got.ExpiresAt != nil {
		t.Fatalf("Update() ExpiresAt = %v, want nil", got.ExpiresAt)
	}
	if repo.updated == nil || repo.updated.ExpiresAt != nil {
		t.Fatalf("persisted ExpiresAt = %v, want nil", repo.updatedExpiresAt())
	}
}

type promoCodeRepoStub struct {
	code    *PromoCode
	updated *PromoCode
}

func (r *promoCodeRepoStub) updatedExpiresAt() *time.Time {
	if r.updated == nil {
		return nil
	}
	return r.updated.ExpiresAt
}

func (r *promoCodeRepoStub) Create(context.Context, *PromoCode) error { return nil }
func (r *promoCodeRepoStub) GetByID(context.Context, int64) (*PromoCode, error) {
	copy := *r.code
	return &copy, nil
}
func (r *promoCodeRepoStub) GetByCode(context.Context, string) (*PromoCode, error) {
	return nil, ErrPromoCodeNotFound
}
func (r *promoCodeRepoStub) GetByCodeForUpdate(context.Context, string) (*PromoCode, error) {
	return nil, ErrPromoCodeNotFound
}
func (r *promoCodeRepoStub) Update(_ context.Context, code *PromoCode) error {
	copy := *code
	r.updated = &copy
	return nil
}
func (r *promoCodeRepoStub) Delete(context.Context, int64) error { return nil }
func (r *promoCodeRepoStub) List(context.Context, pagination.PaginationParams) ([]PromoCode, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *promoCodeRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string) ([]PromoCode, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *promoCodeRepoStub) CreateUsage(context.Context, *PromoCodeUsage) error { return nil }
func (r *promoCodeRepoStub) GetUsageByPromoCodeAndUser(context.Context, int64, int64) (*PromoCodeUsage, error) {
	return nil, nil
}
func (r *promoCodeRepoStub) ListUsagesByPromoCode(context.Context, int64, pagination.PaginationParams) ([]PromoCodeUsage, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *promoCodeRepoStub) IncrementUsedCount(context.Context, int64) error { return nil }
