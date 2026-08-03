import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post },
}))

import {
  FormalPoolOperationError,
  healthcheck,
  replaceSetupToken,
  runtimeRegister,
  startWarming,
  promoteProduction,
  swapProxy,
  quarantine,
} from '@/api/admin/formalPoolOperations'

describe('formalPoolOperations API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })


  it('uses the formal-pool operations URLs and payloads for lifecycle actions', async () => {
    post.mockResolvedValue({ data: { account: { id: 5 } } })

    await runtimeRegister(5)
    expect(post).toHaveBeenLastCalledWith('/admin/accounts/5/formal-pool/runtime-register')

    await healthcheck(5)
    expect(post).toHaveBeenLastCalledWith('/admin/accounts/5/formal-pool/healthcheck')

    await startWarming(5)
    expect(post).toHaveBeenLastCalledWith('/admin/accounts/5/formal-pool/start-warming')

    await promoteProduction(5)
    expect(post).toHaveBeenLastCalledWith('/admin/accounts/5/formal-pool/promote-production')

    await swapProxy(5, {
      proxy_id: 9,
      run_proxy_test: true,
      run_runtime_register: true,
      run_healthcheck: true,
    })
    expect(post).toHaveBeenLastCalledWith('/admin/accounts/5/formal-pool/proxy/swap', {
      proxy_id: 9,
      run_proxy_test: true,
      run_runtime_register: true,
      run_healthcheck: true,
    })
  })

  it('preserves diagnostics recommendations from failed setup-token replacement', async () => {
    post.mockRejectedValue({
      status: 400,
      code: 'SETUP_TOKEN_REPLACE_FAILED',
      message: 'setup-token credential exchange failed',
      account: { id: 5, status: 'error', schedulable: false, onboarding_stage: 'quarantined' },
      diagnostics: {
        account_id: 5,
        is_formal_pool: true,
        schedulable: false,
        effective_schedulable: false,
        failure_origin: 'token_exchange',
        checks: [],
        recommended_actions: [
          { key: 'replace_account_and_proxy', label: 'Replace account and proxy', severity: 'danger' },
        ],
      },
    })

    await expect(replaceSetupToken(5, { session_key: 'SETUP_TOKEN_PLACEHOLDER' })).rejects.toMatchObject({
      name: 'FormalPoolOperationError',
      diagnostics: {
        recommended_actions: [expect.objectContaining({ key: 'replace_account_and_proxy' })],
      },
    })

    await replaceSetupToken(5, { session_key: 'SETUP_TOKEN_PLACEHOLDER' }).catch((error) => {
      expect(error).toBeInstanceOf(FormalPoolOperationError)
      expect(error.diagnostics?.recommended_actions?.[0]?.key).toBe('replace_account_and_proxy')
    })
  })

  it('normalizes legacy quarantine Account responses into a FormalPoolOperationResult', async () => {
    const account = { id: 5, status: 'error' as const, schedulable: false, onboarding_stage: 'quarantined' }
    post.mockResolvedValueOnce({ data: account })
    get.mockResolvedValueOnce({ data: {
      account_id: 5,
      is_formal_pool: true,
      schedulable: false,
      effective_schedulable: false,
      failure_origin: 'upstream',
      checks: [],
      recommended_actions: [{ key: 'monitor', label: 'Monitor' }],
    } })

    await expect(quarantine(5, 'manual-risk: kyc')).resolves.toEqual({
      account,
      diagnostics: expect.objectContaining({ account_id: 5 }),
    })
    expect(post).toHaveBeenLastCalledWith('/admin/accounts/5/quarantine', { reason: 'manual-risk: kyc' })
    expect(get).toHaveBeenLastCalledWith('/admin/accounts/5/formal-pool/diagnostics')
  })

  it('allows FormalPoolOperationError to carry the backend safe account payload', () => {
    const account = { id: 5, status: 'error' as const, schedulable: false, onboarding_stage: 'quarantined' }
    const error = new FormalPoolOperationError('setup-token credential exchange failed', { account })

    expect(error.account).toEqual(account)
  })

})
