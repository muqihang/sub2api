import { apiClient } from '../client'

export interface AugmentGatewaySummary {
  entitlement_groups?: {
    total_count?: number
    rows?: AugmentGatewayEntitlementGroupRow[]
  }
  provider_routing_groups?: {
    total_count?: number
    configured_route_policy_version?: string
    route_policy_version?: string
    source_priority?: string[]
    rows?: AugmentProviderGroupRow[]
  }
  official_session_pool?: {
    total_count?: number
    active_count?: number
    healthy_count?: number
    source_counts?: Record<string, number>
  }
  usage?: {
    estimated_cost?: number
    settled_cost?: number
    free_quota?: number
    paid_balance?: number
    currency?: string
    cache_hit_ratio?: number
    total_cache_read_tokens?: number
    total_cache_creation_tokens?: number
  }
  models?: AugmentGatewayModelRow[]
  provider_groups?: AugmentProviderGroupRow[]
  official_session_count?: number
  active_session_count?: number
  healthy_session_count?: number
  source_priority?: string[]
  configured_route_policy_version?: string
  route_policy_version?: string
  estimated_cost?: number
  settled_cost?: number
  cache_hit_ratio?: number
}

export interface AugmentGatewayEntitlementGroupRow {
  id: number
  name: string
  status: string
  total_accounts: number
  active_accounts: number
}

export interface AugmentProviderGroupRow {
  provider: string
  group_id: number
  healthy: boolean
  active_accounts: number
  total_accounts: number
  version?: number
}

export interface AugmentGatewayModelRow {
  model: {
    id: string
    provider: string
    upstream_model: string
  }
  enabled: boolean
  visible: boolean
  explicit_pricing: boolean
  smoke_status: string
  provider_healthy: boolean
  settings_version: number
  settings_namespace: string
}

export interface AugmentPoolSessionAdminRow {
  id: number
  source: string
  tenant_origin: string
  status: string
  fingerprint_prefix?: string
  has_credential_payload?: boolean
  health_score?: number
  created_by_admin_id?: number
}

export interface AugmentGatewayAdminUsageRow {
  model: string
  upstream_model?: string
  request_scope?: string
  feature_scope?: string
  group_id?: number
  route_policy_version?: string
  augment_session_id?: string
  request_id: string
  estimated_cost: number
  settled_cost: number
  cache_read_tokens: number
  cache_creation_tokens: number
  created_at?: string
}

export async function getAugmentGatewaySummary(): Promise<AugmentGatewaySummary> {
  const { data } = await apiClient.get<AugmentGatewaySummary>('/admin/augment-gateway/summary')
  return data
}

export async function getAugmentProviderGroups(): Promise<{ rows: AugmentProviderGroupRow[] }> {
  const { data } = await apiClient.get<{ rows: AugmentProviderGroupRow[] }>('/admin/augment-gateway/provider-groups')
  return data
}

export async function updateAugmentProviderGroups(payload: {
  provider: string
  group_id: number
  expected_version?: number
  request_id?: string
}): Promise<Record<string, unknown>> {
  const { data } = await apiClient.put('/admin/augment-gateway/provider-groups', payload)
  return data as Record<string, unknown>
}

export async function getAugmentGatewaySourcePriority(): Promise<{ sources: string[] }> {
  const { data } = await apiClient.get<{ sources: string[] }>('/admin/augment-gateway/source-priority')
  return data
}

export async function updateAugmentGatewaySourcePriority(payload: {
  sources: string[]
  expected_version?: number
  request_id?: string
}): Promise<Record<string, unknown>> {
  const { data } = await apiClient.put('/admin/augment-gateway/source-priority', payload)
  return data as Record<string, unknown>
}

export async function getAugmentGatewayModels(): Promise<{ rows: AugmentGatewayModelRow[] }> {
  const { data } = await apiClient.get<{ rows: AugmentGatewayModelRow[] }>('/admin/augment-gateway/models')
  return data
}

export async function updateAugmentGatewayModel(modelId: string, payload: {
  enabled: boolean
  smoke_status: string
  expected_version?: number
  request_id?: string
}): Promise<Record<string, unknown>> {
  const { data } = await apiClient.put(`/admin/augment-gateway/models/${modelId}`, payload)
  return data as Record<string, unknown>
}

export async function listAugmentPoolSessions(): Promise<{ rows: AugmentPoolSessionAdminRow[] }> {
  const { data } = await apiClient.get<{ rows: AugmentPoolSessionAdminRow[] }>('/admin/augment-gateway/pool-sessions')
  return data
}

export async function createAugmentPoolSessionBindIntent(payload: {
  mode: string
  source: string
  tenant_allowlist: string[]
}): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post('/admin/augment-gateway/pool-sessions/bind-intents', payload)
  return data as Record<string, unknown>
}

export async function bindAugmentPoolSession(payload: {
  bind_token: string
  bind_intent_id: string
  state: string
  mode: string
  source: string
  payload: Record<string, unknown>
  request_id?: string
}): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post('/admin/augment-gateway/pool-sessions/bind', payload)
  return data as Record<string, unknown>
}

export async function revokeAugmentPoolSessionAdmin(sessionId: number): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post(`/admin/augment-gateway/pool-sessions/${sessionId}/revoke`, {})
  return data as Record<string, unknown>
}

export async function disableAugmentPoolSessionAdmin(sessionId: number): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post(`/admin/augment-gateway/pool-sessions/${sessionId}/disable`, {})
  return data as Record<string, unknown>
}

export async function requireAugmentPoolSessionReloginAdmin(sessionId: number): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post(`/admin/augment-gateway/pool-sessions/${sessionId}/require-relogin`, {})
  return data as Record<string, unknown>
}

export async function getAugmentPoolSessionDiagnosticsAdmin(sessionId: number): Promise<Record<string, unknown>> {
  const { data } = await apiClient.get(`/admin/augment-gateway/pool-sessions/${sessionId}/diagnostics`)
  return data as Record<string, unknown>
}

export async function getAugmentGatewayAdminUsage(params: {
  page?: number
  page_size?: number
}): Promise<{ rows: AugmentGatewayAdminUsageRow[]; page?: Record<string, unknown> }> {
  const { data } = await apiClient.get('/admin/augment-gateway/usage', { params })
  return data as { rows: AugmentGatewayAdminUsageRow[]; page?: Record<string, unknown> }
}

export const augmentGatewayAPI = {
  getAugmentGatewaySummary,
  getAugmentProviderGroups,
  updateAugmentProviderGroups,
  getAugmentGatewaySourcePriority,
  updateAugmentGatewaySourcePriority,
  getAugmentGatewayModels,
  updateAugmentGatewayModel,
  listAugmentPoolSessions,
  createAugmentPoolSessionBindIntent,
  bindAugmentPoolSession,
  revokeAugmentPoolSessionAdmin,
  disableAugmentPoolSessionAdmin,
  requireAugmentPoolSessionReloginAdmin,
  getAugmentPoolSessionDiagnosticsAdmin,
  getAugmentGatewayAdminUsage,
}

export default augmentGatewayAPI
