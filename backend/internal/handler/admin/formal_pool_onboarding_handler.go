package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type FormalPoolOnboardingHandler struct {
	svc         *service.FormalPoolOnboardingService
	rateLimiter service.FormalPoolEgressRateLimiter
	riskWriter  service.FormalPoolRiskEventWriter
}

func NewFormalPoolOnboardingHandler(svc *service.FormalPoolOnboardingService) *FormalPoolOnboardingHandler {
	return &FormalPoolOnboardingHandler{svc: svc}
}

func NewFormalPoolOnboardingHandlerWithPublicDeps(svc *service.FormalPoolOnboardingService, limiter service.FormalPoolEgressRateLimiter, riskWriter service.FormalPoolRiskEventWriter) *FormalPoolOnboardingHandler {
	return &FormalPoolOnboardingHandler{svc: svc, rateLimiter: limiter, riskWriter: riskWriter}
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
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) BrowserEgressCheck(c *gin.Context) {
	nonce := strings.TrimSpace(c.Param("nonce"))
	remoteIP := c.ClientIP()
	if h == nil || h.svc == nil {
		formalPoolBrowserEgressSafeFailure(c)
		return
	}
	if h.rateLimiter != nil {
		decision := h.rateLimiter.CheckEgressCheck(c.Request.Context(), nonce, remoteIP)
		if !decision.Allowed {
			if h.riskWriter != nil {
				_ = h.riskWriter.RecordPublicRouteRateLimitedBuckets(c.Request.Context(), decision.NonceBucket, decision.IPBucket, decision.Reason)
			}
			c.JSON(http.StatusTooManyRequests, formalPoolBrowserEgressRateLimitedResponse{OK: false})
			return
		}
	}
	_, err := h.svc.VerifyBrowserEgressByNonce(c.Request.Context(), nonce, remoteIP)
	if err != nil {
		formalPoolBrowserEgressSafeFailure(c)
		return
	}
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
	response.Success(c, res)
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
	response.Success(c, res)
}

func (h *FormalPoolOnboardingHandler) notImplemented(c *gin.Context) {
	response.Error(c, 501, "formal pool onboarding step is not implemented yet")
}
