import { apiClient } from '../client'
import type { Account, FormalPoolOperationsDiagnostics } from '@/types'

export interface FormalPoolOperationResult {
  account: FormalPoolOperationFailureAccount
  diagnostics?: FormalPoolOperationsDiagnostics
}

export type FormalPoolOperationFailureAccount = Pick<Account, 'id'> & Partial<Account>

export interface ReplaceSetupTokenRequest {
  session_key: string
  run_runtime_register?: boolean
  run_healthcheck?: boolean
}

export interface SwapProxyRequest {
  proxy_id: number
  run_proxy_test?: boolean
  run_runtime_register?: boolean
  run_healthcheck?: boolean
}

export class FormalPoolOperationError extends Error {
  status?: number
  code?: string | number
  account?: FormalPoolOperationFailureAccount
  diagnostics?: FormalPoolOperationsDiagnostics

  constructor(message: string, options: {
    status?: number
    code?: string | number
    account?: FormalPoolOperationFailureAccount
    diagnostics?: FormalPoolOperationsDiagnostics
  } = {}) {
    super(message)
    this.name = 'FormalPoolOperationError'
    this.status = options.status
    this.code = options.code
    this.account = options.account
    this.diagnostics = options.diagnostics
  }
}

export function toFormalPoolOperationError(error: unknown): FormalPoolOperationError {
  if (error instanceof FormalPoolOperationError) return error
  const payload = (error && typeof error === 'object') ? error as Record<string, any> : {}
  return new FormalPoolOperationError(
    typeof payload.message === 'string' ? payload.message : 'Formal Pool operation failed',
    {
      status: typeof payload.status === 'number' ? payload.status : undefined,
      code: typeof payload.code === 'string' || typeof payload.code === 'number' ? payload.code : payload.error,
      account: payload.account,
      diagnostics: payload.diagnostics,
    }
  )
}

export async function getDiagnostics(accountId: number): Promise<FormalPoolOperationsDiagnostics> {
  const { data } = await apiClient.get<FormalPoolOperationsDiagnostics>(`/admin/accounts/${accountId}/formal-pool/diagnostics`)
  return data
}

async function runOperation<T extends FormalPoolOperationResult>(request: () => Promise<{ data: T }>): Promise<T> {
  try {
    const { data } = await request()
    return data
  } catch (error) {
    throw toFormalPoolOperationError(error)
  }
}

export async function replaceSetupToken(accountId: number, payload: ReplaceSetupTokenRequest): Promise<FormalPoolOperationResult> {
  return runOperation(() => apiClient.post<FormalPoolOperationResult>(`/admin/accounts/${accountId}/setup-token/replace`, payload))
}

export async function runtimeRegister(accountId: number): Promise<FormalPoolOperationResult> {
  return runOperation(() => apiClient.post<FormalPoolOperationResult>(`/admin/accounts/${accountId}/formal-pool/runtime-register`))
}

export async function healthcheck(accountId: number): Promise<FormalPoolOperationResult> {
  return runOperation(() => apiClient.post<FormalPoolOperationResult>(`/admin/accounts/${accountId}/formal-pool/healthcheck`))
}

export async function startWarming(accountId: number): Promise<FormalPoolOperationResult> {
  return runOperation(() => apiClient.post<FormalPoolOperationResult>(`/admin/accounts/${accountId}/formal-pool/start-warming`))
}

export async function promoteProduction(accountId: number): Promise<FormalPoolOperationResult> {
  return runOperation(() => apiClient.post<FormalPoolOperationResult>(`/admin/accounts/${accountId}/formal-pool/promote-production`))
}

export async function swapProxy(accountId: number, payload: SwapProxyRequest): Promise<FormalPoolOperationResult> {
  return runOperation(() => apiClient.post<FormalPoolOperationResult>(`/admin/accounts/${accountId}/formal-pool/proxy/swap`, payload))
}

export async function quarantine(accountId: number, reason: string): Promise<FormalPoolOperationResult> {
  return runOperation(async () => {
    const { data: account } = await apiClient.post<Account>(`/admin/accounts/${accountId}/quarantine`, { reason })
    const diagnostics = await getDiagnostics(account.id).catch(() => undefined)
    return { data: { account, diagnostics } }
  })
}

const formalPoolOperationsAPI = {
  getDiagnostics,
  replaceSetupToken,
  runtimeRegister,
  healthcheck,
  startWarming,
  promoteProduction,
  swapProxy,
  quarantine,
}

export default formalPoolOperationsAPI
