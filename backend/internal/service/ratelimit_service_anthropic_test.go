package service

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestCalculateAnthropic429ResetTime_Only5hExceeded(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.02")
	headers.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.32")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "1771549200")

	result := calculateAnthropic429ResetTime(headers)
	assertAnthropicResult(t, result, 1770998400)

	if result.fiveHourReset == nil || !result.fiveHourReset.Equal(time.Unix(1770998400, 0)) {
		t.Errorf("expected fiveHourReset=1770998400, got %v", result.fiveHourReset)
	}
}

func TestCalculateAnthropic429ResetTime_Only7dExceeded(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.50")
	headers.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.05")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "1771549200")

	result := calculateAnthropic429ResetTime(headers)
	assertAnthropicResult(t, result, 1771549200)

	// fiveHourReset should still be populated for session window calculation
	if result.fiveHourReset == nil || !result.fiveHourReset.Equal(time.Unix(1770998400, 0)) {
		t.Errorf("expected fiveHourReset=1770998400, got %v", result.fiveHourReset)
	}
}

func TestCalculateAnthropic429ResetTime_BothExceeded(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.10")
	headers.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.02")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "1771549200")

	result := calculateAnthropic429ResetTime(headers)
	assertAnthropicResult(t, result, 1771549200)
	if result.window != "both" {
		t.Errorf("expected window=both, got %q", result.window)
	}
}

func TestCalculateAnthropic429ResetTime_ClassifiesWindowForSafeExtra(t *testing.T) {
	cases := []struct {
		name       string
		headers    func() http.Header
		wantWindow string
	}{
		{
			name: "5h",
			headers: func() http.Header {
				h := http.Header{}
				h.Set("anthropic-ratelimit-unified-5h-utilization", "1.01")
				h.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
				return h
			},
			wantWindow: "5h",
		},
		{
			name: "7d",
			headers: func() http.Header {
				h := http.Header{}
				h.Set("anthropic-ratelimit-unified-7d-surpassed-threshold", "true")
				h.Set("anthropic-ratelimit-unified-7d-reset", "1771549200")
				return h
			},
			wantWindow: "7d",
		},
		{
			name: "unknown",
			headers: func() http.Header {
				h := http.Header{}
				h.Set("anthropic-ratelimit-unified-5h-utilization", "0.80")
				h.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
				h.Set("anthropic-ratelimit-unified-7d-utilization", "0.70")
				h.Set("anthropic-ratelimit-unified-7d-reset", "1771549200")
				return h
			},
			wantWindow: "unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := calculateAnthropic429ResetTime(tc.headers())
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.window != tc.wantWindow {
				t.Fatalf("expected window=%q, got %q", tc.wantWindow, result.window)
			}
		})
	}
}

func TestCalculateAnthropic429ResetTime_NoPerWindowHeaders(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", "1771549200")

	result := calculateAnthropic429ResetTime(headers)
	if result != nil {
		t.Errorf("expected nil result when no per-window headers, got resetAt=%v", result.resetAt)
	}
}

type anthropic429FallbackRepo struct {
	sessionWindowMockRepo
	rateLimitedAt *time.Time
}

func (r *anthropic429FallbackRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	r.rateLimitedAt = &resetAt
	return nil
}

func TestHandle429_AnthropicAggregateResetParsesRFC3339AndMillis(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "rfc3339", raw: time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339)},
		{name: "millis", raw: strconv.FormatInt(time.Now().Add(3*time.Hour).UTC().UnixMilli(), 10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &anthropic429FallbackRepo{}
			svc := newRateLimitServiceForTest(repo)
			account := &Account{ID: 70, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
			headers := http.Header{}
			headers.Set("anthropic-ratelimit-unified-reset", tc.raw)

			svc.handle429(context.Background(), account, headers, nil)

			if repo.rateLimitedAt == nil || time.Until(*repo.rateLimitedAt) <= 0 || time.Until(*repo.rateLimitedAt) > 4*time.Hour {
				t.Fatalf("expected aggregate reset to be parsed into near-future cooldown, got %v", repo.rateLimitedAt)
			}
			if len(repo.sessionWindowCalls) != 1 || repo.sessionWindowCalls[0].Status != "rejected" {
				t.Fatalf("expected rejected session window update, got %+v", repo.sessionWindowCalls)
			}
		})
	}
}

func TestCalculateAnthropic429ResetTime_NoHeaders(t *testing.T) {
	result := calculateAnthropic429ResetTime(http.Header{})
	if result != nil {
		t.Errorf("expected nil result for empty headers, got resetAt=%v", result.resetAt)
	}
}

func TestCalculateAnthropic429ResetTime_SurpassedThreshold(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-surpassed-threshold", "true")
	headers.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
	headers.Set("anthropic-ratelimit-unified-7d-surpassed-threshold", "false")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "1771549200")

	result := calculateAnthropic429ResetTime(headers)
	assertAnthropicResult(t, result, 1770998400)
}

func TestCalculateAnthropic429ResetTime_ParsesRFC3339AndMillisResetHeaders(t *testing.T) {
	reset5h := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	reset7d := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "102%")
	headers.Set("anthropic-ratelimit-unified-5h-reset", reset5h.Format(time.RFC3339))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.50")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.UnixMilli(), 10))

	result := calculateAnthropic429ResetTime(headers)
	assertAnthropicResult(t, result, reset5h.Unix())
	if result.fiveHourReset == nil || !result.fiveHourReset.Equal(reset5h) {
		t.Errorf("expected fiveHourReset=%v, got %v", reset5h, result.fiveHourReset)
	}
}

func TestCalculateAnthropic429ResetTime_UtilizationExactlyOne(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.5")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "1771549200")

	result := calculateAnthropic429ResetTime(headers)
	assertAnthropicResult(t, result, 1770998400)
}

func TestCalculateAnthropic429ResetTime_NeitherExceeded_UsesShorter(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.95")
	headers.Set("anthropic-ratelimit-unified-5h-reset", "1770998400") // sooner
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.80")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "1771549200") // later

	result := calculateAnthropic429ResetTime(headers)
	assertAnthropicResult(t, result, 1770998400)
}

func TestCalculateAnthropic429ResetTime_Only5hResetHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.05")
	headers.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")

	result := calculateAnthropic429ResetTime(headers)
	assertAnthropicResult(t, result, 1770998400)
}

func TestCalculateAnthropic429ResetTime_Only7dResetHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.03")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "1771549200")

	result := calculateAnthropic429ResetTime(headers)
	assertAnthropicResult(t, result, 1771549200)

	if result.fiveHourReset != nil {
		t.Errorf("expected fiveHourReset=nil when no 5h headers, got %v", result.fiveHourReset)
	}
}

func TestIsAnthropicWindowExceeded(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		window   string
		expected bool
	}{
		{
			name:     "utilization above 1.0",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "1.02"),
			window:   "5h",
			expected: true,
		},
		{
			name:     "utilization exactly 1.0",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "1.0"),
			window:   "5h",
			expected: true,
		},
		{
			name:     "utilization below 1.0",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "0.99"),
			window:   "5h",
			expected: false,
		},
		{
			name:     "surpassed-threshold true",
			headers:  makeHeader("anthropic-ratelimit-unified-7d-surpassed-threshold", "true"),
			window:   "7d",
			expected: true,
		},
		{
			name:     "percent utilization exactly 100",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "100%"),
			window:   "5h",
			expected: true,
		},
		{
			name:     "percent utilization above 100",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "102%"),
			window:   "5h",
			expected: true,
		},
		{
			name:     "surpassed-threshold True (case insensitive)",
			headers:  makeHeader("anthropic-ratelimit-unified-7d-surpassed-threshold", "True"),
			window:   "7d",
			expected: true,
		},
		{
			name:     "surpassed-threshold false",
			headers:  makeHeader("anthropic-ratelimit-unified-7d-surpassed-threshold", "false"),
			window:   "7d",
			expected: false,
		},
		{
			name:     "no headers",
			headers:  http.Header{},
			window:   "5h",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isAnthropicWindowExceeded(tc.headers, tc.window)
			if got != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

// assertAnthropicResult is a test helper that verifies the result is non-nil and
// has the expected resetAt unix timestamp.
func assertAnthropicResult(t *testing.T, result *anthropic429Result, wantUnix int64) {
	t.Helper()
	if result == nil {
		t.Fatal("expected non-nil result")
		return // unreachable, but satisfies staticcheck SA5011
	}
	want := time.Unix(wantUnix, 0)
	if !result.resetAt.Equal(want) {
		t.Errorf("expected resetAt=%v, got %v", want, result.resetAt)
	}
}

func makeHeader(key, value string) http.Header {
	h := http.Header{}
	h.Set(key, value)
	return h
}
