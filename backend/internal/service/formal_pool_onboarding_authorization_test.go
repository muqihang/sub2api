package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFormalPoolAuthorizeSessionNilRevalidatorFailsClosedBeforeVersionAndState(t *testing.T) {
	store := NewFormalPoolOnboardingStore(time.Hour, time.Now)
	owner := authorizedOnboardingPrincipal()
	store.save(&formalPoolOnboardingSessionRecord{
		ID: "nil-revalidator", Version: 4, Status: FormalPoolOnboardingStatusDraft,
		OwnerSubjectID: owner.SubjectID, OwnerAdministratorID: owner.AdministratorID,
		OwnerTenantID: owner.TenantID, OwnerCreatorID: owner.CreatorID, OwnerRole: owner.Role,
		OwnerGroupID: 101, GroupID: 101, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{Store: store})
	stale := int64(1)
	ctx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{Principal: owner, ExpectedVersion: &stale})

	_, err := svc.authorizeSession(ctx, "nil-revalidator", true, FormalPoolOnboardingStatusWarming)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingAuthenticationRequired)
	require.NotErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)
	require.NotErrorIs(t, err, ErrFormalPoolOnboardingInvalidState)
}

func TestFormalPoolAuthorizeSessionOrdersOwnerBeforeVersionAndState(t *testing.T) {
	svc, owner, session, revalidator := newAuthorizedOnboardingFixture(t)
	revalidator.calls.Store(0)
	intruder := owner
	intruder.SubjectID++
	stale := int64(0)
	ctx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{
		Principal:       intruder,
		ExpectedVersion: &stale,
	})
	_, err := svc.authorizeSession(ctx, session.ID, true, FormalPoolOnboardingStatusWarming)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingForbidden)
	require.NotErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)
	require.NotErrorIs(t, err, ErrFormalPoolOnboardingInvalidState)
	require.Zero(t, revalidator.calls.Load())
}

func TestFormalPoolAuthorizeSessionRevalidatesAfterOwnerBeforeVersionStateAndReservation(t *testing.T) {
	svc, owner, session, revalidator := newAuthorizedOnboardingFixture(t)
	revalidator.calls.Store(0)
	revalidator.err = ErrFormalPoolOnboardingAuthenticationRequired
	stale := int64(0)
	ctx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{
		Principal: owner, ExpectedVersion: &stale,
	})
	_, err := svc.authorizeSession(ctx, session.ID, true, "wrong_state")
	require.ErrorIs(t, err, ErrFormalPoolOnboardingAuthenticationRequired)
	require.Equal(t, int64(1), revalidator.calls.Load())
	rec, ok := svc.store.get(session.ID)
	require.True(t, ok)
	require.Nil(t, rec.ActiveOperation)
}

func TestFormalPoolStartSessionRequiresSystemAdminTenantAndActiveGroup(t *testing.T) {
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
		Proxy: &formalProxyFake{}, Groups: &formalGroupReaderFake{},
	})
	_, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{GroupID: 101})
	require.ErrorIs(t, err, ErrFormalPoolOnboardingAuthenticationRequired)
}

func TestFormalPoolStartSessionSecondRevalidationWinsOverBufferedGroupResult(t *testing.T) {
	groupErr := errors.New("repository detail must not escape")
	cases := []struct {
		name  string
		group *Group
		err   error
	}{
		{name: "success", group: &Group{ID: 101, Status: StatusActive}},
		{name: "repository error", err: groupErr},
		{name: "missing"},
		{name: "inactive", group: &Group{ID: 101, Status: "inactive"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			revalidator := &formalSequencedRevalidator{secondErr: ErrFormalPoolOnboardingAuthenticationRequired}
			groups := &formalSequencedGroupReader{group: tc.group, err: tc.err}
			proxy := &formalCountingProxy{}
			svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
				Proxy: proxy, Groups: groups, PrincipalRevalidator: revalidator,
			})
			ctx := authorizedOnboardingContext("second-revalidation", 0)
			_, err := svc.StartSession(ctx, validAuthorizedStartRequest())
			require.ErrorIs(t, err, ErrFormalPoolOnboardingAuthenticationRequired)
			require.NotErrorIs(t, err, groupErr)
			require.Equal(t, int64(2), revalidator.calls.Load())
			require.Equal(t, int64(1), groups.calls.Load())
			require.Zero(t, proxy.calls.Load())
			require.Empty(t, svc.store.sessions)
		})
	}
}

func TestFormalPoolStartSessionBlockingGroupReadCannotRaceRevocation(t *testing.T) {
	groupErr := errors.New("repository detail must remain buffered")
	cases := []struct {
		name  string
		group *Group
		err   error
	}{
		{name: "success", group: &Group{ID: 101, Status: StatusActive}},
		{name: "repository error", err: groupErr},
		{name: "missing"},
		{name: "inactive", group: &Group{ID: 101, Status: "inactive"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			revalidator := &formalMutableRevalidator{}
			groups := &formalBlockingGroupReader{
				group: tc.group, err: tc.err, entered: make(chan struct{}), release: make(chan struct{}),
			}
			proxy := &formalCountingProxy{}
			svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
				Proxy: proxy, Groups: groups, PrincipalRevalidator: revalidator,
			})
			result := make(chan error, 1)
			go func() {
				_, err := svc.StartSession(authorizedOnboardingContext("blocking-revoke-key", 0), validAuthorizedStartRequest())
				result <- err
			}()
			<-groups.entered
			revalidator.err = ErrFormalPoolOnboardingAuthenticationRequired
			close(groups.release)

			err := <-result
			require.ErrorIs(t, err, ErrFormalPoolOnboardingAuthenticationRequired)
			require.NotErrorIs(t, err, groupErr)
			require.Equal(t, int64(2), revalidator.calls.Load())
			require.Equal(t, int64(1), groups.calls.Load())
			require.Zero(t, proxy.calls.Load())
			require.Empty(t, svc.store.sessions)
		})
	}
}

func TestFormalPoolStartSessionSuccessfulControlUsesTwoRevalidationsOneGroupReadAndOneProxyCall(t *testing.T) {
	revalidator := &formalMutableRevalidator{}
	groups := &formalSequencedGroupReader{group: &Group{ID: 101, Status: StatusActive}}
	proxy := &formalCountingProxy{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
		Proxy: proxy, Groups: groups, PrincipalRevalidator: revalidator,
	})

	session, err := svc.StartSession(authorizedOnboardingContext("successful-control", 0), validAuthorizedStartRequest())
	require.NoError(t, err)
	require.Equal(t, int64(2), revalidator.calls.Load())
	require.Equal(t, int64(1), groups.calls.Load())
	require.Equal(t, int64(1), proxy.calls.Load())
	require.Equal(t, int64(2), session.Version)
	rec, ok := svc.store.get(session.ID)
	require.True(t, ok)
	require.Nil(t, rec.ActiveOperation)
	require.NotEmpty(t, rec.CreateKeySafeRef)
	require.NotEqual(t, "successful-control", rec.CreateKeySafeRef)
}

func TestFormalPoolStartSessionConcurrentCreateReservesOnceAndReplaysCompletedSession(t *testing.T) {
	proxy := &formalBlockingCreateProxy{entered: make(chan struct{}), release: make(chan struct{})}
	revalidator := &formalPrincipalRevalidatorFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
		Proxy: proxy, PrincipalRevalidator: revalidator,
		Groups: &formalGroupReaderFake{groups: map[int64]*Group{101: {ID: 101, Status: StatusActive}}},
	})
	ctx := authorizedOnboardingContext("concurrent-create-key", 0)
	req := validAuthorizedStartRequest()
	type result struct {
		session *FormalPoolOnboardingSession
		err     error
	}
	firstResult := make(chan result, 1)
	go func() {
		session, err := svc.StartSession(ctx, req)
		firstResult <- result{session: session, err: err}
	}()
	<-proxy.entered

	_, err := svc.StartSession(ctx, req)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)
	require.Equal(t, int64(1), proxy.calls.Load())
	close(proxy.release)
	first := <-firstResult
	require.NoError(t, first.err)
	require.Equal(t, int64(2), first.session.Version)

	replayed, err := svc.StartSession(ctx, req)
	require.NoError(t, err)
	require.Equal(t, first.session.ID, replayed.ID)
	require.Equal(t, first.session.Version, replayed.Version)
	require.Equal(t, int64(1), proxy.calls.Load())

	changed := req
	changed.AccountName = "changed"
	_, err = svc.StartSession(ctx, changed)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)
	require.Equal(t, int64(1), proxy.calls.Load())
}

func TestFormalPoolStartSessionProxyFailureFinalizesUnknownAndNeverRetries(t *testing.T) {
	proxyErr := errors.New("ambiguous proxy result")
	proxy := &formalCountingProxy{err: proxyErr}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
		Proxy: proxy, PrincipalRevalidator: &formalPrincipalRevalidatorFake{},
		Groups: &formalGroupReaderFake{groups: map[int64]*Group{101: {ID: 101, Status: StatusActive}}},
	})
	ctx := authorizedOnboardingContext("ambiguous-create-key", 0)
	req := validAuthorizedStartRequest()

	_, err := svc.StartSession(ctx, req)
	require.ErrorIs(t, err, proxyErr)
	require.Equal(t, int64(1), proxy.calls.Load())
	replayed, err := svc.StartSession(ctx, req)
	require.NoError(t, err)
	require.Equal(t, FormalPoolOnboardingStatusOperationOutcomeUnknown, replayed.Status)
	require.Equal(t, int64(2), replayed.Version)
	require.Equal(t, int64(1), proxy.calls.Load())
	rec, ok := svc.store.get(replayed.ID)
	require.True(t, ok)
	require.Nil(t, rec.ActiveOperation)
}

func TestFormalPoolStartSessionPostTTLReservationStaysExclusiveAndFinalizes(t *testing.T) {
	proxyErr := errors.New("ambiguous proxy result after ttl")
	cases := []struct {
		name       string
		proxyErr   error
		wantStatus string
	}{
		{name: "success", wantStatus: FormalPoolOnboardingStatusDraft},
		{name: "error", proxyErr: proxyErr, wantStatus: FormalPoolOnboardingStatusOperationOutcomeUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := newFormalMutableClock(time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))
			store := NewFormalPoolOnboardingStore(time.Minute, clock.Now)
			proxy := &formalBlockingCreateProxy{
				entered: make(chan struct{}), release: make(chan struct{}), err: tc.proxyErr,
			}
			svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
				Store: store, Proxy: proxy, PrincipalRevalidator: &formalPrincipalRevalidatorFake{},
				Groups: &formalGroupReaderFake{groups: map[int64]*Group{101: {ID: 101, Status: StatusActive}}},
			})
			ctx := authorizedOnboardingContext("post-ttl-create-key-"+tc.name, 0)
			req := validAuthorizedStartRequest()
			type result struct {
				session *FormalPoolOnboardingSession
				err     error
			}
			firstResult := make(chan result, 1)
			go func() {
				session, err := svc.StartSession(ctx, req)
				firstResult <- result{session: session, err: err}
			}()
			<-proxy.entered

			clock.Advance(2 * time.Minute)
			secondSession, secondErr := svc.StartSession(ctx, req)
			close(proxy.release)
			first := <-firstResult

			require.Nil(t, secondSession)
			require.ErrorIs(t, secondErr, ErrFormalPoolOnboardingVersionConflict)
			require.Equal(t, int64(1), proxy.calls.Load())
			if tc.proxyErr == nil {
				require.NoError(t, first.err)
				require.NotNil(t, first.session)
				require.Equal(t, int64(2), first.session.Version)
			} else {
				require.Nil(t, first.session)
				require.ErrorIs(t, first.err, tc.proxyErr)
			}

			replayed, err := svc.StartSession(ctx, req)
			require.NoError(t, err)
			require.Equal(t, int64(2), replayed.Version)
			require.Equal(t, tc.wantStatus, replayed.Status)
			require.Equal(t, int64(1), proxy.calls.Load())
		})
	}
}

func TestFormalPoolStartSessionReturnsProxyAndFinalizationErrors(t *testing.T) {
	proxyErr := errors.New("ambiguous proxy result")
	proxy := &formalBlockingCreateProxy{
		entered: make(chan struct{}), release: make(chan struct{}), err: proxyErr,
	}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
		Proxy: proxy, PrincipalRevalidator: &formalPrincipalRevalidatorFake{},
		Groups: &formalGroupReaderFake{groups: map[int64]*Group{101: {ID: 101, Status: StatusActive}}},
	})
	result := make(chan error, 1)
	go func() {
		_, err := svc.StartSession(authorizedOnboardingContext("finalization-error-key", 0), validAuthorizedStartRequest())
		result <- err
	}()
	<-proxy.entered

	svc.store.mu.Lock()
	for _, rec := range svc.store.sessions {
		if rec != nil && rec.ActiveOperation != nil {
			rec.ActiveOperation.OperationID = "invalidated-operation"
		}
	}
	svc.store.mu.Unlock()
	close(proxy.release)

	err := <-result
	require.ErrorIs(t, err, proxyErr)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)
	require.Equal(t, int64(1), proxy.calls.Load())
}

func TestFormalPoolStartSessionCreateKeyChecksCompleteOwnerBeforeReplayClassification(t *testing.T) {
	dimensions := []struct {
		name   string
		mutate func(*formalPoolOnboardingSessionRecord)
	}{
		{name: "subject", mutate: func(rec *formalPoolOnboardingSessionRecord) { rec.OwnerSubjectID++ }},
		{name: "administrator", mutate: func(rec *formalPoolOnboardingSessionRecord) { rec.OwnerAdministratorID++ }},
		{name: "tenant", mutate: func(rec *formalPoolOnboardingSessionRecord) { rec.OwnerTenantID = "other-tenant" }},
		{name: "creator", mutate: func(rec *formalPoolOnboardingSessionRecord) { rec.OwnerCreatorID++ }},
		{name: "role", mutate: func(rec *formalPoolOnboardingSessionRecord) { rec.OwnerRole = "user" }},
		{name: "group integrity", mutate: func(rec *formalPoolOnboardingSessionRecord) { rec.OwnerGroupID++ }},
	}
	classifications := []struct {
		name   string
		mutate func(*formalPoolOnboardingSessionRecord, *FormalPoolOnboardingStartRequest)
	}{
		{
			name: "completed fingerprint mismatch",
			mutate: func(_ *formalPoolOnboardingSessionRecord, req *FormalPoolOnboardingStartRequest) {
				req.AccountName = "changed-fingerprint"
			},
		},
		{
			name: "active reservation",
			mutate: func(rec *formalPoolOnboardingSessionRecord, _ *FormalPoolOnboardingStartRequest) {
				rec.ActiveOperation = &FormalPoolOperationReservation{
					OperationID: "active-operation", Kind: formalPoolOperationCreateSession,
					InputVersion: rec.Version, ReservationVersion: rec.Version,
				}
			},
		},
	}

	for _, classification := range classifications {
		for _, dimension := range dimensions {
			t.Run(classification.name+"/"+dimension.name, func(t *testing.T) {
				proxy := &formalCountingProxy{}
				svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
					Proxy: proxy, PrincipalRevalidator: &formalPrincipalRevalidatorFake{},
					Groups: &formalGroupReaderFake{groups: map[int64]*Group{101: {ID: 101, Status: StatusActive}}},
				})
				ctx := authorizedOnboardingContext("owner-envelope-create-key", 0)
				req := validAuthorizedStartRequest()
				created, err := svc.StartSession(ctx, req)
				require.NoError(t, err)

				svc.store.mu.Lock()
				rec := svc.store.sessions[created.ID]
				dimension.mutate(rec)
				classification.mutate(rec, &req)
				svc.store.mu.Unlock()
				proxy.calls.Store(0)

				got, err := svc.StartSession(ctx, req)
				require.Nil(t, got)
				require.ErrorIs(t, err, ErrFormalPoolOnboardingForbidden)
				require.NotErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)
				require.Zero(t, proxy.calls.Load())
			})
		}
	}
}

func TestFormalPoolSessionResponseIncludesVersionWithoutOwnerEnvelope(t *testing.T) {
	svc, owner, session, _ := newAuthorizedOnboardingFixture(t)
	encoded, err := json.Marshal(session)
	require.NoError(t, err)
	body := string(encoded)
	require.Contains(t, body, `"version":2`)
	require.NotContains(t, body, owner.TenantID)
	require.NotContains(t, body, "1001")
	require.NotContains(t, body, "owner_")
	rec, ok := svc.store.get(session.ID)
	require.True(t, ok)
	require.Equal(t, owner.SubjectID, rec.OwnerSubjectID)
	require.Equal(t, owner.TenantID, rec.OwnerTenantID)
}

func TestFormalPoolGetSessionRequiresOwnerAndRevalidatesWithoutVersion(t *testing.T) {
	svc, owner, session, revalidator := newAuthorizedOnboardingFixture(t)
	revalidator.calls.Store(0)
	intruder := owner
	intruder.CreatorID++
	intruderCtx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{Principal: intruder})
	_, err := svc.GetSession(intruderCtx, session.ID)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingForbidden)
	require.Zero(t, revalidator.calls.Load())

	ownerCtx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{Principal: owner})
	got, err := svc.GetSession(ownerCtx, session.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, got.ID)
	require.Equal(t, int64(1), revalidator.calls.Load())
}

func TestFormalPoolAbortSessionRequiresCurrentVersionAndOneCAS(t *testing.T) {
	svc, owner, session, revalidator := newAuthorizedOnboardingFixture(t)
	revalidator.calls.Store(0)
	withoutVersion := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{Principal: owner})
	_, err := svc.AbortSession(withoutVersion, session.ID)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingVersionRequired)

	stale := session.Version - 1
	staleCtx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{Principal: owner, ExpectedVersion: &stale})
	_, err = svc.AbortSession(staleCtx, session.ID)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)

	current := session.Version
	currentCtx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{Principal: owner, ExpectedVersion: &current})
	aborted, err := svc.AbortSession(currentCtx, session.ID)
	require.NoError(t, err)
	require.Equal(t, session.Version+1, aborted.Version)
	require.Equal(t, FormalPoolOnboardingStatusAborted, aborted.Status)
	require.Equal(t, int64(3), revalidator.calls.Load())
}

func TestFormalPoolAuthorizeSessionMapsMalformedAuthoritiesToAuthenticationRequired(t *testing.T) {
	svc, owner, session, revalidator := newAuthorizedOnboardingFixture(t)
	cases := []struct {
		name   string
		mutate func(*FormalPoolOnboardingPrincipal)
	}{
		{name: "expired", mutate: func(p *FormalPoolOnboardingPrincipal) { p.ExpiresAtUnix = 1 }},
		{name: "inactive", mutate: func(p *FormalPoolOnboardingPrincipal) { p.Active = false }},
		{name: "non human", mutate: func(p *FormalPoolOnboardingPrincipal) { p.CallerKind = "service" }},
		{name: "missing subject", mutate: func(p *FormalPoolOnboardingPrincipal) { p.SubjectID = 0 }},
		{name: "missing tenant", mutate: func(p *FormalPoolOnboardingPrincipal) { p.TenantID = "" }},
		{name: "missing authority revision", mutate: func(p *FormalPoolOnboardingPrincipal) { p.AuthorityRevision = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			revalidator.calls.Store(0)
			principal := owner
			tc.mutate(&principal)
			ctx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{Principal: principal})
			_, err := svc.GetSession(ctx, session.ID)
			require.ErrorIs(t, err, ErrFormalPoolOnboardingAuthenticationRequired)
			require.Zero(t, revalidator.calls.Load())
		})
	}
}

func TestFormalPoolAuthorizeSessionMapsRoleAndOwnerEnvelopeMismatchToForbidden(t *testing.T) {
	svc, owner, session, revalidator := newAuthorizedOnboardingFixture(t)
	cases := []struct {
		name   string
		mutate func(*FormalPoolOnboardingPrincipal)
	}{
		{name: "system admin lost", mutate: func(p *FormalPoolOnboardingPrincipal) { p.SystemAdmin = false }},
		{name: "role changed", mutate: func(p *FormalPoolOnboardingPrincipal) { p.Role = "user" }},
		{name: "subject changed", mutate: func(p *FormalPoolOnboardingPrincipal) { p.SubjectID++ }},
		{name: "administrator changed", mutate: func(p *FormalPoolOnboardingPrincipal) { p.AdministratorID++ }},
		{name: "tenant changed", mutate: func(p *FormalPoolOnboardingPrincipal) { p.TenantID = "tenant-two" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			revalidator.calls.Store(0)
			principal := owner
			tc.mutate(&principal)
			ctx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{Principal: principal})
			_, err := svc.GetSession(ctx, session.ID)
			require.ErrorIs(t, err, ErrFormalPoolOnboardingForbidden)
			require.Zero(t, revalidator.calls.Load())
		})
	}
}

func TestFormalPoolAuthorizeSessionRevalidationFailureClassesPrecedeVersionAndState(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "missing user", err: ErrFormalPoolOnboardingAuthenticationRequired, want: ErrFormalPoolOnboardingAuthenticationRequired},
		{name: "inactive user", err: ErrFormalPoolOnboardingAuthenticationRequired, want: ErrFormalPoolOnboardingAuthenticationRequired},
		{name: "revoked user", err: ErrFormalPoolOnboardingAuthenticationRequired, want: ErrFormalPoolOnboardingAuthenticationRequired},
		{name: "expired user", err: ErrFormalPoolOnboardingAuthenticationRequired, want: ErrFormalPoolOnboardingAuthenticationRequired},
		{name: "token version drift", err: ErrFormalPoolOnboardingAuthenticationRequired, want: ErrFormalPoolOnboardingAuthenticationRequired},
		{name: "current role loss", err: ErrFormalPoolOnboardingForbidden, want: ErrFormalPoolOnboardingForbidden},
		{name: "subject mismatch", err: ErrFormalPoolOnboardingForbidden, want: ErrFormalPoolOnboardingForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, owner, session, revalidator := newAuthorizedOnboardingFixture(t)
			revalidator.calls.Store(0)
			revalidator.err = tc.err
			stale := int64(0)
			ctx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{Principal: owner, ExpectedVersion: &stale})
			_, err := svc.authorizeSession(ctx, session.ID, true, "wrong_state")
			require.ErrorIs(t, err, tc.want)
			require.NotErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)
			require.NotErrorIs(t, err, ErrFormalPoolOnboardingInvalidState)
			require.Equal(t, int64(1), revalidator.calls.Load())
			rec, ok := svc.store.get(session.ID)
			require.True(t, ok)
			require.Nil(t, rec.ActiveOperation)
		})
	}
}

func TestFormalPoolReservedMutationRequiresExactOperationAndReservationVersion(t *testing.T) {
	svc, owner, session, _ := newAuthorizedOnboardingFixture(t)
	ctx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{
		Principal: owner, ExpectedVersion: &session.Version,
	})
	reserved, reservation, err := svc.beginReservedMutation(ctx, session.ID, "test_operation", FormalPoolOnboardingStatusDraft)
	require.NoError(t, err)
	require.Equal(t, session.Version+1, reserved.Version)
	require.Equal(t, reserved.Version, reservation.ReservationVersion)

	wrong := *reservation
	wrong.OperationID = "wrong"
	_, err = svc.finishReservedMutation(session.ID, &wrong, nil)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)
	stillReserved, ok := svc.store.get(session.ID)
	require.True(t, ok)
	require.NotNil(t, stillReserved.ActiveOperation)

	finished, err := svc.finishReservedMutation(session.ID, reservation, func(rec *formalPoolOnboardingSessionRecord) error {
		rec.Status = FormalPoolOnboardingStatusProxyVerified
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, session.Version+2, finished.Version)
	require.Nil(t, finished.ActiveOperation)
	_, err = svc.finishReservedMutation(session.ID, reservation, nil)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)
}

func TestFormalPoolReservedMutationReturnDoesNotAliasStoredReservation(t *testing.T) {
	svc, owner, session, _ := newAuthorizedOnboardingFixture(t)
	ctx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{
		Principal: owner, ExpectedVersion: &session.Version,
	})
	_, reservation, err := svc.beginReservedMutation(ctx, session.ID, "test_operation", FormalPoolOnboardingStatusDraft)
	require.NoError(t, err)
	original := *reservation

	reservation.OperationID = "mutated-operation"
	reservation.Kind = "mutated-kind"
	reservation.InputVersion++
	reservation.ReservationVersion++
	reservation.StartedAt = reservation.StartedAt.Add(time.Hour)

	stored, ok := svc.store.get(session.ID)
	require.True(t, ok)
	require.Equal(t, &original, stored.ActiveOperation)
	_, err = svc.finishReservedMutation(session.ID, reservation, nil)
	require.ErrorIs(t, err, ErrFormalPoolOnboardingVersionConflict)
	finished, err := svc.finishReservedMutation(session.ID, &original, nil)
	require.NoError(t, err)
	require.Nil(t, finished.ActiveOperation)
}

func newAuthorizedOnboardingFixture(t *testing.T) (*FormalPoolOnboardingService, FormalPoolOnboardingPrincipal, *FormalPoolOnboardingSession, *formalPrincipalRevalidatorFake) {
	t.Helper()
	revalidator := &formalPrincipalRevalidatorFake{}
	svc := NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
		Proxy: &formalProxyFake{}, PrincipalRevalidator: revalidator,
		Groups: &formalGroupReaderFake{groups: map[int64]*Group{
			101: {ID: 101, Status: StatusActive, Hydrated: true},
		}},
	})
	owner := authorizedOnboardingPrincipal()
	zero := int64(0)
	ctx := WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{
		Principal: owner, ExpectedVersion: &zero, IdempotencyKey: "fixture-create-key-0001",
	})
	proxyID := int64(9)
	session, err := svc.StartSession(ctx, FormalPoolOnboardingStartRequest{
		ProxyMode: "existing", ProxyID: &proxyID, GroupID: 101, AccountName: "authority-fixture",
	})
	require.NoError(t, err)
	return svc, owner, session, revalidator
}

func authorizedOnboardingPrincipal() FormalPoolOnboardingPrincipal {
	return FormalPoolOnboardingPrincipal{
		SubjectID: 1001, AdministratorID: 1001, TenantID: "tenant-one",
		CreatorID: 1001, Role: RoleAdmin, CallerKind: CallerKindHumanJWT,
		AuthorityRevision: 1, ExpiresAtUnix: 4102444800, Active: true, SystemAdmin: true,
	}
}

func authorizedOnboardingContext(key string, version int64) context.Context {
	return WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{
		Principal: authorizedOnboardingPrincipal(), ExpectedVersion: &version, IdempotencyKey: key,
	})
}

func validAuthorizedStartRequest() FormalPoolOnboardingStartRequest {
	proxyID := int64(9)
	return FormalPoolOnboardingStartRequest{
		ProxyMode: "existing", ProxyID: &proxyID, GroupID: 101, AccountName: "authority-fixture",
	}
}

type formalPrincipalRevalidatorFake struct {
	err   error
	calls atomic.Int64
}

func (f *formalPrincipalRevalidatorFake) Revalidate(ctx context.Context, principal FormalPoolOnboardingPrincipal) error {
	_ = ctx
	_ = principal
	f.calls.Add(1)
	return f.err
}

type formalGroupReaderFake struct{ groups map[int64]*Group }

func (f *formalGroupReaderFake) GetByID(ctx context.Context, id int64) (*Group, error) {
	_ = ctx
	if f.groups == nil && id > 0 {
		return &Group{ID: id, Status: StatusActive, Hydrated: true}, nil
	}
	group := f.groups[id]
	if group == nil {
		return nil, nil
	}
	copy := *group
	return &copy, nil
}

type formalSequencedRevalidator struct {
	secondErr error
	calls     atomic.Int64
}

type formalMutableRevalidator struct {
	err   error
	calls atomic.Int64
}

func (f *formalMutableRevalidator) Revalidate(ctx context.Context, principal FormalPoolOnboardingPrincipal) error {
	_ = ctx
	_ = principal
	f.calls.Add(1)
	return f.err
}

func (f *formalSequencedRevalidator) Revalidate(ctx context.Context, principal FormalPoolOnboardingPrincipal) error {
	_ = ctx
	_ = principal
	if f.calls.Add(1) == 2 {
		return f.secondErr
	}
	return nil
}

type formalSequencedGroupReader struct {
	group *Group
	err   error
	calls atomic.Int64
}

type formalBlockingGroupReader struct {
	group   *Group
	err     error
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func (f *formalBlockingGroupReader) GetByID(ctx context.Context, id int64) (*Group, error) {
	_ = ctx
	_ = id
	f.calls.Add(1)
	close(f.entered)
	<-f.release
	if f.group == nil {
		return nil, f.err
	}
	copy := *f.group
	return &copy, f.err
}

func (f *formalSequencedGroupReader) GetByID(ctx context.Context, id int64) (*Group, error) {
	_ = ctx
	_ = id
	f.calls.Add(1)
	if f.group == nil {
		return nil, f.err
	}
	copy := *f.group
	return &copy, f.err
}

type formalCountingProxy struct {
	err   error
	calls atomic.Int64
}

func (f *formalCountingProxy) ResolveOrCreateProxy(ctx context.Context, req FormalPoolOnboardingStartRequest) (FormalPoolProxyResolution, error) {
	_ = ctx
	f.calls.Add(1)
	if f.err != nil {
		return FormalPoolProxyResolution{}, f.err
	}
	id := int64(9)
	if req.ProxyID != nil {
		id = *req.ProxyID
	}
	return FormalPoolProxyResolution{ProxyID: id, ProxyRef: formalPoolSafeRef("proxy", "counting"), NormalizedProxyURL: "socks5h://proxy.local:1080"}, nil
}

func (f *formalCountingProxy) TestProxy(ctx context.Context, proxyID int64) (FormalPoolProxyTestSummary, error) {
	return FormalPoolProxyTestSummary{}, nil
}

func (f *formalCountingProxy) GetRawEgressIP(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
	return "", nil
}

type formalBlockingCreateProxy struct {
	entered chan struct{}
	release chan struct{}
	err     error
	calls   atomic.Int64
}

func (f *formalBlockingCreateProxy) ResolveOrCreateProxy(ctx context.Context, req FormalPoolOnboardingStartRequest) (FormalPoolProxyResolution, error) {
	_ = ctx
	if f.calls.Add(1) == 1 {
		close(f.entered)
		<-f.release
	}
	if f.err != nil {
		return FormalPoolProxyResolution{}, f.err
	}
	id := int64(9)
	if req.ProxyID != nil {
		id = *req.ProxyID
	}
	return FormalPoolProxyResolution{ProxyID: id, ProxyRef: formalPoolSafeRef("proxy", "blocking"), NormalizedProxyURL: "socks5h://proxy.local:1080"}, nil
}

func (f *formalBlockingCreateProxy) TestProxy(ctx context.Context, proxyID int64) (FormalPoolProxyTestSummary, error) {
	return FormalPoolProxyTestSummary{}, nil
}

func (f *formalBlockingCreateProxy) GetRawEgressIP(ctx context.Context, proxyID int64, normalizedProxyURL string) (string, error) {
	return "", nil
}

type formalMutableClock struct{ nanos atomic.Int64 }

func newFormalMutableClock(now time.Time) *formalMutableClock {
	clock := &formalMutableClock{}
	clock.nanos.Store(now.UnixNano())
	return clock
}

func (c *formalMutableClock) Now() time.Time {
	return time.Unix(0, c.nanos.Load()).UTC()
}

func (c *formalMutableClock) Advance(delta time.Duration) {
	c.nanos.Add(int64(delta))
}

func TestFormalPoolIdempotencyKeyValidationIsCanonicalURLSafe(t *testing.T) {
	require.True(t, validFormalPoolIdempotencyKey(strings.Repeat("a", 16)))
	require.True(t, validFormalPoolIdempotencyKey("fixture-create-key-0001"))
	for _, key := range []string{"", "short", strings.Repeat("a", 129), "contains space 000", "contains/slash/000", " " + strings.Repeat("a", 16) + " "} {
		require.False(t, validFormalPoolIdempotencyKey(key), key)
	}
}
