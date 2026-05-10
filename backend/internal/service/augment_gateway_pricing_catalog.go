package service

import (
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/resources"
)

type AugmentGatewayExplicitPricingChecker interface {
	HasExplicitPricing(modelID string) bool
}

type augmentGatewayEmbeddedExplicitPricingCatalog struct {
	priced map[string]struct{}
}

var defaultAugmentGatewayExplicitPricingChecker AugmentGatewayExplicitPricingChecker = newAugmentGatewayEmbeddedExplicitPricingCatalog(resources.ModelPricingCatalogJSON)

func newAugmentGatewayEmbeddedExplicitPricingCatalog(body []byte) AugmentGatewayExplicitPricingChecker {
	catalog := &augmentGatewayEmbeddedExplicitPricingCatalog{
		priced: make(map[string]struct{}),
	}
	if len(body) == 0 {
		return catalog
	}

	var rawData map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawData); err != nil {
		return catalog
	}

	for modelID, rawEntry := range rawData {
		if strings.TrimSpace(modelID) == "" || modelID == "sample_spec" {
			continue
		}

		var entry LiteLLMRawEntry
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			continue
		}
		if !augmentGatewayHasCompleteExplicitPricing(entry) {
			continue
		}
		catalog.priced[strings.ToLower(strings.TrimSpace(modelID))] = struct{}{}
	}

	return catalog
}

func (c *augmentGatewayEmbeddedExplicitPricingCatalog) HasExplicitPricing(modelID string) bool {
	if c == nil {
		return false
	}
	_, ok := c.priced[strings.ToLower(strings.TrimSpace(modelID))]
	return ok
}

func augmentGatewayHasCompleteExplicitPricing(entry LiteLLMRawEntry) bool {
	if entry.InputCostPerToken == nil || entry.OutputCostPerToken == nil {
		return false
	}
	if entry.SupportsPromptCaching {
		if entry.CacheReadInputTokenCost == nil || entry.CacheCreationInputTokenCost == nil {
			return false
		}
	}
	return true
}
