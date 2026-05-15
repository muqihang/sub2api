package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
)

// ProvideConcurrencyCache 创建并发控制缓存，从配置读取 TTL 参数
// 性能优化：TTL 可配置，支持长时间运行的 LLM 请求场景
func ProvideConcurrencyCache(rdb *redis.Client, cfg *config.Config) service.ConcurrencyCache {
	waitTTLSeconds := int(cfg.Gateway.Scheduling.StickySessionWaitTimeout.Seconds())
	if cfg.Gateway.Scheduling.FallbackWaitTimeout > cfg.Gateway.Scheduling.StickySessionWaitTimeout {
		waitTTLSeconds = int(cfg.Gateway.Scheduling.FallbackWaitTimeout.Seconds())
	}
	if waitTTLSeconds <= 0 {
		waitTTLSeconds = cfg.Gateway.ConcurrencySlotTTLMinutes * 60
	}
	return NewConcurrencyCache(rdb, cfg.Gateway.ConcurrencySlotTTLMinutes, waitTTLSeconds)
}

// ProvideGitHubReleaseClient 创建 GitHub Release 客户端
// 从配置中读取代理设置，支持国内服务器通过代理访问 GitHub
func ProvideGitHubReleaseClient(cfg *config.Config) service.GitHubReleaseClient {
	return NewGitHubReleaseClient(cfg.Update.ProxyURL, cfg.Security.ProxyFallback.AllowDirectOnError)
}

// ProvidePricingRemoteClient 创建定价数据远程客户端
// 从配置中读取代理设置，支持国内服务器通过代理访问 GitHub 上的定价数据
func ProvidePricingRemoteClient(cfg *config.Config) service.PricingRemoteClient {
	return NewPricingRemoteClient(cfg.Update.ProxyURL, cfg.Security.ProxyFallback.AllowDirectOnError)
}

// ProvideSessionLimitCache 创建会话限制缓存
// 用于 Anthropic OAuth/SetupToken 账号的并发会话数量控制
func ProvideSessionLimitCache(rdb *redis.Client, cfg *config.Config) service.SessionLimitCache {
	defaultIdleTimeoutMinutes := 5 // 默认 5 分钟空闲超时
	if cfg != nil && cfg.Gateway.SessionIdleTimeoutMinutes > 0 {
		defaultIdleTimeoutMinutes = cfg.Gateway.SessionIdleTimeoutMinutes
	}
	return NewSessionLimitCache(rdb, defaultIdleTimeoutMinutes)
}

// ProvideSchedulerCache 创建调度快照缓存，并注入快照分块参数。
func ProvideSchedulerCache(rdb *redis.Client, cfg *config.Config) service.SchedulerCache {
	mgetChunkSize := defaultSchedulerSnapshotMGetChunkSize
	writeChunkSize := defaultSchedulerSnapshotWriteChunkSize
	if cfg != nil {
		if cfg.Gateway.Scheduling.SnapshotMGetChunkSize > 0 {
			mgetChunkSize = cfg.Gateway.Scheduling.SnapshotMGetChunkSize
		}
		if cfg.Gateway.Scheduling.SnapshotWriteChunkSize > 0 {
			writeChunkSize = cfg.Gateway.Scheduling.SnapshotWriteChunkSize
		}
	}
	return newSchedulerCacheWithChunkSizes(rdb, mgetChunkSize, writeChunkSize)
}

// ProviderSet is the Wire provider set for all repositories
var ProviderSet = wire.NewSet(
	NewUserRepository,
	NewAPIKeyRepository,
	NewGroupRepository,
	NewAccountRepository,
	NewScheduledTestPlanRepository,   // 定时测试计划仓储
	NewScheduledTestResultRepository, // 定时测试结果仓储
	NewProxyRepository,
	NewRedeemCodeRepository,
	NewPromoCodeRepository,
	NewAnnouncementRepository,
	NewAnnouncementReadRepository,
	NewUsageLogRepository,
	NewUsageBillingRepository,
	NewIdempotencyRepository,
	NewUsageCleanupRepository,
	NewDashboardAggregationRepository,
	NewSettingRepository,
	NewOpsRepository,
	NewUserSubscriptionRepository,
	NewUserAttributeDefinitionRepository,
	NewUserAttributeValueRepository,
	NewUserGroupRateRepository,
	NewErrorPassthroughRepository,
	NewTLSFingerprintProfileRepository,
	NewChannelRepository,
	NewChannelMonitorRepository,
	NewChannelMonitorRequestTemplateRepository,
	NewAffiliateRepository,
	NewEntityRegistryRepository,
	NewEntityRateLimitPolicyRepository,
	NewCodexAgentRepository,
	wire.Bind(new(service.CodexAgentRepository), new(*codexAgentRepository)),

	// Cache implementations
	NewGatewayCache,
	NewBillingCache,
	NewAPIKeyCache,
	NewTempUnschedCache,
	NewTimeoutCounterCache,
	NewOpenAI403CounterCache,
	NewInternal500CounterCache,
	ProvideConcurrencyCache,
	ProvideSessionLimitCache,
	NewRPMCache,
	NewUserRPMCache,
	NewUserMsgQueueCache,
	NewDashboardCache,
	NewEmailCache,
	NewIdentityCache,
	NewRedeemCache,
	NewUpdateCache,
	NewGeminiTokenCache,
	ProvideSchedulerCache,
	ProvideAugmentOfficialSessionStore,
	ProvideAugmentOfficialPoolSessionStore,
	ProvideAugmentGatewaySettingsStore,
	NewSchedulerOutboxRepository,
	NewProxyLatencyCache,
	NewTotpCache,
	NewRefreshTokenCache,
	NewErrorPassthroughCache,
	NewTLSFingerprintProfileCache,
	NewEntityRateLimitCache,

	// Encryptors
	NewAESEncryptor,

	// Backup infrastructure
	NewPgDumper,
	NewS3BackupStoreFactory,

	// HTTP service ports (DI Strategy A: return interface directly)
	NewTurnstileVerifier,
	ProvidePricingRemoteClient,
	ProvideGitHubReleaseClient,
	NewProxyExitInfoProber,
	NewClaudeUsageFetcher,
	NewClaudeOAuthClient,
	NewHTTPUpstream,
	NewOpenAIOAuthClient,
	NewGeminiOAuthClient,
	NewGeminiCliCodeAssistClient,
	NewGeminiDriveClient,

	ProvideEnt,
	ProvideSQLDB,
	ProvideRedis,
)

// ProvideEnt 为依赖注入提供 Ent 客户端。
//
// 该函数是 InitEnt 的包装器，符合 Wire 的依赖提供函数签名要求。
// Wire 会在编译时分析依赖关系，自动生成初始化代码。
//
// 依赖：config.Config
// 提供：*ent.Client
func ProvideEnt(cfg *config.Config) (*ent.Client, error) {
	client, _, err := InitEnt(cfg)
	return client, err
}

// ProvideSQLDB 从 Ent 客户端提取底层的 *sql.DB 连接。
//
// 某些 Repository 需要直接执行原生 SQL（如复杂的批量更新、聚合查询），
// 此时需要访问底层的 sql.DB 而不是通过 Ent ORM。
//
// 设计说明：
//   - Ent 底层使用 sql.DB，通过 Driver 接口可以访问
//   - 这种设计允许在同一事务中混用 Ent 和原生 SQL
//
// 依赖：*ent.Client
// 提供：*sql.DB
func ProvideSQLDB(client *ent.Client) (*sql.DB, error) {
	if client == nil {
		return nil, errors.New("nil ent client")
	}
	// 从 Ent 客户端获取底层驱动
	drv, ok := client.Driver().(*entsql.Driver)
	if !ok {
		return nil, errors.New("ent driver does not expose *sql.DB")
	}
	// 返回驱动持有的 sql.DB 实例
	return drv.DB(), nil
}

// ProvideRedis 为依赖注入提供 Redis 客户端。
//
// Redis 用于：
//   - 分布式锁（如并发控制）
//   - 缓存（如用户会话、API 响应缓存）
//   - 速率限制
//   - 实时统计数据
//
// 依赖：config.Config
// 提供：*redis.Client
func ProvideRedis(cfg *config.Config) *redis.Client {
	return InitRedis(cfg)
}

type augmentOfficialSessionStoreAdapter struct {
	repo *augmentOfficialSessionRepository
}

func ProvideAugmentOfficialSessionStore(client *ent.Client, sqlDB *sql.DB) service.AugmentOfficialSessionStore {
	return &augmentOfficialSessionStoreAdapter{
		repo: NewAugmentOfficialSessionRepository(client, sqlDB),
	}
}

func ProvideAugmentOfficialPoolSessionStore(sqlDB *sql.DB) service.AugmentOfficialPoolSessionStore {
	return NewAugmentOfficialPoolSessionRepository(sqlDB)
}

func ProvideAugmentGatewaySettingsStore(sqlDB *sql.DB) service.AugmentGatewaySettingsStore {
	return NewAugmentGatewaySettingsRepository(sqlDB)
}

func (a *augmentOfficialSessionStoreAdapter) CreateBindIntent(ctx context.Context, input service.AugmentOfficialSessionBindIntentStoreCreateInput) (*service.AugmentOfficialSessionBindIntentStoreRecord, error) {
	record, err := a.repo.CreateBindIntent(ctx, AugmentOfficialSessionBindIntentCreateInput{
		UserID:          input.UserID,
		StateHash:       input.StateHash,
		Mode:            input.Mode,
		Source:          input.Source,
		TenantAllowlist: append([]string(nil), input.TenantAllowlist...),
	})
	if err != nil || record == nil {
		return nil, translateAugmentOfficialSessionStoreError(err)
	}
	return &service.AugmentOfficialSessionBindIntentStoreRecord{
		ID:              record.ID,
		UserID:          record.UserID,
		BindIntentID:    record.BindIntentID,
		StateHash:       record.StateHash,
		Mode:            record.Mode,
		Source:          record.Source,
		TenantAllowlist: append([]string(nil), record.TenantAllowlist...),
		ExpiresAt:       record.ExpiresAt.UTC(),
		ConsumedAt:      cloneTimePtrForAdapter(record.ConsumedAt),
		CreatedAt:       record.CreatedAt.UTC(),
	}, nil
}

func (a *augmentOfficialSessionStoreAdapter) ConsumeBindIntent(ctx context.Context, bindIntentID string, userID int64) (*service.AugmentOfficialSessionBindIntentStoreRecord, error) {
	record, err := a.repo.ConsumeBindIntent(ctx, bindIntentID, userID)
	if err != nil || record == nil {
		return nil, translateAugmentOfficialSessionStoreError(err)
	}
	return &service.AugmentOfficialSessionBindIntentStoreRecord{
		ID:              record.ID,
		UserID:          record.UserID,
		BindIntentID:    record.BindIntentID,
		StateHash:       record.StateHash,
		Mode:            record.Mode,
		Source:          record.Source,
		TenantAllowlist: append([]string(nil), record.TenantAllowlist...),
		ExpiresAt:       record.ExpiresAt.UTC(),
		ConsumedAt:      cloneTimePtrForAdapter(record.ConsumedAt),
		CreatedAt:       record.CreatedAt.UTC(),
	}, nil
}

func (a *augmentOfficialSessionStoreAdapter) UpsertActiveSession(ctx context.Context, input service.AugmentOfficialSessionStoredSessionInput) (*service.AugmentOfficialSessionStoredPublicView, error) {
	view, err := a.repo.UpsertActiveSession(ctx, AugmentOfficialSessionUpsertInput{
		UserID:                     input.UserID,
		Mode:                       input.Mode,
		Source:                     input.Source,
		TenantOrigin:               input.TenantOrigin,
		PortalOrigin:               cloneStringPtrForAdapter(input.PortalOrigin),
		Scopes:                     append([]string(nil), input.Scopes...),
		ExpiresAt:                  cloneTimePtrForAdapter(input.ExpiresAt),
		LastRefreshAt:              cloneTimePtrForAdapter(input.LastRefreshAt),
		LastSuccessAt:              cloneTimePtrForAdapter(input.LastSuccessAt),
		LastErrorAt:                cloneTimePtrForAdapter(input.LastErrorAt),
		LastErrorCode:              cloneStringPtrForAdapter(input.LastErrorCode),
		Status:                     input.Status,
		EncryptedCredentialPayload: append([]byte(nil), input.EncryptedCredentialPayload...),
		CredentialSchemaVersion:    input.CredentialSchemaVersion,
		KeyVersion:                 input.KeyVersion,
		Fingerprint:                input.Fingerprint,
	})
	if err != nil || view == nil {
		return nil, translateAugmentOfficialSessionStoreError(err)
	}
	return adminViewToStoredPublicView(view), nil
}

func (a *augmentOfficialSessionStoreAdapter) GetActiveSessionPublicView(ctx context.Context, userID int64) (*service.AugmentOfficialSessionStoredPublicView, error) {
	view, err := a.repo.GetActiveSessionPublicView(ctx, userID)
	if err != nil || view == nil {
		return nil, translateAugmentOfficialSessionStoreError(err)
	}
	return &service.AugmentOfficialSessionStoredPublicView{
		UserID:                  view.UserID,
		Mode:                    view.Mode,
		Source:                  view.Source,
		TenantOrigin:            view.TenantOrigin,
		PortalOrigin:            cloneStringPtrForAdapter(view.PortalOrigin),
		Scopes:                  append([]string(nil), view.Scopes...),
		ExpiresAt:               cloneTimePtrForAdapter(view.ExpiresAt),
		LastRefreshAt:           cloneTimePtrForAdapter(view.LastRefreshAt),
		LastSuccessAt:           cloneTimePtrForAdapter(view.LastSuccessAt),
		LastErrorAt:             cloneTimePtrForAdapter(view.LastErrorAt),
		LastErrorCode:           cloneStringPtrForAdapter(view.LastErrorCode),
		Status:                  view.Status,
		CredentialSchemaVersion: view.CredentialSchemaVersion,
		KeyVersion:              view.KeyVersion,
		Fingerprint:             view.Fingerprint,
		CreatedAt:               view.CreatedAt.UTC(),
		UpdatedAt:               view.UpdatedAt.UTC(),
		RevokedAt:               cloneTimePtrForAdapter(view.RevokedAt),
	}, nil
}

func (a *augmentOfficialSessionStoreAdapter) GetActiveSessionAdminView(ctx context.Context, userID int64) (*service.AugmentOfficialSessionStoredAdminView, error) {
	view, err := a.repo.GetActiveSessionAdminView(ctx, userID)
	if err != nil || view == nil {
		return nil, translateAugmentOfficialSessionStoreError(err)
	}
	return adminViewToStoredAdminView(view), nil
}

func (a *augmentOfficialSessionStoreAdapter) ListAdminSessions(ctx context.Context) ([]service.AugmentOfficialSessionStoredAdminView, error) {
	views, err := a.repo.ListAdminViews(ctx)
	if err != nil {
		return nil, translateAugmentOfficialSessionStoreError(err)
	}
	out := make([]service.AugmentOfficialSessionStoredAdminView, 0, len(views))
	for i := range views {
		out = append(out, *adminViewToStoredAdminView(&views[i]))
	}
	return out, nil
}

func (a *augmentOfficialSessionStoreAdapter) GetActiveSessionCredentialRow(ctx context.Context, userID int64) (*service.AugmentOfficialSessionStoredCredentialRow, error) {
	row, err := a.repo.GetActiveSessionCredentialRow(ctx, userID)
	if err != nil || row == nil {
		return nil, translateAugmentOfficialSessionStoreError(err)
	}
	return &service.AugmentOfficialSessionStoredCredentialRow{
		UserID:                     row.UserID,
		Mode:                       row.Mode,
		Source:                     row.Source,
		TenantOrigin:               row.TenantOrigin,
		PortalOrigin:               cloneStringPtrForAdapter(row.PortalOrigin),
		Scopes:                     append([]string(nil), row.Scopes...),
		ExpiresAt:                  cloneTimePtrForAdapter(row.ExpiresAt),
		LastRefreshAt:              cloneTimePtrForAdapter(row.LastRefreshAt),
		LastSuccessAt:              cloneTimePtrForAdapter(row.LastSuccessAt),
		LastErrorAt:                cloneTimePtrForAdapter(row.LastErrorAt),
		LastErrorCode:              cloneStringPtrForAdapter(row.LastErrorCode),
		Status:                     row.Status,
		EncryptedCredentialPayload: append([]byte(nil), row.EncryptedCredentialPayload...),
		CredentialSchemaVersion:    row.CredentialSchemaVersion,
		KeyVersion:                 row.KeyVersion,
		Fingerprint:                row.Fingerprint,
		CreatedAt:                  row.CreatedAt.UTC(),
		UpdatedAt:                  row.UpdatedAt.UTC(),
		RevokedAt:                  cloneTimePtrForAdapter(row.RevokedAt),
	}, nil
}

func (a *augmentOfficialSessionStoreAdapter) RevokeActiveSession(ctx context.Context, userID int64) (*service.AugmentOfficialSessionStoredPublicView, error) {
	view, err := a.repo.RevokeActiveSession(ctx, userID)
	if err != nil || view == nil {
		return nil, translateAugmentOfficialSessionStoreError(err)
	}
	return adminViewToStoredPublicView(view), nil
}

func adminViewToStoredPublicView(view *AugmentOfficialSessionAdminView) *service.AugmentOfficialSessionStoredPublicView {
	if view == nil {
		return nil
	}
	return &service.AugmentOfficialSessionStoredPublicView{
		UserID:                  view.UserID,
		Mode:                    view.Mode,
		Source:                  view.Source,
		TenantOrigin:            view.TenantOrigin,
		PortalOrigin:            cloneStringPtrForAdapter(view.PortalOrigin),
		Scopes:                  append([]string(nil), view.Scopes...),
		ExpiresAt:               cloneTimePtrForAdapter(view.ExpiresAt),
		LastRefreshAt:           cloneTimePtrForAdapter(view.LastRefreshAt),
		LastSuccessAt:           cloneTimePtrForAdapter(view.LastSuccessAt),
		LastErrorAt:             cloneTimePtrForAdapter(view.LastErrorAt),
		LastErrorCode:           cloneStringPtrForAdapter(view.LastErrorCode),
		Status:                  view.Status,
		CredentialSchemaVersion: view.CredentialSchemaVersion,
		KeyVersion:              view.KeyVersion,
		Fingerprint:             view.Fingerprint,
		CreatedAt:               view.CreatedAt.UTC(),
		UpdatedAt:               view.UpdatedAt.UTC(),
		RevokedAt:               cloneTimePtrForAdapter(view.RevokedAt),
	}
}

func adminViewToStoredAdminView(view *AugmentOfficialSessionAdminView) *service.AugmentOfficialSessionStoredAdminView {
	if view == nil {
		return nil
	}
	return &service.AugmentOfficialSessionStoredAdminView{
		UserID:                  view.UserID,
		Mode:                    view.Mode,
		Source:                  view.Source,
		TenantOrigin:            view.TenantOrigin,
		PortalOrigin:            cloneStringPtrForAdapter(view.PortalOrigin),
		Scopes:                  append([]string(nil), view.Scopes...),
		ExpiresAt:               cloneTimePtrForAdapter(view.ExpiresAt),
		LastRefreshAt:           cloneTimePtrForAdapter(view.LastRefreshAt),
		LastSuccessAt:           cloneTimePtrForAdapter(view.LastSuccessAt),
		LastErrorAt:             cloneTimePtrForAdapter(view.LastErrorAt),
		LastErrorCode:           cloneStringPtrForAdapter(view.LastErrorCode),
		Status:                  view.Status,
		CredentialSchemaVersion: view.CredentialSchemaVersion,
		KeyVersion:              view.KeyVersion,
		Fingerprint:             view.Fingerprint,
		CreatedAt:               view.CreatedAt.UTC(),
		UpdatedAt:               view.UpdatedAt.UTC(),
		RevokedAt:               cloneTimePtrForAdapter(view.RevokedAt),
		HasCredentialPayload:    view.HasCredentialPayload,
	}
}

func cloneTimePtrForAdapter(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func cloneStringPtrForAdapter(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func translateAugmentOfficialSessionStoreError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrAugmentOfficialSessionBindIntentNotFound):
		return infraerrors.NotFound("AUGMENT_OFFICIAL_BIND_INTENT_NOT_FOUND", "augment official bind intent was not found")
	case errors.Is(err, ErrAugmentOfficialSessionBindIntentExpired):
		return infraerrors.Unauthorized("AUGMENT_OFFICIAL_BIND_INTENT_EXPIRED", "augment official bind intent has expired")
	case errors.Is(err, ErrAugmentOfficialSessionBindIntentCrossUser):
		return infraerrors.Forbidden("AUGMENT_OFFICIAL_BIND_INTENT_FORBIDDEN", "augment official bind intent does not belong to this user")
	case errors.Is(err, ErrAugmentOfficialSessionBindIntentConsumed):
		return infraerrors.Conflict("AUGMENT_OFFICIAL_BIND_INTENT_CONSUMED", "augment official bind intent has already been consumed")
	case errors.Is(err, ErrAugmentOfficialSessionSourceInvalid),
		errors.Is(err, ErrAugmentOfficialSessionModeInvalid),
		errors.Is(err, ErrAugmentOfficialSessionStatusInvalid),
		errors.Is(err, ErrAugmentOfficialSessionCredentialPayloadEmpty):
		return infraerrors.BadRequest("AUGMENT_OFFICIAL_SESSION_INVALID", err.Error())
	default:
		return err
	}
}
