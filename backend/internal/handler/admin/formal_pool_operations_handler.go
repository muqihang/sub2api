package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type FormalPoolOperationsHandler struct {
	svc *service.FormalPoolOperationsService
}

func NewFormalPoolOperationsHandler(svc *service.FormalPoolOperationsService) *FormalPoolOperationsHandler {
	return &FormalPoolOperationsHandler{svc: svc}
}

func (h *FormalPoolOperationsHandler) Diagnostics(c *gin.Context) {
	accountID, ok := h.parseAccountID(c)
	if !ok {
		return
	}
	res, err := h.svc.Diagnostics(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, res)
}

func (h *FormalPoolOperationsHandler) ReplaceSetupToken(c *gin.Context) {
	accountID, ok := h.parseAccountID(c)
	if !ok {
		return
	}
	var req service.FormalPoolSetupTokenReplaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	res, err := h.svc.ReplaceSetupToken(c.Request.Context(), accountID, req)
	h.writeAccountResult(c, res, err)
}

func (h *FormalPoolOperationsHandler) RuntimeRegister(c *gin.Context) {
	accountID, ok := h.parseAccountID(c)
	if !ok {
		return
	}
	res, err := h.svc.RuntimeRegister(c.Request.Context(), accountID)
	h.writeAccountResult(c, res, err)
}

func (h *FormalPoolOperationsHandler) Healthcheck(c *gin.Context) {
	accountID, ok := h.parseAccountID(c)
	if !ok {
		return
	}
	res, err := h.svc.Healthcheck(c.Request.Context(), accountID)
	h.writeAccountResult(c, res, err)
}

func (h *FormalPoolOperationsHandler) StartWarming(c *gin.Context) {
	accountID, ok := h.parseAccountID(c)
	if !ok {
		return
	}
	res, err := h.svc.StartWarming(c.Request.Context(), accountID)
	h.writeAccountResult(c, res, err)
}

func (h *FormalPoolOperationsHandler) SwapProxy(c *gin.Context) {
	accountID, ok := h.parseAccountID(c)
	if !ok {
		return
	}
	var req service.FormalPoolProxySwapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	res, err := h.svc.SwapProxy(c.Request.Context(), accountID, req)
	h.writeAccountResult(c, res, err)
}

func (h *FormalPoolOperationsHandler) parseAccountID(c *gin.Context) (int64, bool) {
	if h == nil || h.svc == nil {
		response.Error(c, http.StatusServiceUnavailable, "formal pool operations unavailable")
		return 0, false
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return 0, false
	}
	return accountID, true
}

func (h *FormalPoolOperationsHandler) writeAccountResult(c *gin.Context, result *service.FormalPoolOperationsAccountResult, err error) {
	if err != nil {
		var opErr *service.FormalPoolOperationFailure
		if errors.As(err, &opErr) && opErr.Result != nil {
			status := opErr.HTTPStatus
			if status == 0 {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{
				"error":       opErr.Code,
				"message":     opErr.Message,
				"account":     formalPoolOperationFailureAccount(opErr.Result.Account),
				"diagnostics": opErr.Result.Diagnostics,
			})
			return
		}
		if result != nil {
			statusCode, status := infraerrors.ToHTTP(err)
			if statusCode == http.StatusInternalServerError && status.Reason == infraerrors.UnknownReason {
				status.Reason = "FORMAL_POOL_OPERATION_FAILED"
				status.Message = "formal pool operation failed"
			}
			c.JSON(statusCode, gin.H{
				"error":       status.Reason,
				"message":     status.Message,
				"account":     formalPoolOperationFailureAccount(result.Account),
				"diagnostics": result.Diagnostics,
			})
			return
		}
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"account":     dto.AccountFromService(resultAccount(result)),
		"diagnostics": resultDiagnostics(result),
	})
}

func resultAccount(result *service.FormalPoolOperationsAccountResult) *service.Account {
	if result == nil {
		return nil
	}
	return result.Account
}

func resultDiagnostics(result *service.FormalPoolOperationsAccountResult) *service.FormalPoolOperationsDiagnostics {
	if result == nil {
		return nil
	}
	return result.Diagnostics
}

type formalPoolOperationFailureAccountPayload struct {
	ID                   int64  `json:"id"`
	Status               string `json:"status"`
	Schedulable          bool   `json:"schedulable"`
	EffectiveSchedulable bool   `json:"effective_schedulable"`
	OnboardingStage      string `json:"onboarding_stage,omitempty"`
}

func formalPoolOperationFailureAccount(account *service.Account) *formalPoolOperationFailureAccountPayload {
	if account == nil {
		return nil
	}
	return &formalPoolOperationFailureAccountPayload{
		ID:                   account.ID,
		Status:               formalPoolOperationFailureSafeStatus(account.Status),
		Schedulable:          account.Schedulable,
		EffectiveSchedulable: account.IsSchedulable(),
		OnboardingStage:      formalPoolOperationFailureSafeStage(service.FormalPoolAccountStage(account)),
	}
}

func formalPoolOperationFailureSafeStatus(status string) string {
	switch status {
	case service.StatusActive, service.StatusDisabled, service.StatusError:
		return status
	default:
		return ""
	}
}

func formalPoolOperationFailureSafeStage(stage string) string {
	switch stage {
	case service.FormalPoolStageImported,
		service.FormalPoolStageRefreshed,
		service.FormalPoolStageRuntimeRegistered,
		service.FormalPoolStageHealthcheckPassed,
		service.FormalPoolStageWarming,
		service.FormalPoolStageProduction,
		service.FormalPoolStageQuarantined,
		service.FormalPoolStageLegacyUnknown:
		return stage
	default:
		return ""
	}
}
