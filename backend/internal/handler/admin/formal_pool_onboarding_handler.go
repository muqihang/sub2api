package admin

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type FormalPoolOnboardingHandler struct {
	svc              *service.FormalPoolOnboardingService
	rateLimiter      service.FormalPoolEgressRateLimiter
	riskWriter       service.FormalPoolRiskEventWriter
	publicRouteDelay func(context.Context)
}

type FormalPoolOnboardingHandlerOption func(*FormalPoolOnboardingHandler)

func WithPublicRouteDelay(delay func(context.Context)) FormalPoolOnboardingHandlerOption {
	return func(h *FormalPoolOnboardingHandler) {
		h.publicRouteDelay = delay
	}
}

func NewFormalPoolOnboardingHandler(svc *service.FormalPoolOnboardingService) *FormalPoolOnboardingHandler {
	return NewFormalPoolOnboardingHandlerWithPublicDeps(svc, nil, nil)
}

func NewFormalPoolOnboardingHandlerWithPublicDeps(svc *service.FormalPoolOnboardingService, limiter service.FormalPoolEgressRateLimiter, riskWriter service.FormalPoolRiskEventWriter, opts ...FormalPoolOnboardingHandlerOption) *FormalPoolOnboardingHandler {
	h := &FormalPoolOnboardingHandler{svc: svc, rateLimiter: limiter, riskWriter: riskWriter}
	h.publicRouteDelay = h.constantDelayFromService
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

func (h *FormalPoolOnboardingHandler) CreateSession(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	var req service.FormalPoolOnboardingStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	res, err := h.svc.StartSession(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) GetSession(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.GetSession(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) TestProxy(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.TestProxy(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) BrowserEgressAttestation(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	var req service.FormalPoolBrowserEgressAttestationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	res, err := h.svc.AttestBrowserEgress(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) BrowserEgressCheck(c *gin.Context) {
	nonce := strings.TrimSpace(c.Param("nonce"))
	remoteIP := c.ClientIP()
	if h == nil || h.svc == nil {
		h.applyPublicRouteDelay(c.Request.Context())
		formalPoolBrowserEgressSafeFailure(c)
		return
	}
	if h.rateLimiter != nil {
		decision := h.rateLimiter.CheckEgressCheck(c.Request.Context(), nonce, remoteIP)
		if !decision.Allowed {
			if h.riskWriter != nil {
				_ = h.riskWriter.RecordPublicRouteRateLimitedBuckets(c.Request.Context(), decision.NonceBucket, decision.IPBucket, decision.Reason)
			}
			h.applyPublicRouteDelay(c.Request.Context())
			c.JSON(http.StatusTooManyRequests, formalPoolBrowserEgressRateLimitedResponse{OK: false})
			return
		}
	}
	_, err := h.svc.VerifyBrowserEgressByNonce(c.Request.Context(), nonce, remoteIP)
	if err != nil {
		h.applyPublicRouteDelay(c.Request.Context())
		formalPoolBrowserEgressSafeFailure(c)
		return
	}
	h.applyPublicRouteDelay(c.Request.Context())
	c.JSON(http.StatusOK, formalPoolBrowserEgressSuccessResponse{OK: true})
}

type formalPoolBrowserEgressSuccessResponse struct {
	OK bool `json:"ok"`
}

type formalPoolBrowserEgressRateLimitedResponse struct {
	OK bool `json:"ok"`
}

type formalPoolBrowserEgressFailureResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func formalPoolBrowserEgressSafeFailure(c *gin.Context) {
	c.JSON(http.StatusOK, formalPoolBrowserEgressFailureResponse{OK: false, Message: "Browser egress check received."})
}

func (h *FormalPoolOnboardingHandler) applyPublicRouteDelay(ctx context.Context) {
	if h == nil || h.publicRouteDelay == nil {
		return
	}
	h.publicRouteDelay(ctx)
}

func (h *FormalPoolOnboardingHandler) constantDelayFromService(ctx context.Context) {
	if h == nil || h.svc == nil {
		return
	}
	minDelay, maxDelay := h.svc.PublicRouteConstantDelayBounds()
	boundedConstantDelay(ctx, minDelay, maxDelay)
}

func boundedConstantDelay(ctx context.Context, minDelay, maxDelay time.Duration) {
	if minDelay <= 0 || maxDelay <= 0 {
		return
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	delay := minDelay
	if maxDelay > minDelay {
		span := uint64(maxDelay - minDelay + 1)
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err == nil {
			delay += time.Duration(binary.LittleEndian.Uint64(buf[:]) % span)
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (h *FormalPoolOnboardingHandler) GenerateAuthURL(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.GenerateAuthURL(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) withAbsoluteBrowserEgressURL(c *gin.Context, res *service.FormalPoolOnboardingSession) {
	if res == nil || strings.TrimSpace(res.BrowserEgressCheckURL) == "" {
		return
	}
	if parsed, err := url.Parse(res.BrowserEgressCheckURL); err == nil && parsed.IsAbs() {
		return
	}
	base := formalPoolRequestPublicBaseURL(c)
	if base == "" {
		return
	}
	path := "/" + strings.TrimLeft(res.BrowserEgressCheckURL, "/")
	res.BrowserEgressCheckURL = base + path
}

func formalPoolRequestPublicBaseURL(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	scheme := firstForwardedHeaderValue(c.GetHeader("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = firstForwardedHeaderValue(c.GetHeader("X-Forwarded-Scheme"))
	}
	if scheme == "" && strings.EqualFold(c.GetHeader("X-Forwarded-Ssl"), "on") {
		scheme = "https"
	}
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme != "https" && scheme != "http" {
		return ""
	}
	host := firstForwardedHeaderValue(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(c.Request.Host)
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func firstForwardedHeaderValue(value string) string {
	if idx := strings.Index(value, ","); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

func (h *FormalPoolOnboardingHandler) ExchangeCodeAndCreate(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	var req service.FormalPoolExchangeCodeAndCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	res, err := h.svc.ExchangeCodeAndCreate(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) SetupTokenCookieAuthAndCreate(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	var req service.FormalPoolSetupTokenCookieAuthAndCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	res, err := h.svc.SetupTokenCookieAuthAndCreate(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) Acceptance(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.RunAcceptance(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) Activate(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.Activate(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) Abort(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.AbortSession(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) RefreshOnly(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.RefreshOnly(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) RuntimeRegister(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.RegisterRuntime(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) Healthcheck(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.RunAcceptance(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) AccountHealthcheck(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	res, err := h.svc.RunAccountHealthcheck(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) StartWarming(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.StartWarming(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) PromoteProduction(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.PromoteProduction(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.withAbsoluteBrowserEgressURL(c, res)
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) notImplemented(c *gin.Context) {
	response.Error(c, 501, "formal pool onboarding step is not implemented yet")
}
