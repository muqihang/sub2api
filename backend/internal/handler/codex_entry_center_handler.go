package handler

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type codexEntryCenterService interface {
	GetSummary(ctx context.Context, userID int64) (*service.CodexEntrySummary, error)
	CreateSetupSession(ctx context.Context, req service.CodexCreateSetupSessionRequest) (*service.CodexCreateSetupSessionResponse, error)
	RegenerateSetupSession(ctx context.Context, userID int64, sessionID string) (*service.CodexRegenerateSetupSessionResponse, error)
	Diagnose(ctx context.Context, req service.CodexDiagnoseRequest) (*service.CodexDiagnoseReport, error)
	ResyncDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error)
	RepairDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error)
	ReattachDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error)
	RevokeAttachment(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error)
	RemoveDevice(ctx context.Context, userID int64, deviceID int64) (*service.CodexDeviceActionResponse, error)
}

type CodexEntryCenterHandler struct {
	service codexEntryCenterService
}

func NewCodexEntryCenterHandler(svc service.CodexEntryCenterService) *CodexEntryCenterHandler {
	return &CodexEntryCenterHandler{service: svc}
}

func NewCodexEntryCenterHandlerWithServiceForTest(svc codexEntryCenterService) *CodexEntryCenterHandler {
	return &CodexEntryCenterHandler{service: svc}
}

// GetSummary handles GET /codex/summary.
func (h *CodexEntryCenterHandler) GetSummary(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	summary, err := h.service.GetSummary(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, summary)
}

type createSetupSessionHTTPRequest struct {
	AttachmentMode  string `json:"attachment_mode" binding:"required"`
	CredentialLabel string `json:"credential_label"`
	ReuseAPIKeyID   *int64 `json:"reuse_api_key_id"`
}

// CreateSetupSession handles POST /codex/setup-sessions.
func (h *CodexEntryCenterHandler) CreateSetupSession(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req createSetupSessionHTTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	resp, err := h.service.CreateSetupSession(c.Request.Context(), service.CodexCreateSetupSessionRequest{
		UserID:          subject.UserID,
		AttachmentMode:  service.CodexAttachmentMode(req.AttachmentMode),
		CredentialLabel: req.CredentialLabel,
		ReuseAPIKeyID:   req.ReuseAPIKeyID,
		ServerOrigin:    codexAgentRequestOrigin(c),
		GatewayOrigin:   codexAgentGatewayOrigin(c),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, resp)
}

// RegenerateSetupSession handles POST /codex/setup-sessions/:id/regenerate.
func (h *CodexEntryCenterHandler) RegenerateSetupSession(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		response.BadRequest(c, "session id is required")
		return
	}

	resp, err := h.service.RegenerateSetupSession(c.Request.Context(), subject.UserID, sessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, resp)
}

type diagnoseHTTPRequest struct {
	SetupSessionID *string `json:"setup_session_id"`
	DeviceID       *int64  `json:"device_id"`
}

// Diagnose handles POST /codex/diagnose.
func (h *CodexEntryCenterHandler) Diagnose(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req diagnoseHTTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	resp, err := h.service.Diagnose(c.Request.Context(), service.CodexDiagnoseRequest{
		UserID:         subject.UserID,
		SetupSessionID: req.SetupSessionID,
		DeviceID:       req.DeviceID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, resp)
}

// ResyncDevice handles POST /codex/devices/:id/resync.
func (h *CodexEntryCenterHandler) ResyncDevice(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	deviceID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid device id")
		return
	}

	resp, err := h.service.ResyncDevice(c.Request.Context(), subject.UserID, deviceID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, resp)
}

// RepairDevice handles POST /codex/devices/:id/repair.
func (h *CodexEntryCenterHandler) RepairDevice(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	deviceID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid device id")
		return
	}

	resp, err := h.service.RepairDevice(c.Request.Context(), subject.UserID, deviceID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, resp)
}

// ReattachDevice handles POST /codex/devices/:id/reattach.
func (h *CodexEntryCenterHandler) ReattachDevice(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	deviceID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid device id")
		return
	}

	resp, err := h.service.ReattachDevice(c.Request.Context(), subject.UserID, deviceID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, resp)
}

// RevokeAttachment handles POST /codex/devices/:id/revoke-attachment.
func (h *CodexEntryCenterHandler) RevokeAttachment(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	deviceID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid device id")
		return
	}

	resp, err := h.service.RevokeAttachment(c.Request.Context(), subject.UserID, deviceID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, resp)
}

// RemoveDevice handles DELETE /codex/devices/:id.
func (h *CodexEntryCenterHandler) RemoveDevice(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	deviceID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid device id")
		return
	}

	resp, err := h.service.RemoveDevice(c.Request.Context(), subject.UserID, deviceID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, resp)
}
