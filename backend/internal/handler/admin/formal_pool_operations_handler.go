package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
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
	res, err := h.svc.ReplaceSetupToken(h.operationContext(c), accountID, req)
	h.writeAccountResult(c, res, err)
}

func (h *FormalPoolOperationsHandler) RuntimeRegister(c *gin.Context) {
	accountID, ok := h.parseAccountID(c)
	if !ok {
		return
	}
	res, err := h.svc.RuntimeRegister(h.operationContext(c), accountID)
	h.writeAccountResult(c, res, err)
}

func (h *FormalPoolOperationsHandler) Healthcheck(c *gin.Context) {
	accountID, ok := h.parseAccountID(c)
	if !ok {
		return
	}
	res, err := h.svc.Healthcheck(h.operationContext(c), accountID)
	h.writeAccountResult(c, res, err)
}

func (h *FormalPoolOperationsHandler) StartWarming(c *gin.Context) {
	accountID, ok := h.parseAccountID(c)
	if !ok {
		return
	}
	res, err := h.svc.StartWarming(h.operationContext(c), accountID)
	h.writeAccountResult(c, res, err)
}

func (h *FormalPoolOperationsHandler) PromoteProduction(c *gin.Context) {
	accountID, ok := h.parseAccountID(c)
	if !ok {
		return
	}
	res, err := h.svc.PromoteProduction(h.operationContext(c), accountID)
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
	res, err := h.svc.SwapProxy(h.operationContext(c), accountID, req)
	h.writeAccountResult(c, res, err)
}

func (h *FormalPoolOperationsHandler) operationContext(c *gin.Context) context.Context {
	ctx := c.Request.Context()
	if subject, ok := servermiddleware.GetAuthSubjectFromContext(c); ok && subject.UserID > 0 {
		ctx = service.WithFormalPoolOperationOperator(ctx, "admin:"+strconv.FormatInt(subject.UserID, 10))
	}
	return ctx
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
		"account":     formalPoolOperationSafeAccount(resultAccount(result)),
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

type formalPoolOperationAccountPayload struct {
	ID                   int64  `json:"id"`
	Status               string `json:"status"`
	Schedulable          bool   `json:"schedulable"`
	EffectiveSchedulable bool   `json:"effective_schedulable"`
	OnboardingStage      string `json:"onboarding_stage,omitempty"`
}

func formalPoolOperationSafeAccount(account *service.Account) *formalPoolOperationAccountPayload {
	if account == nil {
		return nil
	}
	return &formalPoolOperationAccountPayload{
		ID:                   account.ID,
		Status:               formalPoolOperationFailureSafeStatus(account.Status),
		Schedulable:          account.Schedulable,
		EffectiveSchedulable: account.IsSchedulable(),
		OnboardingStage:      formalPoolOperationFailureSafeStage(service.FormalPoolAccountStage(account)),
	}
}

func formalPoolOperationFailureAccount(account *service.Account) *formalPoolOperationAccountPayload {
	return formalPoolOperationSafeAccount(account)
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
