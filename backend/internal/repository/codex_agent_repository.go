package repository

import (
	"context"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/codexdevicetoken"
	"github.com/Wei-Shaw/sub2api/ent/codexmanageddevice"
	"github.com/Wei-Shaw/sub2api/ent/codexsetupgrant"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var (
	ErrCodexSetupGrantNotActive   = service.ErrCodexSetupGrantNotActive
	ErrCodexManagedDeviceNotFound = service.ErrCodexManagedDeviceNotFound
	ErrCodexDeviceTokenNotActive  = service.ErrCodexManagedRefreshTokenInvalid
	ErrCodexDeviceTokenNotFound   = service.ErrCodexManagedRefreshTokenInvalid
)

type CreateCodexSetupGrantParams = service.CreateCodexSetupGrantParams
type CreateCodexManagedDeviceParams = service.CreateCodexManagedDeviceParams
type CreateCodexDeviceTokenParams = service.CreateCodexDeviceTokenParams
type RotateCodexDeviceTokenParams = service.RotateCodexDeviceTokenParams
type InsertCodexDeviceAuditLogParams = service.InsertCodexDeviceAuditLogParams

type codexAgentRepository struct {
	client *dbent.Client
}

func NewCodexAgentRepository(client *dbent.Client) *codexAgentRepository {
	return &codexAgentRepository{client: client}
}

func (r *codexAgentRepository) CreateSetupGrant(ctx context.Context, params CreateCodexSetupGrantParams) (*dbent.CodexSetupGrant, error) {
	return clientFromContext(ctx, r.client).CodexSetupGrant.Create().
		SetCodeHash(params.CodeHash).
		SetUserID(params.UserID).
		SetAPIKeyID(params.APIKeyID).
		SetMode(params.Mode).
		SetServerOrigin(params.ServerOrigin).
		SetGatewayOrigin(params.GatewayOrigin).
		SetExpiresAt(params.ExpiresAt).
		Save(ctx)
}

func (r *codexAgentRepository) ConsumeSetupGrant(ctx context.Context, codeHash string, now time.Time) (*dbent.CodexSetupGrant, error) {
	client := clientFromContext(ctx, r.client)

	grant, err := client.CodexSetupGrant.Query().
		Where(codexsetupgrant.CodeHashEQ(codeHash)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrCodexSetupGrantNotActive
		}
		return nil, err
	}

	updated, err := client.CodexSetupGrant.Update().
		Where(
			codexsetupgrant.IDEQ(grant.ID),
			codexsetupgrant.ConsumedAtIsNil(),
			codexsetupgrant.ExpiresAtGT(now),
		).
		SetConsumedAt(now).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if updated != 1 {
		return nil, ErrCodexSetupGrantNotActive
	}

	return client.CodexSetupGrant.Get(ctx, grant.ID)
}

func (r *codexAgentRepository) CreateManagedDevice(ctx context.Context, params CreateCodexManagedDeviceParams) (*dbent.CodexManagedDevice, error) {
	builder := clientFromContext(ctx, r.client).CodexManagedDevice.Create().
		SetUserID(params.UserID).
		SetAPIKeyID(params.APIKeyID).
		SetName(params.Name).
		SetPlatform(params.Platform).
		SetArch(params.Arch).
		SetManagerVersion(params.ManagerVersion)
	if params.LastSeenAt != nil {
		builder.SetLastSeenAt(*params.LastSeenAt)
	}
	return builder.Save(ctx)
}

func (r *codexAgentRepository) GetManagedDevice(ctx context.Context, id int64) (*dbent.CodexManagedDevice, error) {
	device, err := clientFromContext(ctx, r.client).CodexManagedDevice.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrCodexManagedDeviceNotFound
		}
		return nil, err
	}
	return device, nil
}

func (r *codexAgentRepository) ListManagedDevicesByUser(ctx context.Context, userID int64) ([]*dbent.CodexManagedDevice, error) {
	return clientFromContext(ctx, r.client).CodexManagedDevice.Query().
		Where(codexmanageddevice.UserIDEQ(userID)).
		Order(dbent.Asc(codexmanageddevice.FieldID)).
		All(ctx)
}

func (r *codexAgentRepository) RevokeManagedDevice(ctx context.Context, id int64, revokedAt time.Time) error {
	client := clientFromContext(ctx, r.client)
	updated, err := client.CodexManagedDevice.Update().
		Where(codexmanageddevice.IDEQ(id)).
		SetStatus(codexmanageddevice.StatusRevoked).
		SetRevokedAt(revokedAt).
		SetUpdatedAt(revokedAt).
		Save(ctx)
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrCodexManagedDeviceNotFound
	}
	return nil
}

func (r *codexAgentRepository) CreateDeviceToken(ctx context.Context, params CreateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
	return clientFromContext(ctx, r.client).CodexDeviceToken.Create().
		SetDeviceID(params.DeviceID).
		SetRefreshTokenHash(params.RefreshTokenHash).
		SetExpiresAt(params.ExpiresAt).
		Save(ctx)
}

func (r *codexAgentRepository) RotateDeviceToken(ctx context.Context, params RotateCodexDeviceTokenParams) (*dbent.CodexDeviceToken, error) {
	tx, txClient, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	if tx != nil {
		defer func() { _ = tx.Rollback() }()
	}

	current, err := txClient.CodexDeviceToken.Query().
		Where(codexdevicetoken.RefreshTokenHashEQ(params.CurrentRefreshTokenHash)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrCodexDeviceTokenNotActive
		}
		return nil, err
	}

	updated, err := txClient.CodexDeviceToken.Update().
		Where(
			codexdevicetoken.IDEQ(current.ID),
			codexdevicetoken.RotatedAtIsNil(),
			codexdevicetoken.RevokedAtIsNil(),
			codexdevicetoken.ExpiresAtGT(params.Now),
		).
		SetRotatedAt(params.Now).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if updated != 1 {
		return nil, ErrCodexDeviceTokenNotActive
	}

	next, err := txClient.CodexDeviceToken.Create().
		SetDeviceID(current.DeviceID).
		SetRefreshTokenHash(params.NewRefreshTokenHash).
		SetExpiresAt(params.NewExpiresAt).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	return next, nil
}

func (r *codexAgentRepository) FindActiveTokenByHash(ctx context.Context, refreshTokenHash string, now time.Time) (*dbent.CodexDeviceToken, error) {
	token, err := clientFromContext(ctx, r.client).CodexDeviceToken.Query().
		Where(
			codexdevicetoken.RefreshTokenHashEQ(refreshTokenHash),
			codexdevicetoken.RotatedAtIsNil(),
			codexdevicetoken.RevokedAtIsNil(),
			codexdevicetoken.ExpiresAtGT(now),
			codexdevicetoken.HasDeviceWith(
				codexmanageddevice.StatusEQ(codexmanageddevice.StatusActive),
				codexmanageddevice.RevokedAtIsNil(),
			),
		).
		WithDevice().
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrCodexDeviceTokenNotFound
		}
		return nil, err
	}
	return token, nil
}

func (r *codexAgentRepository) InsertAuditLog(ctx context.Context, params InsertCodexDeviceAuditLogParams) (*dbent.CodexDeviceAuditLog, error) {
	builder := clientFromContext(ctx, r.client).CodexDeviceAuditLog.Create().
		SetDeviceID(params.DeviceID).
		SetUserID(params.UserID).
		SetEvent(params.Event).
		SetIP(params.IP).
		SetUserAgent(params.UserAgent)
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}
	if !params.OccurredAt.IsZero() {
		builder.SetCreatedAt(params.OccurredAt)
	}
	return builder.Save(ctx)
}

func (r *codexAgentRepository) beginTx(ctx context.Context) (*dbent.Tx, *dbent.Client, error) {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return nil, tx.Client(), nil
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		if errors.Is(err, dbent.ErrTxStarted) {
			return nil, clientFromContext(ctx, r.client), nil
		}
		return nil, nil, err
	}
	return tx, tx.Client(), nil
}

func (r *codexAgentRepository) ListPendingSetupGrantsByUser(ctx context.Context, userID int64, now time.Time) ([]*dbent.CodexSetupGrant, error) {
	return clientFromContext(ctx, r.client).CodexSetupGrant.Query().
		Where(
			codexsetupgrant.UserIDEQ(userID),
			codexsetupgrant.ConsumedAtIsNil(),
			codexsetupgrant.ExpiresAtGT(now),
		).
		Order(dbent.Desc(codexsetupgrant.FieldID)).
		All(ctx)
}

func (r *codexAgentRepository) GetSetupGrantByID(ctx context.Context, id int64) (*dbent.CodexSetupGrant, error) {
	grant, err := clientFromContext(ctx, r.client).CodexSetupGrant.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrCodexSetupSessionNotFound
		}
		return nil, err
	}
	return grant, nil
}
