import { apiClient } from '../client'

export type FormalPoolProfile = 'normal' | 'aggressive'
export type FormalPoolProxyMode = 'existing' | 'create'

export interface FormalPoolProxyInput {
  name: string
  protocol: 'http' | 'https' | 'socks5' | 'socks5h'
  host: string
  port: number
  username?: string
  password?: string
}

export interface FormalPoolStartRequest {
  proxy_mode: FormalPoolProxyMode
  proxy_id?: number
  proxy?: FormalPoolProxyInput
  pool_profile?: FormalPoolProfile
  group_id: number
  account_name: string
  notes?: string
  concurrency?: number
}

export interface FormalPoolOAuthSummary {
  email_present?: boolean
  account_uuid_present?: boolean
  organization_uuid_present?: boolean
  scope_contains_user_inference?: boolean
  scope_contains_claude_code?: boolean
  expires_in_bucket?: string
}

export interface FormalPoolCheck {
  name: string
  status: 'pass' | 'warn' | 'fail'
  message?: string
}

export interface FormalPoolSession {
  id: string
  status: string
  proxy_id?: number
  proxy_ref?: string
  egress_bucket?: string
  pool_profile: FormalPoolProfile
  group_id: number
  account_name: string
  concurrency: number
  auth_url?: string
  oauth_session_id?: string
  browser_egress_check_url?: string
  browser_egress_verified?: boolean
  account_id?: number
  account_ref?: string
  oauth_summary?: FormalPoolOAuthSummary
  safe_summary?: Record<string, unknown>
  checks?: FormalPoolCheck[]
  cc_gateway_runtime_registered?: boolean
  healthcheck_passed?: boolean
  production_ready?: boolean
}

export interface FormalPoolAcceptanceResult {
  status: string
  account_id: number
  account_ref: string
  proxy_ref: string
  egress_bucket: string
  pool_profile: FormalPoolProfile
  checks: FormalPoolCheck[]
  no_real_messages_request_performed: boolean
  activation_required: boolean
  status_code_bucket?: string
  cc_gateway_seen?: boolean
  raw_capture_present?: boolean
  raw_capture_ref?: string
  fallback_detected?: boolean
  proxy_mismatch?: boolean
  risk_text_detected?: boolean
}

export interface FormalPoolSetupTokenCookieRequest {
  session_key?: string
  code?: string
  proxy_id?: number
}

export async function createSession(payload: FormalPoolStartRequest): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>('/admin/claude-onboarding/sessions', payload)
  return data
}

export async function getSession(id: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.get<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}`)
  return data
}

export async function testProxy(id: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/test-proxy`)
  return data
}

export async function attestBrowserEgress(id: string, verificationCode: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/browser-egress-attestation`, {
    confirmed: true,
    verification_code: verificationCode
  })
  return data
}

export async function generateAuthUrl(id: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/generate-auth-url`)
  return data
}

export async function exchangeCodeAndCreate(id: string, code: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/exchange-code-and-create`, { code })
  return data
}

export async function setupTokenCookieAuthAndCreate(id: string, sessionKey: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/setup-token-cookie-auth-and-create`, {
    session_key: sessionKey
  })
  return data
}

export async function runAcceptance(id: string): Promise<FormalPoolAcceptanceResult> {
  const { data } = await apiClient.post<FormalPoolAcceptanceResult>(`/admin/claude-onboarding/sessions/${id}/acceptance`)
  return data
}

export async function activate(id: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/activate`)
  return data
}

export async function refreshOnly(id: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/refresh-only`)
  return data
}

export async function runtimeRegister(id: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/runtime-register`)
  return data
}

export async function healthcheck(id: string): Promise<FormalPoolAcceptanceResult> {
  const { data } = await apiClient.post<FormalPoolAcceptanceResult>(`/admin/claude-onboarding/sessions/${id}/healthcheck`)
  return data
}

export async function startWarming(id: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/start-warming`)
  return data
}

export async function promoteProduction(id: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/promote-production`)
  return data
}

export async function abort(id: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/abort`)
  return data
}

export default {
  createSession,
  getSession,
  testProxy,
  attestBrowserEgress,
  generateAuthUrl,
  exchangeCodeAndCreate,
  setupTokenCookieAuthAndCreate,
  runAcceptance,
  activate,
  refreshOnly,
  runtimeRegister,
  healthcheck,
  startWarming,
  promoteProduction,
  abort
}
