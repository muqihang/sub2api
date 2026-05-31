package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type AccountQuarantineInput struct {
	AccountID  int64
	Kind       string
	Reason     string
	Source     string
	StatusCode int
}

type AccountQuarantineService struct {
	accountRepo AccountRepository
	sink        SessionBudgetObserveSink
	now         func() time.Time
}

func NewAccountQuarantineService(repo AccountRepository, sink SessionBudgetObserveSink) *AccountQuarantineService {
	return &AccountQuarantineService{accountRepo: repo, sink: sink, now: time.Now}
}

func (s *AccountQuarantineService) Quarantine(ctx context.Context, input AccountQuarantineInput) (RiskEventLedgerEntry, error) {
	if s == nil || s.accountRepo == nil {
		return RiskEventLedgerEntry{}, fmt.Errorf("account quarantine service unavailable")
	}
	if input.AccountID <= 0 {
		return RiskEventLedgerEntry{}, fmt.Errorf("account quarantine requires account id")
	}
	account, err := s.accountRepo.GetByID(ctx, input.AccountID)
	if err != nil {
		return RiskEventLedgerEntry{}, err
	}
	if account == nil {
		return RiskEventLedgerEntry{}, ErrAccountNotFound
	}
	kind := sanitizeReasonCode(input.Kind)
	if kind == "" {
		kind = RiskEventKindIdentityBoundaryFail
	}
	now := s.now()
	risk, err := BuildRiskEventLedgerEntry(RiskEventLedgerInput{
		Kind:            kind,
		Severity:        RiskSeverityP0,
		RawAccountID:    strconv.FormatInt(account.ID, 10),
		UnsafeRawReason: input.Reason,
		ObservedAt:      now,
		Recommendation:  BudgetActionQuarantine,
	})
	if err != nil {
		return RiskEventLedgerEntry{}, err
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	stamp := formalPoolTimestamp(now)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageQuarantined
	account.Extra[FormalPoolExtraOnboardingStageUpdatedAt] = stamp
	account.Extra[FormalPoolExtraOnboardingLastCheck] = sanitizeReasonCode(input.Source)
	account.Extra[FormalPoolExtraOnboardingLastCheckAt] = stamp
	account.Extra[FormalPoolExtraOnboardingLastErrorCode] = kind
	account.Extra[FormalPoolExtraOnboardingLastErrorBucket] = statusBucketFromHTTP(input.StatusCode)
	account.Extra[FormalPoolExtraHealthcheckStatus] = "quarantined"
	account.Extra[FormalPoolExtraRiskEventRef] = risk.AccountRef + ":" + risk.Timestamp
	account.Extra[FormalPoolExtraQuarantineReason] = reasonBucket(input.Reason)
	account.Extra[FormalPoolExtraQuarantineAt] = stamp
	account.Schedulable = false
	account.Status = StatusError
	if strings.TrimSpace(account.ErrorMessage) == "" {
		account.ErrorMessage = reasonBucket(input.Reason)
	}
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return RiskEventLedgerEntry{}, err
	}
	if s.sink != nil {
		s.sink.ObserveSessionBudget(ctx, SessionBudgetObserveRecord{RiskEvents: []RiskEventLedgerEntry{risk}})
	}
	return risk, nil
}

func FormalPoolShouldQuarantineHTTPStatus(status int, body []byte) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	msg := strings.ToLower(extractUpstreamErrorMessage(body) + " " + string(body))
	if status == http.StatusBadGateway || status == http.StatusUnprocessableEntity {
		return strings.Contains(msg, "missing_account_identity") ||
			strings.Contains(msg, "missing_identity") ||
			strings.Contains(msg, "egress_proxy_failure") ||
			strings.Contains(msg, "proxy_mismatch") ||
			strings.Contains(msg, "proxy mismatch") ||
			strings.Contains(msg, "fallback") ||
			strings.Contains(msg, "verifier") ||
			strings.Contains(msg, "risk")
	}
	return strings.Contains(msg, "unusual activity") ||
		strings.Contains(msg, "account is on hold") ||
		strings.Contains(msg, "kyc") ||
		strings.Contains(msg, "risk") ||
		strings.Contains(msg, "proxy_mismatch") ||
		strings.Contains(msg, "proxy mismatch") ||
		strings.Contains(msg, "fallback") ||
		strings.Contains(msg, "verifier")
}
