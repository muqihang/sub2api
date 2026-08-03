package handler

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequestBodyLimitTooLarge(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limit := int64(16)
	router := gin.New()
	router.Use(middleware.RequestBodyLimit(limit))
	router.POST("/test", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			if maxErr, ok := extractMaxBytesError(err); ok {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{
					"error": buildBodyTooLargeMessage(maxErr.Limit),
				})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "read_failed",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	payload := bytes.Repeat([]byte("a"), int(limit+1))
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(payload))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Contains(t, recorder.Body.String(), buildBodyTooLargeMessage(limit))
}

func TestExtractMaxBytesErrorSupportsDecompressedOverflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantMin int64
	}{
		{
			name:    "typed decompressed overflow",
			err:     &pkghttputil.RequestBodyTooLargeError{Limit: 123},
			wantMin: 123,
		},
		{
			name:    "sentinel decompressed overflow",
			err:     pkghttputil.ErrRequestBodyTooLarge,
			wantMin: 1,
		},
		{
			name:    "wrapped typed decompressed overflow",
			err:     errors.Join(errors.New("outer"), &pkghttputil.RequestBodyTooLargeError{Limit: 456}),
			wantMin: 456,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			maxErr, ok := extractMaxBytesError(tc.err)
			require.True(t, ok)
			require.NotNil(t, maxErr)
			require.GreaterOrEqual(t, maxErr.Limit, tc.wantMin)
		})
	}
}
