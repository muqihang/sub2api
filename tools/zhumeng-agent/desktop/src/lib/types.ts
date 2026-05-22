export type CapabilityKey = "responses" | "streaming" | "tool_calls" | "context_continuation";

export interface ModelCapabilities {
  responses?: boolean;
  streaming?: boolean;
  tool_calls?: boolean;
  context_continuation?: boolean;
  [key: string]: unknown;
}

export interface ModelPricing {
  input_price?: string | number | null;
  output_price?: string | number | null;
  cached_input_price?: string | number | null;
  cache_write_price?: string | number | null;
  currency?: string | null;
  unit?: string | null;
  source?: string | null;
  [key: string]: unknown;
}

export interface CatalogModel {
  slug: string;
  display_name?: string;
  origin?: string | null;
  provider_id?: string | null;
  visibility?: string | null;
  supported_in_api?: boolean;
  capabilities?: ModelCapabilities | null;
  pricing?: ModelPricing | null;
  context_window?: number;
  max_output_tokens?: number;
  description?: string;
  [key: string]: unknown;
}

export interface ModelCatalogSummary {
  modelCount: number;
  mainListCount: number;
  restrictedCount: number;
  incompatibleCount: number;
  missingPricingCount: number;
}

export interface ModelFilter {
  query: string;
  provider: string;
  capability: CapabilityKey | "all";
}

export interface SidecarEnvelope<T = unknown> {
  schema_version?: number;
  ok: boolean;
  command?: string;
  status: string;
  data?: T;
  warnings?: unknown[];
  error?: {
    code?: string;
    message?: string;
  } | null;
}

export interface DesktopStatus {
  status?: string;
  global_status?: string;
  proxy?: {
    status?: string;
    host?: string;
    port?: number;
    pid?: number;
    health_url?: string;
  };
  backend?: {
    status?: string;
    server_base_url?: string;
    gateway_base_url?: string;
  };
  authorization?: {
    status?: string;
    device_id?: string | number;
    managed_session_id_redacted?: string;
  };
  adapters?: {
    codex?: {
      status?: string;
      enhancements?: Record<string, unknown>;
      restart_required?: boolean;
    };
  };
  model_catalog?: {
    model_count?: number;
    main_list_count?: number;
    restricted_count?: number;
    incompatible_count?: number;
    missing_pricing_count?: number;
    last_synced_at?: string;
    catalog_path?: string;
    source?: string;
    models?: CatalogModel[];
  };
  state?: Record<string, unknown>;
}

export type DeepLinkRoute =
  | { action: "setup" | "reauth"; client: string; code: string; server: string }
  | { action: "open"; app: string };
