package service

import (
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/resources"
)

type CodexGatewayPricingReadyChecker interface {
	HasPricing(modelID string) bool
}

type codexGatewayEmbeddedPricingCatalog struct {
	priced map[string]struct{}
}

var defaultCodexGatewayPricingReadyChecker CodexGatewayPricingReadyChecker = newCodexGatewayEmbeddedPricingCatalog(resources.ModelPricingCatalogJSON)

func newCodexGatewayEmbeddedPricingCatalog(body []byte) CodexGatewayPricingReadyChecker {
	catalog := &codexGatewayEmbeddedPricingCatalog{
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

func (c *codexGatewayEmbeddedPricingCatalog) HasPricing(modelID string) bool {
	if c == nil {
		return false
	}
	_, ok := c.priced[strings.ToLower(strings.TrimSpace(modelID))]
	return ok
}
