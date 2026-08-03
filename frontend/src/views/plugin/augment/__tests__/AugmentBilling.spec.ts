import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BillingView from '@/views/plugin/augment/BillingView.vue'

const mockGetSummary = vi.fn()
const mockListUsage = vi.fn()
const mockListRecentErrors = vi.fn()
const mockGetOfficialSession = vi.fn()
const mockShowError = vi.fn()

vi.mock('@/api/augmentBilling', () => ({
  getAugmentBillingSummary: (...args: any[]) => mockGetSummary(...args),
  listAugmentBillingUsage: (...args: any[]) => mockListUsage(...args),
  listAugmentRecentErrors: (...args: any[]) => mockListRecentErrors(...args),
}))

vi.mock('@/api/augment', () => ({
  getAugmentOfficialSession: (...args: any[]) => mockGetOfficialSession(...args),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: (...args: any[]) => mockShowError(...args),
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

describe('AugmentBilling', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetOfficialSession.mockResolvedValue({
      mode: 'official_passthrough',
      source: 'official_quick_login',
      tenant_origin: 'https://tenant.example.com',
      expires_at: '2026-05-08T18:00:00Z',
      status: 'active',
      last_error_code: null,
    })
    mockGetSummary.mockResolvedValue({
      estimated_cost: 12.34,
      settled_cost: 10.01,
      free_quota: 1.23,
      paid_balance: 8.78,
      currency: 'USD',
      cache_hit_ratio: 0.42,
      total_cache_read_tokens: 1234,
      total_cache_creation_tokens: 4321,
    })
    mockListUsage.mockResolvedValue({
      rows: [
        {
          model: 'gpt-5.4',
          upstream_model: 'gpt-5.4-mini',
          endpoint: '/chat-stream',
          status: 'success',
          request_scope: 'augment_gateway',
          feature_scope: 'context_engine',
          group_id: 201,
          augment_session_id: 'augment-session-1',
          route_policy_version: '2026-05-10',
          tokens: 222,
          cache_read_tokens: 120,
          cache_creation_tokens: 64,
          estimated_cost: 1.5,
          settled_cost: 1.2,
          pricing_version: 'v1',
          request_id: 'req-1',
          prompt: 'should-not-render',
          retrieval_body: 'should-not-render',
          token: 'should-not-render',
          cookie: 'should-not-render',
        },
      ],
      page: {
        page: 1,
        page_size: 20,
        pages: 1,
        total: 1,
      },
    })
    mockListRecentErrors.mockResolvedValue({
      rows: [
        {
          model: 'deepseek-v4-pro',
          endpoint: '/chat-stream',
          status: 'error',
          error_class: 'billing_unsettled',
          request_id: 'req-err-1',
        },
      ],
    })
  })

  it('fetches augment scoped billing summary', async () => {
    const wrapper = mount(BillingView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          OfficialSessionStatusCard: { template: '<div class="session-card-stub" />' },
        },
      },
    })

    await flushPromises()

    expect(mockGetSummary).toHaveBeenCalled()
    expect(mockListUsage).toHaveBeenCalledWith({ page: 1, page_size: 20 })
    expect(mockListRecentErrors).toHaveBeenCalledWith({ limit: 10 })
    expect(wrapper.text()).toContain('12.34')
  })

  it('renders cache read and cache creation tokens', async () => {
    const wrapper = mount(BillingView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          OfficialSessionStatusCard: { template: '<div class="session-card-stub" />' },
        },
      },
    })

    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('1234')
    expect(text).toContain('4321')
    expect(text).toContain('120')
    expect(text).toContain('64')
  })

  it('renders estimated and settled cost separately', async () => {
    const wrapper = mount(BillingView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          OfficialSessionStatusCard: { template: '<div class="session-card-stub" />' },
        },
      },
    })

    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('12.34')
    expect(text).toContain('10.01')
    expect(text).toContain('1.5')
    expect(text).toContain('1.2')
  })

  it('renders shared-wallet free quota and paid balance context', async () => {
    const wrapper = mount(BillingView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          OfficialSessionStatusCard: { template: '<div class="session-card-stub" />' },
        },
      },
    })

    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('1.23')
    expect(text).toContain('8.78')
    expect(text).toContain('plugin.augment.billing.sharedWalletTitle')
    expect(text).toContain('plugin.augment.billing.sharedWalletDescription')
    expect(text).toContain('plugin.augment.billing.singleActiveKey')
  })

  it('renders request-level Augment attribution fields', async () => {
    const wrapper = mount(BillingView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          OfficialSessionStatusCard: { template: '<div class="session-card-stub" />' },
        },
      },
    })

    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('gpt-5.4-mini')
    expect(text).toContain('201')
    expect(text).toContain('augment-session-1')
    expect(text).toContain('2026-05-10')
    expect(text).toContain('augment_gateway')
    expect(text).toContain('context_engine')
  })

  it('does not render prompt, retrieval body, source body, token or cookie fields', async () => {
    const wrapper = mount(BillingView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          OfficialSessionStatusCard: { template: '<div class="session-card-stub" />' },
        },
      },
    })

    await flushPromises()

    const text = wrapper.text()
    expect(text).not.toContain('should-not-render')
    expect(text).not.toContain('prompt')
    expect(text).not.toContain('retrieval_body')
    expect(text).not.toContain('token')
    expect(text).not.toContain('cookie')
  })
})
