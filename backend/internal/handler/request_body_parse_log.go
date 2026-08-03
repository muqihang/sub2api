package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"go.uber.org/zap"
)

// logRequestBodyParseFailure records the real reason a request body failed
// JSON parsing/validation. The client keeps receiving the generic
// "Failed to parse request body"; the error and body length land in the server
// log only, so operators can distinguish genuinely invalid JSON from a
// truncated or partially consumed body without recording request contents.
//
// err may be nil for call sites that validate with gjson.ValidBytes directly;
// the diagnostic error is derived from the body in that case.
func logRequestBodyParseFailure(reqLog *zap.Logger, body []byte, err error) {
	if reqLog == nil {
		return
	}
	if err == nil {
		err = service.DescribeInvalidJSON(body)
	}

	fields := []zap.Field{
		zap.Error(err),
		zap.Int("body_len", len(body)),
	}
	reqLog.Warn("parse request body failed", fields...)
}
