import { apiClient } from './client'
import type { CodexManagedDevice, CodexSetupGrantResponse } from '@/types'

export async function createCodexSetupGrant(apiKeyId: number): Promise<CodexSetupGrantResponse> {
  const { data } = await apiClient.post<CodexSetupGrantResponse>('/codex/setup-grants', {
    api_key_id: apiKeyId,
    client: 'codex',
    mode: 'managed_proxy',
  })
  return data
}

export async function listCodexManagedDevices(apiKeyId?: number): Promise<CodexManagedDevice[]> {
  const { data } = await apiClient.get<CodexManagedDevice[]>('/codex/devices', {
    params: apiKeyId != null ? { api_key_id: apiKeyId } : undefined,
  })
  return data
}

export async function revokeCodexManagedDevice(deviceId: number): Promise<{ device_id: number; revoked: boolean }> {
  const { data } = await apiClient.post<{ device_id: number; revoked: boolean }>('/codex/devices/revoke', {
    device_id: deviceId,
  })
  return data
}
