package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func NewFormalPoolOnboardingJWTAuthMiddleware(authService *service.AuthService, userService *service.UserService) FormalPoolOnboardingJWTAuthMiddleware {
	return FormalPoolOnboardingJWTAuthMiddleware(jwtAuthWithFailure(authService, userService, userService, func(c *gin.Context) {
		response.ErrorFrom(c, service.ErrFormalPoolOnboardingAuthenticationRequired)
		c.Abort()
	}))
}
