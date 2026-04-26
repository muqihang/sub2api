import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import QuickLoginView from '@/views/plugin/augment/QuickLoginView.vue'

const mockRequestGrant = vi.fn()
const mockCopyToClipboard = vi.fn()
const mockShowError = vi.fn()
let mockRouteQuery: Record<string, unknown> = {}

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
  requestAugmentQuickLoginGrant: (...args: any[]) => mockRequestGrant(...args),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: (...args: any[]) => mockCopyToClipboard(...args),
  }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: (...args: any[]) => mockShowError(...args),
  }),
}))

describe('QuickLoginView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockRouteQuery = {}
  })

  it('requests a grant even when the browser page was opened without extra query context', async () => {
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

    const buttons = wrapper.findAll('button')
    await buttons[0].trigger('click')
    await flushPromises()

    expect(mockRequestGrant).toHaveBeenCalledWith({ mode: 'local_compat' })
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

  it('does not render raw sensitive query payload values into the page', () => {
    mockRouteQuery = {
      access_token: 'raw-access-token',
      refresh_token: 'raw-refresh-token',
      session_bundle: '{"access_token":"bundle-access"}',
      official_access_token: 'raw-official-access-token',
      mode: 'local_compat',
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
    expect(pageText).not.toContain('raw-access-token')
    expect(pageText).not.toContain('raw-refresh-token')
    expect(pageText).not.toContain('bundle-access')
    expect(pageText).not.toContain('raw-official-access-token')
  })
})
