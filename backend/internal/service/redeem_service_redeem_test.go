package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type redeemRejectRepo struct {
	code      RedeemCode
	useCalled bool
}

func (r *redeemRejectRepo) Create(context.Context, *RedeemCode) error {
	panic("unexpected Create call")
}
func (r *redeemRejectRepo) CreateBatch(context.Context, []RedeemCode) error {
	panic("unexpected CreateBatch call")
}
func (r *redeemRejectRepo) GetByID(_ context.Context, id int64) (*RedeemCode, error) {
	if r.code.ID != id {
		return nil, ErrRedeemCodeNotFound
	}
	clone := r.code
	return &clone, nil
}
func (r *redeemRejectRepo) GetByCode(_ context.Context, code string) (*RedeemCode, error) {
	if r.code.Code != code {
		return nil, ErrRedeemCodeNotFound
	}
	clone := r.code
	return &clone, nil
}
func (r *redeemRejectRepo) Update(context.Context, *RedeemCode) error {
	panic("unexpected Update call")
}
func (r *redeemRejectRepo) BatchUpdate(context.Context, []int64, RedeemCodeBatchUpdateFields) (int64, error) {
	panic("unexpected BatchUpdate call")
}
func (r *redeemRejectRepo) Delete(context.Context, int64) error { panic("unexpected Delete call") }
func (r *redeemRejectRepo) Use(_ context.Context, id, userID int64) error {
	r.useCalled = true
	r.code.Status = StatusUsed
	r.code.UsedBy = &userID
	return nil
}
func (r *redeemRejectRepo) List(context.Context, pagination.PaginationParams) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (r *redeemRejectRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (r *redeemRejectRepo) ListByUser(context.Context, int64, int) ([]RedeemCode, error) {
	panic("unexpected ListByUser call")
}
func (r *redeemRejectRepo) ListByUserPaginated(context.Context, int64, pagination.PaginationParams, string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected ListByUserPaginated call")
}
func (r *redeemRejectRepo) SumPositiveBalanceByUser(context.Context, int64) (float64, error) {
	panic("unexpected SumPositiveBalanceByUser call")
}

type redeemRejectUserRepo struct {
	UserRepository
	getByIDCalls int
}

func (r *redeemRejectUserRepo) GetByID(context.Context, int64) (*User, error) {
	r.getByIDCalls++
	return nil, ErrUserNotFound
}

func TestRedeemRejectsInvitationCodeBeforeTransaction(t *testing.T) {
	redeemRepo := &redeemRejectRepo{code: RedeemCode{ID: 1, Code: "INVITE-001", Type: RedeemTypeInvitation, Status: StatusUnused}}
	userRepo := &redeemRejectUserRepo{}
	redeemService := NewRedeemService(redeemRepo, userRepo, nil, nil, nil, nil, nil, nil)

	got, err := redeemService.Redeem(context.Background(), 2, redeemRepo.code.Code)

	require.Nil(t, got)
	require.Error(t, err)
	require.True(t, infraerrors.IsBadRequest(err))
	require.Equal(t, "REDEEM_CODE_UNSUPPORTED_TYPE", infraerrors.Reason(err))
	require.Equal(t, "invitation codes can only be used during registration", infraerrors.Message(err))
	require.False(t, redeemRepo.useCalled)
	require.Equal(t, 0, userRepo.getByIDCalls)
	require.Equal(t, StatusUnused, redeemRepo.code.Status)
	require.Nil(t, redeemRepo.code.UsedBy)
}
