package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const codexGatewayDeepSeekHostedImageVisionPrefix = "[hosted_image_vision]\n"

func codexGatewayDeepSeekRequestWithHostedVision(ctx context.Context, req CodexGatewayResponsesCreateRequest, cfg CodexGatewayDeepSeekRequestConfig) (CodexGatewayResponsesCreateRequest, error) {
	if cfg.HostedImageVision == nil || len(req.Input) == 0 {
		return req, nil
	}
	items, err := decodeCodexGatewayInputItems(req.Input)
	if err != nil {
		return CodexGatewayResponsesCreateRequest{}, err
	}
	if len(items) == 0 {
		return req, nil
	}

	rewritten, changed := false, false
	for _, itemAny := range items {
		item, ok := itemAny.(map[string]any)
		if !ok {
			continue
		}
		content, ok := item["content"].([]any)
		if !ok || len(content) == 0 {
			continue
		}
		nextContent := make([]any, 0, len(content))
		itemChanged := false
		for _, partAny := range content {
			part, ok := partAny.(map[string]any)
			if !ok {
				nextContent = append(nextContent, partAny)
				continue
			}
			if strings.TrimSpace(firstCodexGatewayToolString(part["type"])) != "input_image" {
				nextContent = append(nextContent, part)
				continue
			}
			imageURL := codexGatewayInputImageURL(part)
			if imageURL == "" {
				nextContent = append(nextContent, part)
				continue
			}
			summary, err := cfg.HostedImageVision(ctx, imageURL)
			if err != nil || strings.TrimSpace(summary) == "" {
				nextContent = append(nextContent, part)
				continue
			}
			nextContent = append(nextContent, map[string]any{
				"type": "input_text",
				"text": codexGatewayDeepSeekHostedImageVisionPrefix + strings.TrimSpace(summary),
			})
			itemChanged = true
			changed = true
		}
		if itemChanged {
			item["content"] = nextContent
			rewritten = true
		}
	}
	if !changed || !rewritten {
		return req, nil
	}

	rawInput, err := json.Marshal(items)
	if err != nil {
		return CodexGatewayResponsesCreateRequest{}, fmt.Errorf("marshal deepseek hosted vision input: %w", err)
	}
	req.Input = rawInput
	return req, nil
}

func codexGatewayInputImageURL(part map[string]any) string {
	imageURL := strings.TrimSpace(firstCodexGatewayToolString(part["image_url"]))
	if imageURL != "" {
		return imageURL
	}
	nested, ok := part["image_url"].(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(firstCodexGatewayToolString(nested["url"]))
}
