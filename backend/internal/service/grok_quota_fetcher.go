package service

import (
	"net/http"
	"time"
)

type GrokQuotaFetcher struct{}

func NewGrokQuotaFetcher() *GrokQuotaFetcher {
	return &GrokQuotaFetcher{}
}

func (f *GrokQuotaFetcher) BuildUsageInfo(account *Account) *UsageInfo {
	now := time.Now()
	usage := &UsageInfo{
		Source:    "passive",
		UpdatedAt: &now,
	}
	if account == nil {
		usage.ErrorCode = "quota_unknown"
		usage.Error = "Grok quota is unknown until the first upstream response includes xAI rate-limit headers"
		return usage
	}

	snapshot, err := grokQuotaSnapshotFromExtra(account.Extra)
	if err != nil || snapshot == nil {
		usage.ErrorCode = "quota_unknown"
		usage.Error = "Grok quota is unknown until the first upstream response includes xAI rate-limit headers"
		return usage
	}

	if parsedAt, err := time.Parse(time.RFC3339, snapshot.UpdatedAt); err == nil {
		usage.UpdatedAt = &parsedAt
	}
	usage.GrokRequestQuota = snapshot.Requests
	usage.GrokTokenQuota = snapshot.Tokens
	usage.GrokRetryAfterSeconds = snapshot.RetryAfterSeconds
	usage.SubscriptionTier = snapshot.SubscriptionTier
	usage.SubscriptionTierRaw = snapshot.SubscriptionTier
	usage.GrokEntitlementStatus = snapshot.EntitlementStatus
	usage.GrokLastQuotaProbeAt = snapshot.LastProbeAt
	usage.GrokLastHeadersSeenAt = snapshot.LastHeadersSeenAt
	usage.GrokLastStatusCode = snapshot.StatusCode
	if snapshot.HasObservedHeaders() {
		usage.GrokQuotaSnapshotState = "observed"
	} else {
		usage.GrokQuotaSnapshotState = "no_headers"
		usage.ErrorCode = "quota_unknown"
		usage.Error = "No xAI quota headers observed on the latest Grok probe"
	}

	switch snapshot.StatusCode {
	case http.StatusUnauthorized:
		usage.NeedsReauth = true
		usage.ErrorCode = "unauthenticated"
	case http.StatusForbidden:
		usage.IsForbidden = true
		usage.ForbiddenType = "forbidden"
		usage.ErrorCode = "forbidden"
		if usage.GrokEntitlementStatus == "" {
			usage.GrokEntitlementStatus = "forbidden"
		}
	case http.StatusTooManyRequests:
		usage.ErrorCode = "rate_limited"
	}
	return usage
}
