package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAugmentOfficialSessionServiceCreatesBackendGeneratedBindIntent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 13, 0, 0, 0, time.UTC)
	store := &augmentOfficialSessionStoreStub{
		createBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
			UserID:          42,
			BindIntentID:    "bind-intent-1",
			StateHash:       "unused",
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
			CreatedAt:       now,
		},
	}
	cipher := newTestAugmentSessionVaultCipher(t)
	svc := NewAugmentOfficialSessionService(store, cipher, "bind-secret-1")
	svc.now = func() time.Time { return now }
	svc.newToken = newStaticTokenGenerator("state-token-1")

	resp, err := svc.CreateBindIntent(context.Background(), 42, AugmentOfficialBindIntentRequest{
		Mode:            "official_passthrough",
		Source:          "official_quick_login",
		TenantAllowlist: []string{" https://official.augment.local/ "},
	})
	require.NoError(t, err)
	require.Equal(t, "bind-intent-1", resp.BindIntentID)
	require.Equal(t, "state-token-1", resp.State)
	require.Equal(t, now.Add(5*time.Minute), resp.ExpiresAt)
	require.NotEmpty(t, resp.BindToken)

	require.NotNil(t, store.createBindIntentInput)
	require.Equal(t, int64(42), store.createBindIntentInput.UserID)
	require.Equal(t, "official_passthrough", store.createBindIntentInput.Mode)
	require.Equal(t, "official_quick_login", store.createBindIntentInput.Source)
	require.Equal(t, []string{"https://official.augment.local"}, store.createBindIntentInput.TenantAllowlist)
	require.Equal(t, sha256Hex("state-token-1"), store.createBindIntentInput.StateHash)
}

func TestAugmentOfficialSessionServiceBindsCredentialPayloadOnce(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 13, 10, 0, 0, time.UTC)
	store := &augmentOfficialSessionStoreStub{
		createBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
			UserID:          42,
			BindIntentID:    "bind-intent-2",
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
			CreatedAt:       now,
		},
		consumeBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
			UserID:          42,
			BindIntentID:    "bind-intent-2",
			StateHash:       sha256Hex("state-token-2"),
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
			CreatedAt:       now,
			ConsumedAt:      timePtr(now),
		},
		upsertSessionView: &AugmentOfficialSessionStoredPublicView{
			UserID:       42,
			Mode:         "official_passthrough",
			Source:       "official_quick_login",
			TenantOrigin: "https://official.augment.local",
			Scopes:       []string{"augment:session"},
			ExpiresAt:    timePtr(now.Add(30 * time.Minute)),
			Status:       "active",
			Fingerprint:  "abcdef0123456789",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
	cipher := newTestAugmentSessionVaultCipher(t)
	svc := NewAugmentOfficialSessionService(store, cipher, "bind-secret-2")
	svc.now = func() time.Time { return now }
	svc.newToken = newStaticTokenGenerator("state-token-2")

	resp, err := svc.CreateBindIntent(context.Background(), 42, AugmentOfficialBindIntentRequest{
		Mode:            "official_passthrough",
		Source:          "official_quick_login",
		TenantAllowlist: []string{"https://official.augment.local"},
	})
	require.NoError(t, err)

	view, err := svc.BindOfficialSession(context.Background(), 42, resp.BindToken, AugmentOfficialBindRequest{
		BindIntentID: resp.BindIntentID,
		State:        resp.State,
		Mode:         "official_passthrough",
		Source:       "official_quick_login",
		Payload: map[string]any{
			"tenant_url":          "https://official.augment.local",
			"access_token":        "access-secret",
			"refresh_token":       "refresh-secret",
			"expires_at":          now.Add(30 * time.Minute).Format(time.RFC3339),
			"scopes":              []any{"augment:session"},
			"portal_origin":       "https://portal.augment.local",
			"official_session_id": "sess_123",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "active", view.Status)
	require.NotNil(t, store.upsertSessionInput)
	require.NotEmpty(t, store.upsertSessionInput.EncryptedCredentialPayload)
	require.NotContains(t, string(store.upsertSessionInput.EncryptedCredentialPayload), "access-secret")
	require.NotContains(t, string(store.upsertSessionInput.EncryptedCredentialPayload), "refresh-secret")

	events := svc.AuditEvents()
	require.Len(t, events, 1)
	require.Equal(t, "bind_success", events[0].Result)
	require.Equal(t, int64(42), events[0].ActorUserID)
	require.Equal(t, "official_quick_login", events[0].Source)
	require.Equal(t, "official.augment.local", events[0].TenantHost)
}

func TestAugmentOfficialSessionServiceRejectsReplayAndCrossUser(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 13, 20, 0, 0, time.UTC)
	t.Run("replay", func(t *testing.T) {
		store := &augmentOfficialSessionStoreStub{
			createBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
				UserID:          42,
				BindIntentID:    "bind-intent-3",
				Mode:            "official_passthrough",
				Source:          "official_quick_login",
				TenantAllowlist: []string{"https://official.augment.local"},
				ExpiresAt:       now.Add(5 * time.Minute),
				CreatedAt:       now,
			},
			consumeBindIntentErr: ErrAugmentOfficialSessionBindIntentConsumed,
		}
		cipher := newTestAugmentSessionVaultCipher(t)
		svc := NewAugmentOfficialSessionService(store, cipher, "bind-secret-3")
		svc.now = func() time.Time { return now }
		svc.newToken = newStaticTokenGenerator("state-token-3")

		resp, err := svc.CreateBindIntent(context.Background(), 42, AugmentOfficialBindIntentRequest{
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
		})
		require.NoError(t, err)

		_, err = svc.BindOfficialSession(context.Background(), 42, resp.BindToken, AugmentOfficialBindRequest{
			BindIntentID: resp.BindIntentID,
			State:        resp.State,
			Mode:         "official_passthrough",
			Source:       "official_quick_login",
			Payload: map[string]any{
				"tenant_url":    "https://official.augment.local",
				"access_token":  "access-secret",
				"refresh_token": "refresh-secret",
				"expires_at":    now.Add(30 * time.Minute).Format(time.RFC3339),
				"scopes":        []any{"augment:session"},
			},
		})
		require.ErrorIs(t, err, ErrAugmentOfficialSessionBindIntentConsumed)
	})

	t.Run("cross user", func(t *testing.T) {
		store := &augmentOfficialSessionStoreStub{
			createBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
				UserID:          42,
				BindIntentID:    "bind-intent-4",
				Mode:            "official_passthrough",
				Source:          "official_quick_login",
				TenantAllowlist: []string{"https://official.augment.local"},
				ExpiresAt:       now.Add(5 * time.Minute),
				CreatedAt:       now,
			},
		}
		cipher := newTestAugmentSessionVaultCipher(t)
		svc := NewAugmentOfficialSessionService(store, cipher, "bind-secret-4")
		svc.now = func() time.Time { return now }
		svc.newToken = newStaticTokenGenerator("state-token-4")

		resp, err := svc.CreateBindIntent(context.Background(), 42, AugmentOfficialBindIntentRequest{
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
		})
		require.NoError(t, err)

		_, err = svc.BindOfficialSession(context.Background(), 99, resp.BindToken, AugmentOfficialBindRequest{
			BindIntentID: resp.BindIntentID,
			State:        resp.State,
			Mode:         "official_passthrough",
			Source:       "official_quick_login",
			Payload: map[string]any{
				"tenant_url":    "https://official.augment.local",
				"access_token":  "access-secret",
				"refresh_token": "refresh-secret",
				"expires_at":    now.Add(30 * time.Minute).Format(time.RFC3339),
				"scopes":        []any{"augment:session"},
			},
		})
		require.ErrorIs(t, err, ErrAugmentOfficialBindTokenInvalid)
	})
}

func TestAugmentOfficialSessionServiceRejectsMissingBindToken(t *testing.T) {
	t.Parallel()

	svc := NewAugmentOfficialSessionService(&augmentOfficialSessionStoreStub{}, newTestAugmentSessionVaultCipher(t), "bind-secret-5")
	_, err := svc.BindOfficialSession(context.Background(), 42, "", AugmentOfficialBindRequest{})
	require.ErrorIs(t, err, ErrAugmentOfficialBindTokenMissing)
}

func TestAugmentOfficialSessionServiceRejectsIntentModeSourceTenantMismatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 13, 30, 0, 0, time.UTC)
	store := &augmentOfficialSessionStoreStub{
		createBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
			UserID:          42,
			BindIntentID:    "bind-intent-5",
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
			CreatedAt:       now,
		},
		consumeBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
			UserID:          42,
			BindIntentID:    "bind-intent-5",
			StateHash:       sha256Hex("state-token-5"),
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
			CreatedAt:       now,
			ConsumedAt:      timePtr(now),
		},
	}
	svc := NewAugmentOfficialSessionService(store, newTestAugmentSessionVaultCipher(t), "bind-secret-6")
	svc.now = func() time.Time { return now }
	svc.newToken = newStaticTokenGenerator("state-token-5")

	resp, err := svc.CreateBindIntent(context.Background(), 42, AugmentOfficialBindIntentRequest{
		Mode:            "official_passthrough",
		Source:          "official_quick_login",
		TenantAllowlist: []string{"https://official.augment.local"},
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		input      AugmentOfficialBindRequest
		wantReason string
	}{
		{
			name: "mode mismatch",
			input: AugmentOfficialBindRequest{
				BindIntentID: resp.BindIntentID,
				State:        resp.State,
				Mode:         "local_compat",
				Source:       "official_quick_login",
				Payload: map[string]any{
					"tenant_url":    "https://official.augment.local",
					"access_token":  "access-secret",
					"refresh_token": "refresh-secret",
					"expires_at":    now.Add(30 * time.Minute).Format(time.RFC3339),
					"scopes":        []any{"augment:session"},
				},
			},
			wantReason: "tenant_session_mismatch",
		},
		{
			name: "source mismatch",
			input: AugmentOfficialBindRequest{
				BindIntentID: resp.BindIntentID,
				State:        resp.State,
				Mode:         "official_passthrough",
				Source:       "wukong_quick_login",
				Payload: map[string]any{
					"tenant_url":    "https://official.augment.local",
					"access_token":  "access-secret",
					"refresh_token": "refresh-secret",
					"expires_at":    now.Add(30 * time.Minute).Format(time.RFC3339),
					"scopes":        []any{"augment:session"},
				},
			},
			wantReason: "tenant_session_mismatch",
		},
		{
			name: "tenant mismatch",
			input: AugmentOfficialBindRequest{
				BindIntentID: resp.BindIntentID,
				State:        resp.State,
				Mode:         "official_passthrough",
				Source:       "official_quick_login",
				Payload: map[string]any{
					"tenant_url":    "https://another.augment.local",
					"access_token":  "access-secret",
					"refresh_token": "refresh-secret",
					"expires_at":    now.Add(30 * time.Minute).Format(time.RFC3339),
					"scopes":        []any{"augment:session"},
				},
			},
			wantReason: "tenant_not_allowlisted",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.BindOfficialSession(context.Background(), 42, resp.BindToken, tc.input)
			require.Error(t, err)
			require.Equal(t, tc.wantReason, infraerrors.Reason(err))
		})
	}
}

func TestAugmentOfficialSessionServiceRejectsNonAllowlistedCredentialSchema(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 13, 40, 0, 0, time.UTC)
	store := &augmentOfficialSessionStoreStub{
		createBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
			UserID:          42,
			BindIntentID:    "bind-intent-6",
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
			CreatedAt:       now,
		},
	}
	svc := NewAugmentOfficialSessionService(store, newTestAugmentSessionVaultCipher(t), "bind-secret-7")
	svc.now = func() time.Time { return now }
	svc.newToken = newStaticTokenGenerator("state-token-6")

	resp, err := svc.CreateBindIntent(context.Background(), 42, AugmentOfficialBindIntentRequest{
		Mode:            "official_passthrough",
		Source:          "official_quick_login",
		TenantAllowlist: []string{"https://official.augment.local"},
	})
	require.NoError(t, err)

	_, err = svc.BindOfficialSession(context.Background(), 42, resp.BindToken, AugmentOfficialBindRequest{
		BindIntentID: resp.BindIntentID,
		State:        resp.State,
		Mode:         "official_passthrough",
		Source:       "official_quick_login",
		Payload: map[string]any{
			"tenant_url":    "https://official.augment.local",
			"access_token":  "access-secret",
			"refresh_token": "refresh-secret",
			"expires_at":    now.Add(30 * time.Minute).Format(time.RFC3339),
			"scopes":        []any{"augment:session"},
			"cookie":        "bad-extra-field",
		},
	})
	require.Error(t, err)
	require.Equal(t, "AUGMENT_OFFICIAL_CREDENTIAL_SCHEMA_INVALID", infraerrors.Reason(err))
}

func TestAugmentOfficialSessionServiceRejectsExpiredCredentialPayload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 13, 50, 0, 0, time.UTC)
	store := &augmentOfficialSessionStoreStub{
		createBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
			UserID:          42,
			BindIntentID:    "bind-intent-7",
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
			CreatedAt:       now,
		},
	}
	svc := NewAugmentOfficialSessionService(store, newTestAugmentSessionVaultCipher(t), "bind-secret-8")
	svc.now = func() time.Time { return now }
	svc.newToken = newStaticTokenGenerator("state-token-7")

	resp, err := svc.CreateBindIntent(context.Background(), 42, AugmentOfficialBindIntentRequest{
		Mode:            "official_passthrough",
		Source:          "official_quick_login",
		TenantAllowlist: []string{"https://official.augment.local"},
	})
	require.NoError(t, err)

	_, err = svc.BindOfficialSession(context.Background(), 42, resp.BindToken, AugmentOfficialBindRequest{
		BindIntentID: resp.BindIntentID,
		State:        resp.State,
		Mode:         "official_passthrough",
		Source:       "official_quick_login",
		Payload: map[string]any{
			"tenant_url":    "https://official.augment.local",
			"access_token":  "access-secret",
			"refresh_token": "refresh-secret",
			"expires_at":    now.Add(-time.Minute).Format(time.RFC3339),
			"scopes":        []any{"augment:session"},
		},
	})
	require.Error(t, err)
	require.Equal(t, "AUGMENT_OFFICIAL_CREDENTIAL_EXPIRED", infraerrors.Reason(err))
}

func TestAugmentOfficialSessionServiceWritesBindAuditWithoutSecrets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	store := &augmentOfficialSessionStoreStub{
		createBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
			UserID:          42,
			BindIntentID:    "bind-intent-8",
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
			CreatedAt:       now,
		},
		consumeBindIntentRecord: &AugmentOfficialSessionBindIntentStoreRecord{
			UserID:          42,
			BindIntentID:    "bind-intent-8",
			StateHash:       sha256Hex("state-token-8"),
			Mode:            "official_passthrough",
			Source:          "official_quick_login",
			TenantAllowlist: []string{"https://official.augment.local"},
			ExpiresAt:       now.Add(5 * time.Minute),
			CreatedAt:       now,
			ConsumedAt:      timePtr(now),
		},
		upsertSessionView: &AugmentOfficialSessionStoredPublicView{
			UserID:       42,
			Mode:         "official_passthrough",
			Source:       "official_quick_login",
			TenantOrigin: "https://official.augment.local",
			Scopes:       []string{"augment:session"},
			ExpiresAt:    timePtr(now.Add(30 * time.Minute)),
			Status:       "active",
			Fingerprint:  "abcdef0123456789",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
	svc := NewAugmentOfficialSessionService(store, newTestAugmentSessionVaultCipher(t), "bind-secret-9")
	svc.now = func() time.Time { return now }
	svc.newToken = newStaticTokenGenerator("state-token-8")

	resp, err := svc.CreateBindIntent(context.Background(), 42, AugmentOfficialBindIntentRequest{
		Mode:            "official_passthrough",
		Source:          "official_quick_login",
		TenantAllowlist: []string{"https://official.augment.local"},
	})
	require.NoError(t, err)

	_, err = svc.BindOfficialSession(context.Background(), 42, resp.BindToken, AugmentOfficialBindRequest{
		BindIntentID: resp.BindIntentID,
		State:        resp.State,
		Mode:         "official_passthrough",
		Source:       "official_quick_login",
		Payload: map[string]any{
			"tenant_url":    "https://official.augment.local",
			"access_token":  "access-secret",
			"refresh_token": "refresh-secret",
			"expires_at":    now.Add(30 * time.Minute).Format(time.RFC3339),
			"scopes":        []any{"augment:session"},
		},
	})
	require.NoError(t, err)

	events := svc.AuditEvents()
	require.Len(t, events, 1)

	data, err := json.Marshal(events)
	require.NoError(t, err)
	text := string(data)
	require.NotContains(t, text, "access-secret")
	require.NotContains(t, text, "refresh-secret")
	require.NotContains(t, text, "Authorization")
	require.NotContains(t, text, "cookie")
	require.NotContains(t, text, "session_bundle")
}

func TestAugmentOfficialSessionServiceReturnsPublicViewWithoutSecrets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 14, 10, 0, 0, time.UTC)
	store := &augmentOfficialSessionStoreStub{
		publicView: &AugmentOfficialSessionStoredPublicView{
			UserID:       42,
			Mode:         "official_passthrough",
			Source:       "official_quick_login",
			TenantOrigin: "https://official.augment.local",
			Scopes:       []string{"augment:session"},
			ExpiresAt:    timePtr(now.Add(20 * time.Minute)),
			Status:       "active",
			Fingerprint:  "abcdef0123456789",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
	svc := NewAugmentOfficialSessionService(store, newTestAugmentSessionVaultCipher(t), "bind-secret-10")

	view, err := svc.GetOfficialSession(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, "abcdef012345", view.FingerprintPrefix)

	data, err := json.Marshal(view)
	require.NoError(t, err)
	text := string(data)
	require.NotContains(t, text, "access_token")
	require.NotContains(t, text, "refresh_token")
	require.NotContains(t, text, "encrypted_credential_payload")
}

func TestAugmentOfficialSessionServiceGetCredentialForRouteRequiresActiveSession(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	cipher := newTestAugmentSessionVaultCipher(t)
	payload := mustEncryptAugmentOfficialPayload(t, cipher, augmentOfficialEncryptedCredentialPayload{
		AccessToken:       "access-secret",
		RefreshToken:      "refresh-secret",
		OfficialSessionID: "sess_123",
	})

	t.Run("inactive session", func(t *testing.T) {
		svc := NewAugmentOfficialSessionService(&augmentOfficialSessionStoreStub{}, cipher, "bind-secret-11")
		_, err := svc.GetCredentialForRoute(context.Background(), 42)
		require.ErrorIs(t, err, ErrAugmentOfficialSessionInactive)
	})

	t.Run("active session", func(t *testing.T) {
		store := &augmentOfficialSessionStoreStub{
			credentialRow: &AugmentOfficialSessionStoredCredentialRow{
				UserID:                     42,
				Mode:                       "official_passthrough",
				Source:                     "official_quick_login",
				TenantOrigin:               "https://official.augment.local",
				Scopes:                     []string{"augment:session"},
				ExpiresAt:                  timePtr(now.Add(24 * time.Hour)),
				Status:                     "active",
				EncryptedCredentialPayload: payload,
				CredentialSchemaVersion:    1,
				KeyVersion:                 "key-active",
				Fingerprint:                "abcdef0123456789",
				CreatedAt:                  now,
				UpdatedAt:                  now,
			},
		}
		svc := NewAugmentOfficialSessionService(store, cipher, "bind-secret-11")
		cred, err := svc.GetCredentialForRoute(context.Background(), 42)
		require.NoError(t, err)
		require.Equal(t, "access-secret", cred.AccessToken)
		require.Equal(t, "refresh-secret", cred.RefreshToken)
		require.Equal(t, "sess_123", cred.OfficialSessionID)
	})
}

func TestAugmentOfficialSessionServiceRevokeClearsCredential(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC)
	store := &augmentOfficialSessionStoreStub{
		revokeView: &AugmentOfficialSessionStoredPublicView{
			UserID:       42,
			Mode:         "official_passthrough",
			Source:       "official_quick_login",
			TenantOrigin: "https://official.augment.local",
			Scopes:       []string{"augment:session"},
			Status:       "revoked",
			Fingerprint:  "abcdef0123456789",
			CreatedAt:    now,
			UpdatedAt:    now,
			RevokedAt:    timePtr(now),
		},
	}
	svc := NewAugmentOfficialSessionService(store, newTestAugmentSessionVaultCipher(t), "bind-secret-12")

	view, err := svc.RevokeOfficialSession(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, "revoked", view.Status)
	require.Len(t, svc.AuditEvents(), 1)

	store.credentialRow = nil
	_, err = svc.GetCredentialForRoute(context.Background(), 42)
	require.ErrorIs(t, err, ErrAugmentOfficialSessionInactive)
}

type augmentOfficialSessionStoreStub struct {
	createBindIntentInput   *AugmentOfficialSessionBindIntentStoreCreateInput
	createBindIntentRecord  *AugmentOfficialSessionBindIntentStoreRecord
	createBindIntentErr     error
	consumeBindIntentRecord *AugmentOfficialSessionBindIntentStoreRecord
	consumeBindIntentErr    error
	upsertSessionInput      *AugmentOfficialSessionStoredSessionInput
	upsertSessionView       *AugmentOfficialSessionStoredPublicView
	upsertSessionErr        error
	publicView              *AugmentOfficialSessionStoredPublicView
	publicViewErr           error
	adminView               *AugmentOfficialSessionStoredAdminView
	credentialRow           *AugmentOfficialSessionStoredCredentialRow
	credentialRowErr        error
	revokeView              *AugmentOfficialSessionStoredPublicView
	revokeErr               error
}

func (s *augmentOfficialSessionStoreStub) CreateBindIntent(ctx context.Context, input AugmentOfficialSessionBindIntentStoreCreateInput) (*AugmentOfficialSessionBindIntentStoreRecord, error) {
	s.createBindIntentInput = &input
	if s.createBindIntentErr != nil {
		return nil, s.createBindIntentErr
	}
	return s.createBindIntentRecord, nil
}

func (s *augmentOfficialSessionStoreStub) ConsumeBindIntent(ctx context.Context, bindIntentID string, userID int64) (*AugmentOfficialSessionBindIntentStoreRecord, error) {
	if s.consumeBindIntentErr != nil {
		return nil, s.consumeBindIntentErr
	}
	return s.consumeBindIntentRecord, nil
}

func (s *augmentOfficialSessionStoreStub) UpsertActiveSession(ctx context.Context, input AugmentOfficialSessionStoredSessionInput) (*AugmentOfficialSessionStoredPublicView, error) {
	s.upsertSessionInput = &input
	if s.upsertSessionErr != nil {
		return nil, s.upsertSessionErr
	}
	return s.upsertSessionView, nil
}

func (s *augmentOfficialSessionStoreStub) GetActiveSessionPublicView(ctx context.Context, userID int64) (*AugmentOfficialSessionStoredPublicView, error) {
	if s.publicViewErr != nil {
		return nil, s.publicViewErr
	}
	return s.publicView, nil
}

func (s *augmentOfficialSessionStoreStub) GetActiveSessionAdminView(ctx context.Context, userID int64) (*AugmentOfficialSessionStoredAdminView, error) {
	if s.publicViewErr != nil {
		return nil, s.publicViewErr
	}
	return s.adminView, nil
}

func (s *augmentOfficialSessionStoreStub) ListAdminSessions(ctx context.Context) ([]AugmentOfficialSessionStoredAdminView, error) {
	if s.adminView == nil {
		return nil, nil
	}
	return []AugmentOfficialSessionStoredAdminView{*s.adminView}, nil
}

func (s *augmentOfficialSessionStoreStub) GetActiveSessionCredentialRow(ctx context.Context, userID int64) (*AugmentOfficialSessionStoredCredentialRow, error) {
	if s.credentialRowErr != nil {
		return nil, s.credentialRowErr
	}
	return s.credentialRow, nil
}

func (s *augmentOfficialSessionStoreStub) RevokeActiveSession(ctx context.Context, userID int64) (*AugmentOfficialSessionStoredPublicView, error) {
	if s.revokeErr != nil {
		return nil, s.revokeErr
	}
	return s.revokeView, nil
}

func newTestAugmentSessionVaultCipher(t *testing.T) *AugmentSessionVaultCipher {
	t.Helper()

	cipher, err := NewAugmentSessionVaultCipher(AugmentSessionVaultKeyset{
		ActiveKeyID: "key-active",
		Keys: map[string][]byte{
			"key-active": []byte("0123456789abcdef0123456789abcdef"),
		},
	})
	require.NoError(t, err)
	return cipher
}

func newStaticTokenGenerator(tokens ...string) func(int) (string, error) {
	index := 0
	return func(int) (string, error) {
		if index >= len(tokens) {
			return "", errors.New("no more static tokens")
		}
		token := tokens[index]
		index++
		return token, nil
	}
}

func mustEncryptAugmentOfficialPayload(t *testing.T, cipher *AugmentSessionVaultCipher, payload augmentOfficialEncryptedCredentialPayload) []byte {
	t.Helper()

	data, err := json.Marshal(payload)
	require.NoError(t, err)
	out, err := cipher.Encrypt(data)
	require.NoError(t, err)
	return out
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func timePtr(t time.Time) *time.Time {
	return &t
}
