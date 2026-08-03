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

// ─── Codex Entry Center API ───

import type {
  CodexEntrySummary,
  CodexCreateSetupSessionRequest,
  CodexCreateSetupSessionResponse,
  CodexRegenerateSetupSessionResponse,
  CodexDiagnoseRequest,
  CodexDiagnoseReport,
  CodexDeviceActionResponse,
} from '@/types'

export async function getCodexSummary(): Promise<CodexEntrySummary> {
  const { data } = await apiClient.get<CodexEntrySummary>('/codex/summary')
  return data
}

export async function createCodexSetupSession(req: CodexCreateSetupSessionRequest): Promise<CodexCreateSetupSessionResponse> {
  const { data } = await apiClient.post<CodexCreateSetupSessionResponse>('/codex/setup-sessions', req)
  return data
}

export async function regenerateCodexSetupSession(sessionId: string): Promise<CodexRegenerateSetupSessionResponse> {
  const { data } = await apiClient.post<CodexRegenerateSetupSessionResponse>(`/codex/setup-sessions/${sessionId}/regenerate`)
  return data
}

export async function diagnoseCodex(req: CodexDiagnoseRequest): Promise<CodexDiagnoseReport> {
  const { data } = await apiClient.post<CodexDiagnoseReport>('/codex/diagnose', req)
  return data
}

export async function resyncCodexDevice(deviceId: number): Promise<CodexDeviceActionResponse> {
  const { data } = await apiClient.post<CodexDeviceActionResponse>(`/codex/devices/${deviceId}/resync`)
  return data
}

export async function repairCodexDevice(deviceId: number): Promise<CodexDeviceActionResponse> {
  const { data } = await apiClient.post<CodexDeviceActionResponse>(`/codex/devices/${deviceId}/repair`)
  return data
}

export async function reattachCodexDevice(deviceId: number): Promise<CodexDeviceActionResponse> {
  const { data } = await apiClient.post<CodexDeviceActionResponse>(`/codex/devices/${deviceId}/reattach`)
  return data
}

export async function revokeCodexAttachment(deviceId: number): Promise<CodexDeviceActionResponse> {
  const { data } = await apiClient.post<CodexDeviceActionResponse>(`/codex/devices/${deviceId}/revoke-attachment`)
  return data
}

export async function removeCodexDevice(deviceId: number): Promise<CodexDeviceActionResponse> {
  const { data } = await apiClient.delete<CodexDeviceActionResponse>(`/codex/devices/${deviceId}`)
  return data
}
