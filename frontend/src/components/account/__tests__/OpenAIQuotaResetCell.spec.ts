import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpenAIQuotaResetCell from '../OpenAIQuotaResetCell.vue'
import type { Account } from '@/types'
import { queryOpenAIQuota, resetOpenAIQuota } from '@/api/admin/accounts'

vi.mock('@/api/admin/accounts', () => ({
  queryOpenAIQuota: vi.fn(),
  resetOpenAIQuota: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (!params) return key
        return `${key}:${Object.entries(params).map(([k, v]) => `${k}=${v}`).join(',')}`
      }
    })
  }
})

function makeAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 1,
    name: 'openai-oauth',
    platform: 'openai',
    type: 'oauth',
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  }
}

describe('OpenAIQuotaResetCell', () => {
  beforeEach(() => {
    vi.mocked(queryOpenAIQuota).mockReset()
    vi.mocked(resetOpenAIQuota).mockReset()
  })

  it('queries available reset credits and shows sanitized expiration details', async () => {
    vi.mocked(queryOpenAIQuota).mockResolvedValue({
      rate_limit_reset_credits: {
        available_count: 2,
        credits: [
          { expires_at: '2026-07-03T04:05:06Z' },
          { expires_at: '2026-07-04T04:05:06Z' }
        ]
      },
      fetched_at: 123
    })

    const wrapper = mount(OpenAIQuotaResetCell, {
      props: { account: makeAccount() },
      global: { stubs: { ConfirmDialog: true } }
    })

    await wrapper.find('[data-testid="openai-quota-query"]').trigger('click')
    await flushPromises()

    expect(queryOpenAIQuota).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('admin.accounts.openaiQuotaReset.count2')
    expect(wrapper.text()).toContain('admin.accounts.openaiQuotaReset.expiresAt:time=')
    expect(wrapper.find('[data-testid="reset-credit-expiry-toggle"]').exists()).toBe(true)
  })

  it('requires confirmation before consuming one reset credit', async () => {
    vi.mocked(queryOpenAIQuota).mockResolvedValue({
      rate_limit_reset_credits: { available_count: 1 },
      fetched_at: 123
    })
    vi.mocked(resetOpenAIQuota).mockResolvedValue({ code: 'ok', windows_reset: 1 })

    const wrapper = mount(OpenAIQuotaResetCell, {
      props: { account: makeAccount() },
      global: {
        stubs: {
          ConfirmDialog: {
            props: ['show', 'title', 'message'],
            emits: ['confirm', 'cancel'],
            template: '<div v-if="show" data-testid="confirm-dialog"><button data-testid="confirm" @click="$emit(\'confirm\')">confirm</button></div>'
          }
        }
      }
    })

    await wrapper.find('[data-testid="openai-quota-query"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="openai-quota-reset"]').trigger('click')
    await flushPromises()

    expect(resetOpenAIQuota).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="confirm-dialog"]').exists()).toBe(true)

    await wrapper.find('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    expect(resetOpenAIQuota).toHaveBeenCalledWith(1)
    expect(queryOpenAIQuota).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('admin.accounts.openaiQuotaReset.resetSuccess:windows=1')
  })
})
