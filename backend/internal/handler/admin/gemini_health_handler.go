package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type GeminiHealthHandler struct {
	healthService *service.GeminiHealthService
}

func NewGeminiHealthHandler(healthService *service.GeminiHealthService) *GeminiHealthHandler {
	return &GeminiHealthHandler{healthService: healthService}
}

func (h *GeminiHealthHandler) Health(c *gin.Context) {
	snapshot, err := h.healthService.BuildHealthSnapshot(c.Request.Context())
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, snapshot)
}

func (h *GeminiHealthHandler) Verify(c *gin.Context) {
	accountID, err := strconv.ParseInt(strings.TrimSpace(c.Query("account_id")), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "account_id is required")
		return
	}
	snapshot, err := h.healthService.BuildVerifySnapshot(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, snapshot)
}
