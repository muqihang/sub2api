//go:build phase0red

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFormalPoolBrowserEgressAttestationRejectsUntrustedProofs(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	newService := func() *FormalPoolOnboardingService {
		cfg := DefaultFormalPoolConfig()
		cfg.NonceTTL = time.Minute
		return NewFormalPoolOnboardingService(FormalPoolOnboardingDeps{
			Store:  NewFormalPoolOnboardingStore(30*time.Minute, func() time.Time { return now }),
			Config: cfg,
			Proxy:  &formalProxyFake{},
		})
	}
	newProxyVerifiedSession := func(t *testing.T, svc *FormalPoolOnboardingService, groupID int64) (*FormalPoolOnboardingSession, string) {
		t.Helper()
		proxyID := int64(9)
		created, err := svc.StartSession(context.Background(), FormalPoolOnboardingStartRequest{
			ProxyMode: "existing", ProxyID: &proxyID, GroupID: groupID, AccountName: "phase0-boundary",
		})
		require.NoError(t, err)
		tested, err := svc.TestProxy(context.Background(), created.ID)
		require.NoError(t, err)
		parts := strings.Split(strings.TrimRight(tested.BrowserEgressCheckURL, "/"), "/")
		proof := parts[len(parts)-1]
		require.NotEmpty(t, proof)
		return tested, proof
	}
	attest := func(svc *FormalPoolOnboardingService, sessionID, proof string) error {
		_, err := svc.AttestBrowserEgress(context.Background(), sessionID, FormalPoolBrowserEgressAttestationRequest{
			Confirmed: true, VerificationCode: proof,
		})
		return err
	}

	t.Run("arbitrary non-empty code", func(t *testing.T) {
		svc := newService()
		session, _ := newProxyVerifiedSession(t, svc, 101)
		require.Error(t, attest(svc, session.ID, "arbitrary-non-empty"), "client-chosen text must not attest browser egress")
	})

	t.Run("wrong server proof", func(t *testing.T) {
		svc := newService()
		session, proof := newProxyVerifiedSession(t, svc, 101)
		require.Error(t, attest(svc, session.ID, proof+"-wrong"), "a modified server proof must be rejected")
	})

	t.Run("expired server proof", func(t *testing.T) {
		svc := newService()
		session, proof := newProxyVerifiedSession(t, svc, 101)
		now = now.Add(2 * time.Minute)
		require.Error(t, attest(svc, session.ID, proof), "an expired server proof must be rejected")
		now = now.Add(-2 * time.Minute)
	})

	t.Run("replayed server proof", func(t *testing.T) {
		svc := newService()
		session, proof := newProxyVerifiedSession(t, svc, 101)
		require.NoError(t, attest(svc, session.ID, proof), "fixture requires one accepted proof before replay")
		require.Error(t, attest(svc, session.ID, proof), "an accepted proof must be single-use")
	})

	t.Run("cross-session server proof", func(t *testing.T) {
		svc := newService()
		_, firstProof := newProxyVerifiedSession(t, svc, 101)
		second, _ := newProxyVerifiedSession(t, svc, 202)
		require.Error(t, attest(svc, second.ID, firstProof), "proofs must be bound to exactly one onboarding session")
	})

	t.Run("proof minted before proxy change", func(t *testing.T) {
		svc := newService()
		session, staleProof := newProxyVerifiedSession(t, svc, 101)
		_, err := svc.TestProxy(context.Background(), session.ID)
		require.NoError(t, err)
		require.Error(t, attest(svc, session.ID, staleProof), "a proxy re-test must invalidate every earlier proof")
	})
}
