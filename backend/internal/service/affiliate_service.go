package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

var (
	ErrAffiliateProfileNotFound = infraerrors.NotFound("AFFILIATE_PROFILE_NOT_FOUND", "affiliate profile not found")
	ErrAffiliateCodeInvalid     = infraerrors.BadRequest("AFFILIATE_CODE_INVALID", "invalid affiliate code")
	ErrAffiliateCodeTaken       = infraerrors.Conflict("AFFILIATE_CODE_TAKEN", "affiliate code already in use")
	ErrAffiliateAlreadyBound    = infraerrors.Conflict("AFFILIATE_ALREADY_BOUND", "affiliate inviter already bound")
	ErrAffiliateQuotaEmpty      = infraerrors.BadRequest("AFFILIATE_QUOTA_EMPTY", "no affiliate quota available to transfer")
)

const (
	affiliateInviteesLimit = 100
	// AffiliateCodeMinLength / AffiliateCodeMaxLength bound both system-generated
	// 12-char codes and admin-customized codes (e.g. "VIP2026").
	AffiliateCodeMinLength = 4
	AffiliateCodeMaxLength = 32
)

// affiliateCodeValidChar accepts uppercase letters, digits, underscore and dash.
// All input passes through strings.ToUpper before validation, so lowercase from
// users is normalized — admins may supply mixed case in their UI.
var affiliateCodeValidChar = func() [256]bool {
	var tbl [256]bool
	for c := byte('A'); c <= 'Z'; c++ {
		tbl[c] = true
	}
	for c := byte('0'); c <= '9'; c++ {
		tbl[c] = true
	}
	tbl['_'] = true
	tbl['-'] = true
	return tbl
}()

// isValidAffiliateCodeFormat validates code format for both binding (user input)
// and admin updates. Caller is expected to upper-case the input first.
func isValidAffiliateCodeFormat(code string) bool {
	if len(code) < AffiliateCodeMinLength || len(code) > AffiliateCodeMaxLength {
		return false
	}
	for i := 0; i < len(code); i++ {
		if !affiliateCodeValidChar[code[i]] {
			return false
		}
	}
	return true
}

type AffiliateSummary struct {
	UserID               int64     `json:"user_id"`
	AffCode              string    `json:"aff_code"`
	AffCodeCustom        bool      `json:"aff_code_custom"`
	AffRebateRatePercent *float64  `json:"aff_rebate_rate_percent,omitempty"`
	InviterID            *int64    `json:"inviter_id,omitempty"`
	AffCount             int       `json:"aff_count"`
	AffQuota             float64   `json:"aff_quota"`
	AffFrozenQuota       float64   `json:"aff_frozen_quota"`
	AffHistoryQuota      float64   `json:"aff_history_quota"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type AffiliateInvitee struct {
	UserID      int64      `json:"user_id"`
	Email       string     `json:"email"`
	Username    string     `json:"username"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	TotalRebate float64    `json:"total_rebate"`
}

type AffiliateDetail struct {
	UserID          int64   `json:"user_id"`
	AffCode         string  `json:"aff_code"`
	InviterID       *int64  `json:"inviter_id,omitempty"`
	AffCount        int     `json:"aff_count"`
	AffQuota        float64 `json:"aff_quota"`
	AffFrozenQuota  float64 `json:"aff_frozen_quota"`
	AffHistoryQuota float64 `json:"aff_history_quota"`
	// EffectiveRebateRatePercent 是当前用户作为邀请人时实际生效的返利比例：
	// 优先用户自己的专属比例（aff_rebate_rate_percent），否则回退到全局比例。
	// 用于在用户的 /affiliate 页面直观展示「分享后能拿到多少」。
	EffectiveRebateRatePercent   float64             `json:"effective_rebate_rate_percent"`
	RebateRateCustom             bool                `json:"rebate_rate_custom"`
	EffectiveInviteeCount        int                 `json:"effective_invitee_count"`
	NextTierInviteeThreshold     int                 `json:"next_tier_invitee_threshold,omitempty"`
	InviteesUntilNextTier        int                 `json:"invitees_until_next_tier,omitempty"`
	GrowthMode                   AffiliateGrowthMode `json:"growth_mode"`
	InviteeFirstPaymentBonusRate float64             `json:"invitee_first_payment_bonus_rate"`
	TierWindowDays               int                 `json:"tier_window_days"`
	TierRules                    []AffiliateTierRule `json:"tier_rules,omitempty"`
	Invitees                     []AffiliateInvitee  `json:"invitees"`
}

type AffiliateRepository interface {
	EnsureUserAffiliate(ctx context.Context, userID int64) (*AffiliateSummary, error)
	GetAffiliateByCode(ctx context.Context, code string) (*AffiliateSummary, error)
	BindInviter(ctx context.Context, userID, inviterID int64) (bool, error)
	AccrueQuota(ctx context.Context, inviterID, inviteeUserID int64, amount float64, freezeHours int, sourceOrderID *int64) (bool, error)
	GetAccruedRebateFromInvitee(ctx context.Context, inviterID, inviteeUserID int64) (float64, error)
	ThawFrozenQuota(ctx context.Context, userID int64) (float64, error)
	TransferQuotaToBalance(ctx context.Context, userID int64) (float64, float64, error)
	ListInvitees(ctx context.Context, inviterID int64, limit int) ([]AffiliateInvitee, error)

	// 管理端：用户级专属配置
	UpdateUserAffCode(ctx context.Context, userID int64, newCode string) error
	ResetUserAffCode(ctx context.Context, userID int64) (string, error)
	SetUserRebateRate(ctx context.Context, userID int64, ratePercent *float64) error
	BatchSetUserRebateRate(ctx context.Context, userIDs []int64, ratePercent *float64) error
	ListUsersWithCustomSettings(ctx context.Context, filter AffiliateAdminFilter) ([]AffiliateAdminEntry, int64, error)
	ListAffiliateInviteRecords(ctx context.Context, filter AffiliateRecordFilter) ([]AffiliateInviteRecord, int64, error)
	ListAffiliateRebateRecords(ctx context.Context, filter AffiliateRecordFilter) ([]AffiliateRebateRecord, int64, error)
	ListAffiliateTransferRecords(ctx context.Context, filter AffiliateRecordFilter) ([]AffiliateTransferRecord, int64, error)
	GetAffiliateUserOverview(ctx context.Context, userID int64) (*AffiliateUserOverview, error)
}

// AffiliateGrowthRepository is an optional capability used only by tiered_v1.
// Keeping it separate preserves compatibility for legacy repository adapters.
type AffiliateGrowthRepository interface {
	CountEffectivePaidInvitees(ctx context.Context, inviterID int64, since time.Time, minNetPaymentAmount float64) (int, error)
	HasEffectivePaidInvitee(ctx context.Context, inviterID, inviteeUserID int64, since time.Time, minNetPaymentAmount float64) (bool, error)
}

type AffiliateTieredRewardRepository interface {
	ApplyTieredOrderRewards(ctx context.Context, input AffiliateTieredRewardLedgerInput) (AffiliateTieredRewardLedgerResult, error)
}

type AffiliateRewardReversalRepository interface {
	ReverseOrderRewards(ctx context.Context, sourceOrderID int64, refundAmount, orderAmount float64) (AffiliateRewardReversalResult, error)
}

type AffiliateOrderRewardInput struct {
	InviteeUserID    int64
	BaseAmount       float64
	NetPaymentAmount float64
	SourceOrderID    int64
}

type AffiliateOrderRewardResult struct {
	InviterRebateAmount float64
	InviteeBonusAmount  float64
	RatePercent         float64
	EffectiveInvitees   int
}

type AffiliateTieredRewardLedgerInput struct {
	InviterUserID       int64
	InviteeUserID       int64
	SourceOrderID       int64
	BaseAmount          float64
	InviterRebateAmount float64
	InviteeBonusAmount  float64
	RatePercent         float64
	EffectiveInvitees   int
	FreezeHours         int
}

type AffiliateTieredRewardLedgerResult struct {
	InviterRebateApplied bool
	InviteeBonusApplied  bool
}

type AffiliateRewardReversalResult struct {
	InviterUserID         int64
	InviteeUserID         int64
	InviterRebateReversed float64
	InviteeBonusReversed  float64
}

// AffiliateAdminFilter 列表筛选条件
type AffiliateAdminFilter struct {
	Search   string
	Page     int
	PageSize int
}

// AffiliateAdminEntry 专属用户列表条目
type AffiliateAdminEntry struct {
	UserID               int64    `json:"user_id"`
	Email                string   `json:"email"`
	Username             string   `json:"username"`
	AffCode              string   `json:"aff_code"`
	AffCodeCustom        bool     `json:"aff_code_custom"`
	AffRebateRatePercent *float64 `json:"aff_rebate_rate_percent,omitempty"`
	AffCount             int      `json:"aff_count"`
}

type AffiliateRecordFilter struct {
	Search   string
	Page     int
	PageSize int
	StartAt  *time.Time
	EndAt    *time.Time
	SortBy   string
	SortDesc bool
}

type AffiliateInviteRecord struct {
	InviterID       int64     `json:"inviter_id"`
	InviterEmail    string    `json:"inviter_email"`
	InviterUsername string    `json:"inviter_username"`
	InviteeID       int64     `json:"invitee_id"`
	InviteeEmail    string    `json:"invitee_email"`
	InviteeUsername string    `json:"invitee_username"`
	AffCode         string    `json:"aff_code"`
	TotalRebate     float64   `json:"total_rebate"`
	CreatedAt       time.Time `json:"created_at"`
}

type AffiliateRebateRecord struct {
	OrderID         int64     `json:"order_id"`
	OutTradeNo      string    `json:"out_trade_no"`
	InviterID       int64     `json:"inviter_id"`
	InviterEmail    string    `json:"inviter_email"`
	InviterUsername string    `json:"inviter_username"`
	InviteeID       int64     `json:"invitee_id"`
	InviteeEmail    string    `json:"invitee_email"`
	InviteeUsername string    `json:"invitee_username"`
	OrderAmount     float64   `json:"order_amount"`
	PayAmount       float64   `json:"pay_amount"`
	RebateAmount    float64   `json:"rebate_amount"`
	PaymentType     string    `json:"payment_type"`
	OrderStatus     string    `json:"order_status"`
	CreatedAt       time.Time `json:"created_at"`
}

type AffiliateTransferRecord struct {
	LedgerID            int64     `json:"ledger_id"`
	UserID              int64     `json:"user_id"`
	UserEmail           string    `json:"user_email"`
	Username            string    `json:"username"`
	Amount              float64   `json:"amount"`
	BalanceAfter        *float64  `json:"balance_after,omitempty"`
	AvailableQuotaAfter *float64  `json:"available_quota_after,omitempty"`
	FrozenQuotaAfter    *float64  `json:"frozen_quota_after,omitempty"`
	HistoryQuotaAfter   *float64  `json:"history_quota_after,omitempty"`
	SnapshotAvailable   bool      `json:"snapshot_available"`
	CurrentBalance      float64   `json:"-"`
	RemainingQuota      float64   `json:"-"`
	FrozenQuota         float64   `json:"-"`
	HistoryQuota        float64   `json:"-"`
	CreatedAt           time.Time `json:"created_at"`
}

type AffiliateUserOverview struct {
	UserID              int64   `json:"user_id"`
	Email               string  `json:"email"`
	Username            string  `json:"username"`
	AffCode             string  `json:"aff_code"`
	RebateRatePercent   float64 `json:"rebate_rate_percent"`
	RebateRateCustom    bool    `json:"-"`
	InvitedCount        int     `json:"invited_count"`
	RebatedInviteeCount int     `json:"rebated_invitee_count"`
	AvailableQuota      float64 `json:"available_quota"`
	HistoryQuota        float64 `json:"history_quota"`
}

type AffiliateService struct {
	repo                 AffiliateRepository
	settingService       *SettingService
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCacheService  *BillingCacheService
}

func NewAffiliateService(repo AffiliateRepository, settingService *SettingService, authCacheInvalidator APIKeyAuthCacheInvalidator, billingCacheService *BillingCacheService) *AffiliateService {
	return &AffiliateService{
		repo:                 repo,
		settingService:       settingService,
		authCacheInvalidator: authCacheInvalidator,
		billingCacheService:  billingCacheService,
	}
}

// IsEnabled reports whether the affiliate (邀请返利) feature is turned on.
func (s *AffiliateService) IsEnabled(ctx context.Context) bool {
	if s == nil || s.settingService == nil {
		return AffiliateEnabledDefault
	}
	return s.settingService.IsAffiliateEnabled(ctx)
}

func (s *AffiliateService) EnsureUserAffiliate(ctx context.Context, userID int64) (*AffiliateSummary, error) {
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	if s == nil || s.repo == nil {
		return nil, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	return s.repo.EnsureUserAffiliate(ctx, userID)
}

func (s *AffiliateService) GetAffiliateDetail(ctx context.Context, userID int64) (*AffiliateDetail, error) {
	// Lazy thaw: move any matured frozen quota to available before reading.
	if s != nil && s.repo != nil {
		// best-effort: thaw failure is non-fatal
		_, _ = s.repo.ThawFrozenQuota(ctx, userID)
	}

	summary, err := s.EnsureUserAffiliate(ctx, userID)
	if err != nil {
		return nil, err
	}
	invitees, err := s.listInvitees(ctx, userID)
	if err != nil {
		return nil, err
	}
	ratePercent := s.resolveRebateRatePercent(ctx, summary)
	if s.settingService != nil && s.settingService.GetAffiliateGrowthMode(ctx) == AffiliateGrowthModeTieredV1 && summary.AffRebateRatePercent == nil {
		if tierRate, _, err := s.resolveAccrualRebateRatePercent(ctx, summary); err == nil {
			ratePercent = tierRate
		}
	}
	effectiveInvitees, nextThreshold, untilNext := s.resolveGrowthProgress(ctx, summary)
	growthMode := AffiliateGrowthModeLegacy
	bonusRate := AffiliateInviteeBonusRateDefault
	tierWindowDays := AffiliateTierWindowDaysDefault
	if s.settingService != nil {
		growthMode = s.settingService.GetAffiliateGrowthMode(ctx)
		bonusRate = s.settingService.GetAffiliateInviteeBonusRatePercent(ctx)
		tierWindowDays = s.settingService.GetAffiliateTierWindowDays(ctx)
	}
	return &AffiliateDetail{
		UserID:                       summary.UserID,
		AffCode:                      summary.AffCode,
		InviterID:                    summary.InviterID,
		AffCount:                     summary.AffCount,
		AffQuota:                     summary.AffQuota,
		AffFrozenQuota:               summary.AffFrozenQuota,
		AffHistoryQuota:              summary.AffHistoryQuota,
		EffectiveRebateRatePercent:   ratePercent,
		RebateRateCustom:             summary.AffRebateRatePercent != nil,
		EffectiveInviteeCount:        effectiveInvitees,
		NextTierInviteeThreshold:     nextThreshold,
		InviteesUntilNextTier:        untilNext,
		GrowthMode:                   growthMode,
		InviteeFirstPaymentBonusRate: bonusRate,
		TierWindowDays:               tierWindowDays,
		TierRules:                    s.affiliateTierRules(ctx, growthMode),
		Invitees:                     invitees,
	}, nil
}

func (s *AffiliateService) affiliateTierRules(ctx context.Context, mode AffiliateGrowthMode) []AffiliateTierRule {
	if mode != AffiliateGrowthModeTieredV1 || s == nil || s.settingService == nil {
		return nil
	}
	return s.settingService.GetAffiliateTierRules(ctx)
}

func (s *AffiliateService) resolveGrowthProgress(ctx context.Context, inviter *AffiliateSummary) (int, int, int) {
	if s == nil || s.settingService == nil || s.settingService.GetAffiliateGrowthMode(ctx) != AffiliateGrowthModeTieredV1 || inviter == nil || inviter.AffRebateRatePercent != nil {
		return 0, 0, 0
	}
	repo, ok := s.repo.(AffiliateGrowthRepository)
	if !ok {
		return 0, 0, 0
	}
	count, err := repo.CountEffectivePaidInvitees(ctx, inviter.UserID, time.Now().AddDate(0, 0, -s.settingService.GetAffiliateTierWindowDays(ctx)), s.settingService.GetAffiliateEffectivePaymentMinAmount(ctx))
	if err != nil {
		return 0, 0, 0
	}
	policy, err := NewAffiliateGrowthPolicy(s.settingService.GetAffiliateTierRules(ctx))
	if err != nil {
		return count, 0, 0
	}
	for _, rule := range policy.Rules() {
		if rule.MinEffectiveInvitees > count {
			return count, rule.MinEffectiveInvitees, rule.MinEffectiveInvitees - count
		}
	}
	return count, 0, 0
}

func (s *AffiliateService) BindInviterByCode(ctx context.Context, userID int64, rawCode string) error {
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	if code == "" {
		return nil
	}
	if s == nil || s.repo == nil {
		return infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	// 总开关关闭时，注册阶段静默忽略 aff 参数（不报错，避免阻断注册流程）
	if !s.IsEnabled(ctx) {
		return nil
	}
	if !isValidAffiliateCodeFormat(code) {
		return ErrAffiliateCodeInvalid
	}

	selfSummary, err := s.repo.EnsureUserAffiliate(ctx, userID)
	if err != nil {
		return err
	}
	if selfSummary.InviterID != nil {
		return nil
	}

	inviterSummary, err := s.repo.GetAffiliateByCode(ctx, code)
	if err != nil {
		if errors.Is(err, ErrAffiliateProfileNotFound) {
			return ErrAffiliateCodeInvalid
		}
		return err
	}
	if inviterSummary == nil || inviterSummary.UserID <= 0 || inviterSummary.UserID == userID {
		return ErrAffiliateCodeInvalid
	}

	bound, err := s.repo.BindInviter(ctx, userID, inviterSummary.UserID)
	if err != nil {
		return err
	}
	if !bound {
		return ErrAffiliateAlreadyBound
	}
	return nil
}

func (s *AffiliateService) AccrueInviteRebate(ctx context.Context, inviteeUserID int64, baseRechargeAmount float64) (float64, error) {
	return s.AccrueInviteRebateForOrder(ctx, inviteeUserID, baseRechargeAmount, nil)
}

func (s *AffiliateService) ApplyOrderRewards(ctx context.Context, input AffiliateOrderRewardInput) (AffiliateOrderRewardResult, error) {
	var result AffiliateOrderRewardResult
	if s == nil || s.repo == nil || !s.IsEnabled(ctx) {
		return result, nil
	}
	if s.settingService == nil || s.settingService.GetAffiliateGrowthMode(ctx) != AffiliateGrowthModeTieredV1 {
		rebate, err := s.AccrueInviteRebateForOrder(ctx, input.InviteeUserID, input.BaseAmount, &input.SourceOrderID)
		result.InviterRebateAmount = rebate
		return result, err
	}
	if input.InviteeUserID <= 0 || input.SourceOrderID <= 0 || !isFinitePositive(input.BaseAmount) || !isFinitePositive(input.NetPaymentAmount) {
		return result, nil
	}
	if input.NetPaymentAmount < s.settingService.GetAffiliateEffectivePaymentMinAmount(ctx) {
		return result, nil
	}

	inviteeSummary, err := s.repo.EnsureUserAffiliate(ctx, input.InviteeUserID)
	if err != nil {
		return result, err
	}
	if inviteeSummary.InviterID == nil || *inviteeSummary.InviterID <= 0 {
		return result, nil
	}
	inviterSummary, err := s.repo.EnsureUserAffiliate(ctx, *inviteeSummary.InviterID)
	if err != nil {
		return result, err
	}

	ratePercent, effectiveInvitees, err := s.resolveAccrualRebateRatePercent(ctx, inviterSummary)
	if err != nil {
		return result, err
	}
	if inviterSummary.AffRebateRatePercent == nil {
		growthRepo, ok := s.repo.(AffiliateGrowthRepository)
		if !ok {
			return result, infraerrors.ServiceUnavailable("AFFILIATE_GROWTH_REPOSITORY_UNAVAILABLE", "affiliate growth repository unavailable")
		}
		windowStart := time.Now().AddDate(0, 0, -s.settingService.GetAffiliateTierWindowDays(ctx))
		alreadyEffective, lookupErr := growthRepo.HasEffectivePaidInvitee(
			ctx,
			inviterSummary.UserID,
			input.InviteeUserID,
			windowStart,
			s.settingService.GetAffiliateEffectivePaymentMinAmount(ctx),
		)
		if lookupErr != nil {
			return result, lookupErr
		}
		if !alreadyEffective {
			effectiveInvitees++
			policy, policyErr := NewAffiliateGrowthPolicy(s.settingService.GetAffiliateTierRules(ctx))
			if policyErr != nil {
				return result, policyErr
			}
			ratePercent = policy.RatePercent(effectiveInvitees)
		}
	}
	rebateAmount := roundTo(input.BaseAmount*(ratePercent/100), 8)
	if perInviteeCap := s.settingService.GetAffiliateRebatePerInviteeCap(ctx); rebateAmount > 0 && perInviteeCap > 0 {
		existing, getErr := s.repo.GetAccruedRebateFromInvitee(ctx, inviterSummary.UserID, input.InviteeUserID)
		if getErr != nil {
			return result, getErr
		}
		remaining := perInviteeCap - existing
		if remaining <= 0 {
			rebateAmount = 0
		} else if rebateAmount > remaining {
			rebateAmount = roundTo(remaining, 8)
		}
	}
	bonusRate := s.settingService.GetAffiliateInviteeBonusRatePercent(ctx)
	bonusAmount := roundTo(input.BaseAmount*(bonusRate/100), 8)
	if rebateAmount <= 0 && bonusAmount <= 0 {
		return result, nil
	}

	growthRepo, ok := s.repo.(AffiliateTieredRewardRepository)
	if !ok {
		return result, infraerrors.ServiceUnavailable("AFFILIATE_GROWTH_REPOSITORY_UNAVAILABLE", "affiliate growth repository unavailable")
	}
	ledgerResult, err := growthRepo.ApplyTieredOrderRewards(ctx, AffiliateTieredRewardLedgerInput{
		InviterUserID:       inviterSummary.UserID,
		InviteeUserID:       input.InviteeUserID,
		SourceOrderID:       input.SourceOrderID,
		BaseAmount:          input.BaseAmount,
		InviterRebateAmount: rebateAmount,
		InviteeBonusAmount:  bonusAmount,
		RatePercent:         ratePercent,
		EffectiveInvitees:   effectiveInvitees,
		FreezeHours:         s.settingService.GetAffiliateRebateFreezeHours(ctx),
	})
	if err != nil {
		return result, err
	}
	result.RatePercent = ratePercent
	result.EffectiveInvitees = effectiveInvitees
	if ledgerResult.InviterRebateApplied {
		result.InviterRebateAmount = rebateAmount
	}
	if ledgerResult.InviteeBonusApplied {
		result.InviteeBonusAmount = bonusAmount
	}
	return result, nil
}

func (s *AffiliateService) ReverseOrderRewards(ctx context.Context, sourceOrderID int64, refundAmount, orderAmount float64) (AffiliateRewardReversalResult, error) {
	var result AffiliateRewardReversalResult
	if s == nil || s.repo == nil || sourceOrderID <= 0 || !isFinitePositive(refundAmount) || !isFinitePositive(orderAmount) {
		return result, nil
	}
	reversalRepo, ok := s.repo.(AffiliateRewardReversalRepository)
	if !ok {
		return result, nil
	}
	return reversalRepo.ReverseOrderRewards(ctx, sourceOrderID, refundAmount, orderAmount)
}

func isFinitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *AffiliateService) AccrueInviteRebateForOrder(ctx context.Context, inviteeUserID int64, baseRechargeAmount float64, sourceOrderID *int64) (float64, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	if inviteeUserID <= 0 || baseRechargeAmount <= 0 || math.IsNaN(baseRechargeAmount) || math.IsInf(baseRechargeAmount, 0) {
		return 0, nil
	}
	// 总开关关闭时，新充值不再产生返利
	if !s.IsEnabled(ctx) {
		return 0, nil
	}
	if s.settingService != nil && s.settingService.GetAffiliateGrowthMode(ctx) == AffiliateGrowthModeTieredV1 && sourceOrderID == nil {
		// Tiered mode only rewards auditable real-payment orders. Redeem codes and
		// legacy callers without an order id cannot qualify for the new program.
		return 0, nil
	}

	inviteeSummary, err := s.repo.EnsureUserAffiliate(ctx, inviteeUserID)
	if err != nil {
		return 0, err
	}
	if inviteeSummary.InviterID == nil || *inviteeSummary.InviterID <= 0 {
		return 0, nil
	}

	// 加载邀请人 profile，优先使用专属比例（覆盖全局）
	inviterSummary, err := s.repo.EnsureUserAffiliate(ctx, *inviteeSummary.InviterID)
	if err != nil {
		return 0, err
	}
	// 有效期检查：超过返利有效期后不再产生返利
	if s.settingService != nil {
		if durationDays := s.settingService.GetAffiliateRebateDurationDays(ctx); durationDays > 0 {
			if time.Now().After(inviteeSummary.CreatedAt.AddDate(0, 0, durationDays)) {
				return 0, nil
			}
		}
	}

	rebateRatePercent, _, err := s.resolveAccrualRebateRatePercent(ctx, inviterSummary)
	if err != nil {
		return 0, err
	}
	rebate := roundTo(baseRechargeAmount*(rebateRatePercent/100), 8)
	if rebate <= 0 {
		return 0, nil
	}

	// 单人上限检查：精确截断到剩余额度
	if s.settingService != nil {
		if perInviteeCap := s.settingService.GetAffiliateRebatePerInviteeCap(ctx); perInviteeCap > 0 {
			existing, err := s.repo.GetAccruedRebateFromInvitee(ctx, *inviteeSummary.InviterID, inviteeUserID)
			if err != nil {
				return 0, err
			}
			if existing >= perInviteeCap {
				return 0, nil
			}
			if remaining := perInviteeCap - existing; rebate > remaining {
				rebate = roundTo(remaining, 8)
			}
		}
	}

	var freezeHours int
	if s.settingService != nil {
		freezeHours = s.settingService.GetAffiliateRebateFreezeHours(ctx)
	}

	applied, err := s.repo.AccrueQuota(ctx, *inviteeSummary.InviterID, inviteeUserID, rebate, freezeHours, sourceOrderID)
	if err != nil {
		return 0, err
	}
	if !applied {
		return 0, nil
	}
	return rebate, nil
}

func (s *AffiliateService) resolveAccrualRebateRatePercent(ctx context.Context, inviter *AffiliateSummary) (float64, int, error) {
	if inviter != nil && inviter.AffRebateRatePercent != nil {
		v := *inviter.AffRebateRatePercent
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return clampAffiliateRebateRate(v), 0, nil
		}
	}
	if s == nil || s.settingService == nil || s.settingService.GetAffiliateGrowthMode(ctx) != AffiliateGrowthModeTieredV1 {
		return s.globalRebateRatePercent(ctx), 0, nil
	}
	if inviter == nil || inviter.UserID <= 0 {
		return s.globalRebateRatePercent(ctx), 0, nil
	}

	growthRepo, ok := s.repo.(AffiliateGrowthRepository)
	if !ok {
		return 0, 0, infraerrors.ServiceUnavailable("AFFILIATE_GROWTH_REPOSITORY_UNAVAILABLE", "affiliate growth repository unavailable")
	}
	windowDays := s.settingService.GetAffiliateTierWindowDays(ctx)
	effectiveInvitees, err := growthRepo.CountEffectivePaidInvitees(
		ctx,
		inviter.UserID,
		time.Now().AddDate(0, 0, -windowDays),
		s.settingService.GetAffiliateEffectivePaymentMinAmount(ctx),
	)
	if err != nil {
		return 0, 0, err
	}
	policy, err := NewAffiliateGrowthPolicy(s.settingService.GetAffiliateTierRules(ctx))
	if err != nil {
		return 0, 0, err
	}
	return policy.RatePercent(effectiveInvitees), effectiveInvitees, nil
}

// resolveRebateRatePercent returns the inviter's exclusive rate when set,
// otherwise the global setting value (clamped to [Min, Max]).
func (s *AffiliateService) resolveRebateRatePercent(ctx context.Context, inviter *AffiliateSummary) float64 {
	if inviter != nil && inviter.AffRebateRatePercent != nil {
		v := *inviter.AffRebateRatePercent
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return s.globalRebateRatePercent(ctx)
		}
		return clampAffiliateRebateRate(v)
	}
	return s.globalRebateRatePercent(ctx)
}

// globalRebateRatePercent reads the system-wide rebate rate via SettingService,
// returning the documented default when SettingService is unavailable.
func (s *AffiliateService) globalRebateRatePercent(ctx context.Context) float64 {
	if s == nil || s.settingService == nil {
		return AffiliateRebateRateDefault
	}
	return s.settingService.GetAffiliateRebateRatePercent(ctx)
}

func (s *AffiliateService) TransferAffiliateQuota(ctx context.Context, userID int64) (float64, float64, error) {
	if s == nil || s.repo == nil {
		return 0, 0, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}

	transferred, balance, err := s.repo.TransferQuotaToBalance(ctx, userID)
	if err != nil {
		return 0, 0, err
	}
	if transferred > 0 {
		s.invalidateAffiliateCaches(ctx, userID)
	}
	return transferred, balance, nil
}

func (s *AffiliateService) listInvitees(ctx context.Context, inviterID int64) ([]AffiliateInvitee, error) {
	if s == nil || s.repo == nil {
		return nil, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	invitees, err := s.repo.ListInvitees(ctx, inviterID, affiliateInviteesLimit)
	if err != nil {
		return nil, err
	}
	for i := range invitees {
		invitees[i].Email = maskEmail(invitees[i].Email)
	}
	return invitees, nil
}

func roundTo(v float64, scale int) float64 {
	factor := math.Pow10(scale)
	return math.Round(v*factor) / factor
}

func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	at := strings.Index(email, "@")
	if at <= 0 || at >= len(email)-1 {
		return "***"
	}

	local := email[:at]
	domain := email[at+1:]
	dot := strings.LastIndex(domain, ".")

	maskedLocal := maskSegment(local)
	if dot <= 0 || dot >= len(domain)-1 {
		return maskedLocal + "@" + maskSegment(domain)
	}

	domainName := domain[:dot]
	tld := domain[dot:]
	return maskedLocal + "@" + maskSegment(domainName) + tld
}

func maskSegment(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return "***"
	}
	if len(r) == 1 {
		return string(r[0]) + "***"
	}
	return string(r[0]) + "***"
}

func (s *AffiliateService) invalidateAffiliateCaches(ctx context.Context, userID int64) {
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCacheService != nil {
		if err := s.billingCacheService.InvalidateUserBalance(ctx, userID); err != nil {
			logger.LegacyPrintf("service.affiliate", "[Affiliate] Failed to invalidate billing cache for user %d: %v", userID, err)
		}
	}
}

// =========================
// Admin: 专属配置管理
// =========================

// validateExclusiveRate ensures a per-user override is finite and within
// [Min, Max]. nil is always valid (means "clear / fall back to global").
func validateExclusiveRate(ratePercent *float64) error {
	if ratePercent == nil {
		return nil
	}
	v := *ratePercent
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return infraerrors.BadRequest("INVALID_RATE", "invalid rebate rate")
	}
	if v < AffiliateRebateRateMin || v > AffiliateRebateRateMax {
		return infraerrors.BadRequest("INVALID_RATE", "rebate rate out of range")
	}
	return nil
}

// AdminUpdateUserAffCode 管理员改写用户的邀请码（专属邀请码）。
func (s *AffiliateService) AdminUpdateUserAffCode(ctx context.Context, userID int64, rawCode string) error {
	if s == nil || s.repo == nil {
		return infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	if !isValidAffiliateCodeFormat(code) {
		return ErrAffiliateCodeInvalid
	}
	return s.repo.UpdateUserAffCode(ctx, userID, code)
}

// AdminResetUserAffCode 重置用户邀请码为系统随机码。
func (s *AffiliateService) AdminResetUserAffCode(ctx context.Context, userID int64) (string, error) {
	if s == nil || s.repo == nil {
		return "", infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	return s.repo.ResetUserAffCode(ctx, userID)
}

// AdminSetUserRebateRate 设置/清除用户专属返利比例。ratePercent==nil 表示清除。
func (s *AffiliateService) AdminSetUserRebateRate(ctx context.Context, userID int64, ratePercent *float64) error {
	if s == nil || s.repo == nil {
		return infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	if err := validateExclusiveRate(ratePercent); err != nil {
		return err
	}
	return s.repo.SetUserRebateRate(ctx, userID, ratePercent)
}

// AdminBatchSetUserRebateRate 批量设置/清除用户专属返利比例。
func (s *AffiliateService) AdminBatchSetUserRebateRate(ctx context.Context, userIDs []int64, ratePercent *float64) error {
	if s == nil || s.repo == nil {
		return infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	if err := validateExclusiveRate(ratePercent); err != nil {
		return err
	}
	cleaned := make([]int64, 0, len(userIDs))
	for _, uid := range userIDs {
		if uid > 0 {
			cleaned = append(cleaned, uid)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return s.repo.BatchSetUserRebateRate(ctx, cleaned, ratePercent)
}

// AdminListCustomUsers 列出有专属配置的用户。
func (s *AffiliateService) AdminListCustomUsers(ctx context.Context, filter AffiliateAdminFilter) ([]AffiliateAdminEntry, int64, error) {
	if s == nil || s.repo == nil {
		return nil, 0, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	return s.repo.ListUsersWithCustomSettings(ctx, filter)
}

func (s *AffiliateService) AdminListInviteRecords(ctx context.Context, filter AffiliateRecordFilter) ([]AffiliateInviteRecord, int64, error) {
	if s == nil || s.repo == nil {
		return nil, 0, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	return s.repo.ListAffiliateInviteRecords(ctx, normalizeAffiliateRecordFilter(filter))
}

func (s *AffiliateService) AdminListRebateRecords(ctx context.Context, filter AffiliateRecordFilter) ([]AffiliateRebateRecord, int64, error) {
	if s == nil || s.repo == nil {
		return nil, 0, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	return s.repo.ListAffiliateRebateRecords(ctx, normalizeAffiliateRecordFilter(filter))
}

func (s *AffiliateService) AdminListTransferRecords(ctx context.Context, filter AffiliateRecordFilter) ([]AffiliateTransferRecord, int64, error) {
	if s == nil || s.repo == nil {
		return nil, 0, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	return s.repo.ListAffiliateTransferRecords(ctx, normalizeAffiliateRecordFilter(filter))
}

func (s *AffiliateService) AdminGetUserOverview(ctx context.Context, userID int64) (*AffiliateUserOverview, error) {
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	if s == nil || s.repo == nil {
		return nil, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "affiliate service unavailable")
	}
	overview, err := s.repo.GetAffiliateUserOverview(ctx, userID)
	if err != nil {
		return nil, err
	}
	if overview != nil {
		if !overview.RebateRateCustom {
			overview.RebateRatePercent = s.globalRebateRatePercent(ctx)
		}
		overview.RebateRatePercent = clampAffiliateRebateRate(overview.RebateRatePercent)
	}
	return overview, nil
}

func normalizeAffiliateRecordFilter(filter AffiliateRecordFilter) AffiliateRecordFilter {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	filter.Search = strings.TrimSpace(filter.Search)
	filter.SortBy = strings.TrimSpace(filter.SortBy)
	return filter
}
