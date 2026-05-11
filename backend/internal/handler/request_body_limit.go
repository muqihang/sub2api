package handler

import (
	"errors"
	"fmt"
	"net/http"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
)

const decompressedRequestBodyTooLargeLimit = 64 << 20

func extractMaxBytesError(err error) (*http.MaxBytesError, bool) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return maxErr, true
	}
	var bodyTooLargeErr *pkghttputil.RequestBodyTooLargeError
	if errors.As(err, &bodyTooLargeErr) {
		if bodyTooLargeErr.Limit > 0 {
			return &http.MaxBytesError{Limit: bodyTooLargeErr.Limit}, true
		}
		return &http.MaxBytesError{Limit: decompressedRequestBodyTooLargeLimit}, true
	}
	if errors.Is(err, pkghttputil.ErrRequestBodyTooLarge) {
		return &http.MaxBytesError{Limit: decompressedRequestBodyTooLargeLimit}, true
	}
	return nil, false
}

func formatBodyLimit(limit int64) string {
	const mb = 1024 * 1024
	if limit >= mb {
		return fmt.Sprintf("%dMB", limit/mb)
	}
	return fmt.Sprintf("%dB", limit)
}

func buildBodyTooLargeMessage(limit int64) string {
	return fmt.Sprintf("Request body too large, limit is %s", formatBodyLimit(limit))
}
