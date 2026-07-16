package service

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const CallerKindHumanJWT = "human_jwt"

const formalPoolOperationCreateSession = "create_session"

var (
	ErrFormalPoolOnboardingAuthenticationRequired = infraerrors.Unauthorized(
		"FORMAL_POOL_AUTH_REQUIRED", "formal pool authorization is required",
	)
	ErrFormalPoolOnboardingForbidden = infraerrors.Forbidden(
		"FORMAL_POOL_FORBIDDEN", "formal pool operation is forbidden",
	)
	ErrFormalPoolOnboardingVersionRequired = infraerrors.New(
		http.StatusPreconditionRequired,
		"FORMAL_POOL_ONBOARDING_VERSION_REQUIRED",
		"formal pool expected version is required",
	)
	ErrFormalPoolOnboardingInvalidState = infraerrors.Conflict(
		"FORMAL_POOL_ONBOARDING_INVALID_STATE", "formal pool onboarding session state conflict",
	)
	ErrFormalPoolIdempotencyKeyRequired = infraerrors.New(
		http.StatusPreconditionRequired,
		"FORMAL_POOL_IDEMPOTENCY_KEY_REQUIRED",
		"formal pool idempotency key is required",
	)
	errFormalPoolGroupUnavailable = infraerrors.ServiceUnavailable(
		"FORMAL_POOL_GROUP_VALIDATION_UNAVAILABLE", "formal pool group validation is unavailable",
	)
	errFormalPoolGroupInvalid = infraerrors.BadRequest(
		"FORMAL_POOL_GROUP_INVALID", "formal pool group must be active",
	)
	formalPoolIdempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
)

type FormalPoolOnboardingPrincipal struct {
	SubjectID         int64
	AdministratorID   int64
	TenantID          string
	CreatorID         int64
	Role              string
	CallerKind        string
	AuthorityRevision int64
	ExpiresAtUnix     int64
	Active            bool
	SystemAdmin       bool
}

type FormalPoolRequestAuthority struct {
	Principal       FormalPoolOnboardingPrincipal
	ExpectedVersion *int64
	IdempotencyKey  string
}

type FormalPoolOnboardingGroupReader interface {
	GetByID(ctx context.Context, id int64) (*Group, error)
}

type FormalPoolOnboardingPrincipalRevalidator interface {
	Revalidate(ctx context.Context, principal FormalPoolOnboardingPrincipal) error
}

type FormalPoolOperationReservation struct {
	OperationID        string
	Kind               string
	InputVersion       int64
	ReservationVersion int64
	StartedAt          time.Time
}

type formalPoolRequestAuthorityContextKey struct{}

func WithFormalPoolRequestAuthority(ctx context.Context, authority FormalPoolRequestAuthority) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, formalPoolRequestAuthorityContextKey{}, authority)
}

func FormalPoolRequestAuthorityFromContext(ctx context.Context) (FormalPoolRequestAuthority, bool) {
	if ctx == nil {
		return FormalPoolRequestAuthority{}, false
	}
	authority, ok := ctx.Value(formalPoolRequestAuthorityContextKey{}).(FormalPoolRequestAuthority)
	return authority, ok
}

func (s *FormalPoolOnboardingService) authorizeCreate(ctx context.Context, groupID int64) (FormalPoolRequestAuthority, error) {
	authority, err := s.authorityFromContext(ctx)
	if err != nil {
		return FormalPoolRequestAuthority{}, err
	}
	if err := s.revalidatePrincipal(ctx, authority.Principal); err != nil {
		return FormalPoolRequestAuthority{}, err
	}

	var group *Group
	var groupErr error
	if s.groups == nil {
		groupErr = errFormalPoolGroupUnavailable
	} else {
		group, groupErr = s.groups.GetByID(ctx, groupID)
	}

	if err := s.revalidatePrincipal(ctx, authority.Principal); err != nil {
		return FormalPoolRequestAuthority{}, err
	}
	if groupErr != nil {
		return FormalPoolRequestAuthority{}, errFormalPoolGroupUnavailable
	}
	if group == nil || group.ID != groupID || group.Status != StatusActive {
		return FormalPoolRequestAuthority{}, errFormalPoolGroupInvalid
	}
	return authority, nil
}

func (s *FormalPoolOnboardingService) authorizeSession(ctx context.Context, id string, requireVersion bool, allowedStates ...string) (*formalPoolOnboardingSessionRecord, error) {
	authority, err := s.authorityFromContext(ctx)
	if err != nil {
		return nil, err
	}
	rec, ok := s.store.get(id)
	if !ok {
		return nil, ErrFormalPoolOnboardingNotFound
	}
	if !formalPoolOwnerMatches(authority.Principal, rec) {
		return nil, ErrFormalPoolOnboardingForbidden
	}
	if err := s.revalidatePrincipal(ctx, authority.Principal); err != nil {
		return nil, err
	}
	return authorizeFormalPoolVersionAndState(authority, rec, requireVersion, allowedStates...)
}

func (s *FormalPoolOnboardingService) authorizeAccount(ctx context.Context, accountID int64, requireVersion bool, allowedStates ...string) (*formalPoolOnboardingSessionRecord, error) {
	authority, err := s.authorityFromContext(ctx)
	if err != nil {
		return nil, err
	}
	rec, err := s.store.snapshotByAccountID(accountID)
	if err != nil {
		return nil, err
	}
	if !formalPoolOwnerMatches(authority.Principal, rec) {
		return nil, ErrFormalPoolOnboardingForbidden
	}
	if err := s.revalidatePrincipal(ctx, authority.Principal); err != nil {
		return nil, err
	}
	return authorizeFormalPoolVersionAndState(authority, rec, requireVersion, allowedStates...)
}

func (s *FormalPoolOnboardingService) authorizeBrowserEgressOwner(ctx context.Context, id string) (FormalPoolRequestAuthority, *formalPoolOnboardingSessionRecord, error) {
	authority, err := s.authorityFromContext(ctx)
	if err != nil {
		return FormalPoolRequestAuthority{}, nil, err
	}
	rec, ok := s.store.get(id)
	if !ok {
		return FormalPoolRequestAuthority{}, nil, ErrFormalPoolOnboardingNotFound
	}
	if !formalPoolOwnerMatches(authority.Principal, rec) {
		return FormalPoolRequestAuthority{}, nil, ErrFormalPoolOnboardingForbidden
	}
	if err := s.revalidatePrincipal(ctx, authority.Principal); err != nil {
		return FormalPoolRequestAuthority{}, nil, err
	}
	return authority, rec, nil
}

func (s *FormalPoolOnboardingService) beginReservedMutation(ctx context.Context, id, operationKind string, allowedStates ...string) (*formalPoolOnboardingSessionRecord, *FormalPoolOperationReservation, error) {
	snapshot, err := s.authorizeSession(ctx, id, true, allowedStates...)
	if err != nil {
		return nil, nil, err
	}
	reservation := &FormalPoolOperationReservation{
		OperationID:        formalPoolRandomID("fpo_op_"),
		Kind:               strings.TrimSpace(operationKind),
		InputVersion:       snapshot.Version,
		ReservationVersion: snapshot.Version + 1,
		StartedAt:          s.store.now(),
	}
	updated, err := s.store.casUpdate(snapshot.ID, snapshot.Version, func(rec *formalPoolOnboardingSessionRecord) error {
		if rec.ActiveOperation != nil {
			return ErrFormalPoolOnboardingVersionConflict
		}
		rec.ActiveOperation = reservation
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return updated, reservation, nil
}

func (s *FormalPoolOnboardingService) beginCreateReservation(rec *formalPoolOnboardingSessionRecord) (*formalPoolOnboardingSessionRecord, bool, error) {
	if s == nil || s.store == nil {
		return nil, false, ErrFormalPoolOnboardingVersionConflict
	}
	return s.store.beginCreateReservation(rec)
}

func (s *FormalPoolOnboardingService) finishReservedMutation(id string, reservation *FormalPoolOperationReservation, mutate func(*formalPoolOnboardingSessionRecord) error) (*formalPoolOnboardingSessionRecord, error) {
	return s.completeReservedMutation(id, reservation, mutate)
}

func (s *FormalPoolOnboardingService) failReservedMutation(id string, reservation *FormalPoolOperationReservation, mutate func(*formalPoolOnboardingSessionRecord) error) (*formalPoolOnboardingSessionRecord, error) {
	return s.completeReservedMutation(id, reservation, mutate)
}

func (s *FormalPoolOnboardingService) completeReservedMutation(id string, reservation *FormalPoolOperationReservation, mutate func(*formalPoolOnboardingSessionRecord) error) (*formalPoolOnboardingSessionRecord, error) {
	if reservation == nil || strings.TrimSpace(reservation.OperationID) == "" {
		return nil, ErrFormalPoolOnboardingVersionConflict
	}
	return s.store.casUpdate(id, reservation.ReservationVersion, func(rec *formalPoolOnboardingSessionRecord) error {
		active := rec.ActiveOperation
		if active == nil || active.OperationID != reservation.OperationID || active.ReservationVersion != reservation.ReservationVersion {
			return ErrFormalPoolOnboardingVersionConflict
		}
		if mutate != nil {
			if err := mutate(rec); err != nil {
				return err
			}
		}
		rec.ActiveOperation = nil
		return nil
	})
}

func (s *FormalPoolOnboardingService) authorityFromContext(ctx context.Context) (FormalPoolRequestAuthority, error) {
	authority, ok := FormalPoolRequestAuthorityFromContext(ctx)
	if !ok || !s.validPrincipalShape(authority.Principal) {
		return FormalPoolRequestAuthority{}, ErrFormalPoolOnboardingAuthenticationRequired
	}
	if !authority.Principal.SystemAdmin || authority.Principal.Role != RoleAdmin {
		return FormalPoolRequestAuthority{}, ErrFormalPoolOnboardingForbidden
	}
	return authority, nil
}

func (s *FormalPoolOnboardingService) validPrincipalShape(principal FormalPoolOnboardingPrincipal) bool {
	if principal.CallerKind != CallerKindHumanJWT || !principal.Active {
		return false
	}
	if principal.SubjectID <= 0 || principal.AdministratorID <= 0 || principal.CreatorID <= 0 || principal.AuthorityRevision <= 0 {
		return false
	}
	if strings.TrimSpace(principal.TenantID) == "" {
		return false
	}
	now := time.Now().Unix()
	if s != nil && s.store != nil && s.store.now != nil {
		now = s.store.now().Unix()
	}
	return principal.ExpiresAtUnix > now
}

func (s *FormalPoolOnboardingService) revalidatePrincipal(ctx context.Context, principal FormalPoolOnboardingPrincipal) error {
	if s == nil || s.principalRevalidator == nil {
		return ErrFormalPoolOnboardingAuthenticationRequired
	}
	if err := s.principalRevalidator.Revalidate(ctx, principal); err != nil {
		if errors.Is(err, ErrFormalPoolOnboardingForbidden) {
			return ErrFormalPoolOnboardingForbidden
		}
		return ErrFormalPoolOnboardingAuthenticationRequired
	}
	return nil
}

func formalPoolOwnerMatches(principal FormalPoolOnboardingPrincipal, rec *formalPoolOnboardingSessionRecord) bool {
	return rec != nil &&
		principal.SubjectID == rec.OwnerSubjectID &&
		principal.AdministratorID == rec.OwnerAdministratorID &&
		principal.TenantID == rec.OwnerTenantID &&
		principal.CreatorID == rec.OwnerCreatorID &&
		principal.Role == rec.OwnerRole &&
		rec.OwnerGroupID > 0 && rec.OwnerGroupID == rec.GroupID
}

func authorizeFormalPoolVersionAndState(authority FormalPoolRequestAuthority, rec *formalPoolOnboardingSessionRecord, requireVersion bool, allowedStates ...string) (*formalPoolOnboardingSessionRecord, error) {
	if requireVersion && authority.ExpectedVersion == nil {
		return nil, ErrFormalPoolOnboardingVersionRequired
	}
	if requireVersion && *authority.ExpectedVersion != rec.Version {
		return nil, ErrFormalPoolOnboardingVersionConflict
	}
	if len(allowedStates) > 0 {
		allowed := false
		for _, state := range allowedStates {
			if rec.Status == state {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, ErrFormalPoolOnboardingInvalidState
		}
	}
	return rec, nil
}

func validFormalPoolIdempotencyKey(key string) bool {
	trimmed := strings.TrimSpace(key)
	return key == trimmed && formalPoolIdempotencyKeyPattern.MatchString(trimmed)
}
