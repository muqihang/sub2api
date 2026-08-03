package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type formalPoolRiskEventCaptureSink struct {
	records []SessionBudgetObserveRecord
}

func (s *formalPoolRiskEventCaptureSink) ObserveSessionBudget(_ context.Context, record SessionBudgetObserveRecord) {
	s.records = append(s.records, record)
}

func TestFormalPoolRiskEventWriterRecordsSafeEvents(t *testing.T) {
	ctx := context.Background()
	sink := &formalPoolRiskEventCaptureSink{}
	writer := NewFormalPoolRiskEventWriter(sink)
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	raw := FormalPoolRiskEventInput{
		RawSessionID:       "123e4567-e89b-12d3-a456-426614174000",
		RawUserID:          "user@example.test",
		RawAccountID:       "account-42",
		UnsafeRawReason:    "nonce raw-nonce-123 from 198.51.100.7 user@example.test Bearer tokensecret123 proxy_credential http://user:pass@proxy.local:8080 123e4567-e89b-12d3-a456-426614174000",
		ObservedAt:         now,
		SafeContextBuckets: []string{"proxy", "198.51.100.7", "raw-nonce-123", "Bearer tokensecret123"},
	}

	calls := []struct {
		name string
		kind string
		call func() error
	}{
		{name: "egress verified", kind: "formal_pool_egress_verified", call: func() error { return writer.RecordEgressVerified(ctx, raw) }},
		{name: "egress mismatch", kind: "formal_pool_egress_mismatch", call: func() error { return writer.RecordEgressMismatch(ctx, raw) }},
		{name: "nonce expired", kind: "formal_pool_nonce_expired", call: func() error { return writer.RecordNonceExpired(ctx, raw) }},
		{name: "egress no proxy", kind: "formal_pool_egress_no_proxy", call: func() error { return writer.RecordEgressNoProxy(ctx, raw) }},
		{name: "rate limited", kind: "formal_pool_public_route_rate_limited", call: func() error { return writer.RecordPublicRouteRateLimited(ctx, raw) }},
	}

	for _, tc := range calls {
		if err := tc.call(); err != nil {
			t.Fatalf("%s returned error: %v", tc.name, err)
		}
		if len(sink.records) == 0 {
			t.Fatalf("%s did not write a record", tc.name)
		}
		record := sink.records[len(sink.records)-1]
		if len(record.RiskEvents) != 1 {
			t.Fatalf("%s risk event count = %d, want 1", tc.name, len(record.RiskEvents))
		}
		event := record.RiskEvents[0]
		if event.Kind != tc.kind {
			t.Fatalf("%s kind = %q, want %q", tc.name, event.Kind, tc.kind)
		}
		if event.SafeReason == "" || strings.Contains(event.SafeReason, "raw-nonce") || strings.Contains(event.SafeReason, "198.51.100.7") {
			t.Fatalf("%s unsafe safe reason: %q", tc.name, event.SafeReason)
		}
		if err := ValidateNoRawSensitiveLedger(event); err != nil {
			t.Fatalf("%s event is unsafe: %v", tc.name, err)
		}
		assertFormalPoolRecordSafe(t, record)
	}
}

func TestFormalPoolRiskEventWriterRecordPublicRouteRateLimitedSupportsOrphanEvent(t *testing.T) {
	ctx := context.Background()
	sink := &formalPoolRiskEventCaptureSink{}
	writer := NewFormalPoolRiskEventWriter(sink)

	input := FormalPoolRiskEventInput{
		UnsafeRawReason:    "nonce raw-orphan-nonce from 203.0.113.9 email orphan@example.test token Bearer orphansecret123 proxy_credential socks5://user:pass@127.0.0.1:1080 123e4567-e89b-12d3-a456-426614174000",
		SafeContextBuckets: []string{"per_ip", "203.0.113.9", "raw-orphan-nonce", "orphan@example.test"},
	}
	if err := writer.RecordPublicRouteRateLimited(ctx, input); err != nil {
		t.Fatalf("RecordPublicRouteRateLimited returned error: %v", err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("record count = %d, want 1", len(sink.records))
	}
	record := sink.records[0]
	if record.RiskEvents[0].SessionRef == "" || record.RiskEvents[0].AccountRef == "" {
		t.Fatalf("orphan event should write safe anonymous refs: %+v", record.RiskEvents[0])
	}
	if len(record.RiskEvents) != 1 || record.RiskEvents[0].Kind != "formal_pool_public_route_rate_limited" {
		t.Fatalf("unexpected orphan risk events: %+v", record.RiskEvents)
	}
	assertFormalPoolRecordSafe(t, record)
}

func TestFormalPoolRiskEventWriterPreservesSafeBucketsForNonOrphanEvent(t *testing.T) {
	ctx := context.Background()
	sink := &formalPoolRiskEventCaptureSink{}
	writer := NewFormalPoolRiskEventWriter(sink)

	input := FormalPoolRiskEventInput{
		RawSessionID:       "safe-session-source",
		RawAccountID:       "account-99",
		SafeReasonCode:     "egress_mismatch",
		SafeContextBuckets: []string{"browser_bucket_a7f2c3d4", "proxy_bucket_b91c0d8e", "per_nonce", "nonce_expired"},
	}
	if err := writer.RecordEgressMismatch(ctx, input); err != nil {
		t.Fatalf("RecordEgressMismatch returned error: %v", err)
	}
	if len(sink.records) != 1 || len(sink.records[0].RiskEvents) != 1 {
		t.Fatalf("unexpected records: %+v", sink.records)
	}
	text := marshalFormalPoolRecord(t, sink.records[0])
	for _, want := range []string{"egress_mismatch", "browser_bucket_a7f2c3d4", "proxy_bucket_b91c0d8e", "per_nonce", "nonce_expired"} {
		if !strings.Contains(text, want) {
			t.Fatalf("marshaled ledger missing safe bucket %q: %s", want, text)
		}
	}
}

func TestFormalPoolRiskEventWriterFiltersRawNetworkIdentifiers(t *testing.T) {
	ctx := context.Background()
	sink := &formalPoolRiskEventCaptureSink{}
	writer := NewFormalPoolRiskEventWriter(sink)

	input := FormalPoolRiskEventInput{
		RawSessionID:       "session-raw",
		RawAccountID:       "account-raw",
		SafeReasonCode:     "network_identifier_filter",
		SafeContextBuckets: []string{"192.0.2.9", "2001:db8::1", "198.51.100.0/24", "proxy.example.test:8080", "browser_bucket_a7f2c3d4"},
	}
	if err := writer.RecordEgressMismatch(ctx, input); err != nil {
		t.Fatalf("RecordEgressMismatch returned error: %v", err)
	}
	text := marshalFormalPoolRecord(t, sink.records[0])
	for _, forbidden := range []string{"192.0.2.9", "2001:db8::1", "198.51.100.0/24", "proxy.example.test:8080"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("marshaled ledger contains raw network identifier %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "browser_bucket_a7f2c3d4") {
		t.Fatalf("safe bucket should remain after filtering raw networks: %s", text)
	}
}

func TestFormalPoolRiskEventWriterRejectsMutatedBucketIdentifiers(t *testing.T) {
	ctx := context.Background()
	sink := &formalPoolRiskEventCaptureSink{}
	writer := NewFormalPoolRiskEventWriter(sink)

	input := FormalPoolRiskEventInput{
		RawSessionID:       "safe-session-source",
		RawAccountID:       "account-safe",
		SafeReasonCode:     "reason_upstream_401_api_example_test",
		NonceBucket:        "nonce_bucket_raw_nonce_123",
		IPBucket:           "ip_bucket_198_51_100_7",
		SafeContextBuckets: []string{"browser_bucket_api_example_test", "proxy_bucket_user_pass_proxy_local_8080"},
	}
	if err := writer.RecordEgressMismatch(ctx, input); err != nil {
		t.Fatalf("RecordEgressMismatch returned error: %v", err)
	}
	if len(sink.records) != 1 || len(sink.records[0].RiskEvents) != 1 {
		t.Fatalf("unexpected records: %+v", sink.records)
	}
	text := marshalFormalPoolRecord(t, sink.records[0])
	for _, forbidden := range []string{
		"ip_bucket_198_51_100_7",
		"nonce_bucket_raw_nonce_123",
		"browser_bucket_api_example_test",
		"proxy_bucket_user_pass_proxy_local_8080",
		"reason_upstream_401_api_example_test",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("marshaled ledger contains mutated raw identifier bucket %q: %s", forbidden, text)
		}
	}
	if got := sink.records[0].RiskEvents[0].SafeReason; got != "reason_other" {
		t.Fatalf("unsafe non-public SafeReasonCode should fall back to reason_other, got %q", got)
	}
}

func TestFormalPoolRiskEventWriterPreservesHexBuckets(t *testing.T) {
	ctx := context.Background()
	sink := &formalPoolRiskEventCaptureSink{}
	writer := NewFormalPoolRiskEventWriter(sink)

	input := FormalPoolRiskEventInput{
		RawSessionID:       "safe-session-source",
		RawAccountID:       "account-safe",
		SafeReasonCode:     "egress_mismatch",
		NonceBucket:        "nonce_bucket_a1b2c3d4",
		IPBucket:           "ip_bucket_09af12cd",
		SafeContextBuckets: []string{"browser_bucket_abcdef12", "proxy_bucket_1234abcd"},
	}
	if err := writer.RecordEgressMismatch(ctx, input); err != nil {
		t.Fatalf("RecordEgressMismatch returned error: %v", err)
	}
	if len(sink.records) != 1 || len(sink.records[0].RiskEvents) != 1 {
		t.Fatalf("unexpected records: %+v", sink.records)
	}
	text := marshalFormalPoolRecord(t, sink.records[0])
	for _, want := range []string{
		"nonce_bucket_a1b2c3d4",
		"ip_bucket_09af12cd",
		"browser_bucket_abcdef12",
		"proxy_bucket_1234abcd",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("marshaled ledger missing hex bucket %q: %s", want, text)
		}
	}
}

func TestFormalPoolRiskEventWriterRecordPublicRouteRateLimitedBuckets(t *testing.T) {
	ctx := context.Background()
	sink := &formalPoolRiskEventCaptureSink{}
	writer := NewFormalPoolRiskEventWriter(sink)

	if err := writer.RecordPublicRouteRateLimitedBuckets(ctx, "nonce_bucket_c0a8f00d", "ip_bucket_09af12cd", "per_nonce"); err != nil {
		t.Fatalf("RecordPublicRouteRateLimitedBuckets returned error: %v", err)
	}
	if len(sink.records) != 1 || len(sink.records[0].RiskEvents) != 1 {
		t.Fatalf("unexpected records: %+v", sink.records)
	}
	record := sink.records[0]
	event := record.RiskEvents[0]
	if event.SessionRef == "" || event.AccountRef == "" {
		t.Fatalf("orphan event should write safe anonymous refs: %+v", event)
	}
	text := marshalFormalPoolRecord(t, record)
	for _, want := range []string{"orphan", "nonce_bucket_c0a8f00d", "ip_bucket_09af12cd", "per_nonce"} {
		if !strings.Contains(text, want) {
			t.Fatalf("marshaled ledger missing public route bucket %q: %s", want, text)
		}
	}
}

func TestFormalPoolRiskEventWriterNilSinkAndNilWriterAreNoop(t *testing.T) {
	ctx := context.Background()
	if err := NewFormalPoolRiskEventWriter(nil).RecordEgressVerified(ctx, FormalPoolRiskEventInput{SafeReasonCode: "noop"}); err != nil {
		t.Fatalf("nil sink writer returned error: %v", err)
	}
	var writer *sessionBudgetFormalPoolRiskEventWriter
	if err := writer.RecordPublicRouteRateLimited(ctx, FormalPoolRiskEventInput{SafeReasonCode: "noop"}); err != nil {
		t.Fatalf("nil writer returned error: %v", err)
	}
}

func marshalFormalPoolRecord(t *testing.T, record SessionBudgetObserveRecord) string {
	t.Helper()
	if err := ValidateNoRawSensitiveLedger(record); err != nil {
		t.Fatalf("record failed sensitive ledger validation: %v", err)
	}
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	return string(b)
}

func assertFormalPoolRecordSafe(t *testing.T, record SessionBudgetObserveRecord) {
	t.Helper()
	text := marshalFormalPoolRecord(t, record)
	for _, forbidden := range []string{
		"raw-nonce", "198.51.100.7", "203.0.113.9", "user@example.test", "orphan@example.test",
		"123e4567-e89b-12d3-a456-426614174000", "tokensecret123", "orphansecret123",
		"proxy_credential", "user:pass", "127.0.0.1:1080",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("marshaled record contains forbidden raw value length=%d: %s", len(forbidden), text)
		}
	}
}

func TestFormalPoolRiskEventWriterPublicRouteReasonRejectsRawIdentifiers(t *testing.T) {
	ctx := context.Background()
	sink := &formalPoolRiskEventCaptureSink{}
	writer := NewFormalPoolRiskEventWriter(sink)

	if err := writer.RecordPublicRouteRateLimitedBuckets(ctx, "nonce_bucket_a1b2c3d4", "ip_bucket_09af12cd", "198.51.100.7"); err != nil {
		t.Fatalf("RecordPublicRouteRateLimitedBuckets returned error: %v", err)
	}
	if err := writer.RecordPublicRouteRateLimitedBuckets(ctx, "nonce_bucket_c0ffee12", "ip_bucket_1234abcd", "api.example.test"); err != nil {
		t.Fatalf("RecordPublicRouteRateLimitedBuckets returned error: %v", err)
	}
	if len(sink.records) != 2 {
		t.Fatalf("record count = %d, want 2", len(sink.records))
	}

	for _, record := range sink.records {
		event := record.RiskEvents[0]
		if strings.Contains(event.SafeReason, "198") || strings.Contains(event.SafeReason, "51") || strings.Contains(event.SafeReason, "100") || strings.Contains(event.SafeReason, "api") || strings.Contains(event.SafeReason, "example") || strings.Contains(event.SafeReason, "test") {
			t.Fatalf("public route safe reason leaked raw identifier fragments: %q", event.SafeReason)
		}
		if !strings.Contains(event.SafeReason, "rate_limited") && !strings.Contains(event.SafeReason, "reason_other") {
			t.Fatalf("public route unsafe reason should fall back to safe enum, got %q", event.SafeReason)
		}
		assertFormalPoolRecordSafe(t, record)
	}
}

func TestFormalPoolRiskEventWriterPublicRouteReasonPreservesAllowedEnums(t *testing.T) {
	ctx := context.Background()
	sink := &formalPoolRiskEventCaptureSink{}
	writer := NewFormalPoolRiskEventWriter(sink)

	for _, reason := range []string{"per_nonce", "per_ip", "nonce_total", "redis_unavailable_fallback"} {
		if err := writer.RecordPublicRouteRateLimitedBuckets(ctx, "nonce_bucket_a1b2c3d4", "ip_bucket_09af12cd", reason); err != nil {
			t.Fatalf("RecordPublicRouteRateLimitedBuckets(%q) returned error: %v", reason, err)
		}
	}
	if len(sink.records) != 4 {
		t.Fatalf("record count = %d, want 4", len(sink.records))
	}
	for i, reason := range []string{"per_nonce", "per_ip", "nonce_total", "redis_unavailable_fallback"} {
		if !strings.Contains(sink.records[i].RiskEvents[0].SafeReason, reason) {
			t.Fatalf("safe reason %q missing allowed enum %q", sink.records[i].RiskEvents[0].SafeReason, reason)
		}
	}
}

func TestFormalPoolRiskEventWriterSafeReasonCodeRejectsRawIdentifiers(t *testing.T) {
	ctx := context.Background()
	sink := &formalPoolRiskEventCaptureSink{}
	writer := NewFormalPoolRiskEventWriter(sink)

	for _, reason := range []string{"198.51.100.7", "api.example.test"} {
		if err := writer.RecordEgressMismatch(ctx, FormalPoolRiskEventInput{
			RawSessionID:   "safe-session-source",
			RawAccountID:   "account-safe",
			SafeReasonCode: reason,
		}); err != nil {
			t.Fatalf("RecordEgressMismatch returned error: %v", err)
		}
	}
	for _, record := range sink.records {
		event := record.RiskEvents[0]
		if strings.Contains(event.SafeReason, "198") || strings.Contains(event.SafeReason, "51") || strings.Contains(event.SafeReason, "100") || strings.Contains(event.SafeReason, "api") || strings.Contains(event.SafeReason, "example") || strings.Contains(event.SafeReason, "test") {
			t.Fatalf("safe reason leaked raw identifier fragments: %q", event.SafeReason)
		}
		if event.SafeReason != "reason_other" {
			t.Fatalf("unsafe SafeReasonCode should fall back to reason_other, got %q", event.SafeReason)
		}
		assertFormalPoolRecordSafe(t, record)
	}
}
