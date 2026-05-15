//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/codexdevicetoken"
	"github.com/Wei-Shaw/sub2api/ent/codexmanageddevice"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexAgentRepository(t *testing.T) {
	t.Run("setup grant can be created and consumed once", func(t *testing.T) {
		ctx := context.Background()
		repo, client := newTestCodexAgentRepository(t)
		user, apiKey := mustCreateCodexUserAndKey(t, client)

		grant, err := repo.CreateSetupGrant(ctx, CreateCodexSetupGrantParams{
			CodeHash:      uniqueCodexValue("grant"),
			UserID:        user.ID,
			APIKeyID:      apiKey.ID,
			Mode:          "desktop",
			ServerOrigin:  "https://server.example.com",
			GatewayOrigin: "https://gateway.example.com",
			ExpiresAt:     time.Now().Add(30 * time.Minute),
		})
		require.NoError(t, err)
		require.NotZero(t, grant.ID)
		require.Nil(t, grant.ConsumedAt)

		consumed, err := repo.ConsumeSetupGrant(ctx, grant.CodeHash, time.Now())
		require.NoError(t, err)
		require.Equal(t, grant.ID, consumed.ID)
		require.NotNil(t, consumed.ConsumedAt)

		_, err = repo.ConsumeSetupGrant(ctx, grant.CodeHash, time.Now().Add(time.Second))
		require.ErrorIs(t, err, ErrCodexSetupGrantNotActive)
	})

	t.Run("concurrent setup grant consume allows exactly one success", func(t *testing.T) {
		ctx := context.Background()
		repo, client := newTestCodexAgentRepository(t)
		user, apiKey := mustCreateCodexUserAndKey(t, client)

		grant, err := repo.CreateSetupGrant(ctx, CreateCodexSetupGrantParams{
			CodeHash:      uniqueCodexValue("grant-concurrent"),
			UserID:        user.ID,
			APIKeyID:      apiKey.ID,
			Mode:          "desktop",
			ServerOrigin:  "https://server.example.com",
			GatewayOrigin: "https://gateway.example.com",
			ExpiresAt:     time.Now().Add(30 * time.Minute),
		})
		require.NoError(t, err)

		now := time.Now()
		errs := runConcurrent(2, func() error {
			_, consumeErr := repo.ConsumeSetupGrant(ctx, grant.CodeHash, now)
			return consumeErr
		})

		require.Equal(t, 1, countNilErrors(errs))
		require.Equal(t, 1, countErrorIs(errs, ErrCodexSetupGrantNotActive))
	})

	t.Run("expired grants are rejected", func(t *testing.T) {
		ctx := context.Background()
		repo, client := newTestCodexAgentRepository(t)
		user, apiKey := mustCreateCodexUserAndKey(t, client)

		grant, err := repo.CreateSetupGrant(ctx, CreateCodexSetupGrantParams{
			CodeHash:      uniqueCodexValue("grant-expired"),
			UserID:        user.ID,
			APIKeyID:      apiKey.ID,
			Mode:          "desktop",
			ServerOrigin:  "https://server.example.com",
			GatewayOrigin: "https://gateway.example.com",
			ExpiresAt:     time.Now().Add(-1 * time.Minute),
		})
		require.NoError(t, err)

		_, err = repo.ConsumeSetupGrant(ctx, grant.CodeHash, time.Now())
		require.ErrorIs(t, err, ErrCodexSetupGrantNotActive)
	})

	t.Run("device can be created listed revoked and audited", func(t *testing.T) {
		ctx := context.Background()
		repo, client := newTestCodexAgentRepository(t)
		user, apiKey := mustCreateCodexUserAndKey(t, client)

		lastSeenAt := time.Now().Add(-2 * time.Minute)
		device, err := repo.CreateManagedDevice(ctx, CreateCodexManagedDeviceParams{
			UserID:         user.ID,
			APIKeyID:       apiKey.ID,
			Name:           uniqueCodexValue("device"),
			Platform:       "darwin",
			Arch:           "arm64",
			ManagerVersion: "1.0.0",
			LastSeenAt:     &lastSeenAt,
		})
		require.NoError(t, err)
		require.NotZero(t, device.ID)
		require.Equal(t, codexmanageddevice.StatusActive, device.Status)

		got, err := repo.GetManagedDevice(ctx, device.ID)
		require.NoError(t, err)
		require.Equal(t, device.ID, got.ID)
		require.Equal(t, device.Name, got.Name)

		devices, err := repo.ListManagedDevicesByUser(ctx, user.ID)
		require.NoError(t, err)
		require.Len(t, devices, 1)
		require.Equal(t, device.ID, devices[0].ID)

		auditLog, err := repo.InsertAuditLog(ctx, InsertCodexDeviceAuditLogParams{
			DeviceID:   device.ID,
			UserID:     user.ID,
			Event:      "device.created",
			IP:         "127.0.0.1",
			UserAgent:  "codex-agent-test",
			Metadata:   map[string]any{"source": "integration"},
			OccurredAt: time.Now(),
		})
		require.NoError(t, err)
		require.Equal(t, device.ID, auditLog.DeviceID)
		require.Equal(t, user.ID, auditLog.UserID)
		require.Equal(t, "device.created", auditLog.Event)
		require.Equal(t, "integration", auditLog.Metadata["source"])

		revokedAt := time.Now()
		require.NoError(t, repo.RevokeManagedDevice(ctx, device.ID, revokedAt))

		revoked, err := repo.GetManagedDevice(ctx, device.ID)
		require.NoError(t, err)
		require.Equal(t, codexmanageddevice.StatusRevoked, revoked.Status)
		require.NotNil(t, revoked.RevokedAt)
	})

	t.Run("refresh token hash can rotate", func(t *testing.T) {
		ctx := context.Background()
		repo, client := newTestCodexAgentRepository(t)
		user, apiKey := mustCreateCodexUserAndKey(t, client)
		device := mustCreateCodexManagedDevice(t, ctx, repo, user.ID, apiKey.ID)

		token, err := repo.CreateDeviceToken(ctx, CreateCodexDeviceTokenParams{
			DeviceID:         device.ID,
			RefreshTokenHash: uniqueCodexValue("refresh"),
			ExpiresAt:        time.Now().Add(30 * time.Minute),
		})
		require.NoError(t, err)

		rotated, err := repo.RotateDeviceToken(ctx, RotateCodexDeviceTokenParams{
			CurrentRefreshTokenHash: token.RefreshTokenHash,
			NewRefreshTokenHash:     uniqueCodexValue("refresh-next"),
			NewExpiresAt:            time.Now().Add(2 * time.Hour),
			Now:                     time.Now(),
		})
		require.NoError(t, err)
		require.NotZero(t, rotated.ID)
		require.NotEqual(t, token.ID, rotated.ID)
		require.Equal(t, device.ID, rotated.DeviceID)

		oldToken, err := client.CodexDeviceToken.Get(ctx, token.ID)
		require.NoError(t, err)
		require.NotNil(t, oldToken.RotatedAt)

		active, err := repo.FindActiveTokenByHash(ctx, rotated.RefreshTokenHash, time.Now())
		require.NoError(t, err)
		require.Equal(t, rotated.ID, active.ID)
	})

	t.Run("concurrent refresh token rotate allows exactly one success", func(t *testing.T) {
		ctx := context.Background()
		repo, client := newTestCodexAgentRepository(t)
		user, apiKey := mustCreateCodexUserAndKey(t, client)
		device := mustCreateCodexManagedDevice(t, ctx, repo, user.ID, apiKey.ID)

		token, err := repo.CreateDeviceToken(ctx, CreateCodexDeviceTokenParams{
			DeviceID:         device.ID,
			RefreshTokenHash: uniqueCodexValue("refresh-concurrent"),
			ExpiresAt:        time.Now().Add(30 * time.Minute),
		})
		require.NoError(t, err)

		errs := runConcurrent(2, funcFactory(func(i int) func() error {
			return func() error {
				_, rotateErr := repo.RotateDeviceToken(ctx, RotateCodexDeviceTokenParams{
					CurrentRefreshTokenHash: token.RefreshTokenHash,
					NewRefreshTokenHash:     uniqueCodexValue(fmt.Sprintf("refresh-concurrent-next-%d", i)),
					NewExpiresAt:            time.Now().Add(2 * time.Hour),
					Now:                     time.Now(),
				})
				return rotateErr
			}
		}))

		require.Equal(t, 1, countNilErrors(errs))
		require.Equal(t, 1, countErrorIs(errs, ErrCodexDeviceTokenNotActive))

		tokens, err := client.CodexDeviceToken.Query().All(ctx)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(tokens), 2)
	})

	t.Run("rotate token reuses outer transaction and rolls back with caller", func(t *testing.T) {
		ctx := context.Background()
		repo, client := newTestCodexAgentRepository(t)
		user, apiKey := mustCreateCodexUserAndKey(t, client)
		device := mustCreateCodexManagedDevice(t, ctx, repo, user.ID, apiKey.ID)

		token, err := repo.CreateDeviceToken(ctx, CreateCodexDeviceTokenParams{
			DeviceID:         device.ID,
			RefreshTokenHash: uniqueCodexValue("refresh-outer-tx"),
			ExpiresAt:        time.Now().Add(30 * time.Minute),
		})
		require.NoError(t, err)

		tx, err := client.Tx(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		ctxTx := dbent.NewTxContext(ctx, tx)
		newHash := uniqueCodexValue("refresh-outer-tx-next")
		_, err = repo.RotateDeviceToken(ctxTx, RotateCodexDeviceTokenParams{
			CurrentRefreshTokenHash: token.RefreshTokenHash,
			NewRefreshTokenHash:     newHash,
			NewExpiresAt:            time.Now().Add(2 * time.Hour),
			Now:                     time.Now(),
		})
		require.NoError(t, err)

		txTokens, err := tx.Client().CodexDeviceToken.Query().
			Where(codexdevicetoken.DeviceIDEQ(device.ID)).
			All(ctxTx)
		require.NoError(t, err)
		require.Len(t, txTokens, 2)

		require.NoError(t, tx.Rollback())

		original, err := client.CodexDeviceToken.Get(ctx, token.ID)
		require.NoError(t, err)
		require.Nil(t, original.RotatedAt)

		_, err = repo.FindActiveTokenByHash(ctx, token.RefreshTokenHash, time.Now())
		require.NoError(t, err)

		_, err = repo.FindActiveTokenByHash(ctx, newHash, time.Now())
		require.ErrorIs(t, err, ErrCodexDeviceTokenNotFound)
	})

	t.Run("revoked devices cannot find active tokens", func(t *testing.T) {
		ctx := context.Background()
		repo, client := newTestCodexAgentRepository(t)
		user, apiKey := mustCreateCodexUserAndKey(t, client)
		device := mustCreateCodexManagedDevice(t, ctx, repo, user.ID, apiKey.ID)

		token, err := repo.CreateDeviceToken(ctx, CreateCodexDeviceTokenParams{
			DeviceID:         device.ID,
			RefreshTokenHash: uniqueCodexValue("refresh-revoked"),
			ExpiresAt:        time.Now().Add(30 * time.Minute),
		})
		require.NoError(t, err)

		require.NoError(t, repo.RevokeManagedDevice(ctx, device.ID, time.Now()))

		_, err = repo.FindActiveTokenByHash(ctx, token.RefreshTokenHash, time.Now())
		require.ErrorIs(t, err, ErrCodexDeviceTokenNotFound)
	})
}

func newTestCodexAgentRepository(t *testing.T) (*codexAgentRepository, *dbent.Client) {
	t.Helper()
	client := testEntClient(t)
	return NewCodexAgentRepository(client), client
}

func mustCreateCodexUserAndKey(t *testing.T, client *dbent.Client) (*service.User, *service.APIKey) {
	t.Helper()

	user := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("%s@example.com", uniqueCodexValue("user")),
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    uniqueCodexValue("sk"),
		Name:   uniqueCodexValue("key"),
	})
	return user, apiKey
}

func mustCreateCodexManagedDevice(t *testing.T, ctx context.Context, repo *codexAgentRepository, userID, apiKeyID int64) *dbent.CodexManagedDevice {
	t.Helper()

	device, err := repo.CreateManagedDevice(ctx, CreateCodexManagedDeviceParams{
		UserID:         userID,
		APIKeyID:       apiKeyID,
		Name:           uniqueCodexValue("device"),
		Platform:       "darwin",
		Arch:           "arm64",
		ManagerVersion: "1.0.0",
	})
	require.NoError(t, err)
	return device
}

func uniqueCodexValue(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func runConcurrent(n int, fn func() error) []error {
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			errs[idx] = fn()
		}(i)
	}

	close(start)
	wg.Wait()
	return errs
}

func funcFactory(build func(i int) func() error) func() error {
	var (
		mu  sync.Mutex
		idx int
	)

	return func() error {
		mu.Lock()
		current := idx
		idx++
		mu.Unlock()
		return build(current)()
	}
}

func countNilErrors(errs []error) int {
	count := 0
	for _, err := range errs {
		if err == nil {
			count++
		}
	}
	return count
}

func countErrorIs(errs []error, target error) int {
	count := 0
	for _, err := range errs {
		if errors.Is(err, target) {
			count++
		}
	}
	return count
}
