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

export type KnownBrowserEgressCheckStatus =
  | 'idle'
  | 'waiting'
  | 'verified'
  | 'mismatch'
  | 'expired'

export type BrowserEgressCheckStatus = KnownBrowserEgressCheckStatus | string

export type KnownBrowserEgressErrorCode =
  | 'nonce_expired'
  | 'mismatch'
  | 'no_proxy_egress'

export type BrowserEgressErrorCode = KnownBrowserEgressErrorCode | string

export interface FormalPoolSession {
  id: string
  version: number
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
  browser_egress_check_status?: BrowserEgressCheckStatus
  browser_egress_browser_ip_bucket?: string
  browser_egress_proxy_ip_bucket?: string
  browser_egress_last_error_code?: BrowserEgressErrorCode
  browser_egress_mismatch_at?: string
  nonce_expires_at?: string
  account_id?: number
  account_ref?: string
  oauth_summary?: FormalPoolOAuthSummary
  safe_summary?: Record<string, unknown>
  checks?: FormalPoolCheck[]
  cc_gateway_runtime_registered?: boolean
  healthcheck_passed?: boolean
  production_ready?: boolean
}

export interface FormalPoolMutationResult {
  version: number
  status: string
}

export interface FormalPoolAcceptanceResult extends FormalPoolMutationResult {
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

function versionHeaders(session: Pick<FormalPoolSession, 'version'>) {
  return { headers: { 'If-Match': `"${session.version}"` } }
}

function idempotencyHeaders(session: Pick<FormalPoolSession, 'version'>, key: string) {
  return { headers: { ...versionHeaders(session).headers, 'Idempotency-Key': key } }
}

export async function createSession(payload: FormalPoolStartRequest, idempotencyKey: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>('/admin/claude-onboarding/sessions', payload, {
    headers: { 'If-Match': '"0"', 'Idempotency-Key': idempotencyKey }
  })
  return data
}

export async function getSession(id: string, signal?: AbortSignal): Promise<FormalPoolSession> {
  const { data } = await apiClient.get<FormalPoolSession>(`/admin/claude-onboarding/sessions/${id}`, { signal })
  return data
}

export async function testProxy(session: FormalPoolSession): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/test-proxy`, {}, versionHeaders(session))
  return data
}

export async function attestBrowserEgress(session: FormalPoolSession, verificationCode: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/browser-egress-attestation`, {
    confirmed: true,
    verification_code: verificationCode
  }, versionHeaders(session))
  return data
}

export async function generateAuthUrl(session: FormalPoolSession): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/generate-auth-url`, {}, versionHeaders(session))
  return data
}

export async function exchangeCodeAndCreate(session: FormalPoolSession, code: string, idempotencyKey: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/exchange-code-and-create`, { code }, idempotencyHeaders(session, idempotencyKey))
  return data
}

export async function setupTokenCookieAuthAndCreate(session: FormalPoolSession, sessionKey: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/setup-token-cookie-auth-and-create`, {
    session_key: sessionKey
  }, versionHeaders(session))
  return data
}

export async function runAcceptance(session: FormalPoolSession): Promise<FormalPoolAcceptanceResult> {
  const { data } = await apiClient.post<FormalPoolAcceptanceResult>(`/admin/claude-onboarding/sessions/${session.id}/acceptance`, {}, versionHeaders(session))
  return data
}

export async function activate(session: FormalPoolSession): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/activate`, {}, versionHeaders(session))
  return data
}

export async function refreshOnly(session: FormalPoolSession): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/refresh-only`, {}, versionHeaders(session))
  return data
}

export async function runtimeRegister(session: FormalPoolSession): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/runtime-register`, {}, versionHeaders(session))
  return data
}

export async function healthcheck(session: FormalPoolSession): Promise<FormalPoolAcceptanceResult> {
  const { data } = await apiClient.post<FormalPoolAcceptanceResult>(`/admin/claude-onboarding/sessions/${session.id}/healthcheck`, {}, versionHeaders(session))
  return data
}

export async function startWarming(session: FormalPoolSession): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/start-warming`, {}, versionHeaders(session))
  return data
}

export async function promoteProduction(session: FormalPoolSession, idempotencyKey: string): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/promote-production`, {}, idempotencyHeaders(session, idempotencyKey))
  return data
}

export async function abort(session: FormalPoolSession): Promise<FormalPoolSession> {
  const { data } = await apiClient.post<FormalPoolSession>(`/admin/claude-onboarding/sessions/${session.id}/abort`, {}, versionHeaders(session))
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
