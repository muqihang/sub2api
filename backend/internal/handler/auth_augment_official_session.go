package handler

import (
	"errors"
	"io"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type augmentOfficialSessionBindIntentRequest struct {
	Mode            string   `json:"mode"`
	Source          string   `json:"source"`
	TenantAllowlist []string `json:"tenant_allowlist"`
}

type augmentOfficialSessionBindRequest struct {
	BindToken    string         `json:"bind_token"`
	BindIntentID string         `json:"bind_intent_id"`
	State        string         `json:"state"`
	Mode         string         `json:"mode"`
	Source       string         `json:"source"`
	Payload      map[string]any `json:"payload"`
	RequestID    string         `json:"request_id,omitempty"`
}

func (h *AuthHandler) AugmentOfficialSessionBindIntent(c *gin.Context) {
	if h == nil || h.augmentOfficialSessionService == nil {
		response.InternalError(c, "Augment official session service is unavailable")
		return
	}
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req augmentOfficialSessionBindIntentRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.ensureAugmentOfficialSessionOrigin(c); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	respData, err := h.augmentOfficialSessionService.CreateBindIntent(c.Request.Context(), subject.UserID, service.AugmentOfficialBindIntentRequest{
		Mode:            strings.TrimSpace(req.Mode),
		Source:          strings.TrimSpace(req.Source),
		TenantAllowlist: append([]string(nil), req.TenantAllowlist...),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, respData)
}

func (h *AuthHandler) AugmentOfficialSessionBind(c *gin.Context) {
	if h == nil || h.augmentOfficialSessionService == nil {
		response.InternalError(c, "Augment official session service is unavailable")
		return
	}
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req augmentOfficialSessionBindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.ensureAugmentOfficialSessionOrigin(c); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	view, err := h.augmentOfficialSessionService.BindOfficialSession(c.Request.Context(), subject.UserID, strings.TrimSpace(req.BindToken), service.AugmentOfficialBindRequest{
		BindIntentID: strings.TrimSpace(req.BindIntentID),
		State:        strings.TrimSpace(req.State),
		Mode:         strings.TrimSpace(req.Mode),
		Source:       strings.TrimSpace(req.Source),
		Payload:      req.Payload,
		RequestID:    strings.TrimSpace(req.RequestID),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

func (h *AuthHandler) ensureAugmentOfficialSessionOrigin(c *gin.Context) error {
	origin := normalizeAbsoluteURL(c.GetHeader("Origin"), true)
	if origin == "" {
		return nil
	}

	expected := requestOrigin(c)
	if expected == "" && h != nil && h.cfg != nil {
		expected = normalizeAbsoluteURL(h.cfg.Server.FrontendURL, true)
	}
	if expected == "" || origin == expected {
		return nil
	}
	return infraerrors.Forbidden("AUGMENT_OFFICIAL_BIND_ORIGIN_INVALID", "augment official session origin is invalid")
}

func (h *AuthHandler) AugmentOfficialSessionStatus(c *gin.Context) {
	if h == nil || h.augmentOfficialSessionService == nil {
		response.InternalError(c, "Augment official session service is unavailable")
		return
	}
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	view, err := h.augmentOfficialSessionService.GetOfficialSession(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

func (h *AuthHandler) AugmentOfficialSessionRevoke(c *gin.Context) {
	if h == nil || h.augmentOfficialSessionService == nil {
		response.InternalError(c, "Augment official session service is unavailable")
		return
	}
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	view, err := h.augmentOfficialSessionService.RevokeOfficialSession(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}
