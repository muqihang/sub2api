package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProvideFormalPoolOnboardingHandlerWiresPublicLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewFormalPoolOnboardingService(service.FormalPoolOnboardingDeps{})
	limiter := &formalPoolOnboardingProviderLimiter{decision: service.FormalPoolEgressRateLimitDecision{
		Allowed: false,
		Reason:  "per_nonce",
	}}
	risk := &formalPoolOnboardingProviderRiskWriter{}
	h := ProvideFormalPoolOnboardingHandler(svc, limiter, risk)

	router := gin.New()
	router.GET("/browser-egress-check/:nonce", h.BrowserEgressCheck)
	req := httptest.NewRequest(http.MethodGet, "/browser-egress-check/raw-nonce", nil)
	req.RemoteAddr = "198.51.100.4:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.JSONEq(t, `{"ok":false}`, rec.Body.String())
	require.Equal(t, 1, limiter.calls)
	require.Equal(t, 1, risk.rateLimited)
}

type formalPoolOnboardingProviderLimiter struct {
	decision service.FormalPoolEgressRateLimitDecision
	calls    int
}

func (l *formalPoolOnboardingProviderLimiter) CheckEgressCheck(ctx context.Context, nonce, ip string) service.FormalPoolEgressRateLimitDecision {
	_ = ctx
	_ = nonce
	_ = ip
	l.calls++
	return l.decision
}

type formalPoolOnboardingProviderRiskWriter struct{ rateLimited int }

func (w *formalPoolOnboardingProviderRiskWriter) RecordEgressVerified(ctx context.Context, input service.FormalPoolRiskEventInput) error {
	return nil
}
func (w *formalPoolOnboardingProviderRiskWriter) RecordEgressMismatch(ctx context.Context, input service.FormalPoolRiskEventInput) error {
	return nil
}
func (w *formalPoolOnboardingProviderRiskWriter) RecordNonceExpired(ctx context.Context, input service.FormalPoolRiskEventInput) error {
	return nil
}
func (w *formalPoolOnboardingProviderRiskWriter) RecordEgressNoProxy(ctx context.Context, input service.FormalPoolRiskEventInput) error {
	return nil
}
func (w *formalPoolOnboardingProviderRiskWriter) RecordPublicRouteRateLimited(ctx context.Context, input service.FormalPoolRiskEventInput) error {
	w.rateLimited++
	return nil
}
func (w *formalPoolOnboardingProviderRiskWriter) RecordPublicRouteRateLimitedBuckets(ctx context.Context, nonceBucket, ipBucket, reason string) error {
	w.rateLimited++
	return nil
}
