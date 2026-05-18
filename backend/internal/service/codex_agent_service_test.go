package service

import (
	"context"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/codexdeviceauditlog"
	"github.com/Wei-Shaw/sub2api/ent/codexdevicetoken"
	"github.com/Wei-Shaw/sub2api/ent/codexmanageddevice"
	"github.com/Wei-Shaw/sub2api/ent/codexsetupgrant"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexAgentSchemaConstants(t *testing.T) {
	require.Equal(t, "codex_setup_grants", codexsetupgrant.Table)
	require.Equal(t, "code_hash", codexsetupgrant.FieldCodeHash)
	require.Equal(t, "consumed_at", codexsetupgrant.FieldConsumedAt)
	require.Equal(t, "user", codexsetupgrant.EdgeUser)
	require.Equal(t, "api_key", codexsetupgrant.EdgeAPIKey)
	require.Equal(t, "user_id", codexsetupgrant.UserColumn)
	require.Equal(t, "api_key_id", codexsetupgrant.APIKeyColumn)

	require.Equal(t, "codex_managed_devices", codexmanageddevice.Table)
	require.Equal(t, "manager_version", codexmanageddevice.FieldManagerVersion)
	require.Equal(t, "last_seen_at", codexmanageddevice.FieldLastSeenAt)
	require.Equal(t, codexmanageddevice.StatusActive, codexmanageddevice.DefaultStatus)
	require.Equal(t, codexmanageddevice.Status("active"), codexmanageddevice.StatusActive)
	require.Equal(t, codexmanageddevice.Status("revoked"), codexmanageddevice.StatusRevoked)
	require.Equal(t, codexmanageddevice.Status("reauthorization_required"), codexmanageddevice.StatusReauthorizationRequired)
	require.Equal(t, "user", codexmanageddevice.EdgeUser)
	require.Equal(t, "api_key", codexmanageddevice.EdgeAPIKey)
	require.Equal(t, "user_id", codexmanageddevice.UserColumn)
	require.Equal(t, "api_key_id", codexmanageddevice.APIKeyColumn)

	require.Equal(t, "codex_device_tokens", codexdevicetoken.Table)
	require.Equal(t, "refresh_token_hash", codexdevicetoken.FieldRefreshTokenHash)
	require.Equal(t, "rotated_at", codexdevicetoken.FieldRotatedAt)
	require.Equal(t, "device", codexdevicetoken.EdgeDevice)
	require.Equal(t, "device_id", codexdevicetoken.DeviceColumn)

	require.Equal(t, "codex_device_audit_logs", codexdeviceauditlog.Table)
	require.Equal(t, "user_agent", codexdeviceauditlog.FieldUserAgent)
	require.Equal(t, "metadata", codexdeviceauditlog.FieldMetadata)
	require.Equal(t, "device", codexdeviceauditlog.EdgeDevice)
	require.Equal(t, "user", codexdeviceauditlog.EdgeUser)
	require.Equal(t, "device_id", codexdeviceauditlog.DeviceColumn)
	require.Equal(t, "user_id", codexdeviceauditlog.UserColumn)
}

func TestCodexAgentServiceCreateSetupGrantDoesNotLeakAPIKey(t *testing.T) {
	var captured CreateCodexSetupGrantParams
	svc := newTestCodexAgentService(
		&codexAgentRepositoryStub{
			createSetupGrant: func(ctx context.Context, params CreateCodexSetupGrantParams) (*dbent.CodexSetupGrant, error) {
				captured = params
				return &dbent.CodexSetupGrant{ID: 1, CodeHash: params.CodeHash}, nil
			},
		},
		&codexAPIKeyReaderStub{
			verifyOwnership: func(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error) {
				return apiKeyIDs, nil
			},
		},
	)

	resp, err := svc.CreateSetupGrant(context.Background(), CreateCodexSetupGrantRequest{
		UserID:       7,
		APIKeyID:     42,
		ServerOrigin: "https://sub2api.example.com",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Code)
	require.Contains(t, resp.DeepLink, "zhumeng-agent://setup")
	require.NotContains(t, resp.DeepLink, "sk-")
	require.NotContains(t, resp.DeepLink, "api_key")
	require.NotEqual(t, resp.Code, captured.CodeHash)
	require.Equal(t, hashManagedSecret(resp.Code), captured.CodeHash)
}

func TestCodexAgentServiceExchangeSetupGrantRejectsNonHTTPSOrigin(t *testing.T) {
	svc := newTestCodexAgentService(&codexAgentRepositoryStub{}, &codexAPIKeyReaderStub{})

	_, err := svc.ExchangeSetupGrant(context.Background(), ExchangeCodexSetupGrantRequest{
		Code:         "abc",
		ServerOrigin: "http://sub2api.example.com",
	})
	require.ErrorIs(t, err, ErrCodexSetupGrantOriginInvalid)
}

func TestCodexAgentServiceCreateSetupGrantAllowsLocalLoopbackOrigin(t *testing.T) {
	var captured CreateCodexSetupGrantParams
	svc := newTestCodexAgentService(
		&codexAgentRepositoryStub{
			createSetupGrant: func(ctx context.Context, params CreateCodexSetupGrantParams) (*dbent.CodexSetupGrant, error) {
				captured = params
				return &dbent.CodexSetupGrant{ID: 1, CodeHash: params.CodeHash}, nil
			},
		},
		&codexAPIKeyReaderStub{
			verifyOwnership: func(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error) {
				return apiKeyIDs, nil
			},
		},
	)

	resp, err := svc.CreateSetupGrant(context.Background(), CreateCodexSetupGrantRequest{
		UserID:       7,
		APIKeyID:     42,
		ServerOrigin: "http://127.0.0.1:3000",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Code)
	require.Contains(t, resp.DeepLink, "server=http%3A%2F%2F127.0.0.1%3A3000")
	require.Equal(t, "http://127.0.0.1:3000", captured.ServerOrigin)
}

func TestCodexAgentServiceExchangeSetupGrantAllowsLocalLoopbackOrigin(t *testing.T) {
	svc := newTestCodexAgentService(
		&codexAgentRepositoryStub{
			consumeSetupGrant: func(ctx context.Context, codeHash string, now time.Time) (*dbent.CodexSetupGrant, error) {
				return &dbent.CodexSetupGrant{
					ID:            1,
					UserID:        7,
					APIKeyID:      42,
					ServerOrigin:  "http://localhost:3000",
					GatewayOrigin: "http://localhost:3000",
				}, nil
			},
			createManagedDevice: func(ctx context.Context, params CreateCodexManagedDeviceParams) (*dbent.CodexManagedDevice, error) {
				return &dbent.CodexManagedDevice{
					ID:       99,
					UserID:   params.UserID,
					APIKeyID: params.APIKeyID,
					Status:   codexmanageddevice.StatusActive,
				}, nil
			},
			createDeviceToken: func(ctx context.Context, params CreateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
				return &dbent.CodexDeviceToken{ID: 5, DeviceID: params.DeviceID}, nil
			},
		},
		&codexAPIKeyReaderStub{
			getByID: func(ctx context.Context, id int64) (*APIKey, error) {
				return &APIKey{
					ID:     id,
					Status: StatusActive,
					User:   &User{ID: 7, Status: StatusActive},
				}, nil
			},
		},
	)

	resp, err := svc.ExchangeSetupGrant(context.Background(), ExchangeCodexSetupGrantRequest{
		Code:         "abc",
		ServerOrigin: "http://localhost:3000",
	})
	require.NoError(t, err)
	require.Equal(t, "http://localhost:3000", resp.ServerBaseURL)
	require.Equal(t, "http://localhost:3000", resp.GatewayBaseURL)
}

func TestCodexAgentServiceCreateSetupGrantRejectsMalformedOrigin(t *testing.T) {
	svc := newTestCodexAgentService(&codexAgentRepositoryStub{}, &codexAPIKeyReaderStub{})

	_, err := svc.CreateSetupGrant(context.Background(), CreateCodexSetupGrantRequest{
		UserID:       7,
		APIKeyID:     42,
		ServerOrigin: "https://example.com/path",
	})
	require.ErrorIs(t, err, ErrCodexSetupGrantOriginInvalid)

	_, err = svc.CreateSetupGrant(context.Background(), CreateCodexSetupGrantRequest{
		UserID:       7,
		APIKeyID:     42,
		ServerOrigin: "https://user:pass@example.com",
	})
	require.ErrorIs(t, err, ErrCodexSetupGrantOriginInvalid)
}

func TestCodexAgentServiceExchangeSetupGrantRejectsOriginMismatch(t *testing.T) {
	deviceCreated := false
	tokenCreated := false
	svc := newTestCodexAgentService(
		&codexAgentRepositoryStub{
			consumeSetupGrant: func(ctx context.Context, codeHash string, now time.Time) (*dbent.CodexSetupGrant, error) {
				return &dbent.CodexSetupGrant{
					ID:            1,
					UserID:        7,
					APIKeyID:      42,
					ServerOrigin:  "https://expected.example.com",
					GatewayOrigin: "https://gateway.example.com",
				}, nil
			},
			createManagedDevice: func(ctx context.Context, params CreateCodexManagedDeviceParams) (*dbent.CodexManagedDevice, error) {
				deviceCreated = true
				return nil, nil
			},
			createDeviceToken: func(ctx context.Context, params CreateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
				tokenCreated = true
				return nil, nil
			},
		},
		&codexAPIKeyReaderStub{},
	)

	_, err := svc.ExchangeSetupGrant(context.Background(), ExchangeCodexSetupGrantRequest{
		Code:         "abc",
		ServerOrigin: "https://other.example.com",
	})
	require.ErrorIs(t, err, ErrCodexSetupGrantOriginMismatch)
	require.False(t, deviceCreated)
	require.False(t, tokenCreated)
}

func TestCodexAgentServiceExchangeSetupGrantReturnsConfigProfileAndCredentials(t *testing.T) {
	var createdToken CreateCodexDeviceTokenParams
	svc := newTestCodexAgentService(
		&codexAgentRepositoryStub{
			consumeSetupGrant: func(ctx context.Context, codeHash string, now time.Time) (*dbent.CodexSetupGrant, error) {
				return &dbent.CodexSetupGrant{
					ID:            1,
					UserID:        7,
					APIKeyID:      42,
					ServerOrigin:  "https://sub2api.example.com",
					GatewayOrigin: "https://gateway.example.com",
				}, nil
			},
			createManagedDevice: func(ctx context.Context, params CreateCodexManagedDeviceParams) (*dbent.CodexManagedDevice, error) {
				return &dbent.CodexManagedDevice{
					ID:       99,
					UserID:   params.UserID,
					APIKeyID: params.APIKeyID,
					Status:   codexmanageddevice.StatusActive,
				}, nil
			},
			createDeviceToken: func(ctx context.Context, params CreateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
				createdToken = params
				return &dbent.CodexDeviceToken{ID: 5, DeviceID: params.DeviceID}, nil
			},
		},
		&codexAPIKeyReaderStub{
			getByID: func(ctx context.Context, id int64) (*APIKey, error) {
				return &APIKey{
					ID:     id,
					Status: StatusActive,
					User:   &User{ID: 7, Status: StatusActive},
				}, nil
			},
		},
	)

	resp, err := svc.ExchangeSetupGrant(context.Background(), ExchangeCodexSetupGrantRequest{
		Code:           "grant-code",
		ServerOrigin:   "https://sub2api.example.com",
		DeviceName:     "MacBook Pro",
		Platform:       "darwin",
		Arch:           "arm64",
		ManagerVersion: "1.0.0",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.AccessToken)
	require.NotEmpty(t, resp.RefreshToken)
	require.NotEmpty(t, resp.ManagedSessionID)
	require.Equal(t, int64(99), resp.DeviceID)
	require.Equal(t, "https://sub2api.example.com", resp.ServerBaseURL)
	require.Equal(t, "https://gateway.example.com", resp.GatewayBaseURL)
	require.Equal(t, "zhumeng-codex", resp.ConfigProfile.ModelProvider)
	require.Equal(t, "responses", resp.ConfigProfile.WireAPI)
	require.True(t, resp.ConfigProfile.RequiresOpenAIAuth)
	require.False(t, resp.ConfigProfile.SupportsWebsockets)
	require.Equal(t, hashManagedSecret(resp.RefreshToken), createdToken.RefreshTokenHash)

	claims, err := svc.parseManagedAccessToken(resp.AccessToken)
	require.NoError(t, err)
	require.Equal(t, int64(99), claims.DeviceID)
	require.Equal(t, int64(42), claims.APIKeyID)
	require.Equal(t, resp.ManagedSessionID, claims.ManagedSessionID)
}

func TestCodexAgentServiceRefreshDeviceTokenRotatesRefreshToken(t *testing.T) {
	var rotated RotateCodexDeviceTokenParams
	rawRefreshToken := "refresh-token-1"
	svc := newTestCodexAgentService(
		&codexAgentRepositoryStub{
			findActiveTokenByHash: func(ctx context.Context, refreshTokenHash string, now time.Time) (*dbent.CodexDeviceToken, error) {
				require.Equal(t, hashManagedSecret(rawRefreshToken), refreshTokenHash)
				return &dbent.CodexDeviceToken{
					ID:       1,
					DeviceID: 9,
					Edges: dbent.CodexDeviceTokenEdges{
						Device: &dbent.CodexManagedDevice{
							ID:       9,
							APIKeyID: 42,
							Status:   codexmanageddevice.StatusActive,
						},
					},
				}, nil
			},
			rotateDeviceToken: func(ctx context.Context, params RotateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
				rotated = params
				return &dbent.CodexDeviceToken{ID: 2, DeviceID: 9}, nil
			},
		},
		&codexAPIKeyReaderStub{
			getByID: func(ctx context.Context, id int64) (*APIKey, error) {
				return &APIKey{
					ID:     id,
					Status: StatusActive,
					User:   &User{ID: 7, Status: StatusActive},
				}, nil
			},
		},
	)

	resp, err := svc.RefreshDeviceToken(context.Background(), RefreshCodexDeviceTokenRequest{
		DeviceID:     9,
		RefreshToken: rawRefreshToken,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.AccessToken)
	require.NotEmpty(t, resp.RefreshToken)
	require.NotEmpty(t, resp.ManagedSessionID)
	require.Equal(t, hashManagedSecret(rawRefreshToken), rotated.CurrentRefreshTokenHash)
	require.Equal(t, hashManagedSecret(resp.RefreshToken), rotated.NewRefreshTokenHash)
}

func TestCodexAgentServiceConcurrentRefreshAllowsExactlyOneSuccess(t *testing.T) {
	rawRefreshToken := "refresh-token-1"
	var (
		mu          sync.Mutex
		rotateCalls int
	)
	svc := newTestCodexAgentService(
		&codexAgentRepositoryStub{
			findActiveTokenByHash: func(ctx context.Context, refreshTokenHash string, now time.Time) (*dbent.CodexDeviceToken, error) {
				return &dbent.CodexDeviceToken{
					ID:       1,
					DeviceID: 9,
					Edges: dbent.CodexDeviceTokenEdges{
						Device: &dbent.CodexManagedDevice{
							ID:       9,
							APIKeyID: 42,
							Status:   codexmanageddevice.StatusActive,
						},
					},
				}, nil
			},
			rotateDeviceToken: func(ctx context.Context, params RotateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
				mu.Lock()
				defer mu.Unlock()
				rotateCalls++
				if rotateCalls == 1 {
					return &dbent.CodexDeviceToken{ID: 2, DeviceID: 9}, nil
				}
				return nil, ErrCodexManagedRefreshTokenInvalid
			},
		},
		&codexAPIKeyReaderStub{
			getByID: func(ctx context.Context, id int64) (*APIKey, error) {
				return &APIKey{
					ID:     id,
					Status: StatusActive,
					User:   &User{ID: 7, Status: StatusActive},
				}, nil
			},
		},
	)

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = svc.RefreshDeviceToken(context.Background(), RefreshCodexDeviceTokenRequest{
				DeviceID:     9,
				RefreshToken: rawRefreshToken,
			})
		}(i)
	}
	wg.Wait()

	successes := 0
	failures := 0
	for _, err := range errs {
		if err == nil {
			successes++
			continue
		}
		require.ErrorIs(t, err, ErrCodexManagedRefreshTokenInvalid)
		failures++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, failures)
}

func TestCodexAgentServiceRevokeDeviceCausesSubsequentRefreshFail(t *testing.T) {
	repo := &codexAgentRepositoryStub{}
	device := &dbent.CodexManagedDevice{ID: 9, UserID: 7, APIKeyID: 42, Status: codexmanageddevice.StatusActive}
	repo.getManagedDevice = func(ctx context.Context, id int64) (*dbent.CodexManagedDevice, error) {
		return device, nil
	}
	repo.revokeManagedDevice = func(ctx context.Context, id int64, revokedAt time.Time) error {
		device.Status = codexmanageddevice.StatusRevoked
		device.RevokedAt = &revokedAt
		return nil
	}
	repo.findActiveTokenByHash = func(ctx context.Context, refreshTokenHash string, now time.Time) (*dbent.CodexDeviceToken, error) {
		if device.Status != codexmanageddevice.StatusActive {
			return nil, ErrCodexManagedRefreshTokenInvalid
		}
		return &dbent.CodexDeviceToken{
			ID:       1,
			DeviceID: 9,
			Edges: dbent.CodexDeviceTokenEdges{
				Device: device,
			},
		}, nil
	}

	svc := newTestCodexAgentService(
		repo,
		&codexAPIKeyReaderStub{
			verifyOwnership: func(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error) {
				return apiKeyIDs, nil
			},
			getByID: func(ctx context.Context, id int64) (*APIKey, error) {
				return &APIKey{
					ID:     id,
					Status: StatusActive,
					User:   &User{ID: 7, Status: StatusActive},
				}, nil
			},
		},
	)

	require.NoError(t, svc.RevokeDevice(context.Background(), 7, 9))

	_, err := svc.RefreshDeviceToken(context.Background(), RefreshCodexDeviceTokenRequest{
		DeviceID:     9,
		RefreshToken: "refresh-token-1",
	})
	require.ErrorIs(t, err, ErrCodexManagedRefreshTokenInvalid)
}

func TestCodexAgentServiceValidateManagedDeviceAccessRequiresSessionMatch(t *testing.T) {
	svc := newTestCodexAgentService(
		&codexAgentRepositoryStub{
			getManagedDevice: func(ctx context.Context, id int64) (*dbent.CodexManagedDevice, error) {
				return &dbent.CodexManagedDevice{ID: id, APIKeyID: 42, Status: codexmanageddevice.StatusActive}, nil
			},
		},
		&codexAPIKeyReaderStub{
			getByID: func(ctx context.Context, id int64) (*APIKey, error) {
				return &APIKey{
					ID:     id,
					Status: StatusActive,
					User:   &User{ID: 7, Status: StatusActive},
				}, nil
			},
		},
	)

	accessToken, _, err := svc.signManagedAccessToken(9, 42, "session-a", &User{ID: 7, Status: StatusActive})
	require.NoError(t, err)

	_, err = svc.ValidateManagedDeviceAccess(context.Background(), ValidateManagedDeviceAccessRequest{
		AccessToken:      "Bearer " + accessToken,
		DeviceID:         9,
		ManagedSessionID: "session-b",
	})
	require.ErrorIs(t, err, ErrCodexManagedSessionMismatch)
}

func TestCodexAgentServiceValidateManagedDeviceAccessRequiresBearerToken(t *testing.T) {
	svc := newTestCodexAgentService(
		&codexAgentRepositoryStub{},
		&codexAPIKeyReaderStub{},
	)

	accessToken, _, err := svc.signManagedAccessToken(9, 42, "session-a", &User{ID: 7, Status: StatusActive})
	require.NoError(t, err)

	_, err = svc.ValidateManagedDeviceAccess(context.Background(), ValidateManagedDeviceAccessRequest{
		AccessToken:      accessToken,
		DeviceID:         9,
		ManagedSessionID: "session-a",
	})
	require.ErrorIs(t, err, ErrCodexManagedAccessInvalid)
}

func TestCodexAgentServiceValidateManagedDeviceAccessRejectsRevokedDevice(t *testing.T) {
	now := time.Now()
	svc := newTestCodexAgentService(
		&codexAgentRepositoryStub{
			getManagedDevice: func(ctx context.Context, id int64) (*dbent.CodexManagedDevice, error) {
				return &dbent.CodexManagedDevice{
					ID:        id,
					APIKeyID:  42,
					Status:    codexmanageddevice.StatusRevoked,
					RevokedAt: &now,
				}, nil
			},
		},
		&codexAPIKeyReaderStub{
			getByID: func(ctx context.Context, id int64) (*APIKey, error) {
				return &APIKey{
					ID:     id,
					Status: StatusActive,
					User:   &User{ID: 7, Status: StatusActive},
				}, nil
			},
		},
	)

	accessToken, _, err := svc.signManagedAccessToken(9, 42, "session-a", &User{ID: 7, Status: StatusActive})
	require.NoError(t, err)

	_, err = svc.ValidateManagedDeviceAccess(context.Background(), ValidateManagedDeviceAccessRequest{
		AccessToken:      "Bearer " + accessToken,
		DeviceID:         9,
		ManagedSessionID: "session-a",
	})
	require.ErrorIs(t, err, ErrCodexManagedDeviceRevoked)
}

type codexAgentRepositoryStub struct {
	createSetupGrant      func(ctx context.Context, params CreateCodexSetupGrantParams) (*dbent.CodexSetupGrant, error)
	consumeSetupGrant     func(ctx context.Context, codeHash string, now time.Time) (*dbent.CodexSetupGrant, error)
	createManagedDevice   func(ctx context.Context, params CreateCodexManagedDeviceParams) (*dbent.CodexManagedDevice, error)
	getManagedDevice      func(ctx context.Context, id int64) (*dbent.CodexManagedDevice, error)
	listManagedDevices    func(ctx context.Context, userID int64) ([]*dbent.CodexManagedDevice, error)
	revokeManagedDevice   func(ctx context.Context, id int64, revokedAt time.Time) error
	createDeviceToken     func(ctx context.Context, params CreateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error)
	rotateDeviceToken     func(ctx context.Context, params RotateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error)
	findActiveTokenByHash func(ctx context.Context, refreshTokenHash string, now time.Time) (*dbent.CodexDeviceToken, error)
	insertAuditLog        func(ctx context.Context, params InsertCodexDeviceAuditLogParams) (*dbent.CodexDeviceAuditLog, error)
}

func (s *codexAgentRepositoryStub) CreateSetupGrant(ctx context.Context, params CreateCodexSetupGrantParams) (*dbent.CodexSetupGrant, error) {
	if s.createSetupGrant != nil {
		return s.createSetupGrant(ctx, params)
	}
	return nil, nil
}

func (s *codexAgentRepositoryStub) ConsumeSetupGrant(ctx context.Context, codeHash string, now time.Time) (*dbent.CodexSetupGrant, error) {
	if s.consumeSetupGrant != nil {
		return s.consumeSetupGrant(ctx, codeHash, now)
	}
	return nil, ErrCodexSetupGrantNotActive
}

func (s *codexAgentRepositoryStub) CreateManagedDevice(ctx context.Context, params CreateCodexManagedDeviceParams) (*dbent.CodexManagedDevice, error) {
	if s.createManagedDevice != nil {
		return s.createManagedDevice(ctx, params)
	}
	return nil, nil
}

func (s *codexAgentRepositoryStub) GetManagedDevice(ctx context.Context, id int64) (*dbent.CodexManagedDevice, error) {
	if s.getManagedDevice != nil {
		return s.getManagedDevice(ctx, id)
	}
	return nil, ErrCodexManagedDeviceNotFound
}

func (s *codexAgentRepositoryStub) ListManagedDevicesByUser(ctx context.Context, userID int64) ([]*dbent.CodexManagedDevice, error) {
	if s.listManagedDevices != nil {
		return s.listManagedDevices(ctx, userID)
	}
	return nil, nil
}

func (s *codexAgentRepositoryStub) RevokeManagedDevice(ctx context.Context, id int64, revokedAt time.Time) error {
	if s.revokeManagedDevice != nil {
		return s.revokeManagedDevice(ctx, id, revokedAt)
	}
	return nil
}

func (s *codexAgentRepositoryStub) CreateDeviceToken(ctx context.Context, params CreateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
	if s.createDeviceToken != nil {
		return s.createDeviceToken(ctx, params)
	}
	return nil, nil
}

func (s *codexAgentRepositoryStub) RotateDeviceToken(ctx context.Context, params RotateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
	if s.rotateDeviceToken != nil {
		return s.rotateDeviceToken(ctx, params)
	}
	return nil, ErrCodexManagedRefreshTokenInvalid
}

func (s *codexAgentRepositoryStub) FindActiveTokenByHash(ctx context.Context, refreshTokenHash string, now time.Time) (*dbent.CodexDeviceToken, error) {
	if s.findActiveTokenByHash != nil {
		return s.findActiveTokenByHash(ctx, refreshTokenHash, now)
	}
	return nil, ErrCodexManagedRefreshTokenInvalid
}

func (s *codexAgentRepositoryStub) InsertAuditLog(ctx context.Context, params InsertCodexDeviceAuditLogParams) (*dbent.CodexDeviceAuditLog, error) {
	if s.insertAuditLog != nil {
		return s.insertAuditLog(ctx, params)
	}
	return nil, nil
}

type codexAPIKeyReaderStub struct {
	verifyOwnership func(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error)
	getByID         func(ctx context.Context, id int64) (*APIKey, error)
}

func (s *codexAPIKeyReaderStub) VerifyOwnership(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error) {
	if s.verifyOwnership != nil {
		return s.verifyOwnership(ctx, userID, apiKeyIDs)
	}
	return nil, nil
}

func (s *codexAPIKeyReaderStub) GetByID(ctx context.Context, id int64) (*APIKey, error) {
	if s.getByID != nil {
		return s.getByID(ctx, id)
	}
	return nil, nil
}

func newTestCodexAgentService(repo CodexAgentRepository, apiKeyReader codexManagedAPIKeyReader) *CodexAgentService {
	return NewCodexAgentService(nil, repo, apiKeyReader, &config.Config{
		JWT: config.JWTConfig{
			Secret:                   "12345678901234567890123456789012",
			AccessTokenExpireMinutes: 15,
			RefreshTokenExpireDays:   30,
		},
	})
}
