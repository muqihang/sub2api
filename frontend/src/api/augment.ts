import { apiClient } from './client'

export type AugmentQuickLoginGrantRequest = Record<string, string>

export interface AugmentQuickLoginGrantResponse {
  vscode_deeplink?: string
  deeplink?: string
  deeplink_url?: string
  url?: string
  [key: string]: unknown
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
