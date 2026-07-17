package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestFormalPoolBrowserEgressAttestationRejectsUntrustedProofs(t *testing.T) {
	type fixture struct {
		svc *FormalPoolOnboardingService
		now *time.Time
	}
	newService := func() fixture {
		now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
		cfg := DefaultFormalPoolConfig()
		cfg.NonceTTL = time.Minute
		return fixture{
			svc: newAuthorizedFlowService(FormalPoolOnboardingDeps{
				Store:  NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now }),
				Config: cfg,
				Proxy:  &formalPoolBrowserEgressProxyFake{rawIP: "198.51.100.10"},
			}),
			now: &now,
		}
	}
	newProxyVerifiedSession := func(t *testing.T, svc *FormalPoolOnboardingService, groupID int64) (*FormalPoolOnboardingSession, string) {
		t.Helper()
		proxyID := int64(9)
		created, err := svc.StartSession(authorizedFlowContext(t, 0), FormalPoolOnboardingStartRequest{
			ProxyMode: "existing", ProxyID: &proxyID, GroupID: groupID, AccountName: "phase0-boundary",
		})
		require.NoError(t, err)
		tested, err := svc.TestProxy(authorizedFlowContext(t, created.Version), created.ID)
		require.NoError(t, err)
		parts := strings.Split(strings.TrimRight(tested.BrowserEgressCheckURL, "/"), "/")
		proof := parts[len(parts)-1]
		require.NotEmpty(t, proof)
		return tested, proof
	}
	attest := func(t *testing.T, svc *FormalPoolOnboardingService, sessionID, proof string, version int64) error {
		t.Helper()
		_, err := svc.AttestBrowserEgress(authorizedFlowContext(t, version), sessionID, FormalPoolBrowserEgressAttestationRequest{
			Confirmed: true, VerificationCode: proof,
		})
		return err
	}

	t.Run("arbitrary non-empty code", func(t *testing.T) {
		fx := newService()
		session, _ := newProxyVerifiedSession(t, fx.svc, 42)
		require.Error(t, attest(t, fx.svc, session.ID, "arbitrary-non-empty", session.Version), "client-chosen text must not attest browser egress")
	})

	t.Run("wrong server proof", func(t *testing.T) {
		fx := newService()
		session, proof := newProxyVerifiedSession(t, fx.svc, 42)
		require.Error(t, attest(t, fx.svc, session.ID, proof+"-wrong", session.Version), "a modified server proof must be rejected")
	})

	t.Run("expired server proof", func(t *testing.T) {
		fx := newService()
		session, proof := newProxyVerifiedSession(t, fx.svc, 42)
		*fx.now = fx.now.Add(2 * time.Minute)
		require.Error(t, attest(t, fx.svc, session.ID, proof, session.Version), "an expired server proof must be rejected")
	})

	t.Run("replayed server proof", func(t *testing.T) {
		fx := newService()
		session, proof := newProxyVerifiedSession(t, fx.svc, 42)
		observed, err := fx.svc.VerifyBrowserEgressByNonce(context.Background(), proof, "198.51.100.10")
		require.NoError(t, err)
		require.NoError(t, attest(t, fx.svc, session.ID, proof, observed.Version), "fixture requires one accepted proof before replay")
		require.Error(t, attest(t, fx.svc, session.ID, proof, observed.Version), "an accepted proof must be single-use")
	})

	t.Run("cross-session server proof", func(t *testing.T) {
		fx := newService()
		_, firstProof := newProxyVerifiedSession(t, fx.svc, 42)
		second, _ := newProxyVerifiedSession(t, fx.svc, 42)
		require.Error(t, attest(t, fx.svc, second.ID, firstProof, second.Version), "proofs must be bound to exactly one onboarding session")
	})

	t.Run("proof minted before proxy change", func(t *testing.T) {
		fx := newService()
		session, staleProof := newProxyVerifiedSession(t, fx.svc, 42)
		retested, err := fx.svc.TestProxy(authorizedFlowContext(t, session.Version), session.ID)
		require.NoError(t, err)
		require.Error(t, attest(t, fx.svc, session.ID, staleProof, retested.Version), "a proxy re-test must invalidate every earlier proof")
	})
}

func TestFormalPoolBrowserEgressConsumedProofReplayReasonPrecedesOnlyVersionAndState(t *testing.T) {
	svc, owner, observed, proof, revalidator := newServerObservedBrowserProofFixture(t)
	consumeInputVersion := observed.Version
	consumed, err := svc.AttestBrowserEgress(
		formalPoolBrowserProofAuthorityContext(owner, consumeInputVersion), observed.ID,
		FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: proof},
	)
	require.NoError(t, err)
	require.Equal(t, FormalPoolOnboardingStatusBrowserEgressVerified, consumed.Status)
	require.True(t, consumed.BrowserEgressVerified)
	require.Empty(t, consumed.BrowserEgressCheckURL)

	stored, ok := svc.store.get(observed.ID)
	require.True(t, ok)
	require.Empty(t, stored.BrowserNonce)
	require.Equal(t, formalPoolProofDigest(proof), stored.BrowserProofConsumedHash)
	require.NotEqual(t, proof, stored.BrowserProofConsumedHash)
	require.NotEmpty(t, stored.BrowserProofConsumedHash)
	require.False(t, stored.BrowserProofConsumedAt.IsZero())
	rawResponse, marshalErr := json.Marshal(consumed)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(rawResponse), proof)
	require.NotContains(t, string(rawResponse), stored.BrowserProofConsumedHash)

	for _, version := range []int64{consumeInputVersion, consumed.Version} {
		_, err = svc.AttestBrowserEgress(
			formalPoolBrowserProofAuthorityContext(owner, version), observed.ID,
			FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: proof},
		)
		requireFormalPoolReason(t, err, "FORMAL_POOL_BROWSER_PROOF_REJECTED")
	}

	intruder := owner
	intruder.SubjectID++
	for _, version := range []int64{consumeInputVersion, consumed.Version} {
		_, err = svc.AttestBrowserEgress(
			formalPoolBrowserProofAuthorityContext(intruder, version), observed.ID,
			FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: proof},
		)
		requireFormalPoolReason(t, err, "FORMAL_POOL_FORBIDDEN")
	}

	for _, tc := range []struct {
		name   string
		reason string
		err    error
	}{
		{name: "revoked", reason: "FORMAL_POOL_AUTH_REQUIRED", err: ErrFormalPoolOnboardingAuthenticationRequired},
		{name: "inactive", reason: "FORMAL_POOL_AUTH_REQUIRED", err: ErrFormalPoolOnboardingAuthenticationRequired},
		{name: "expired", reason: "FORMAL_POOL_AUTH_REQUIRED", err: ErrFormalPoolOnboardingAuthenticationRequired},
		{name: "token version changed", reason: "FORMAL_POOL_AUTH_REQUIRED", err: ErrFormalPoolOnboardingAuthenticationRequired},
		{name: "system admin role removed", reason: "FORMAL_POOL_FORBIDDEN", err: ErrFormalPoolOnboardingForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			revalidator.err = tc.err
			before, exists := svc.store.get(observed.ID)
			require.True(t, exists)
			_, attestErr := svc.AttestBrowserEgress(
				formalPoolBrowserProofAuthorityContext(owner, consumeInputVersion), observed.ID,
				FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: proof},
			)
			requireFormalPoolReason(t, attestErr, tc.reason)
			after, exists := svc.store.get(observed.ID)
			require.True(t, exists)
			require.Equal(t, before.Version, after.Version, "authority failure must happen before attestation CAS")
			revalidator.err = nil
		})
	}

	_, err = svc.AttestBrowserEgress(
		formalPoolBrowserProofAuthorityContext(owner, consumeInputVersion), observed.ID,
		FormalPoolBrowserEgressAttestationRequest{Confirmed: true, VerificationCode: proof + "0"},
	)
	requireFormalPoolReason(t, err, "FORMAL_POOL_ONBOARDING_VERSION_CONFLICT")
}

func newServerObservedBrowserProofFixture(t *testing.T) (*FormalPoolOnboardingService, FormalPoolOnboardingPrincipal, *FormalPoolOnboardingSession, string, *formalPrincipalRevalidatorFake) {
	t.Helper()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	store := NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now })
	revalidator := &formalPrincipalRevalidatorFake{}
	svc := newAuthorizedFlowService(FormalPoolOnboardingDeps{
		Store:                store,
		Proxy:                &formalPoolBrowserEgressProxyFake{rawIP: "198.51.100.10"},
		PrincipalRevalidator: revalidator,
	})
	owner := authorizedOnboardingPrincipal()
	created, err := svc.StartSession(
		formalPoolBrowserProofAuthorityContext(owner, 0),
		FormalPoolOnboardingStartRequest{ProxyMode: "existing", ProxyID: formalPtrInt64(9), GroupID: 42, AccountName: "server-observed-proof"},
	)
	require.NoError(t, err)
	tested, err := svc.TestProxy(formalPoolBrowserProofAuthorityContext(owner, created.Version), created.ID)
	require.NoError(t, err)
	parts := strings.Split(strings.TrimRight(tested.BrowserEgressCheckURL, "/"), "/")
	proof := parts[len(parts)-1]
	require.Regexp(t, `^nonce_[0-9a-f]{32}$`, proof)
	observed, err := svc.VerifyBrowserEgressByNonce(context.Background(), proof, "198.51.100.10")
	require.NoError(t, err)
	require.Equal(t, "verified_pending_finalize", observed.BrowserEgressCheckStatus)
	require.False(t, observed.BrowserEgressVerified)
	return svc, owner, observed, proof, revalidator
}

func formalPoolBrowserProofAuthorityContext(owner FormalPoolOnboardingPrincipal, version int64) context.Context {
	return WithFormalPoolRequestAuthority(context.Background(), FormalPoolRequestAuthority{
		Principal: owner, ExpectedVersion: &version, IdempotencyKey: "server-proof-fixture-0001",
	})
}

func requireFormalPoolReason(t *testing.T, err error, reason string) {
	t.Helper()
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, reason, appErr.Reason)
}
