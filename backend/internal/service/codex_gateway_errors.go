package service

import (
	"encoding/json"
	"net/http"
)

const (
	CodexGatewayErrorTypeInvalidRequest = "invalid_request_error"
	CodexGatewayErrorTypeAuthentication = "authentication_error"
	CodexGatewayErrorTypeRateLimit      = "rate_limit_error"
	CodexGatewayErrorTypeAPI            = "api_error"
	CodexGatewayErrorCodeInvalidRequest = "invalid_request"
)

type codexGatewayErrorEnvelope struct {
	Error codexGatewayErrorPayload `json:"error"`
}

type codexGatewayErrorPayload struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func MarshalCodexGatewayErrorJSON(errorType, code, message string) ([]byte, error) {
	return json.Marshal(codexGatewayErrorEnvelope{
		Error: codexGatewayErrorPayload{
			Type:    errorType,
			Code:    code,
			Message: message,
		},
	})
}

func WriteCodexGatewayErrorJSON(w http.ResponseWriter, status int, errorType, code, message string) {
	body, err := MarshalCodexGatewayErrorJSON(errorType, code, message)
	if err != nil {
		body = []byte(`{"error":{"type":"invalid_request_error","code":"invalid_request","message":"failed to encode error response"}}`)
	}
	header := w.Header()
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
