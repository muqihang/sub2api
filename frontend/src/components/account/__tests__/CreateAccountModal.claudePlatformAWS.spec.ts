import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const { createClaudePlatformAWSBatchMock, createAccountMock, checkMixedChannelRiskMock, showErrorMock } = vi.hoisted(() => ({
  createClaudePlatformAWSBatchMock: vi.fn(),
  createAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
  showErrorMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
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
      createClaudePlatformAWSBatch: createClaudePlatformAWSBatchMock,
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
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

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
    },
    proxies: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: `
    <div>
      <button type="button" data-testid="proxy-none" @click="$emit('update:modelValue', null)">No Proxy</button>
      <button
        v-for="proxy in proxies"
        :key="proxy.id"
        type="button"
        :data-testid="'proxy-option-' + proxy.id"
        @click="$emit('update:modelValue', proxy.id)"
      >
        {{ proxy.name }}
      </button>
      <span data-testid="proxy-value">{{ modelValue ?? '' }}</span>
    </div>
  `
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

const QuotaLimitCardStub = defineComponent({
  name: 'QuotaLimitCard',
  template: '<div data-testid="quota-limit-card"></div>'
})

function mountModal() {
  return mount(CreateAccountModal, {
    props: {
      show: true,
      proxies: [
        { id: 10, name: 'proxy-a', protocol: 'http', host: 'proxy-a.local', port: 8080 },
        { id: 11, name: 'proxy-b', protocol: 'http', host: 'proxy-b.local', port: 8080 }
      ],
      groups: []
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        Select: true,
        Icon: true,
        ProxySelector: ProxySelectorStub,
        ProxyAdBanner: true,
        GroupSelector: true,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: QuotaLimitCardStub,
        OAuthAuthorizationFlow: true
      }
    }
  })
}

const workspaceFixture = (suffix: string) => ['wrkspc', suffix].join('_')
const awsCredentialFixture = () => ['synthetic', 'aws', 'credential'].join('-')

async function selectClaudePlatformAWS(wrapper: ReturnType<typeof mountModal>) {
  await wrapper.get('[data-testid="account-type-claude-platform-aws"]').trigger('click')
  await nextTick()
}

async function setInput(wrapper: ReturnType<typeof mountModal>, testId: string, value: string) {
  await wrapper.get(`[data-testid="${testId}"]`).setValue(value)
}

describe('CreateAccountModal Claude Platform on AWS', () => {
  beforeEach(() => {
    createClaudePlatformAWSBatchMock.mockReset()
    createAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    showErrorMock.mockReset()
    createClaudePlatformAWSBatchMock.mockResolvedValue({ rows: [] })
    createAccountMock.mockResolvedValue({ id: 1 })
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
  })

  it('renders Claude Platform on AWS as an independent Anthropic card without changing existing cards', async () => {
    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.get('[data-testid="account-type-oauth-based"]').text()).toContain('admin.accounts.claudeCode')
    expect(wrapper.get('[data-testid="account-type-oauth-based"]').text()).toContain('admin.accounts.oauthSetupToken')
    expect(wrapper.get('[data-testid="account-type-bedrock"]').text()).toContain('admin.accounts.bedrockLabel')
    expect(wrapper.get('[data-testid="account-type-bedrock"]').text()).toContain('admin.accounts.bedrockDesc')
    expect(wrapper.get('[data-testid="account-type-claude-platform-aws"]').text()).toContain('admin.accounts.claudePlatformAWS.label')
    expect(wrapper.get('[data-testid="account-type-claude-platform-aws"]').text()).toContain('admin.accounts.claudePlatformAWS.desc')
    expect(wrapper.get('[data-testid="claude-platform-aws-card-gate-badge"]').text()).toContain(
      'admin.accounts.claudePlatformAWS.authProfileGate.cardBadge'
    )

    await selectClaudePlatformAWS(wrapper)
    expect(wrapper.find('[data-testid="claude-platform-aws-fields"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="bedrock-fields"]').exists()).toBe(false)
  })

  it('shows a read-only auth-profile gate warning with mutually exclusive profiles and fail-closed CP0 state', async () => {
    const wrapper = mountModal()
    await flushPromises()
    await selectClaudePlatformAWS(wrapper)

    const gate = wrapper.get('[data-testid="claude-platform-aws-auth-profile-gate"]')
    expect(gate.text()).toContain('admin.accounts.claudePlatformAWS.authProfileGate.title')
    expect(gate.text()).toContain('x_api_key')
    expect(gate.text()).toContain('bearer_api_key')
    expect(gate.text()).toContain('BLOCKED_AUTH_PROFILE')
    expect(gate.text()).toContain('anthropic_aws_production_admitted=false')
    expect(gate.text()).toContain('admin.accounts.claudePlatformAWS.authProfileGate.mutualExclusion')
    expect(gate.text()).toContain('admin.accounts.claudePlatformAWS.authProfileGate.cp0Blocked')
    expect(gate.text()).toContain('admin.accounts.claudePlatformAWS.authProfileGate.noSilentFallback')
  })

  it('hides shared account fields that the Claude Platform on AWS batch payload does not submit', async () => {
    const wrapper = mountModal()
    await flushPromises()
    await selectClaudePlatformAWS(wrapper)

    expect(wrapper.text()).toContain('admin.accounts.concurrency')
    expect(wrapper.text()).toContain('admin.accounts.priority')
    expect(wrapper.text()).not.toContain('admin.accounts.loadFactor')
    expect(wrapper.text()).not.toContain('admin.accounts.billingRateMultiplier')
    expect(wrapper.text()).not.toContain('admin.accounts.expiresAt')
  })

  it('provides localized auth-profile gate copy for CP0 evidence and silent fallback', () => {
    expect(en.admin.accounts.claudePlatformAWS.authProfileGate.mutualExclusion).toContain('x_api_key')
    expect(en.admin.accounts.claudePlatformAWS.authProfileGate.mutualExclusion).toContain('bearer_api_key')
    expect(en.admin.accounts.claudePlatformAWS.authProfileGate.cp0Blocked).toContain('CP0')
    expect(en.admin.accounts.claudePlatformAWS.authProfileGate.noSilentFallback.toLowerCase()).toContain(
      'silent fallback'
    )
    expect(zh.admin.accounts.claudePlatformAWS.authProfileGate.mutualExclusion).toContain('x_api_key')
    expect(zh.admin.accounts.claudePlatformAWS.authProfileGate.mutualExclusion).toContain('bearer_api_key')
    expect(zh.admin.accounts.claudePlatformAWS.authProfileGate.cp0Blocked).toContain('CP0')
    expect(zh.admin.accounts.claudePlatformAWS.authProfileGate.noSilentFallback).toContain('silent fallback')
  })

  it('requires every workspace row to have its own proxy before batch import', async () => {
    const wrapper = mountModal()
    await flushPromises()
    await selectClaudePlatformAWS(wrapper)

    await setInput(wrapper, 'account-name-input', 'aws platform import')
    await setInput(wrapper, 'claude-platform-aws-api-key', awsCredentialFixture())
    await setInput(wrapper, 'claude-platform-aws-workspace-0', workspaceFixture('ROWONE123'))
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createClaudePlatformAWSBatchMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.claudePlatformAWS.proxyRequired')
    expect(wrapper.get('[data-testid="claude-platform-aws-row-status-0"]').text()).toContain('admin.accounts.claudePlatformAWS.status.needsProxy')
  })

  it('submits multiple workspace rows through the dedicated batch endpoint without raw workspace IDs in safe fields', async () => {
    const wrapper = mountModal()
    await flushPromises()
    await selectClaudePlatformAWS(wrapper)
    const workspaceOne = workspaceFixture('ROWONE123')
    const workspaceTwo = workspaceFixture('ROWTWO456')
    const apiKey = awsCredentialFixture()

    await setInput(wrapper, 'account-name-input', 'aws platform import')
    await setInput(wrapper, 'claude-platform-aws-api-key', apiKey)
    await setInput(wrapper, 'claude-platform-aws-workspace-0', workspaceOne)
    await wrapper.get('[data-testid="claude-platform-aws-row-0"]').find('[data-testid="proxy-option-10"]').trigger('click')

    await wrapper.get('[data-testid="claude-platform-aws-add-row"]').trigger('click')
    await setInput(wrapper, 'claude-platform-aws-workspace-1', workspaceTwo)
    await wrapper.get('[data-testid="claude-platform-aws-row-1"]').find('[data-testid="proxy-option-11"]').trigger('click')

    expect(wrapper.get('[data-testid="claude-platform-aws-row-status-0"]').text()).toContain('admin.accounts.claudePlatformAWS.status.ready')
    expect(wrapper.get('[data-testid="claude-platform-aws-row-status-1"]').text()).toContain('admin.accounts.claudePlatformAWS.status.ready')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createClaudePlatformAWSBatchMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock).not.toHaveBeenCalled()
    const payload = createClaudePlatformAWSBatchMock.mock.calls[0]?.[0]
    expect(payload).toMatchObject({
      group_ids: [],
      rows: [
        {
          name: 'aws platform import #1',
          aws_region: 'us-east-1',
          workspace_id: workspaceOne,
          api_key: apiKey,
          proxy_id: 10,
          concurrency: 10,
          priority: 1
        },
        {
          name: 'aws platform import #2',
          aws_region: 'us-east-1',
          workspace_id: workspaceTwo,
          api_key: apiKey,
          proxy_id: 11,
          concurrency: 10,
          priority: 1
        }
      ]
    })
    expect(payload).not.toHaveProperty('extra')
    expect(JSON.stringify(payload.rows.map((row: Record<string, unknown>) => row.name))).not.toContain('wrkspc_')
    expect(JSON.stringify(payload)).not.toContain('workspace_ref')
    expect(JSON.stringify(payload)).not.toContain('evidence')
  })
})
