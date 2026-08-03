package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) AugmentBillingSummary(c *gin.Context) {
	if h == nil || h.augmentGatewayUsageService == nil {
		response.InternalError(c, "Augment gateway usage service is unavailable")
		return
	}
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	summary, err := h.augmentGatewayUsageService.GetSummary(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, summary)
}

func (h *AuthHandler) AugmentBillingUsage(c *gin.Context) {
	if h == nil || h.augmentGatewayUsageService == nil {
		response.InternalError(c, "Augment gateway usage service is unavailable")
		return
	}
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	params := pagination.PaginationParams{
		Page:     parsePositiveInt(c.Query("page"), 1),
		PageSize: parsePositiveInt(c.Query("page_size"), 20),
	}
	rows, page, err := h.augmentGatewayUsageService.ListUsage(c.Request.Context(), subject.UserID, params)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"rows": rows,
		"page": page,
	})
}

func (h *AuthHandler) AugmentBillingRecentErrors(c *gin.Context) {
	if h == nil || h.augmentGatewayUsageService == nil {
		response.InternalError(c, "Augment gateway usage service is unavailable")
		return
	}
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	limit := parsePositiveInt(c.Query("limit"), 20)
	rows, err := h.augmentGatewayUsageService.ListRecentErrors(c.Request.Context(), subject.UserID, limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"rows": rows})
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
