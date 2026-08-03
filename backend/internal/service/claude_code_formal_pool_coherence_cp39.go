package service

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ClaudeCodeRuntimeMappingArtifactCheck struct {
	OK     bool   `json:"ok"`
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}

type claudeCodeRuntimeMappingArtifact struct {
	AccountRef   string `json:"account_ref"`
	EgressBucket string `json:"egress_bucket"`
	DeviceRef    string `json:"device_ref"`
	SessionRef   string `json:"session_ref"`
}

func CheckClaudeCodeRuntimeMappingArtifact(path string) ClaudeCodeRuntimeMappingArtifactCheck {
	path = strings.TrimSpace(path)
	if path == "" {
		return ClaudeCodeRuntimeMappingArtifactCheck{Status: "fail", Code: "missing_path"}
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ClaudeCodeRuntimeMappingArtifactCheck{Status: "fail", Code: "missing_file"}
	}
	if dirInfo, err := os.Stat(filepath.Dir(path)); err != nil || !dirInfo.IsDir() || dirInfo.Mode().Perm() != 0o700 {
		return ClaudeCodeRuntimeMappingArtifactCheck{Status: "fail", Code: "unsafe_directory_permissions"}
	}
	if info.Mode().Perm() != 0o600 {
		return ClaudeCodeRuntimeMappingArtifactCheck{Status: "fail", Code: "unsafe_file_permissions"}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return ClaudeCodeRuntimeMappingArtifactCheck{Status: "fail", Code: "read_failed"}
	}
	var artifact claudeCodeRuntimeMappingArtifact
	if err := json.Unmarshal(body, &artifact); err != nil {
		return ClaudeCodeRuntimeMappingArtifactCheck{Status: "fail", Code: "invalid_shape"}
	}
	if !isSafeLedgerRef(artifact.AccountRef) || strings.TrimSpace(artifact.EgressBucket) == "" || !isSafeLedgerRef(artifact.DeviceRef) || !isSafeLedgerRef(artifact.SessionRef) {
		return ClaudeCodeRuntimeMappingArtifactCheck{Status: "fail", Code: "invalid_shape"}
	}
	return ClaudeCodeRuntimeMappingArtifactCheck{OK: true, Status: "pass"}
}

type FormalPoolNativeHardGateInput struct {
	Account         *Account
	RawSessionID    string
	RawUserID       string
	Gate            string
	Status          int
	UnsafeRawReason string
}

func BuildFormalPoolNativeHardGateRiskRecord(input FormalPoolNativeHardGateInput) SessionBudgetObserveRecord {
	action := formalPoolNativeHardGateAction(input.Gate, input.Status)
	kind := formalPoolNativeHardGateKind(input.Gate)
	risk, err := BuildRiskEventLedgerEntry(RiskEventLedgerInput{
		Kind:            kind,
		Severity:        RiskSeverityP0,
		RawSessionID:    input.RawSessionID,
		RawUserID:       input.RawUserID,
		RawAccountID:    accountBudgetRawID(input.Account),
		UnsafeRawReason: input.Gate + ":" + input.UnsafeRawReason,
		ObservedAt:      time.Now().UTC(),
		Recommendation:  action,
	})
	if err != nil {
		return SessionBudgetObserveRecord{}
	}
	accountEntry, _ := BuildAccountBudgetLedgerEntry(AccountBudgetLedgerInput{
		Account:              input.Account,
		ResponseHeaders:      http.Header{},
		SchedulingWeight:     1,
		LastRiskEventSummary: input.Gate,
	})
	decision := BudgetDecision{
		Mode:          BudgetModeObserveOnly,
		Action:        action,
		AccountWeight: 0,
		QueuePriority: 0,
		ReasonCode:    sanitizeReasonCode(input.Gate),
		SafeSummary: map[string]any{
			"gate":        sanitizeReasonCode(input.Gate),
			"status":      statusBucketFromHTTP(input.Status),
			"account_ref": safeDecisionRef(accountEntry.AccountRef),
		},
	}
	record := SessionBudgetObserveRecord{
		Account:    accountEntry,
		RiskEvents: []RiskEventLedgerEntry{risk},
		Decision:   decision,
	}
	if ValidateNoRawSensitiveLedger(record) != nil {
		return SessionBudgetObserveRecord{}
	}
	return record
}

func formalPoolNativeHardGateAction(gate string, status int) string {
	gate = strings.ToLower(strings.TrimSpace(gate))
	if status == http.StatusTooManyRequests || strings.Contains(gate, "429") {
		return BudgetActionCooldown
	}
	return BudgetActionP0Block
}

func formalPoolNativeHardGateKind(gate string) string {
	gate = strings.ToLower(strings.TrimSpace(gate))
	switch {
	case strings.Contains(gate, "proxy"):
		return RiskEventKindProxyMismatch
	case strings.Contains(gate, "shape") || strings.Contains(gate, "verifier"):
		return RiskEventKindVerifierFail
	default:
		return RiskEventKindIdentityBoundaryFail
	}
}
