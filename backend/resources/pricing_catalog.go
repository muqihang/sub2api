package resources

import _ "embed"

// ModelPricingCatalogJSON contains the canonical model pricing catalog used by runtime guards.
//
//go:embed model-pricing/model_prices_and_context_window.json
var ModelPricingCatalogJSON []byte
