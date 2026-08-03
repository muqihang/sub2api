/**
 * Admin Gemini API endpoints
 * Handles Gemini OAuth flows for administrators
 */

import { apiClient } from '../client'

export interface GeminiAuthUrlResponse {
  auth_url: string
  session_id: string
  state: string
}

export interface GeminiOAuthCapabilities {
  ai_studio_oauth_enabled: boolean
  required_redirect_uris: string[]
}

export interface GeminiAuthUrlRequest {
  proxy_id?: number
  project_id?: string
  oauth_type?: 'code_assist' | 'google_one' | 'ai_studio'
  tier_id?: string
}

export interface GeminiExchangeCodeRequest {
  session_id: string
  state: string
  code: string
  proxy_id?: number
  oauth_type?: 'code_assist' | 'google_one' | 'ai_studio'
  tier_id?: string
}

export interface GeminiCreateFromOAuthRequest {
  session_id: string
  state: string
  code: string
  proxy_id?: number
  oauth_type?: 'code_assist' | 'google_one' | 'ai_studio'
  tier_id?: string
  name?: string
  notes?: string
  extra?: Record<string, unknown>
  concurrency?: number
  priority?: number
  rate_multiplier?: number
  load_factor?: number
  group_ids?: number[]
  expires_at?: number
  auto_pause_on_expired?: boolean
  confirm_mixed_channel_risk?: boolean
}

export type GeminiTokenInfo = {
  access_token?: string
  refresh_token?: string
  token_type?: string
  scope?: string
  expires_in?: number
  expires_at?: number
  project_id?: string
  oauth_type?: string
  tier_id?: string
  extra?: Record<string, unknown>
  [key: string]: unknown
}

export async function generateAuthUrl(
  payload: GeminiAuthUrlRequest
): Promise<GeminiAuthUrlResponse> {
  const { data } = await apiClient.post<GeminiAuthUrlResponse>(
    '/admin/gemini/oauth/auth-url',
    payload
  )
  return data
}

export async function exchangeCode(payload: GeminiExchangeCodeRequest): Promise<GeminiTokenInfo> {
  const { data } = await apiClient.post<GeminiTokenInfo>(
    '/admin/gemini/oauth/exchange-code',
    payload
  )
  return data
}

export async function getCapabilities(): Promise<GeminiOAuthCapabilities> {
  const { data } = await apiClient.get<GeminiOAuthCapabilities>('/admin/gemini/oauth/capabilities')
  return data
}

export async function createFromOAuth(payload: GeminiCreateFromOAuthRequest): Promise<unknown> {
  const { data } = await apiClient.post('/admin/gemini/create-from-oauth', payload)
  return data
}

export async function reauthorizeAccountFromOAuth(
  accountId: number,
  payload: GeminiExchangeCodeRequest
): Promise<unknown> {
  const { data } = await apiClient.post(
    `/admin/gemini/accounts/${accountId}/reauthorize-from-oauth`,
    payload
  )
  return data
}

export default { generateAuthUrl, exchangeCode, getCapabilities, createFromOAuth, reauthorizeAccountFromOAuth }
