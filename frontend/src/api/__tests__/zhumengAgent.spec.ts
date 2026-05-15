import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post, get } = vi.hoisted(() => ({
  post: vi.fn(),
  get: vi.fn(),
}))

vi.mock('../client', () => ({
  apiClient: {
    post,
    get,
  },
}))

import { createCodexSetupGrant, listCodexManagedDevices, revokeCodexManagedDevice } from '../zhumengAgent'

describe('zhumengAgent api', () => {
  beforeEach(() => {
    post.mockReset()
    get.mockReset()
  })

  it('createCodexSetupGrant posts expected payload', async () => {
    post.mockResolvedValue({
      data: {
        code: 'grant-1',
        expires_at: '2026-05-11T12:00:00Z',
        deeplink: 'zhumeng-agent://setup?client=codex&code=grant-1',
      },
    })

    const result = await createCodexSetupGrant(123)

    expect(post).toHaveBeenCalledWith('/codex/setup-grants', {
      api_key_id: 123,
      client: 'codex',
      mode: 'managed_proxy',
    })
    expect(result.deeplink).toContain('zhumeng-agent://setup')
  })

  it('listCodexManagedDevices scopes by api key id when provided', async () => {
    get.mockResolvedValue({ data: [] })

    await listCodexManagedDevices(42)

    expect(get).toHaveBeenCalledWith('/codex/devices', {
      params: { api_key_id: 42 },
    })
  })

  it('revokeCodexManagedDevice posts body contract', async () => {
    post.mockResolvedValue({ data: { device_id: 9, revoked: true } })

    await revokeCodexManagedDevice(9)

    expect(post).toHaveBeenCalledWith('/codex/devices/revoke', {
      device_id: 9,
    })
  })
})
