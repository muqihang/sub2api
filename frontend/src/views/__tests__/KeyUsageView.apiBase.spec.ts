import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

const { showInfo, showSuccess, showError, fetchPublicSettings } = vi.hoisted(() => ({
  showInfo: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  fetchPublicSettings: vi.fn(),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'en' },
    }),
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    siteName: 'Sub2API',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings,
    showInfo,
    showSuccess,
    showError,
  }),
}))

describe('KeyUsageView API base', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.example.com/api/v1')
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false }),
    })
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => window.setTimeout(() => cb(0), 0))
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        mode: 'quota_limited',
        isValid: true,
        status: 'active',
        quota: { limit: 10, used: 1, remaining: 9, unit: 'USD' },
        usage: {
          today: { requests: 0, total_tokens: 0, actual_cost: 0 },
          total: { requests: 0, total_tokens: 0, actual_cost: 0 },
          rpm: 0,
          tpm: 0,
        },
        daily_usage: [],
      }),
    }))
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    vi.unstubAllGlobals()
  })

  it('queries gateway usage through the configured API origin', async () => {
    const { default: KeyUsageView } = await import('../KeyUsageView.vue')
    const wrapper = mount(KeyUsageView, {
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, LocaleSwitcher: true, Icon: true } },
    })

    await wrapper.find('input').setValue('sk-test-key')
    await wrapper.find('input').trigger('keydown.enter')
    await flushPromises()
    await nextTick()

    expect(fetch).toHaveBeenCalledWith(
      expect.stringMatching(/^https:\/\/api\.example\.com\/v1\/usage\?/),
      expect.objectContaining({ headers: { Authorization: 'Bearer sk-test-key' } })
    )

    wrapper.unmount()
    await new Promise((resolve) => window.setTimeout(resolve, 80))
  })
})
