import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import QuickLoginView from '@/views/plugin/augment/QuickLoginView.vue'

const mockRequestGrant = vi.fn()
const mockCreateBindIntent = vi.fn()
const mockBindOfficialSession = vi.fn()
const mockCreatePoolBindIntent = vi.fn()
const mockBindPoolSession = vi.fn()
const mockCopyToClipboard = vi.fn()
const mockShowError = vi.fn()
let mockRouteQuery: Record<string, unknown> = {}
let mockIsAdmin = false

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: mockRouteQuery }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/api/augment', () => ({
  createAugmentOfficialSessionBindIntent: (...args: any[]) => mockCreateBindIntent(...args),
  bindAugmentOfficialSession: (...args: any[]) => mockBindOfficialSession(...args),
  requestAugmentQuickLoginGrant: (...args: any[]) => mockRequestGrant(...args),
}))

vi.mock('@/api/admin/augmentGateway', () => ({
  createAugmentPoolSessionBindIntent: (...args: any[]) => mockCreatePoolBindIntent(...args),
  bindAugmentPoolSession: (...args: any[]) => mockBindPoolSession(...args),
  default: {
    createAugmentPoolSessionBindIntent: (...args: any[]) => mockCreatePoolBindIntent(...args),
    bindAugmentPoolSession: (...args: any[]) => mockBindPoolSession(...args),
  },
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: (...args: any[]) => mockCopyToClipboard(...args),
  }),
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    isAdmin: mockIsAdmin,
  }),
  useAppStore: () => ({
    showError: (...args: any[]) => mockShowError(...args),
  }),
}))

describe('QuickLoginView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockRouteQuery = {}
    mockIsAdmin = false
  })

  it('requests a platform-pool official passthrough grant for normal users without binding a user session', async () => {
    mockRequestGrant.mockResolvedValue({
      vscode_deeplink: 'vscode://Augment.vscode-augment/autoAuth?grant=g1&state=s1',
    })

    const wrapper = mount(QuickLoginView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-test="quick-login-continue"]').trigger('click')
    await flushPromises()

    expect(mockRequestGrant).toHaveBeenCalledWith({
      mode: 'official_passthrough',
      source: 'official_quick_login',
    })
    expect(mockCreateBindIntent).not.toHaveBeenCalled()
    expect(mockBindOfficialSession).not.toHaveBeenCalled()
    expect(mockCreatePoolBindIntent).not.toHaveBeenCalled()
    expect(mockBindPoolSession).not.toHaveBeenCalled()

    const deeplinkInput = wrapper.get('input[readonly]')
    expect((deeplinkInput.element as HTMLInputElement).value).toBe(
      'vscode://Augment.vscode-augment/autoAuth?grant=g1&state=s1',
    )
    expect(mockCopyToClipboard).toHaveBeenCalledWith(
      'vscode://Augment.vscode-augment/autoAuth?grant=g1&state=s1',
      'plugin.augment.quickLogin.copySuccess',
    )
    expect(mockShowError).not.toHaveBeenCalled()
  })

  it('does not render internal capture controls unless the emergency admin gate is enabled', async () => {
    const wrapper = mount(QuickLoginView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>',
          },
        },
      },
    })

    expect(wrapper.text()).not.toContain('plugin.augment.quickLogin.internalCapture.title')

    mockIsAdmin = true
    mockRouteQuery = {
      emergency_local_compat: '1',
    }

    const adminWrapper = mount(QuickLoginView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>',
          },
        },
      },
    })

    await flushPromises()
    expect(adminWrapper.text()).toContain('plugin.augment.quickLogin.internalCapture.title')
    expect(adminWrapper.text()).toContain('plugin.augment.quickLogin.modes.localCompat.title')
  })

  it('never renders secret field names in diagnostics', () => {
    mockRouteQuery = {
      access_token: 'raw-access-token',
      refresh_token: 'raw-refresh-token',
      session_bundle: '{"access_token":"bundle-access"}',
      official_access_token: 'raw-official-access-token',
      official_refresh_token: 'raw-official-refresh-token',
      bind_token: 'bind-token-secret',
      mode: 'official_passthrough',
      tenant_url: 'https://tenant.local',
    }

    const wrapper = mount(QuickLoginView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>',
          },
        },
      },
    })

    const pageText = wrapper.text()
    expect(pageText).toContain('mode')
    expect(pageText).toContain('tenant_url')
    expect(pageText).not.toContain('access_token')
    expect(pageText).not.toContain('refresh_token')
    expect(pageText).not.toContain('official_access_token')
    expect(pageText).not.toContain('bind_token')
    expect(pageText).not.toContain('raw-access-token')
    expect(pageText).not.toContain('raw-refresh-token')
    expect(pageText).not.toContain('bundle-access')
    expect(pageText).not.toContain('raw-official-access-token')
  })

  it('shows source-specific consent copy before official and wukong quick login', async () => {
    const wrapper = mount(QuickLoginView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>',
          },
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('plugin.augment.quickLogin.consent.title')
    expect(wrapper.text()).toContain('plugin.augment.quickLogin.consent.official')

    await wrapper.get('[data-test="source-wukong_quick_login"]').trigger('click')

    expect(wrapper.text()).toContain('plugin.augment.quickLogin.consent.wukong')
  })

  it('binds callback payload into a pool session before requesting grant in admin capture mode', async () => {
    mockIsAdmin = true
    mockRouteQuery = {
      emergency_local_compat: '1',
      capture_target: 'pool_session',
      official_tenant_url: 'https://official.augment.local',
      official_access_token: 'official-access-from-query',
      official_refresh_token: 'official-refresh-from-query',
      official_expires_at: '2026-05-08T16:00:00Z',
      official_scopes: 'augment:session,augment:summary',
    }
    mockCreatePoolBindIntent.mockResolvedValue({
      bind_intent_id: 'pool-bind-intent-1',
      state: 'pool-bind-state-1',
      expires_at: '2026-05-08T15:30:00Z',
      bind_token: 'pool-bind-token-secret',
    })
    mockBindPoolSession.mockResolvedValue({
      id: 42,
      source: 'official_quick_login',
      tenant_origin: 'https://official.augment.local',
      status: 'active',
    })
    mockRequestGrant.mockResolvedValue({
      vscode_deeplink: 'vscode://Augment.vscode-augment/autoAuth?grant=g2&state=s2',
    })

    const wrapper = mount(QuickLoginView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-test="quick-login-continue"]').trigger('click')
    await flushPromises()

    expect(mockCreatePoolBindIntent).toHaveBeenCalledWith({
      mode: 'official_passthrough',
      source: 'official_quick_login',
      tenant_allowlist: ['https://official.augment.local'],
    })
    expect(mockBindPoolSession).toHaveBeenCalledWith({
      bind_token: 'pool-bind-token-secret',
      bind_intent_id: 'pool-bind-intent-1',
      state: 'pool-bind-state-1',
      mode: 'official_passthrough',
      source: 'official_quick_login',
      payload: {
        tenant_url: 'https://official.augment.local',
        access_token: 'official-access-from-query',
        refresh_token: 'official-refresh-from-query',
        expires_at: '2026-05-08T16:00:00Z',
        scopes: ['augment:session', 'augment:summary'],
      },
    })
    expect(mockCreateBindIntent).not.toHaveBeenCalled()
    expect(mockBindOfficialSession).not.toHaveBeenCalled()
    expect(mockCreatePoolBindIntent.mock.invocationCallOrder[0]).toBeLessThan(
      mockRequestGrant.mock.invocationCallOrder[0],
    )
    expect(mockBindPoolSession.mock.invocationCallOrder[0]).toBeLessThan(
      mockRequestGrant.mock.invocationCallOrder[0],
    )
    expect(mockRequestGrant).toHaveBeenCalledWith({
      mode: 'official_passthrough',
      source: 'official_quick_login',
    })
  })
})
