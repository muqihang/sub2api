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

export async function runAcceptance(id: string): Promise<FormalPoolAcceptanceResult> {
  const { data } = await apiClient.post<FormalPoolAcceptanceResult>(`/admin/claude-onboarding/sessions/${id}/acceptance`)
  return data
}

export async function activate(id: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}/activate`)
  return data
}

export default {
  createSession,
  getSession,
  testProxy,
  attestBrowserEgress,
  generateAuthUrl,
  exchangeCodeAndCreate,
  runAcceptance,
  activate
}
