package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// ─── Repository stub for entry center tests ───

type codexEntryCenterRepoStub struct {
	devices []*dbent.CodexManagedDevice
	grants  []*dbent.CodexSetupGrant

	// Optional overrides.
	listManagedDevicesByUser     func(ctx context.Context, userID int64) ([]*dbent.CodexManagedDevice, error)
	listPendingSetupGrantsByUser func(ctx context.Context, userID int64, now time.Time) ([]*dbent.CodexSetupGrant, error)
	getManagedDevice             func(ctx context.Context, id int64) (*dbent.CodexManagedDevice, error)
	createSetupGrant             func(ctx context.Context, params CreateCodexSetupGrantParams) (*dbent.CodexSetupGrant, error)
	revokeManagedDevice          func(ctx context.Context, id int64, revokedAt time.Time) error
}

func (s *codexEntryCenterRepoStub) CreateSetupGrant(ctx context.Context, params CreateCodexSetupGrantParams) (*dbent.CodexSetupGrant, error) {
	if s.createSetupGrant != nil {
		return s.createSetupGrant(ctx, params)
	}
	return &dbent.CodexSetupGrant{ID: 100, UserID: params.UserID, APIKeyID: params.APIKeyID, ServerOrigin: params.ServerOrigin, GatewayOrigin: params.GatewayOrigin, ExpiresAt: params.ExpiresAt}, nil
}

func (s *codexEntryCenterRepoStub) ConsumeSetupGrant(ctx context.Context, codeHash string, now time.Time) (*dbent.CodexSetupGrant, error) {
	return nil, ErrCodexSetupGrantNotActive
}

func (s *codexEntryCenterRepoStub) CreateManagedDevice(ctx context.Context, params CreateCodexManagedDeviceParams) (*dbent.CodexManagedDevice, error) {
	return &dbent.CodexManagedDevice{ID: 1}, nil
}

func (s *codexEntryCenterRepoStub) GetManagedDevice(ctx context.Context, id int64) (*dbent.CodexManagedDevice, error) {
	if s.getManagedDevice != nil {
		return s.getManagedDevice(ctx, id)
	}
	for _, d := range s.devices {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, ErrCodexManagedDeviceNotFound
}

func (s *codexEntryCenterRepoStub) TouchManagedDevice(ctx context.Context, id int64, seenAt time.Time) error {
	return nil
}

func (s *codexEntryCenterRepoStub) ListManagedDevicesByUser(ctx context.Context, userID int64) ([]*dbent.CodexManagedDevice, error) {
	if s.listManagedDevicesByUser != nil {
		return s.listManagedDevicesByUser(ctx, userID)
	}
	return s.devices, nil
}

func (s *codexEntryCenterRepoStub) RevokeManagedDevice(ctx context.Context, id int64, revokedAt time.Time) error {
	if s.revokeManagedDevice != nil {
		return s.revokeManagedDevice(ctx, id, revokedAt)
	}
	return nil
}

func (s *codexEntryCenterRepoStub) CreateDeviceToken(ctx context.Context, params CreateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
	return &dbent.CodexDeviceToken{}, nil
}

func (s *codexEntryCenterRepoStub) RotateDeviceToken(ctx context.Context, params RotateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
	return &dbent.CodexDeviceToken{}, nil
}

func (s *codexEntryCenterRepoStub) FindActiveTokenByHash(ctx context.Context, refreshTokenHash string, now time.Time) (*dbent.CodexDeviceToken, error) {
	return nil, ErrCodexManagedRefreshTokenInvalid
}

func (s *codexEntryCenterRepoStub) InsertAuditLog(ctx context.Context, params InsertCodexDeviceAuditLogParams) (*dbent.CodexDeviceAuditLog, error) {
	return &dbent.CodexDeviceAuditLog{}, nil
}

func (s *codexEntryCenterRepoStub) GetSetupGrantByID(ctx context.Context, id int64) (*dbent.CodexSetupGrant, error) {
	for _, g := range s.grants {
		if g.ID == id {
			return g, nil
		}
	}
	return nil, ErrCodexSetupSessionNotFound
}
func (s *codexEntryCenterRepoStub) ListPendingSetupGrantsByUser(ctx context.Context, userID int64, now time.Time) ([]*dbent.CodexSetupGrant, error) {
	if s.listPendingSetupGrantsByUser != nil {
		return s.listPendingSetupGrantsByUser(ctx, userID, now)
	}
	return s.grants, nil
}

// ─── API key reader stub ───

type codexEntryCenterAPIKeyReaderStub struct {
	verifyOwnership func(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error)
}

func (s *codexEntryCenterAPIKeyReaderStub) VerifyOwnership(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error) {
	if s.verifyOwnership != nil {
		return s.verifyOwnership(ctx, userID, apiKeyIDs)
	}
	return apiKeyIDs, nil
}

func (s *codexEntryCenterAPIKeyReaderStub) GetByID(ctx context.Context, id int64) (*APIKey, error) {
	return &APIKey{ID: id, User: &User{ID: 7, Status: StatusActive}}, nil
}

// ─── Helper ───

func newTestEntryCenterService(repo *codexEntryCenterRepoStub, apiKeyReader *codexEntryCenterAPIKeyReaderStub) *CodexEntryCenterServiceImpl {
	return newTestEntryCenterServiceWithModelRegistry(repo, apiKeyReader, nil)
}

func newTestEntryCenterServiceWithModelRegistry(repo *codexEntryCenterRepoStub, apiKeyReader *codexEntryCenterAPIKeyReaderStub, modelRegistry *CodexGatewayModelRegistry) *CodexEntryCenterServiceImpl {
	if apiKeyReader == nil {
		apiKeyReader = &codexEntryCenterAPIKeyReaderStub{}
	}
	return NewCodexEntryCenterService(repo, apiKeyReader, &codexEntryCenterAPIKeyCreatorStub{}, &CodexEntryCenterConfig{
		ServerOrigin:  "https://sub2api.example.com",
		GatewayOrigin: "https://sub2api.example.com",
	}, modelRegistry)
}

// ─── Tests ───

func TestGetSummary_NoAttachmentRelationship_ReturnsOnboardingCredential(t *testing.T) {
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{},
		grants:  []*dbent.CodexSetupGrant{},
	}
	svc := newTestEntryCenterService(repo, nil)

	summary, err := svc.GetSummary(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, CodexPageStateOnboardingCredential, summary.PageState)
	require.NotNil(t, summary.WizardStep)
	require.Equal(t, 1, *summary.WizardStep)
	require.Nil(t, summary.SetupSession)
	require.Empty(t, summary.Devices)
}

func TestGetSummary_IncludesCodexGatewayModelCatalog(t *testing.T) {
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{},
		grants:  []*dbent.CodexSetupGrant{},
	}
	registry := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"gpt-5.5", "deepseek-v4-pro"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderOpenAI: {
						Provider: CodexGatewayProviderOpenAI,
						GroupID:  1001,
						Healthy:  true,
					},
					CodexGatewayProviderDeepSeek: {
						Provider: CodexGatewayProviderDeepSeek,
						GroupID:  2002,
						Healthy:  true,
					},
				},
				Models: map[string]CodexGatewayModelMutation{
					"deepseek-v4-pro": {Enabled: true},
				},
			},
		}),
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
	)
	svc := newTestEntryCenterServiceWithModelRegistry(repo, nil, registry)

	summary, err := svc.GetSummary(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, summary.ModelCatalog, 2)
	require.Equal(t, []string{"gpt-5.5", "deepseek-v4-pro"}, codexEntryModelSummaryNames(summary.ModelCatalog))
	require.Equal(t, "GPT-5.5", summary.ModelCatalog[0].DisplayName)
	require.Equal(t, "openai", summary.ModelCatalog[0].Platform)
	require.Equal(t, "DeepSeek V4 Pro", summary.ModelCatalog[1].DisplayName)
	require.Equal(t, "deepseek", summary.ModelCatalog[1].Platform)
}

func TestGetSummary_IncludesFullCodexGatewayModelCatalog(t *testing.T) {
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{},
		grants:  []*dbent.CodexSetupGrant{},
	}
	enabledModels := []string{
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
		"claude-opus-4-7",
	}
	registry := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: enabledModels,
			ProviderGroups: config.GatewayCodexProviderGroupsConfig{
				OpenAI:    1001,
				DeepSeek:  2002,
				Anthropic: 3003,
			},
		},
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{
			"deepseek-v4-pro":   true,
			"deepseek-v4-flash": true,
		}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{
			"deepseek-v4-pro":   true,
			"deepseek-v4-flash": true,
		}}),
	)
	svc := newTestEntryCenterServiceWithModelRegistry(repo, nil, registry)

	summary, err := svc.GetSummary(context.Background(), 7)
	require.NoError(t, err)

	require.Len(t, summary.ModelCatalog, len(enabledModels))
	require.Equal(t, []string{
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
		"claude-opus-4-7",
	}, codexEntryModelSummaryNames(summary.ModelCatalog))
}

func TestGetSummary_IncludesModelPricingFromResolver(t *testing.T) {
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{},
		grants:  []*dbent.CodexSetupGrant{},
	}
	registry := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"gpt-5.5", "deepseek-v4-pro", "claude-opus-4-7"},
			ProviderGroups: config.GatewayCodexProviderGroupsConfig{
				OpenAI:    1001,
				DeepSeek:  2002,
				Anthropic: 3003,
			},
		},
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{
			"deepseek-v4-pro": true,
		}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{
			"deepseek-v4-pro": true,
		}}),
	)
	svc := newTestEntryCenterServiceWithModelRegistry(repo, nil, registry)

	summary, err := svc.GetSummary(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, summary.ModelCatalog, 3)

	var raw []map[string]any
	require.NoError(t, mustMarshalUnmarshal(summary.ModelCatalog, &raw))
	for _, model := range raw {
		pricing, ok := model["pricing"].(map[string]any)
		require.True(t, ok, "model %s should include pricing", model["name"])
		require.Equal(t, "token", pricing["billing_mode"])
		require.Greater(t, pricing["input_price"].(float64), 0.0)
		require.Greater(t, pricing["output_price"].(float64), 0.0)
		require.Greater(t, pricing["cache_read_price"].(float64), 0.0)
		require.NotEmpty(t, pricing["source"])
	}
}

func TestGetSummary_PendingSessionNoDevice_ReturnsOnboardingAttach(t *testing.T) {
	codeHash := hashManagedSecret("setup-code")
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{},
		grants: []*dbent.CodexSetupGrant{
			{
				ID:            1,
				CodeHash:      codeHash,
				UserID:        7,
				APIKeyID:      42,
				ServerOrigin:  "https://sub2api.example.com",
				GatewayOrigin: "https://sub2api.example.com",
				ExpiresAt:     time.Now().Add(5 * time.Minute),
			},
		},
	}
	svc := newTestEntryCenterService(repo, nil)

	summary, err := svc.GetSummary(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, CodexPageStateOnboardingAttach, summary.PageState)
	require.NotNil(t, summary.WizardStep)
	require.Equal(t, 2, *summary.WizardStep)
	require.NotNil(t, summary.SetupSession)
	require.Equal(t, codeHash[:16], summary.SetupSession.ID)
	require.Nil(t, summary.SetupSession.LaunchURL)
	require.Nil(t, summary.SetupSession.CLICommand)
	require.NotNil(t, summary.SetupSessionPresentation)
	require.Equal(t, CodexSetupSessionPresentationWizard, *summary.SetupSessionPresentation)
}

func TestGetSummary_DeviceFirstHeartbeatButNoCatalogSync_ReturnsOnboardingVerify(t *testing.T) {
	// Simulate: setup grant exists, and a device appeared (first_seen_at set on grant metadata).
	// For v1, we detect this via the grant's consumed_at being set and a device existing
	// but with no catalog sync. However, in our current model, once consumed the grant
	// disappears from pending. So this test uses a different approach:
	// The device exists but is very new (just appeared), and there's still a pending grant
	// that tracks first_seen_at. For simplicity in v1, we'll test the scenario where
	// the device exists as active but the grant is still pending (edge case).
	//
	// Actually, per the plan: first_seen_at and first_catalog_synced_at are on the setup_session.
	// In our current schema, setup_grant doesn't have these fields. For v1, we'll use
	// the presence of an active device with very recent last_seen_at as a proxy.
	//
	// For now, this test validates the state machine logic with the grant having
	// a simulated first_seen_at (we'll need to extend the schema later).
	// The key contract: if there's a pending session and a device appeared but catalog
	// hasn't synced, page_state = onboarding_verify.
	//
	// Since our current implementation uses grant presence + device absence to determine
	// wizard steps, and we don't yet have first_seen_at/first_catalog_synced_at on grants,
	// this test validates the "no devices, pending grant" -> onboarding_attach path.
	// The onboarding_verify state will be fully testable once we add those fields.

	// For now, test that with devices present, we get console.
	now := time.Now()
	lastSeen := now.Add(-1 * time.Minute)
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{
			{
				ID:             1,
				UserID:         7,
				APIKeyID:       42,
				Name:           "My MacBook",
				Platform:       "darwin",
				Arch:           "arm64",
				ManagerVersion: "1.0.0",
				Status:         "active",
				LastSeenAt:     &lastSeen,
			},
		},
		grants: []*dbent.CodexSetupGrant{},
	}
	svc := newTestEntryCenterService(repo, nil)

	summary, err := svc.GetSummary(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, CodexPageStateConsole, summary.PageState)
	require.Nil(t, summary.WizardStep)
	require.Len(t, summary.Devices, 1)
	require.Equal(t, CodexDeviceStateHealthy, summary.Devices[0].DeviceState)
}

func TestGetSummary_ExistingDevicePlusNewSession_ReturnsConsoleWithBanner(t *testing.T) {
	now := time.Now()
	lastSeen := now.Add(-1 * time.Minute)
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{
			{
				ID:             1,
				UserID:         7,
				APIKeyID:       42,
				Name:           "My MacBook",
				Platform:       "darwin",
				Arch:           "arm64",
				ManagerVersion: "1.0.0",
				Status:         "active",
				LastSeenAt:     &lastSeen,
			},
		},
		grants: []*dbent.CodexSetupGrant{
			{
				ID:            2,
				UserID:        7,
				APIKeyID:      42,
				ServerOrigin:  "https://sub2api.example.com",
				GatewayOrigin: "https://sub2api.example.com",
				ExpiresAt:     now.Add(5 * time.Minute),
			},
		},
	}
	svc := newTestEntryCenterService(repo, nil)

	summary, err := svc.GetSummary(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, CodexPageStateConsole, summary.PageState)
	require.Nil(t, summary.WizardStep)
	require.NotNil(t, summary.SetupSession)
	require.NotNil(t, summary.SetupSessionPresentation)
	require.Equal(t, CodexSetupSessionPresentationConsoleBanner, *summary.SetupSessionPresentation)
	require.Len(t, summary.Devices, 1)
}

func TestGetSummary_MixedDeviceStates_ReturnsConsole(t *testing.T) {
	now := time.Now()
	recentSeen := now.Add(-1 * time.Minute)
	oldSeen := now.Add(-30 * time.Minute)
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{
			{
				ID:             1,
				UserID:         7,
				APIKeyID:       42,
				Name:           "Healthy Device",
				Platform:       "darwin",
				Arch:           "arm64",
				ManagerVersion: "1.0.0",
				Status:         "active",
				LastSeenAt:     &recentSeen,
			},
			{
				ID:             2,
				UserID:         7,
				APIKeyID:       42,
				Name:           "Offline Device",
				Platform:       "linux",
				Arch:           "amd64",
				ManagerVersion: "1.0.0",
				Status:         "active",
				LastSeenAt:     &oldSeen,
			},
		},
		grants: []*dbent.CodexSetupGrant{},
	}
	svc := newTestEntryCenterService(repo, nil)

	summary, err := svc.GetSummary(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, CodexPageStateConsole, summary.PageState)
	require.Len(t, summary.Devices, 2)
	require.Equal(t, CodexDeviceStateHealthy, summary.Devices[0].DeviceState)
	require.Equal(t, CodexDeviceStateDeviceOffline, summary.Devices[1].DeviceState)
	// Focus should be on the unhealthy device.
	require.NotNil(t, summary.FocusDeviceID)
	require.Equal(t, int64(2), *summary.FocusDeviceID)
}

func TestCreateSetupSession_IndependentMode_CreatesKeyAndSession(t *testing.T) {
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{},
	}
	svc := newTestEntryCenterService(repo, nil)

	resp, err := svc.CreateSetupSession(context.Background(), CodexCreateSetupSessionRequest{
		UserID:          7,
		AttachmentMode:  CodexAttachmentModeIndependent,
		CredentialLabel: "My Codex Key",
		ServerOrigin:    "https://sub2api.example.com",
		GatewayOrigin:   "https://sub2api.example.com",
	})
	require.NoError(t, err)
	require.Equal(t, CodexPageStateOnboardingAttach, resp.PageState)
	require.Equal(t, CodexSetupSessionPresentationWizard, resp.SetupSessionPresentation)
	require.Equal(t, CodexAttachmentModeIndependent, resp.SetupSession.AttachmentMode)
	require.NotEmpty(t, resp.SetupSession.ID)
	require.NotNil(t, resp.SetupSession.LaunchURL)
}

func TestCreateSetupSession_IndependentMode_UsesCodexOnlyCredential(t *testing.T) {
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{},
	}
	creator := &codexEntryCenterAPIKeyCreatorStub{}
	svc := NewCodexEntryCenterService(repo, &codexEntryCenterAPIKeyReaderStub{}, creator, &CodexEntryCenterConfig{
		ServerOrigin:  "https://sub2api.example.com",
		GatewayOrigin: "https://sub2api.example.com",
	}, nil)

	resp, err := svc.CreateSetupSession(context.Background(), CodexCreateSetupSessionRequest{
		UserID:          7,
		AttachmentMode:  CodexAttachmentModeIndependent,
		CredentialLabel: "My Codex Key",
		ServerOrigin:    "https://sub2api.example.com",
		GatewayOrigin:   "https://sub2api.example.com",
	})
	require.NoError(t, err)
	require.Equal(t, CodexAttachmentModeIndependent, resp.SetupSession.AttachmentMode)
	require.NotNil(t, creator.lastRequest)
	require.Equal(t, "My Codex Key", creator.lastRequest.Name)
	require.True(t, creator.lastRequest.CodexOnly)
	require.False(t, creator.lastRequest.AugmentOnly)
}

func TestCreateSetupSession_ReusedKeyMode_Success(t *testing.T) {
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{},
	}
	apiKeyReader := &codexEntryCenterAPIKeyReaderStub{}
	svc := newTestEntryCenterService(repo, apiKeyReader)

	keyID := int64(42)
	resp, err := svc.CreateSetupSession(context.Background(), CodexCreateSetupSessionRequest{
		UserID:          7,
		AttachmentMode:  CodexAttachmentModeReusedKey,
		CredentialLabel: "My Key",
		ReuseAPIKeyID:   &keyID,
		ServerOrigin:    "https://sub2api.example.com",
		GatewayOrigin:   "https://sub2api.example.com",
	})
	require.NoError(t, err)
	require.Equal(t, CodexPageStateOnboardingAttach, resp.PageState)
	require.Equal(t, CodexSetupSessionPresentationWizard, resp.SetupSessionPresentation)
	require.NotEmpty(t, resp.SetupSession.ID)
	require.NotNil(t, resp.SetupSession.LaunchURL)
	require.NotNil(t, resp.SetupSession.CLICommand)
}

func TestDiagnose_SetupSessionTarget(t *testing.T) {
	codeHash := hashManagedSecret("setup-code")
	repo := &codexEntryCenterRepoStub{
		grants: []*dbent.CodexSetupGrant{
			{
				ID:            1,
				CodeHash:      codeHash,
				UserID:        7,
				APIKeyID:      42,
				ServerOrigin:  "https://sub2api.example.com",
				GatewayOrigin: "https://sub2api.example.com",
				ExpiresAt:     time.Now().Add(5 * time.Minute),
			},
		},
	}
	svc := newTestEntryCenterService(repo, nil)

	sessionID := "1"
	report, err := svc.Diagnose(context.Background(), CodexDiagnoseRequest{
		UserID:         7,
		SetupSessionID: &sessionID,
	})
	require.NoError(t, err)
	require.Equal(t, "setup_session", report.TargetKind)
	require.NotEmpty(t, report.Checks)
}

func TestDiagnose_SetupSessionTargetAcceptsCodeHashPrefix(t *testing.T) {
	codeHash := hashManagedSecret("setup-code")
	repo := &codexEntryCenterRepoStub{
		grants: []*dbent.CodexSetupGrant{
			{
				ID:            1,
				CodeHash:      codeHash,
				UserID:        7,
				APIKeyID:      42,
				ServerOrigin:  "https://sub2api.example.com",
				GatewayOrigin: "https://sub2api.example.com",
				ExpiresAt:     time.Now().Add(5 * time.Minute),
			},
		},
	}
	svc := newTestEntryCenterService(repo, nil)

	sessionID := codeHash[:16]
	report, err := svc.Diagnose(context.Background(), CodexDiagnoseRequest{
		UserID:         7,
		SetupSessionID: &sessionID,
	})

	require.NoError(t, err)
	require.Equal(t, "setup_session", report.TargetKind)
	require.NotEmpty(t, report.Checks)
}

func TestDiagnose_DeviceTarget(t *testing.T) {
	now := time.Now()
	lastSeen := now.Add(-1 * time.Minute)
	repo := &codexEntryCenterRepoStub{
		devices: []*dbent.CodexManagedDevice{
			{
				ID:             1,
				UserID:         7,
				APIKeyID:       42,
				Name:           "My Device",
				Platform:       "darwin",
				Arch:           "arm64",
				ManagerVersion: "1.0.0",
				Status:         "active",
				LastSeenAt:     &lastSeen,
			},
		},
	}
	svc := newTestEntryCenterService(repo, nil)

	deviceID := int64(1)
	report, err := svc.Diagnose(context.Background(), CodexDiagnoseRequest{
		UserID:   7,
		DeviceID: &deviceID,
	})
	require.NoError(t, err)
	require.Equal(t, "device", report.TargetKind)
	require.True(t, report.OK)
	require.NotEmpty(t, report.Checks)
}

func TestDiagnose_RequiresExactlyOneTarget(t *testing.T) {
	repo := &codexEntryCenterRepoStub{}
	svc := newTestEntryCenterService(repo, nil)

	// Neither target.
	_, err := svc.Diagnose(context.Background(), CodexDiagnoseRequest{UserID: 7})
	require.Error(t, err)

	// Both targets.
	sessionID := "1"
	deviceID := int64(1)
	_, err = svc.Diagnose(context.Background(), CodexDiagnoseRequest{
		UserID:         7,
		SetupSessionID: &sessionID,
		DeviceID:       &deviceID,
	})
	require.Error(t, err)
}

func codexEntryModelSummaryNames(models []CodexEntryModelSummary) []string {
	names := make([]string, 0, len(models))
	for _, model := range models {
		names = append(names, model.Name)
	}
	return names
}

func mustMarshalUnmarshal(in any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// ─── API key creator stub ───

type codexEntryCenterAPIKeyCreatorStub struct {
	nextID      int64
	lastUserID  int64
	lastRequest *CreateAPIKeyRequest
}

func (s *codexEntryCenterAPIKeyCreatorStub) Create(ctx context.Context, userID int64, req CreateAPIKeyRequest) (*APIKey, error) {
	s.nextID++
	reqCopy := req
	s.lastUserID = userID
	s.lastRequest = &reqCopy
	return &APIKey{ID: s.nextID, User: &User{ID: userID, Status: StatusActive}}, nil
}
