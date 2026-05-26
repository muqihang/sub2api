package admin

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type FormalPoolOnboardingHandler struct {
	svc *service.FormalPoolOnboardingService
}

func NewFormalPoolOnboardingHandler(svc *service.FormalPoolOnboardingService) *FormalPoolOnboardingHandler {
	return &FormalPoolOnboardingHandler{svc: svc}
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
	if h == nil || h.svc == nil {
		response.InternalError(c, "formal pool onboarding unavailable")
		return
	}
	res, err := h.svc.VerifyBrowserEgressByNonce(c.Request.Context(), strings.TrimSpace(c.Param("nonce")), c.ClientIP())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, res)
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

func (h *FormalPoolOnboardingHandler) notImplemented(c *gin.Context) {
	response.Error(c, 501, "formal pool onboarding step is not implemented yet")
}
