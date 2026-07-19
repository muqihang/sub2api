package middleware

import "github.com/gin-gonic/gin"

// AuthSubject is the safe authenticated identity stored in gin context.
type AuthSubject struct {
	UserID        int64
	Concurrency   int
	AuthMethod    string
	TokenVersion  int64
	ExpiresAtUnix int64
}

func GetAuthSubjectFromContext(c *gin.Context) (AuthSubject, bool) {
	value, exists := c.Get(string(ContextKeyUser))
	if !exists {
		return AuthSubject{}, false
	}
	subject, ok := value.(AuthSubject)
	return subject, ok
}

func GetUserRoleFromContext(c *gin.Context) (string, bool) {
	value, exists := c.Get(string(ContextKeyUserRole))
	if !exists {
		return "", false
	}
	role, ok := value.(string)
	return role, ok
}
