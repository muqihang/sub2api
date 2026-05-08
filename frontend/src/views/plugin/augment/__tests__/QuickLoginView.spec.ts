import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import QuickLoginView from '@/views/plugin/augment/QuickLoginView.vue'

const mockRequestGrant = vi.fn()
const mockCreateBindIntent = vi.fn()
const mockGetOfficialSession = vi.fn()
const mockRevokeOfficialSession = vi.fn()
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
  getAugmentOfficialSession: (...args: any[]) => mockGetOfficialSession(...args),
  revokeAugmentOfficialSession: (...args: any[]) => mockRevokeOfficialSession(...args),
  requestAugmentQuickLoginGrant: (...args: any[]) => mockRequestGrant(...args),
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
    mockGetOfficialSession.mockResolvedValue(null)
    mockRevokeOfficialSession.mockResolvedValue(null)
  })

  it('creates bind intent before opening deeplink', async () => {
    mockGetOfficialSession.mockResolvedValue({
      mode: 'official_passthrough',
      source: 'official_quick_login',
      tenant_origin: 'https://official.augment.local',
      expires_at: '2026-05-08T16:00:00Z',
      status: 'active',
      last_error_code: 'NONE',
    })
    mockCreateBindIntent.mockResolvedValue({
      bind_intent_id: 'bind-intent-1',
      state: 'bind-state-1',
      expires_at: '2026-05-08T15:30:00Z',
      bind_token: 'bind-token-secret',
    })
    mockRequestGrant.mockResolvedValue({
      vscode_deeplink: 'vscode://Augment.vscode-augment/autoAuth?grant=g1&state=s1'
    })

    const wrapper = mount(QuickLoginView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>'
          }
        }
      }
    })

    await flushPromises()
    await wrapper.get('input[type="checkbox"]').setValue(true)

    const buttons = wrapper.findAll('button')
    await buttons[0].trigger('click')
    await flushPromises()

    expect(mockCreateBindIntent).toHaveBeenCalledWith({
      mode: 'official_passthrough',
      source: 'official_quick_login',
      tenant_allowlist: ['https://official.augment.local'],
    })
    expect(mockRequestGrant).toHaveBeenCalledWith({ mode: 'official_passthrough' })
    expect(mockCreateBindIntent.mock.invocationCallOrder[0]).toBeLessThan(
      mockRequestGrant.mock.invocationCallOrder[0]
    )
    const deeplinkInput = wrapper.find('input[readonly]')
    expect(deeplinkInput.exists()).toBe(true)
    expect((deeplinkInput.element as HTMLInputElement).value).toBe(
      'vscode://Augment.vscode-augment/autoAuth?grant=g1&state=s1'
    )
    expect(mockCopyToClipboard).toHaveBeenCalledWith(
      'vscode://Augment.vscode-augment/autoAuth?grant=g1&state=s1',
      'plugin.augment.quickLogin.copySuccess'
    )
    expect(mockShowError).not.toHaveBeenCalled()
  })

  it('does not render local compat unless emergency/admin gate is enabled', async () => {
    const wrapper = mount(QuickLoginView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>'
          }
        }
      }
    })

    expect(wrapper.text()).not.toContain('plugin.augment.quickLogin.modes.localCompat.title')

    mockIsAdmin = true
    mockRouteQuery = {
      emergency_local_compat: '1',
    }

    const adminWrapper = mount(QuickLoginView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>'
          }
        }
      }
    })

    await flushPromises()
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
            template: '<div><slot /></div>'
          }
        }
      }
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

  it('shows consent copy before official and wukong bind', async () => {
    mockGetOfficialSession.mockResolvedValue({
      mode: 'official_passthrough',
      source: 'official_quick_login',
      tenant_origin: 'https://official.augment.local',
      expires_at: '2026-05-08T16:00:00Z',
      status: 'active',
      last_error_code: null,
    })

    const wrapper = mount(QuickLoginView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>'
          }
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('plugin.augment.quickLogin.consent.title')
    expect(wrapper.text()).toContain('plugin.augment.quickLogin.consent.official')

    const sourceButtons = wrapper
      .findAll('button')
      .filter((button) => button.text().includes('plugin.augment.quickLogin.sources.wukong'))
    await sourceButtons[0].trigger('click')

    expect(wrapper.text()).toContain('plugin.augment.quickLogin.consent.wukong')
  })
})
