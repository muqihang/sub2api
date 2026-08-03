package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type entityRateLimitPolicySQLDB interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type entityRateLimitPolicyRepository struct {
	db entityRateLimitPolicySQLDB
}

func NewEntityRateLimitPolicyRepository(sqlDB *sql.DB) service.EntityRateLimitPolicyRepository {
	return &entityRateLimitPolicyRepository{db: sqlDB}
}

func (r *entityRateLimitPolicyRepository) GetActiveByEntityID(ctx context.Context, entityID int64) (*service.EntityRateLimitPolicy, error) {
	if r == nil || r.db == nil || entityID <= 0 {
		return nil, nil
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, entity_id, status, rpm_limit, tpm_limit, concurrency_limit, cost_limit_usd, metadata, created_at, updated_at
		FROM entity_rate_limit_policies
		WHERE entity_id = $1 AND status = $2
		ORDER BY id DESC
		LIMIT 1
	`, entityID, service.EntityRateLimitPolicyStatusActive)
	return scanEntityRateLimitPolicy(row)
}

type entityRateLimitScanner interface {
	Scan(dest ...any) error
}

func scanEntityRateLimitPolicy(row entityRateLimitScanner) (*service.EntityRateLimitPolicy, error) {
	var (
		policy   service.EntityRateLimitPolicy
		metadata string
	)
	err := row.Scan(
		&policy.ID,
		&policy.EntityID,
		&policy.Status,
		&policy.RPMLimit,
		&policy.TPMLimit,
		&policy.ConcurrencyLimit,
		&policy.CostLimitUSD,
		&metadata,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	policy.Metadata = unmarshalEntityJSON(metadata)
	return &policy, nil
}

const (
	entityRateLimitRPMKeyPrefix         = "entity:rl:rpm:"
	entityRateLimitTPMKeyPrefix         = "entity:rl:tpm:"
	entityRateLimitCostKeyPrefix        = "entity:rl:cost:"
	entityRateLimitConcurrencyKeyPrefix = "entity:rl:concurrency:"
	entityRateLimitMinuteTTL            = 120 * time.Second
)

type entityRateLimitCache struct {
	rdb            *redis.Client
	slotTTLSeconds int
}

func NewEntityRateLimitCache(rdb *redis.Client) service.EntityRateLimitCache {
	return &entityRateLimitCache{
		rdb:            rdb,
		slotTTLSeconds: defaultSlotTTLMinutes * 60,
	}
}

func (c *entityRateLimitCache) AcquireEntitySlot(ctx context.Context, entityID int64, maxConcurrency int, requestID string) (bool, error) {
	key := fmt.Sprintf("%s%d", entityRateLimitConcurrencyKeyPrefix, entityID)
	result, err := acquireScript.Run(ctx, c.rdb, []string{key}, maxConcurrency, c.slotTTLSeconds, requestID).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (c *entityRateLimitCache) ReleaseEntitySlot(ctx context.Context, entityID int64, requestID string) error {
	key := fmt.Sprintf("%s%d", entityRateLimitConcurrencyKeyPrefix, entityID)
	return c.rdb.ZRem(ctx, key, requestID).Err()
}

func (c *entityRateLimitCache) IncrementEntityRPM(ctx context.Context, entityID int64) (int, error) {
	key, err := c.currentMinuteKey(ctx, entityRateLimitRPMKeyPrefix, entityID)
	if err != nil {
		return 0, fmt.Errorf("entity rpm increment: %w", err)
	}
	pipe := c.rdb.TxPipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, entityRateLimitMinuteTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("entity rpm increment: %w", err)
	}
	return int(incrCmd.Val()), nil
}

func (c *entityRateLimitCache) AddEntityTPM(ctx context.Context, entityID int64, tokens int) (int, error) {
	if tokens <= 0 {
		return 0, nil
	}
	key, err := c.currentMinuteKey(ctx, entityRateLimitTPMKeyPrefix, entityID)
	if err != nil {
		return 0, fmt.Errorf("entity tpm add: %w", err)
	}
	pipe := c.rdb.TxPipeline()
	incrCmd := pipe.IncrBy(ctx, key, int64(tokens))
	pipe.Expire(ctx, key, entityRateLimitMinuteTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("entity tpm add: %w", err)
	}
	return int(incrCmd.Val()), nil
}

func (c *entityRateLimitCache) AddEntityCost(ctx context.Context, entityID int64, amount float64) (float64, error) {
	if amount <= 0 {
		return 0, nil
	}
	key, err := c.currentMinuteKey(ctx, entityRateLimitCostKeyPrefix, entityID)
	if err != nil {
		return 0, fmt.Errorf("entity cost add: %w", err)
	}
	pipe := c.rdb.TxPipeline()
	incrCmd := pipe.IncrByFloat(ctx, key, amount)
	pipe.Expire(ctx, key, entityRateLimitMinuteTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("entity cost add: %w", err)
	}
	return incrCmd.Val(), nil
}

func (c *entityRateLimitCache) currentMinuteKey(ctx context.Context, prefix string, entityID int64) (string, error) {
	if c == nil || c.rdb == nil {
		return "", errors.New("redis client unavailable")
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || entityID <= 0 {
		return "", errors.New("invalid entity rate-limit key")
	}
	serverTime, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return "", fmt.Errorf("redis TIME: %w", err)
	}
	return fmt.Sprintf("%s%d:%d", prefix, entityID, serverTime.Unix()/60), nil
}
