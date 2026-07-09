package xai

import (
	"fmt"
	"net/url"
	"strings"
)

const DefaultBaseURL = "https://api.x.ai/v1"

func BuildImagesGenerationsURL(baseURL string) (string, error) {
	return buildValidatedEndpointURL(baseURL, "/images/generations")
}

func BuildImagesEditsURL(baseURL string) (string, error) {
	return buildValidatedEndpointURL(baseURL, "/images/edits")
}

func BuildVideosGenerationsURL(baseURL string) (string, error) {
	return buildValidatedEndpointURL(baseURL, "/videos/generations")
}

func BuildVideoURL(baseURL, requestID string) (string, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return "", fmt.Errorf("request_id is required")
	}
	return buildValidatedEndpointURL(baseURL, "/videos/"+url.PathEscape(requestID))
}

func buildValidatedEndpointURL(baseURL, endpoint string) (string, error) {
	validatedBaseURL, err := ValidatedBaseURL(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base url: %w", err)
	}
	return validatedBaseURL + "/" + strings.TrimLeft(strings.TrimSpace(endpoint), "/"), nil
}
