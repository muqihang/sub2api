package service

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SessionBudgetObserveRecord struct {
	Session    SessionBudgetLedgerEntry    `json:"session"`
	Account    AccountBudgetLedgerEntry    `json:"account"`
	User       UserBudgetLedgerEntry       `json:"user"`
	Pool       PoolUtilizationBudgetLedger `json:"pool"`
	RiskEvents []RiskEventLedgerEntry      `json:"risk_events,omitempty"`
	Decision   BudgetDecision              `json:"decision"`
}

type SessionBudgetObserveSink interface {
	ObserveSessionBudget(ctx context.Context, record SessionBudgetObserveRecord)
}

type InMemorySessionBudgetObserveSink struct {
	mu      sync.Mutex
	max     int
	records []SessionBudgetObserveRecord
}

type FileSessionBudgetObserveSink struct {
	mu   sync.Mutex
	path string
}

func NewInMemorySessionBudgetObserveSink(max int) *InMemorySessionBudgetObserveSink {
	if max <= 0 {
		max = 1024
	}
	return &InMemorySessionBudgetObserveSink{max: max}
}

func (s *InMemorySessionBudgetObserveSink) ObserveSessionBudget(_ context.Context, record SessionBudgetObserveRecord) {
	if s == nil || ValidateNoRawSensitiveLedger(record) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.records) >= s.max {
		copy(s.records, s.records[1:])
		s.records[len(s.records)-1] = record
		return
	}
	s.records = append(s.records, record)
}

func NewFileSessionBudgetObserveSink(path string) (*FileSessionBudgetObserveSink, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	return &FileSessionBudgetObserveSink{path: path}, nil
}

func (s *FileSessionBudgetObserveSink) ObserveSessionBudget(_ context.Context, record SessionBudgetObserveRecord) {
	if s == nil || ValidateNoRawSensitiveLedger(record) != nil {
		return
	}
	line, err := json.Marshal(record)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(line, '\n'))
}

type sessionBudgetRequestObservation struct {
	session  SessionBudgetLedgerEntry
	decision BudgetDecision
}

func (s *GatewayService) observeSessionBudgetRequest(ctx context.Context, account *Account, parsed *ParsedRequest, body []byte, reqStream bool) sessionBudgetRequestObservation {
	if s == nil || s.sessionBudgetObserve == nil || parsed == nil {
		return sessionBudgetRequestObservation{}
	}
	entry, decision, err := BuildSessionBudgetLedgerEntry(SessionBudgetLedgerInput{
		RawSessionID:    parsed.MetadataUserID,
		RawUserID:       parsed.MetadataUserID,
		RawAccountID:    accountBudgetRawID(account),
		RequestBody:     body,
		RequestIsStream: reqStream,
		VerifierResult:  BudgetVerifierPass,
		FallbackResult:  BudgetFallbackFalse,
		StatusBucket:    "request_received",
	})
	if err != nil {
		return sessionBudgetRequestObservation{}
	}
	if accountRef := safeAccountRef(account); isSafeLedgerRef(accountRef) {
		entry.AccountRef = accountRef
	}
	decision.SafeSummary["account_ref"] = safeDecisionRef(entry.AccountRef)
	return sessionBudgetRequestObservation{session: entry, decision: decision}
}

func (s *GatewayService) observeSessionBudgetResponse(ctx context.Context, account *Account, req sessionBudgetRequestObservation, headers http.Header, status int, responseBody []byte) {
	if s == nil || s.sessionBudgetObserve == nil || req.session.SessionRef == "" {
		return
	}
	accountEntry, err := BuildAccountBudgetLedgerEntry(AccountBudgetLedgerInput{
		Account:              account,
		ResponseHeaders:      headers,
		SchedulingWeight:     1,
		LastRiskEventSummary: statusRiskReason(status, responseBody),
	})
	if err != nil {
		return
	}
	sessionEntry := req.session
	sessionEntry.StatusBucket = statusBucketFromHTTP(status)
	pool := BuildPoolUtilizationBudgetLedger(PoolUtilizationBudgetInput{
		Profile:        accountEntry.PoolProfile,
		Utilization7d:  utilizationFromBucket(accountEntry.Utilization7dPercentageBucket),
		WindowAge:      poolWindowAgeFromReset(accountEntry.PoolProfile, headers),
		CooldownActive: status == http.StatusTooManyRequests || accountEntry.CooldownState != "none",
	})
	risks := buildBudgetRiskEvents(account, sessionEntry, accountEntry, status, responseBody)
	decision := BuildBudgetDecision(BudgetDecisionInput{
		Session:                   sessionEntry,
		Account:                   accountEntry,
		Pool:                      pool,
		RiskEvents:                risks,
		UpstreamStatus:            status,
		CooldownActive:            status == http.StatusTooManyRequests || accountEntry.CooldownState != "none",
		UtilizationHeadersPresent: utilizationHeadersPresent(headers),
	})
	record := SessionBudgetObserveRecord{Session: sessionEntry, Account: accountEntry, Pool: pool, RiskEvents: risks, Decision: decision}
	record.User = UserBudgetLedgerEntry{UserRef: sessionEntry.UserRef, ActiveSessionCountBucket: "unknown", UsageShareBucket: "unknown", RiskScoreBucket: "unknown"}
	if ValidateNoRawSensitiveLedger(record) != nil {
		return
	}
	s.sessionBudgetObserve.ObserveSessionBudget(ctx, record)
}

func accountBudgetRawID(account *Account) string {
	if account == nil || account.ID <= 0 {
		return "unknown"
	}
	return strconv.FormatInt(account.ID, 10)
}

func utilizationHeadersPresent(headers http.Header) bool {
	if headers == nil {
		return false
	}
	return strings.TrimSpace(headers.Get("anthropic-ratelimit-unified-5h-utilization")) != "" && strings.TrimSpace(headers.Get("anthropic-ratelimit-unified-7d-utilization")) != ""
}

func utilizationFromBucket(bucket string) float64 {
	switch bucket {
	case "pct_0_10":
		return 0.05
	case "pct_10_20":
		return 0.15
	case "pct_20_30":
		return 0.25
	case "pct_30_40":
		return 0.35
	case "pct_40_50":
		return 0.45
	case "pct_50_60":
		return 0.55
	case "pct_60_70":
		return 0.65
	case "pct_70_80":
		return 0.75
	case "pct_80_90":
		return 0.85
	case "pct_90_95":
		return 0.925
	case "pct_95_100":
		return 0.975
	case "pct_100":
		return 1
	default:
		return 0
	}
}

func statusRiskReason(status int, body []byte) string {
	if status == http.StatusTooManyRequests {
		return "cooldown"
	}
	if status == http.StatusForbidden || status == http.StatusUnauthorized {
		return string(body)
	}
	return ""
}

func poolWindowAgeFromReset(profile string, headers http.Header) time.Duration {
	const resetWindowDays = 7
	if headers == nil {
		return time.Duration(resetWindowDays) * 24 * time.Hour
	}
	resetRaw := strings.TrimSpace(headers.Get("anthropic-ratelimit-unified-7d-reset"))
	if resetRaw == "" {
		return time.Duration(resetWindowDays) * 24 * time.Hour
	}
	reset, ok := parseAnthropicUnifiedReset(resetRaw)
	if !ok {
		return time.Duration(resetWindowDays) * 24 * time.Hour
	}
	remaining := time.Until(reset)
	total := time.Duration(resetWindowDays) * 24 * time.Hour
	age := total - remaining
	if age < 0 {
		return 0
	}
	if age > total {
		return total
	}
	return age
}

func statusBucketFromHTTP(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "status_2xx"
	case status == http.StatusTooManyRequests:
		return "status_429"
	case status == http.StatusForbidden:
		return "status_403"
	case status == http.StatusUnauthorized:
		return "status_401"
	case status >= 500:
		return "status_5xx"
	case status >= 400:
		return "status_4xx"
	default:
		return "status_unknown"
	}
}

func buildBudgetRiskEvents(account *Account, session SessionBudgetLedgerEntry, accountEntry AccountBudgetLedgerEntry, status int, body []byte) []RiskEventLedgerEntry {
	if status != http.StatusForbidden && status != http.StatusUnauthorized {
		return nil
	}
	msg := strings.ToLower(extractUpstreamErrorMessage(body) + " " + string(body))
	kind := RiskEventKindRiskText
	action := BudgetActionQuarantine
	if !strings.Contains(msg, "risk") && !strings.Contains(msg, "kyc") && !strings.Contains(msg, "unusual") {
		kind = RiskEventKindIdentityBoundaryFail
		action = BudgetActionP0Block
	}
	ev := RiskEventLedgerEntry{
		Kind:                 kind,
		Severity:             RiskSeverityP0,
		SessionRef:           session.SessionRef,
		UserRef:              session.UserRef,
		AccountRef:           accountEntry.AccountRef,
		SafeReason:           reasonBucket(string(body)),
		Timestamp:            time.Now().UTC().Format(time.RFC3339),
		ActionRecommendation: action,
	}
	if ValidateNoRawSensitiveLedger(ev) != nil {
		return nil
	}
	return []RiskEventLedgerEntry{ev}
}
