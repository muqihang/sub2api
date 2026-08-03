import type { AugmentQuickLoginEditorTarget } from '@/utils/augmentIdeTargets'
import { apiClient } from './client'

export interface AugmentQuickLoginGrantRequest {
  mode: string
  source: string
  editor_target?: AugmentQuickLoginEditorTarget
}

export interface AugmentQuickLoginGrantResponse {
  vscode_deeplink?: string
  deeplink?: string
  deeplink_url?: string
  url?: string
  editor_target?: string
  target_scheme?: string
  target_verified?: boolean
  target_warning?: string
  [key: string]: unknown
}

export interface AugmentOfficialSessionBindIntentRequest {
  mode: string
  source: string
  tenant_allowlist: string[]
}

export interface AugmentOfficialSessionBindIntentResponse {
  bind_intent_id: string
  state: string
  expires_at: string
  bind_token: string
}

export interface AugmentOfficialSessionView {
  user_id: number
  mode: string
  source: string
  tenant_origin: string
  portal_origin?: string | null
  scopes?: string[]
  expires_at?: string | null
  last_refresh_at?: string | null
  last_success_at?: string | null
  last_error_at?: string | null
  last_error_code?: string | null
  status: string
  fingerprint_prefix?: string
  created_at?: string
  updated_at?: string
  revoked_at?: string | null
}

export interface AugmentOfficialSessionBindRequest {
  bind_token: string
  bind_intent_id: string
  state: string
  mode: string
  source: string
  payload: Record<string, unknown>
  request_id?: string
}

export async function requestAugmentQuickLoginGrant(
  payload: AugmentQuickLoginGrantRequest
): Promise<AugmentQuickLoginGrantResponse> {
  const { data } = await apiClient.post<AugmentQuickLoginGrantResponse>(
    '/plugin/augment/quick-login/grant',
    payload
  )
  return data
}

export async function createAugmentOfficialSessionBindIntent(
  payload: AugmentOfficialSessionBindIntentRequest
): Promise<AugmentOfficialSessionBindIntentResponse> {
  const { data } = await apiClient.post<AugmentOfficialSessionBindIntentResponse>(
    '/plugin/augment/official-session/bind-intents',
    payload
  )
  return data
}

export async function bindAugmentOfficialSession(
  payload: AugmentOfficialSessionBindRequest
): Promise<AugmentOfficialSessionView> {
  const { data } = await apiClient.post<AugmentOfficialSessionView>(
    '/plugin/augment/official-session/bind',
    payload
  )
  return data
}

export async function getAugmentOfficialSession(): Promise<AugmentOfficialSessionView | null> {
  const { data } = await apiClient.get<AugmentOfficialSessionView | null>(
    '/plugin/augment/official-session'
  )
  return data
}

export async function revokeAugmentOfficialSession(): Promise<AugmentOfficialSessionView | null> {
  const { data } = await apiClient.post<AugmentOfficialSessionView | null>(
    '/plugin/augment/official-session/revoke',
    {}
  )
  return data
}
