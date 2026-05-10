package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAugmentOfficialPoolSessionServiceAcquireSessionBundleUsesSourcePriorityFallback(t *testing.T) {
	t.Parallel()

	cipher := newTestAugmentSessionVaultCipher(t)
	payload := mustEncryptAugmentOfficialPayload(t, cipher, augmentOfficialEncryptedCredentialPayload{
		AccessToken:  "pool-access",
		RefreshToken: "pool-refresh",
	})
	store := &augmentOfficialPoolSessionStoreStub{
		acquireBySource: map[string]*AugmentOfficialPoolStoredCredentialRow{
			augmentOfficialSessionSourceWukongQuickLogin: {
				ID:                         9,
				Source:                     augmentOfficialSessionSourceWukongQuickLogin,
				TenantOrigin:               "https://official.augment.local",
				Scopes:                     []string{"augment:session"},
				ExpiresAt:                  timePtr(time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)),
				Status:                     AugmentOfficialPoolSessionStatusActive,
				EncryptedCredentialPayload: payload,
				CredentialSchemaVersion:    1,
				KeyVersion:                 "key-active",
				Fingerprint:                "poolfingerprint",
				HealthScore:                100,
			},
		},
	}
	svc := NewAugmentOfficialPoolSessionService(store, cipher, "bind-secret")
	svc.SetSourcePriorityProvider(augmentOfficialPoolSourcePriorityProviderStub{
		sources: []string{
			augmentOfficialSessionSourceOfficialQuickLogin,
			augmentOfficialSessionSourceWukongQuickLogin,
		},
	})

	lease, err := svc.AcquireSessionBundle(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.Equal(t, "pool-access", lease.Bundle.AccessToken)
	require.Equal(t, []string{
		augmentOfficialSessionSourceOfficialQuickLogin,
		augmentOfficialSessionSourceWukongQuickLogin,
	}, store.acquiredSources)
}

func TestAugmentOfficialPoolSessionServiceBindSessionStoresPoolCredential(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	store := &augmentOfficialPoolSessionStoreStub{
		createIntentRecord: &AugmentOfficialPoolBindIntentStoreRecord{
			AdminUserID:     7,
			BindIntentID:    "pool-bind-intent",
			StateHash:       sha256Hex("state-1"),
			Mode:            augmentOfficialSessionModeOfficialPassthrough,
			Source:          augmentOfficialSessionSourceOfficialQuickLogin,
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
		},
		consumeIntentRecord: &AugmentOfficialPoolBindIntentStoreRecord{
			AdminUserID:     7,
			BindIntentID:    "pool-bind-intent",
			StateHash:       sha256Hex("state-1"),
			Mode:            augmentOfficialSessionModeOfficialPassthrough,
			Source:          augmentOfficialSessionSourceOfficialQuickLogin,
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
		},
		upsertView: &AugmentOfficialPoolStoredAdminView{
			ID:                   21,
			Source:               augmentOfficialSessionSourceOfficialQuickLogin,
			TenantOrigin:         "https://official.augment.local",
			Status:               AugmentOfficialPoolSessionStatusActive,
			Fingerprint:          "abcdef0123456789",
			CreatedAt:            now,
			UpdatedAt:            now,
			HealthScore:          100,
			CreatedByAdminID:     7,
			HasCredentialPayload: true,
		},
	}
	cipher := newTestAugmentSessionVaultCipher(t)
	svc := NewAugmentOfficialPoolSessionService(store, cipher, "bind-secret")
	svc.now = func() time.Time { return now }

	intent, err := svc.CreateBindIntent(context.Background(), 7, AugmentOfficialPoolBindIntentRequest{
		Mode:            augmentOfficialSessionModeOfficialPassthrough,
		Source:          augmentOfficialSessionSourceOfficialQuickLogin,
		TenantAllowlist: []string{"https://official.augment.local"},
	})
	require.NoError(t, err)

	view, err := svc.BindSession(context.Background(), 7, intent.BindToken, AugmentOfficialPoolBindRequest{
		BindIntentID: intent.BindIntentID,
		State:        intent.State,
		Mode:         augmentOfficialSessionModeOfficialPassthrough,
		Source:       augmentOfficialSessionSourceOfficialQuickLogin,
		Payload: map[string]any{
			"tenant_url":    "https://official.augment.local",
			"access_token":  "official-access",
			"refresh_token": "official-refresh",
			"expires_at":    now.Add(time.Hour).Format(time.RFC3339),
			"scopes":        []any{"augment:session"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, int64(21), view.ID)
	require.NotNil(t, store.upsertInput)
	require.NotEmpty(t, store.upsertInput.EncryptedCredentialPayload)
}

func TestAugmentOfficialPoolSessionServiceRevokeSessionClearsCredential(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 13, 0, 0, 0, time.UTC)
	store := &augmentOfficialPoolSessionStoreStub{
		revokeView: &AugmentOfficialPoolStoredAdminView{
			ID:                   7,
			Source:               augmentOfficialSessionSourceOfficialQuickLogin,
			TenantOrigin:         "https://official.augment.local",
			Status:               AugmentOfficialPoolSessionStatusRevoked,
			Fingerprint:          "abcdef0123456789",
			CreatedAt:            now,
			UpdatedAt:            now,
			HealthScore:          100,
			CreatedByAdminID:     1,
			HasCredentialPayload: false,
		},
	}
	svc := NewAugmentOfficialPoolSessionService(store, nil, "bind-secret")
	svc.now = func() time.Time { return now }

	view, err := svc.RevokeSessionForAdmin(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, AugmentOfficialPoolSessionStatusRevoked, view.Status)
	require.False(t, view.HasCredentialPayload)
}

func TestAugmentOfficialPoolSessionServiceImportCursorSessionStoresPoolCredential(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 9, 8, 0, 0, 0, time.UTC)
	store := &augmentOfficialPoolSessionStoreStub{
		upsertView: &AugmentOfficialPoolStoredAdminView{
			ID:                   31,
			Source:               augmentOfficialSessionSourceOfficialQuickLogin,
			TenantOrigin:         "https://d12.api.augmentcode.com",
			Status:               AugmentOfficialPoolSessionStatusActive,
			Fingerprint:          "abcdef0123456789",
			CreatedAt:            now,
			UpdatedAt:            now,
			HealthScore:          100,
			CreatedByAdminID:     1,
			HasCredentialPayload: true,
		},
	}
	cipher := newTestAugmentSessionVaultCipher(t)
	svc := NewAugmentOfficialPoolSessionService(store, cipher, "bind-secret")
	svc.now = func() time.Time { return now }
	svc.SetLocalCursorSessionReader(augmentLocalCursorSessionReaderFunc(func(context.Context, AugmentOfficialPoolLocalCursorImportRequest) (*augmentLocalCursorImportedSession, error) {
		return &augmentLocalCursorImportedSession{
			AccessToken:   "official-access-token",
			TenantURL:     "https://d12.api.augmentcode.com/",
			Scopes:        []string{"email"},
			SessionSource: "official",
		}, nil
	}))

	view, err := svc.ImportLocalCursorSessionForAdmin(context.Background(), 1, AugmentOfficialPoolLocalCursorImportRequest{
		Source: augmentOfficialSessionSourceOfficialQuickLogin,
	})
	require.NoError(t, err)
	require.Equal(t, int64(31), view.ID)
	require.NotNil(t, store.upsertInput)
	require.Equal(t, augmentOfficialSessionSourceOfficialQuickLogin, store.upsertInput.Source)
	require.Equal(t, "https://d12.api.augmentcode.com", store.upsertInput.TenantOrigin)
	require.Nil(t, store.upsertInput.ExpiresAt)
	require.NotEmpty(t, store.upsertInput.EncryptedCredentialPayload)
}

type augmentOfficialPoolSessionStoreStub struct {
	createIntentRecord  *AugmentOfficialPoolBindIntentStoreRecord
	createIntentErr     error
	consumeIntentRecord *AugmentOfficialPoolBindIntentStoreRecord
	consumeIntentErr    error
	upsertInput         *AugmentOfficialPoolStoredSessionInput
	upsertView          *AugmentOfficialPoolStoredAdminView
	upsertErr           error
	adminViews          []AugmentOfficialPoolStoredAdminView
	adminView           *AugmentOfficialPoolStoredAdminView
	acquireBySource     map[string]*AugmentOfficialPoolStoredCredentialRow
	acquireByID         map[int64]*AugmentOfficialPoolStoredCredentialRow
	acquiredSources     []string
	acquiredIDs         []int64
	releaseErr          error
	revokeView          *AugmentOfficialPoolStoredAdminView
	revokeErr           error
}

func (s *augmentOfficialPoolSessionStoreStub) CreateBindIntent(ctx context.Context, input AugmentOfficialPoolBindIntentStoreCreateInput) (*AugmentOfficialPoolBindIntentStoreRecord, error) {
	if s.createIntentErr != nil {
		return nil, s.createIntentErr
	}
	return s.createIntentRecord, nil
}

func (s *augmentOfficialPoolSessionStoreStub) ConsumeBindIntent(ctx context.Context, bindIntentID string, adminUserID int64) (*AugmentOfficialPoolBindIntentStoreRecord, error) {
	if s.consumeIntentErr != nil {
		return nil, s.consumeIntentErr
	}
	return s.consumeIntentRecord, nil
}

func (s *augmentOfficialPoolSessionStoreStub) UpsertPoolSession(ctx context.Context, input AugmentOfficialPoolStoredSessionInput) (*AugmentOfficialPoolStoredAdminView, error) {
	s.upsertInput = &input
	if s.upsertErr != nil {
		return nil, s.upsertErr
	}
	return s.upsertView, nil
}

func (s *augmentOfficialPoolSessionStoreStub) ListAdminSessions(ctx context.Context) ([]AugmentOfficialPoolStoredAdminView, error) {
	return s.adminViews, nil
}

func (s *augmentOfficialPoolSessionStoreStub) GetAdminSession(ctx context.Context, sessionID int64) (*AugmentOfficialPoolStoredAdminView, error) {
	return s.adminView, nil
}

func (s *augmentOfficialPoolSessionStoreStub) AcquireUsableSession(ctx context.Context, source string, now, leaseUntil time.Time) (*AugmentOfficialPoolStoredCredentialRow, error) {
	s.acquiredSources = append(s.acquiredSources, source)
	if s.acquireBySource == nil {
		return nil, nil
	}
	return s.acquireBySource[source], nil
}

func (s *augmentOfficialPoolSessionStoreStub) AcquireUsableSessionByID(ctx context.Context, sessionID int64, now, leaseUntil time.Time) (*AugmentOfficialPoolStoredCredentialRow, error) {
	s.acquiredIDs = append(s.acquiredIDs, sessionID)
	if s.acquireByID == nil {
		return nil, nil
	}
	return s.acquireByID[sessionID], nil
}

func (s *augmentOfficialPoolSessionStoreStub) ReleaseLease(ctx context.Context, sessionID int64, input AugmentOfficialPoolLeaseReleaseInput) (*AugmentOfficialPoolStoredAdminView, error) {
	if s.releaseErr != nil {
		return nil, s.releaseErr
	}
	return s.revokeView, nil
}

func (s *augmentOfficialPoolSessionStoreStub) RevokePoolSession(ctx context.Context, sessionID int64, status string, now time.Time) (*AugmentOfficialPoolStoredAdminView, error) {
	if s.revokeErr != nil {
		return nil, s.revokeErr
	}
	return s.revokeView, nil
}

type augmentOfficialPoolSourcePriorityProviderStub struct {
	sources []string
	err     error
}

func (s augmentOfficialPoolSourcePriorityProviderStub) GetSourcePriority(ctx context.Context) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.sources, nil
}

func mustMarshalPoolJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

var _ = errors.New
