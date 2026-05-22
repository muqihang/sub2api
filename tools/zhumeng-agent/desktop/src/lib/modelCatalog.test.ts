import { describe, expect, it } from "vitest";

import { filterCatalogModels, modelPriceRows, summarizeCatalog } from "./modelCatalog";
import type { CatalogModel } from "./types";

const models: CatalogModel[] = [
  {
    slug: "gpt-5.5",
    display_name: "GPT-5.5",
    origin: "zhumeng",
    provider_id: "openai",
    visibility: "list",
    supported_in_api: true,
    capabilities: { responses: true, streaming: true, tool_calls: true, context_continuation: true },
    pricing: {
      input_price: "2.50",
      output_price: "15.00",
      cached_input_price: "0.25",
      cache_write_price: "3.00",
      currency: "USD",
      unit: "per_1m_tokens",
      source: "database_model_pricing"
    }
  },
  {
    slug: "claude-sonnet-4.5",
    display_name: "Claude Sonnet 4.5",
    origin: "zhumeng",
    provider_id: "anthropic",
    visibility: "list",
    supported_in_api: true,
    capabilities: { responses: true, streaming: true, tool_calls: true, context_continuation: true },
    pricing: null
  },
  {
    slug: "legacy-chat",
    display_name: "Legacy Chat",
    origin: "custom",
    provider_id: "legacy",
    visibility: "hide",
    supported_in_api: false,
    capabilities: { responses: true, streaming: false, tool_calls: false, context_continuation: false },
    pricing: null
  }
];

describe("modelCatalog", () => {
  it("summarizes model compatibility and pricing gaps", () => {
    expect(summarizeCatalog(models)).toEqual({
      modelCount: 3,
      mainListCount: 2,
      restrictedCount: 0,
      incompatibleCount: 1,
      missingPricingCount: 2
    });
  });

  it("filters by search text and provider", () => {
    expect(filterCatalogModels(models, { query: "claude", provider: "anthropic", capability: "tool_calls" })).toHaveLength(1);
    expect(filterCatalogModels(models, { query: "gpt", provider: "all", capability: "all" })[0].slug).toBe("gpt-5.5");
  });

  it("returns tooltip rows from database pricing fields", () => {
    expect(modelPriceRows(models[0])).toEqual([
      ["输入", "$2.50 / 100万 tokens"],
      ["输出", "$15.00 / 100万 tokens"],
      ["命中缓存", "$0.25 / 100万 tokens"],
      ["写入缓存", "$3.00 / 100万 tokens"]
    ]);
    expect(modelPriceRows(models[1])).toEqual([["价格", "未配置"]]);
  });
});
