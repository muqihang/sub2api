import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const { createOpenAICodexPATMock, showSuccessMock, showErrorMock } = vi.hoisted(() => ({
  createOpenAICodexPATMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: showSuccessMock,
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
      createOpenAICodexPAT: createOpenAICodexPATMock
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

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: {
    showCodexPatOption: {
      type: Boolean,
      default: false
    }
  },
  emits: ['import-codex-pat'],
  template: `
    <div data-testid="oauth-flow" :data-show-codex-pat="String(showCodexPatOption)">
      <button type="button" data-testid="emit-codex-pat" @click="$emit('import-codex-pat', '  at-modal-test  ')">
        import pat
      </button>
    </div>
  `
})

function mountModal() {
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
        Select: true,
        Icon: true,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: true,
        ModelWhitelistSelector: true,
        QuotaLimitCard: true,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub
      }
    }
  })
}

describe('CreateAccountModal Codex PAT import', () => {
  beforeEach(() => {
    createOpenAICodexPATMock.mockReset()
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    createOpenAICodexPATMock.mockResolvedValue({ id: 123 })
  })

  it('passes OpenAI Codex PAT imports to the dedicated admin endpoint without rendering the token', async () => {
    const wrapper = mountModal()
    await flushPromises()

    const openAIButton = wrapper.findAll('button').find((button) => button.text().includes('OpenAI'))
    expect(openAIButton).toBeTruthy()
    await openAIButton!.trigger('click')
    await wrapper.get('[data-testid="account-name-input"]').setValue('codex pat account')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.get('[data-testid="oauth-flow"]').attributes('data-show-codex-pat')).toBe('true')
    expect(wrapper.html()).not.toContain('at-modal-test')

    await wrapper.get('[data-testid="emit-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock).toHaveBeenCalledTimes(1)
    const payload = createOpenAICodexPATMock.mock.calls[0]?.[0]
    expect(payload).toMatchObject({
      access_token: 'at-modal-test',
      name: 'codex pat account'
    })
    expect(payload).not.toHaveProperty('platform')
    expect(payload).not.toHaveProperty('refresh_token')
    expect(showSuccessMock).toHaveBeenCalledWith('admin.accounts.accountCreated')
  })
})
