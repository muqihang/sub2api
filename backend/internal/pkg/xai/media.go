package xai

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const DefaultBaseURL = "https://api.x.ai/v1"

func BuildImagesGenerationsURL(baseURL string) (string, error) {
	return buildEndpointURL(baseURL, "/v1/images/generations")
}

func BuildImagesEditsURL(baseURL string) (string, error) {
	return buildEndpointURL(baseURL, "/v1/images/edits")
}

func BuildVideosGenerationsURL(baseURL string) (string, error) {
	return buildEndpointURL(baseURL, "/v1/videos/generations")
}

func BuildVideoURL(baseURL, requestID string) (string, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return "", fmt.Errorf("request_id is required")
	}
	return buildEndpointURL(baseURL, "/v1/videos/"+url.PathEscape(requestID))
}

func ParseQuotaHeaders(http.Header, int) any { return nil }

func buildEndpointURL(baseURL, endpoint string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid xAI base URL")
	}
	endpoint = "/" + strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	relative := strings.TrimPrefix(endpoint, "/v1")
	if strings.HasSuffix(baseURL, endpoint) || strings.HasSuffix(baseURL, relative) {
		return baseURL, nil
	}
	if strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/v1") {
		return baseURL + relative, nil
	}
	return baseURL + endpoint, nil
}
