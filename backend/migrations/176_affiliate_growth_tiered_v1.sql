-- Tiered affiliate growth ledger metadata and idempotency guards.

ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS base_amount DECIMAL(20,8) NULL;

ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS rate_percent DECIMAL(10,4) NULL;

ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS effective_invitee_count INTEGER NULL;

ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS growth_mode VARCHAR(32) NULL;

ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS reversal_of_id BIGINT NULL REFERENCES user_affiliate_ledger(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_ual_tiered_order_action_unique
    ON user_affiliate_ledger(source_order_id, action)
    WHERE source_order_id IS NOT NULL
      AND action IN ('accrue', 'invitee_bonus');

CREATE UNIQUE INDEX IF NOT EXISTS idx_ual_invitee_first_payment_bonus_unique
    ON user_affiliate_ledger(user_id)
    WHERE action = 'invitee_bonus';

CREATE INDEX IF NOT EXISTS idx_ual_reversal_of_id
    ON user_affiliate_ledger(reversal_of_id)
    WHERE reversal_of_id IS NOT NULL;

COMMENT ON COLUMN user_affiliate_ledger.base_amount IS 'Reward calculation base amount snapshot';
COMMENT ON COLUMN user_affiliate_ledger.rate_percent IS 'Reward rate percentage snapshot';
COMMENT ON COLUMN user_affiliate_ledger.effective_invitee_count IS 'Effective paid invitee count snapshot';
COMMENT ON COLUMN user_affiliate_ledger.growth_mode IS 'Affiliate rule mode snapshot';
COMMENT ON COLUMN user_affiliate_ledger.reversal_of_id IS 'Original ledger entry reversed by this entry';
COMMENT ON COLUMN user_affiliate_ledger.action IS 'accrue|transfer|invitee_bonus|rebate_reverse|invitee_bonus_reverse';
