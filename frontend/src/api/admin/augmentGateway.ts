import { apiClient } from '../client'

export interface AugmentGatewaySummary {
  provider_groups?: Array<Record<string, unknown>>
  models?: Array<Record<string, unknown>>
  official_session_count?: number
  cache_hit_ratio?: number
  estimated_cost?: number
  settled_cost?: number
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
  smoke_status: string
  provider_healthy: boolean
  settings_version: number
  settings_namespace: string
}

export interface AugmentOfficialSessionAdminRow {
  user_id: number
  source: string
  tenant_origin: string
  status: string
  fingerprint_prefix?: string
  has_credential_payload?: boolean
}

export interface AugmentGatewayAdminUsageRow {
  model: string
  request_id: string
  estimated_cost: number
  settled_cost: number
  cache_read_tokens: number
  cache_creation_tokens: number
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

export async function listAugmentOfficialSessions(): Promise<{ rows: AugmentOfficialSessionAdminRow[] }> {
  const { data } = await apiClient.get<{ rows: AugmentOfficialSessionAdminRow[] }>('/admin/augment-gateway/official-sessions')
  return data
}

export async function revokeAugmentOfficialSessionAdmin(userId: number): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post(`/admin/augment-gateway/official-sessions/${userId}/revoke`, {})
  return data as Record<string, unknown>
}

export async function disableAugmentOfficialSessionAdmin(userId: number): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post(`/admin/augment-gateway/official-sessions/${userId}/disable`, {})
  return data as Record<string, unknown>
}

export async function requireAugmentOfficialSessionReloginAdmin(userId: number): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post(`/admin/augment-gateway/official-sessions/${userId}/require-relogin`, {})
  return data as Record<string, unknown>
}

export async function getAugmentOfficialSessionDiagnosticsAdmin(userId: number): Promise<Record<string, unknown>> {
  const { data } = await apiClient.get(`/admin/augment-gateway/official-sessions/${userId}/diagnostics`)
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
  getAugmentGatewayModels,
  updateAugmentGatewayModel,
  listAugmentOfficialSessions,
  revokeAugmentOfficialSessionAdmin,
  disableAugmentOfficialSessionAdmin,
  requireAugmentOfficialSessionReloginAdmin,
  getAugmentOfficialSessionDiagnosticsAdmin,
  getAugmentGatewayAdminUsage,
}

export default augmentGatewayAPI
