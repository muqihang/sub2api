package handler

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type codexAgentService interface {
	CreateSetupGrant(ctx context.Context, req service.CreateCodexSetupGrantRequest) (*service.CreateCodexSetupGrantResponse, error)
	ExchangeSetupGrant(ctx context.Context, req service.ExchangeCodexSetupGrantRequest) (*service.ExchangeCodexSetupGrantResponse, error)
	RefreshDeviceToken(ctx context.Context, req service.RefreshCodexDeviceTokenRequest) (*service.RefreshCodexDeviceTokenResponse, error)
	ListDevices(ctx context.Context, userID int64, apiKeyID *int64) ([]*dbent.CodexManagedDevice, error)
	RevokeDevice(ctx context.Context, userID, deviceID int64) error
	ValidateManagedDeviceAccess(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error)
}

type CodexAgentHandler struct {
	service codexAgentService
}

func NewCodexAgentHandler(svc *service.CodexAgentService) *CodexAgentHandler {
	return &CodexAgentHandler{service: svc}
}

func NewCodexAgentHandlerWithServiceForTest(svc codexAgentService) *CodexAgentHandler {
	return &CodexAgentHandler{service: svc}
}

type createCodexSetupGrantRequest struct {
	APIKeyID int64  `json:"api_key_id" binding:"required"`
	Client   string `json:"client"`
	Mode     string `json:"mode"`
}

type exchangeCodexSetupGrantRequest struct {
	Code           string `json:"code" binding:"required"`
	ServerOrigin   string `json:"server_origin" binding:"required"`
	DeviceName     string `json:"device_name"`
	Platform       string `json:"platform"`
	Arch           string `json:"arch"`
	ManagerVersion string `json:"manager_version"`
}

type refreshCodexDeviceTokenRequest struct {
	DeviceID     int64  `json:"device_id" binding:"required"`
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type revokeCodexDeviceRequest struct {
	DeviceID int64 `json:"device_id" binding:"required"`
}

func (h *CodexAgentHandler) CreateSetupGrant(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req createCodexSetupGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	resp, err := h.service.CreateSetupGrant(c.Request.Context(), service.CreateCodexSetupGrantRequest{
		UserID:       subject.UserID,
		APIKeyID:     req.APIKeyID,
		Client:       req.Client,
		Mode:         req.Mode,
		ServerOrigin: codexAgentRequestOrigin(c),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, resp)
}

func (h *CodexAgentHandler) ExchangeSetupGrant(c *gin.Context) {
	var req exchangeCodexSetupGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	resp, err := h.service.ExchangeSetupGrant(c.Request.Context(), service.ExchangeCodexSetupGrantRequest{
		Code:           req.Code,
		ServerOrigin:   req.ServerOrigin,
		DeviceName:     req.DeviceName,
		Platform:       req.Platform,
		Arch:           req.Arch,
		ManagerVersion: req.ManagerVersion,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, resp)
}

func (h *CodexAgentHandler) RefreshDeviceToken(c *gin.Context) {
	var req refreshCodexDeviceTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	resp, err := h.service.RefreshDeviceToken(c.Request.Context(), service.RefreshCodexDeviceTokenRequest{
		DeviceID:     req.DeviceID,
		RefreshToken: req.RefreshToken,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, resp)
}

func (h *CodexAgentHandler) ListDevices(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var apiKeyID *int64
	if raw := strings.TrimSpace(c.Query("api_key_id")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid api_key_id")
			return
		}
		apiKeyID = &parsed
	}

	devices, err := h.service.ListDevices(c.Request.Context(), subject.UserID, apiKeyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, devices)
}

func (h *CodexAgentHandler) RevokeDevice(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req revokeCodexDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if err := h.service.RevokeDevice(c.Request.Context(), subject.UserID, req.DeviceID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"device_id": req.DeviceID, "revoked": true})
}

func (h *CodexAgentHandler) RevokeManagedDevice(c *gin.Context) {
	var req revokeCodexDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	managedSessionID := strings.TrimSpace(c.GetHeader("X-Zhumeng-Managed-Session"))
	access, err := h.service.ValidateManagedDeviceAccess(c.Request.Context(), service.ValidateManagedDeviceAccessRequest{
		AccessToken:      c.GetHeader("Authorization"),
		DeviceID:         req.DeviceID,
		ManagedSessionID: managedSessionID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := h.service.RevokeDevice(c.Request.Context(), access.User.ID, req.DeviceID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"device_id": req.DeviceID, "revoked": true})
}

func codexAgentRequestOrigin(c *gin.Context) string {
	if origin := strings.TrimSpace(c.GetHeader("Origin")); origin != "" {
		return origin
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}

	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}

	u := url.URL{Scheme: scheme, Host: host}
	return strings.TrimRight(u.String(), "/")
}
