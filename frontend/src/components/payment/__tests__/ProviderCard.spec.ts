import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ProviderCard from '../ProviderCard.vue'
import type { ProviderInstance } from '@/types/payment'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

function providerFactory(overrides: Partial<ProviderInstance> = {}): ProviderInstance {
  return {
    id: 1,
    provider_key: 'alipay',
    name: 'Alipay',
    config: {},
    supported_types: ['alipay'],
    enabled: true,
    payment_mode: '',
    refund_enabled: false,
    allow_user_refund: false,
    limits: '',
    sort_order: 0,
    ...overrides,
  }
}

describe('ProviderCard supported_types handling', () => {
  it('keeps provider cards visible when supported_types is null', () => {
    const wrapper = mount(ProviderCard, {
      props: {
        provider: providerFactory({ supported_types: null as unknown as string[] }),
        enabled: true,
        availableTypes: [{ value: 'alipay', label: 'Alipay' }],
      },
      global: {
        stubs: {
          Icon: true,
          ToggleSwitch: true,
        },
      },
    })

    expect(wrapper.text()).toContain('Alipay')
    expect(wrapper.text()).toContain('Alipay')
  })
})
