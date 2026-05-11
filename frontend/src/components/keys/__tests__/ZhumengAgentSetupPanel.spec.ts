import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const {
  createCodexSetupGrant,
  listCodexManagedDevices,
  revokeCodexManagedDevice,
} = vi.hoisted(() => ({
  createCodexSetupGrant: vi.fn(),
  listCodexManagedDevices: vi.fn(),
  revokeCodexManagedDevice: vi.fn(),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/api/zhumengAgent', () => ({
  createCodexSetupGrant,
  listCodexManagedDevices,
  revokeCodexManagedDevice,
}))

import ZhumengAgentSetupPanel from '../ZhumengAgentSetupPanel.vue'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

describe('ZhumengAgentSetupPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    createCodexSetupGrant.mockReset()
    listCodexManagedDevices.mockReset()
    revokeCodexManagedDevice.mockReset()
    vi.spyOn(window, 'open').mockImplementation(() => null)
  })

  it('shows one-click setup and disables when apiKeyId is missing', async () => {
    listCodexManagedDevices.mockResolvedValue([])
    const wrapper = mount(ZhumengAgentSetupPanel, {
      props: { apiKeyId: null },
    })

    const button = wrapper.find('button.btn.btn-primary')
    expect(button.text()).toContain('keys.zhumengAgent.setup')
    expect(button.attributes('disabled')).toBeDefined()
  })

  it('creates setup grant with numeric apiKeyId and opens deeplink', async () => {
    listCodexManagedDevices.mockResolvedValue([])
    createCodexSetupGrant.mockResolvedValue({
      code: 'grant-1',
      expires_at: '2026-05-11T12:00:00Z',
      deeplink: 'zhumeng-agent://setup?client=codex&code=grant-1',
    })

    const wrapper = mount(ZhumengAgentSetupPanel, {
      props: { apiKeyId: 42 },
    })

    await wrapper.find('button.btn.btn-primary').trigger('click')

    expect(createCodexSetupGrant).toHaveBeenCalledWith(42)
    expect(window.open).toHaveBeenCalledWith('zhumeng-agent://setup?client=codex&code=grant-1', '_self')
  })

  it('shows fallback help after deeplink attempt', async () => {
    listCodexManagedDevices.mockResolvedValue([])
    createCodexSetupGrant.mockResolvedValue({
      code: 'grant-1',
      expires_at: '2026-05-11T12:00:00Z',
      deeplink: 'zhumeng-agent://setup?client=codex&code=grant-1',
    })

    const wrapper = mount(ZhumengAgentSetupPanel, {
      props: { apiKeyId: 42 },
    })

    await wrapper.find('button.btn.btn-primary').trigger('click')
    await vi.advanceTimersByTimeAsync(1500)

    expect(wrapper.text()).toContain('keys.zhumengAgent.fallbackHelp')
  })

  it('loads devices for current api key and revokes selected device', async () => {
    listCodexManagedDevices
      .mockResolvedValueOnce([
        {
          id: 9,
          user_id: 7,
          api_key_id: 42,
          name: 'MacBook Pro',
          platform: 'darwin',
          arch: 'arm64',
          manager_version: '1.0.0',
          status: 'active',
          last_seen_at: null,
          revoked_at: null,
          created_at: '2026-05-11T00:00:00Z',
          updated_at: '2026-05-11T00:00:00Z',
        },
      ])
      .mockResolvedValueOnce([])
    revokeCodexManagedDevice.mockResolvedValue({ device_id: 9, revoked: true })

    const wrapper = mount(ZhumengAgentSetupPanel, {
      props: { apiKeyId: 42 },
    })

    await wrapper.vm.$nextTick()
    expect(listCodexManagedDevices).toHaveBeenCalledWith(42)
    expect(wrapper.text()).toContain('MacBook Pro')

    const buttons = wrapper.findAll('button')
    const revokeButton = buttons.find((button) => button.text().includes('keys.zhumengAgent.revoke'))
    expect(revokeButton).toBeDefined()
    await revokeButton!.trigger('click')

    const confirmButton = wrapper.findAll('button').find((button) => button.text().includes('keys.zhumengAgent.confirmRevoke'))
    expect(confirmButton).toBeDefined()
    await confirmButton!.trigger('click')

    expect(revokeCodexManagedDevice).toHaveBeenCalledWith(9)
  })

  it('ignores stale device responses after api key switch', async () => {
    const first = deferred<any[]>()
    const second = deferred<any[]>()
    listCodexManagedDevices
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)

    const wrapper = mount(ZhumengAgentSetupPanel, {
      props: { apiKeyId: 42 },
    })

    await wrapper.setProps({ apiKeyId: 43 })

    second.resolve([
      {
        id: 2,
        user_id: 7,
        api_key_id: 43,
        name: 'Key B Device',
        platform: 'darwin',
        arch: 'arm64',
        manager_version: '1.0.0',
        status: 'active',
        last_seen_at: null,
        revoked_at: null,
        created_at: '2026-05-11T00:00:00Z',
        updated_at: '2026-05-11T00:00:00Z',
      },
    ])
    await Promise.resolve()
    await wrapper.vm.$nextTick()

    first.resolve([
      {
        id: 1,
        user_id: 7,
        api_key_id: 42,
        name: 'Key A Device',
        platform: 'darwin',
        arch: 'arm64',
        manager_version: '1.0.0',
        status: 'active',
        last_seen_at: null,
        revoked_at: null,
        created_at: '2026-05-11T00:00:00Z',
        updated_at: '2026-05-11T00:00:00Z',
      },
    ])
    await Promise.resolve()
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Key B Device')
    expect(wrapper.text()).not.toContain('Key A Device')
  })
})
