import type { CapabilityKey, CatalogModel, ModelCatalogSummary, ModelFilter, ModelPricing } from "./types";
import { translations, type Language } from "./i18n";

const REQUIRED_CAPABILITIES: CapabilityKey[] = ["responses", "streaming", "tool_calls", "context_continuation"];

export function modelIsCompatible(model: CatalogModel): boolean {
  const capabilities = model.capabilities;
  if (!capabilities) {
    return false;
  }
  return REQUIRED_CAPABILITIES.every((key) => Boolean(capabilities[key]));
}

export function modelInMainList(model: CatalogModel): boolean {
  const visibility = String(model.visibility ?? "list").toLowerCase();
  return model.supported_in_api !== false && ["list", "visible"].includes(visibility);
}

export function pricingMissing(pricing: ModelPricing | null | undefined): boolean {
  if (!pricing) {
    return true;
  }
  return !["input_price", "output_price", "cached_input_price", "cache_write_price"].some((key) => {
    const value = pricing[key];
    return value !== undefined && value !== null && String(value).trim() !== "";
  });
}

export function summarizeCatalog(models: CatalogModel[]): ModelCatalogSummary {
  return models.reduce<ModelCatalogSummary>(
    (summary, model) => {
      const compatible = modelIsCompatible(model);
      summary.modelCount += 1;
      if (!compatible) {
        summary.incompatibleCount += 1;
      } else if (modelInMainList(model)) {
        summary.mainListCount += 1;
      } else {
        summary.restrictedCount += 1;
      }
      if (pricingMissing(model.pricing)) {
        summary.missingPricingCount += 1;
      }
      return summary;
    },
    {
      modelCount: 0,
      mainListCount: 0,
      restrictedCount: 0,
      incompatibleCount: 0,
      missingPricingCount: 0
    }
  );
}

export function filterCatalogModels(models: CatalogModel[], filter: ModelFilter): CatalogModel[] {
  const query = filter.query.trim().toLowerCase();
  return models.filter((model) => {
    const haystack = `${model.slug} ${model.display_name ?? ""} ${model.provider_id ?? ""} ${model.origin ?? ""}`.toLowerCase();
    if (query && !haystack.includes(query)) {
      return false;
    }
    if (filter.provider !== "all" && model.provider_id !== filter.provider) {
      return false;
    }
    if (filter.capability !== "all" && !model.capabilities?.[filter.capability]) {
      return false;
    }
    return true;
  });
}

export function providerOptions(models: CatalogModel[]): string[] {
  return Array.from(new Set(models.map((model) => model.provider_id).filter((provider): provider is string => Boolean(provider)))).sort();
}

export function modelPriceRows(model: CatalogModel, language: Language = "zh"): [string, string][] {
  const pricing = model.pricing;
  const labels = translations[language].price;
  if (pricingMissing(pricing)) {
    return [[labels.price, labels.notConfigured]];
  }
  const rows: [string, string][] = [];
  addPriceRow(rows, labels.input, pricing?.input_price, pricing, labels.perMillionTokens);
  addPriceRow(rows, labels.output, pricing?.output_price, pricing, labels.perMillionTokens);
  addPriceRow(rows, labels.cachedInput, pricing?.cached_input_price, pricing, labels.perMillionTokens);
  addPriceRow(rows, labels.cacheWrite, pricing?.cache_write_price, pricing, labels.perMillionTokens);
  return rows.length ? rows : [[labels.price, labels.notConfigured]];
}

export function modelPriceSummary(model: CatalogModel, language: Language = "zh"): { primary: string; secondary: string; hasDetails: boolean } {
  const rows = modelPriceRows(model, language);
  const labels = translations[language].price;
  if (rows.length === 1 && rows[0]?.[0] === labels.price) {
    return { primary: rows[0][1], secondary: "", hasDetails: false };
  }
  const [primaryLabel, primaryValue] = rows[0] || [labels.price, labels.notConfigured];
  const [secondaryLabel, secondaryValue] = rows[1] || ["", ""];
  return {
    primary: compactPriceText(primaryLabel, primaryValue),
    secondary: secondaryLabel ? compactPriceText(secondaryLabel, secondaryValue) : "",
    hasDetails: rows.length > 2
  };
}

function addPriceRow(rows: [string, string][], label: string, value: string | number | null | undefined, pricing: ModelPricing | null | undefined, perMillionTokens: string) {
  if (value === undefined || value === null || String(value).trim() === "") {
    return;
  }
  const currency = String(pricing?.currency || "USD").toUpperCase();
  const unit = pricing?.unit === "per_1m_tokens" || !pricing?.unit ? perMillionTokens : String(pricing.unit);
  const prefix = currency === "USD" ? "$" : `${currency} `;
  rows.push([label, `${prefix}${value} / ${unit}`]);
}

function compactPriceText(label: string, value: string) {
  return `${label} ${value.split(" / ")[0] || value}`;
}
