package middleware

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	headerZhumengDeviceID       = "X-Zhumeng-Device-ID"
	headerZhumengManagedSession = "X-Zhumeng-Managed-Session"
	headerZhumengAgentVersion   = "X-Zhumeng-Agent-Version"
	headerZhumengConfigHash     = "X-Zhumeng-Config-Hash"
)

type ManagedDeviceAccessValidator interface {
	ValidateManagedDeviceAccess(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error)
}

type ManagedDeviceAccessValidatorFunc func(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error)

func (fn ManagedDeviceAccessValidatorFunc) ValidateManagedDeviceAccess(ctx context.Context, req service.ValidateManagedDeviceAccessRequest) (*service.ManagedDeviceAccessContext, error) {
	return fn(ctx, req)
}

func ManagedDeviceOrAPIKeyAuth(
	validator ManagedDeviceAccessValidator,
	rawAuth APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	cfg *config.Config,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceIDHeader := strings.TrimSpace(c.GetHeader(headerZhumengDeviceID))
		sessionHeader := strings.TrimSpace(c.GetHeader(headerZhumengManagedSession))

		if deviceIDHeader == "" && sessionHeader == "" {
			gin.HandlerFunc(rawAuth)(c)
			return
		}
		if validator == nil {
			AbortWithError(c, 500, "INTERNAL_ERROR", "Managed device auth is unavailable")
			return
		}
		if deviceIDHeader == "" || sessionHeader == "" {
			AbortWithError(c, 401, "CODEX_MANAGED_HEADERS_REQUIRED", "Managed device headers are incomplete")
			return
		}

		deviceID, err := strconv.ParseInt(deviceIDHeader, 10, 64)
		if err != nil || deviceID <= 0 {
			AbortWithError(c, 401, "CODEX_MANAGED_DEVICE_ID_INVALID", "Managed device id is invalid")
			return
		}

		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		if authHeader == "" {
			AbortWithError(c, 401, "CODEX_MANAGED_AUTHORIZATION_REQUIRED", "Managed device authorization is required")
			return
		}

		access, err := validator.ValidateManagedDeviceAccess(c.Request.Context(), service.ValidateManagedDeviceAccessRequest{
			AccessToken:      authHeader,
			DeviceID:         deviceID,
			ManagedSessionID: sessionHeader,
		})
		if err != nil {
			status, body := infraerrors.ToHTTP(err)
			c.JSON(status, body)
			c.Abort()
			return
		}
		if access == nil || access.APIKey == nil {
			AbortWithError(c, 401, "CODEX_MANAGED_ACCESS_INVALID", "Managed device access is invalid")
			return
		}

		if !validateAndSetAPIKeyContext(c, access.APIKey, apiKeyService, subscriptionService, cfg, "managed-device", authHeader, codexGatewayAPIKeyAuthErrorWriter) {
			return
		}
		c.Next()
	}
}
