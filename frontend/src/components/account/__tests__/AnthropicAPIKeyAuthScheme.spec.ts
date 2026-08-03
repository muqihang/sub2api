import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const { createAccountMock, updateAccountMock, checkMixedChannelRiskMock } = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  updateAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isSimpleMode: true
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      update: updateAccountMock,
      checkMixedChannelRisk: checkMixedChannelRiskMock
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'
import EditAccountModal from '../EditAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: {
      type: Boolean,
      default: false
    }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const ProxySelectorStub = defineComponent({
  name: 'ProxySelector',
  props: {
    modelValue: {
      type: [Number, null],
      default: null
    }
  },
  emits: ['update:modelValue'],
  template: '<div data-testid="proxy-selector"></div>'
})

const GroupSelectorStub = defineComponent({
  name: 'GroupSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: '<div data-testid="group-selector"></div>'
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: '<div data-testid="model-whitelist"></div>'
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: {
      type: [String, Number, Boolean, null],
      default: ''
    },
    options: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: `
    <select v-bind="$attrs" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
      <option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option>
    </select>
  `
})

function mountCreateModal() {
  return mount(CreateAccountModal, {
    props: {
      show: true,
      proxies: [],
      groups: []
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        Select: SelectStub,
        Icon: true,
        ProxySelector: ProxySelectorStub,
        ProxyAdBanner: true,
        GroupSelector: GroupSelectorStub,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: true,
        OAuthAuthorizationFlow: true
      }
    }
  })
}

function anthropicAPIKeyAccount(extra: Record<string, unknown> = {}) {
  return {
    id: 42,
    name: 'Anthropic API key',
    notes: '',
    platform: 'anthropic',
    type: 'apikey',
    credentials: {
      api_key: 'sk-ant-test',
      base_url: 'https://api.anthropic.com'
    },
    credentials_status: { has_api_key: true },
    extra,
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  } as any
}

function mountEditModal(account = anthropicAPIKeyAccount()) {
  return mount(EditAccountModal, {
    props: {
      show: true,
      account,
      proxies: [],
      groups: []
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        ProxySelector: ProxySelectorStub,
        GroupSelector: GroupSelectorStub,
        ModelWhitelistSelector: ModelWhitelistSelectorStub
      }
    }
  })
}

describe('Anthropic API-key auth scheme UI', () => {
  beforeEach(() => {
    createAccountMock.mockReset()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 1 })
    updateAccountMock.mockResolvedValue({ id: 42 })
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
  })

  it('submits bearer auth scheme when creating an Anthropic API-key account', async () => {
    const wrapper = mountCreateModal()
    await flushPromises()

    await wrapper.get('[data-testid="account-type-apikey"]').trigger('click')
    await wrapper.get('[data-testid="account-name-input"]').setValue('Ollama Cloud')
    await wrapper.get('input[placeholder="https://api.anthropic.com"]').setValue('https://ollama.com')
    await wrapper.get('input[placeholder="sk-ant-..."]').setValue('ollama-key')
    await wrapper.get('[data-testid="anthropic-apikey-auth-scheme-select"]').setValue('authorization_bearer')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload.platform).toBe('anthropic')
    expect(payload.type).toBe('apikey')
    expect(payload.extra?.anthropic_apikey_auth_scheme).toBe('authorization_bearer')
  })

  it('saves and clears bearer auth scheme when editing an Anthropic API-key account', async () => {
    const wrapper = mountEditModal(anthropicAPIKeyAccount({ anthropic_apikey_auth_scheme: 'authorization_bearer' }))
    await flushPromises()

    const select = wrapper.get<HTMLSelectElement>('[data-testid="anthropic-apikey-auth-scheme-select"]')
    expect(select.element.value).toBe('authorization_bearer')

    await select.setValue('x_api_key')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('anthropic_apikey_auth_scheme')

    updateAccountMock.mockClear()
    await select.setValue('authorization_bearer')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.anthropic_apikey_auth_scheme).toBe('authorization_bearer')
  })
})
