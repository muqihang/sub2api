package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func scopeOpenAIBodyPromptCacheKey(ctx context.Context, body []byte) ([]byte, bool, error) {
	raw := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String())
	if raw == "" {
		return body, false, nil
	}
	scoped := EntityScopedSeedFromContext(ctx, raw)
	if scoped == raw {
		return body, false, nil
	}
	updated, err := sjson.SetBytes(body, "prompt_cache_key", scoped)
	if err != nil {
		return nil, false, fmt.Errorf("scope prompt_cache_key by entity: %w", err)
	}
	return updated, true, nil
}
