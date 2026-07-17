import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, readonly, ref } from 'vue'

import ClaudeFormalPoolOnboardingWizardV2 from '../ClaudeFormalPoolOnboardingWizardV2.vue'
import type { FormalPoolAcceptanceResult, FormalPoolSession } from '@/api/admin/claudeOnboarding'

vi.mock('@/components/common/ConfirmDialog.vue', () => ({
  default: defineComponent({
    name: 'ConfirmDialog',
    props: ['show', 'title', 'message', 'confirmText', 'cancelText', 'danger', 'zIndex'],
    emits: ['confirm', 'cancel'],
    template:
      '<div v-if="show" data-testid="confirm-dialog-stub" :data-title="title" :data-message="message" :data-danger="String(danger)">' +
      '<button data-testid="confirm-dialog-stub-confirm" @click="$emit(\'confirm\')">{{ confirmText }}</button>' +
      '<button data-testid="confirm-dialog-stub-cancel" @click="$emit(\'cancel\')">{{ cancelText }}</button>' +
      '</div>',
  }),
}))

const onboardingApi = vi.hoisted(() => ({
  createSession: vi.fn(),
  getSession: vi.fn(),
  testProxy: vi.fn(),
  generateAuthUrl: vi.fn(),
  exchangeCodeAndCreate: vi.fn(),
  setupTokenCookieAuthAndCreate: vi.fn(),
  refreshOnly: vi.fn(),
  runtimeRegister: vi.fn(),
  healthcheck: vi.fn(),
  startWarming: vi.fn(),
  promoteProduction: vi.fn(),
  abort: vi.fn(),
}))

const adminApiMock = vi.hoisted(() => ({
  proxies: {
    getAllWithCount: vi.fn(),
    getAll: vi.fn(),
  },
  groups: {
    getAll: vi.fn(),
    getCapacitySummary: vi.fn(),
  },
}))

const egressPollingActions = vi.hoisted(() => ({
  start: vi.fn(),
  stop: vi.fn(),
  abort: vi.fn(),
}))

const egressPollingSession = ref<FormalPoolSession | null>(null)
const egressPollingMock = {
  session: readonly(egressPollingSession),
  status: readonly(ref('idle')),
  running: readonly(ref(false)),
  error: readonly(ref('')),
  ...egressPollingActions,
}

vi.mock('@/api/admin/claudeOnboarding', () => ({
  default: onboardingApi,
  ...onboardingApi,
}))

vi.mock('@/api/admin', () => ({
  adminAPI: adminApiMock,
  default: adminApiMock,
}))

vi.mock('@/composables/useEgressCheckPolling', () => ({
  useEgressCheckPolling: () => egressPollingMock,
}))

const routerLinkStub = {
  props: ['to'],
  template: '<a :href="typeof to === \'string\' ? to : to.path"><slot /></a>',
}

function mountWizard() {
  return mount(ClaudeFormalPoolOnboardingWizardV2, {
    global: {
      stubs: {
        RouterLink: routerLinkStub,
      },
    },
  })
}

function sessionFixture(overrides: Partial<FormalPoolSession> = {}): FormalPoolSession {
  return {
    id: 'session-1',
    version: 1,
    status: 'idle',
    pool_profile: 'normal',
    group_id: 9,
    account_name: 'claude-safe-name',
    concurrency: 1,
    browser_egress_check_status: 'idle',
    browser_egress_verified: false,
    ...overrides,
  }
}

function acceptanceFixture(overrides: Partial<FormalPoolAcceptanceResult> = {}): FormalPoolAcceptanceResult {
  return {
    version: 2,
    status: 'healthcheck_passed',
    account_id: 42,
    account_ref: 'acct_bucket_42',
    proxy_ref: 'proxy_bucket_1',
    egress_bucket: 'egress_bucket_1',
    pool_profile: 'normal',
    checks: [],
    no_real_messages_request_performed: false,
    activation_required: false,
    cc_gateway_seen: true,
    raw_capture_present: true,
    ...overrides,
  }
}

function proxyFixture(overrides: Record<string, unknown> = {}) {
  return {
    id: 7,
    name: 'Claude Tokyo SOCKS',
    protocol: 'socks5',
    host: '203.0.113.25',
    port: 1080,
    username: null,
    status: 'active',
    account_count: 2,
    latency_ms: 118,
    quality_grade: 'A',
    quality_status: 'healthy',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function groupFixture(overrides: Record<string, unknown> = {}) {
  return {
    id: 9,
    name: 'Claude Code 正式池',
    description: null,
    platform: 'anthropic',
    rate_multiplier: 1,
    is_exclusive: true,
    status: 'active',
    subscription_type: 'standard',
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    augment_gateway_entitled: false,
    allow_image_generation: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    claude_code_only: true,
    fallback_group_id: null,
    fallback_group_id_on_invalid_request: null,
    require_oauth_only: false,
    require_privacy_set: false,
    model_routing: null,
    model_routing_enabled: false,
    mcp_xml_inject: false,
    sort_order: 1,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

async function startSession(wrapper: ReturnType<typeof mount>, overrides: Partial<FormalPoolSession> = {}) {
  onboardingApi.createSession.mockResolvedValueOnce(sessionFixture(overrides))
  await flushPromises()
  await wrapper.find('[data-testid="account-name-input"]').setValue('claude-safe-name')
  await wrapper.find('[data-testid="proxy-card-7"]').trigger('click')
  await wrapper.find('[data-testid="group-card-9"]').trigger('click')
  await wrapper.find('[data-testid="start-session"]').trigger('click')
  await flushPromises()
}

describe('ClaudeFormalPoolOnboardingWizardV2', () => {
  beforeEach(() => {
    Object.values(onboardingApi).forEach((mock) => mock.mockReset())
    egressPollingMock.start.mockReset()
    egressPollingMock.stop.mockReset()
    egressPollingMock.abort.mockReset()
    egressPollingSession.value = null
    onboardingApi.getSession.mockResolvedValue(sessionFixture())
    adminApiMock.proxies.getAllWithCount.mockReset()
    adminApiMock.proxies.getAll.mockReset()
    adminApiMock.groups.getAll.mockReset()
    adminApiMock.groups.getCapacitySummary.mockReset()
    adminApiMock.proxies.getAllWithCount.mockResolvedValue([proxyFixture()])
    adminApiMock.proxies.getAll.mockResolvedValue([proxyFixture()])
    adminApiMock.groups.getAll.mockResolvedValue([groupFixture()])
    adminApiMock.groups.getCapacitySummary.mockResolvedValue([])
	})

	it('reuses a create operation key after an ambiguous failure', async () => {
		const wrapper = mountWizard()
		await flushPromises()
		onboardingApi.createSession.mockRejectedValueOnce(new Error('network unavailable')).mockResolvedValueOnce(sessionFixture())
		await wrapper.find('[data-testid="account-name-input"]').setValue('claude-safe-name')
		await wrapper.find('[data-testid="proxy-card-7"]').trigger('click')
		await wrapper.find('[data-testid="group-card-9"]').trigger('click')

		await wrapper.find('[data-testid="start-session"]').trigger('click')
		await flushPromises()
		await wrapper.find('[data-testid="start-session"]').trigger('click')
		await flushPromises()

		expect(onboardingApi.createSession).toHaveBeenCalledTimes(2)
		expect(onboardingApi.createSession.mock.calls[1][1]).toBe(onboardingApi.createSession.mock.calls[0][1])
	})

	it('rotates a create operation key after a definitive failure', async () => {
		const wrapper = mountWizard()
		await flushPromises()
		onboardingApi.createSession.mockRejectedValueOnce({ response: { status: 400, data: { message: 'invalid request' } } }).mockResolvedValueOnce(sessionFixture())
		await wrapper.find('[data-testid="account-name-input"]').setValue('claude-safe-name')
		await wrapper.find('[data-testid="proxy-card-7"]').trigger('click')
		await wrapper.find('[data-testid="group-card-9"]').trigger('click')

		await wrapper.find('[data-testid="start-session"]').trigger('click')
		await flushPromises()
		await wrapper.find('[data-testid="start-session"]').trigger('click')
		await flushPromises()

		expect(onboardingApi.createSession).toHaveBeenCalledTimes(2)
		expect(onboardingApi.createSession.mock.calls[1][1]).not.toBe(onboardingApi.createSession.mock.calls[0][1])
	})

	it('refetches on 409 and rejects a stale reconciliation snapshot', async () => {
		const wrapper = mountWizard()
		await startSession(wrapper, { version: 5 })
		onboardingApi.getSession.mockResolvedValueOnce(sessionFixture({ version: 4, status: 'stale' }))
		onboardingApi.testProxy.mockRejectedValueOnce({ response: { status: 409, data: { message: 'conflict' } } })

		await wrapper.find('[data-testid="test-proxy"]').trigger('click')
		await flushPromises()
		expect(onboardingApi.getSession).toHaveBeenCalledWith('session-1')

		onboardingApi.testProxy.mockResolvedValueOnce(sessionFixture({ version: 6, status: 'proxy_verified' }))
		await wrapper.find('[data-testid="test-proxy"]').trigger('click')
		await flushPromises()
		expect(onboardingApi.testProxy.mock.calls[0][0].version).toBe(5)
		expect(onboardingApi.testProxy.mock.calls[1][0].version).toBe(5)
	})

  it('does not show numeric proxy_id or group_id inputs in existing mode', async () => {
    const wrapper = mountWizard()
    await flushPromises()

    expect(wrapper.find('[data-testid="proxy-id-input"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-id-input"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="proxy-card-7"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="group-card-9"]').exists()).toBe(true)
  })

  it('selects an existing proxy card and submits its proxy_id', async () => {
    const wrapper = mountWizard()
    await flushPromises()

    onboardingApi.createSession.mockResolvedValueOnce(sessionFixture())
    await wrapper.find('[data-testid="account-name-input"]').setValue('claude-safe-name')
    await wrapper.find('[data-testid="proxy-card-7"]').trigger('click')
    await wrapper.find('[data-testid="group-card-9"]').trigger('click')
    await wrapper.find('[data-testid="start-session"]').trigger('click')
    await flushPromises()

    expect(onboardingApi.createSession).toHaveBeenCalledWith(expect.objectContaining({
      proxy_id: 7,
      group_id: 9,
      account_name: 'claude-safe-name',
    }), expect.any(String))
  })

  it('selects an Anthropic group card and submits its group_id', async () => {
    adminApiMock.groups.getAll.mockResolvedValueOnce([
      groupFixture({ id: 9, name: 'Default Claude' }),
      groupFixture({ id: 12, name: 'Claude Code 专用组' }),
    ])
    const wrapper = mountWizard()
    await flushPromises()

    onboardingApi.createSession.mockResolvedValueOnce(sessionFixture({ group_id: 12 }))
    await wrapper.find('[data-testid="account-name-input"]').setValue('claude-safe-name')
    await wrapper.find('[data-testid="proxy-card-7"]').trigger('click')
    await wrapper.find('[data-testid="group-card-12"]').trigger('click')
    await wrapper.find('[data-testid="start-session"]').trigger('click')
    await flushPromises()

    expect(adminApiMock.groups.getAll).toHaveBeenCalledWith('anthropic')
    expect(onboardingApi.createSession).toHaveBeenCalledWith(expect.objectContaining({
      proxy_id: 7,
      group_id: 12,
    }), expect.any(String))
  })

  it('shows management guidance when proxy and group lists are empty', async () => {
    adminApiMock.proxies.getAllWithCount.mockResolvedValueOnce([])
    adminApiMock.groups.getAll.mockResolvedValueOnce([])
    const wrapper = mountWizard()
    await flushPromises()

    expect(wrapper.text()).toContain('暂无可选代理')
    expect(wrapper.text()).toContain('去代理管理添加 IP')
    expect(wrapper.text()).toContain('暂无 Anthropic/Claude 分组')
    expect(wrapper.text()).toContain('去分组管理创建 Claude Code 专用分组')
    expect(wrapper.html()).toContain('/admin/proxies')
    expect(wrapper.html()).toContain('/admin/groups')
  })

  it('falls back to getAll and renders fallback proxies when getAllWithCount fails', async () => {
    adminApiMock.proxies.getAllWithCount.mockRejectedValueOnce(new Error('count endpoint unavailable'))
    adminApiMock.proxies.getAll.mockResolvedValueOnce([
      proxyFixture({ id: 14, name: 'Fallback proxy', host: '198.51.100.99' }),
    ])
    const wrapper = mountWizard()
    await flushPromises()

    expect(adminApiMock.proxies.getAllWithCount).toHaveBeenCalledTimes(1)
    expect(adminApiMock.proxies.getAll).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="proxy-list-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="proxy-card-14"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Fallback proxy')
  })

  it('sorts proxy cards by low binding count and collapses long proxy lists', async () => {
    adminApiMock.proxies.getAllWithCount.mockResolvedValueOnce([
      proxyFixture({ id: 41, name: 'busy proxy', account_count: 8, latency_ms: 20 }),
      proxyFixture({ id: 42, name: 'zero proxy b', account_count: 0, latency_ms: 90 }),
      proxyFixture({ id: 43, name: 'two proxy', account_count: 2, latency_ms: 10 }),
      proxyFixture({ id: 44, name: 'zero proxy a', account_count: 0, latency_ms: 30 }),
      proxyFixture({ id: 45, name: 'one proxy', account_count: 1, latency_ms: 50 }),
      proxyFixture({ id: 46, name: 'three proxy', account_count: 3, latency_ms: 50 }),
      proxyFixture({ id: 47, name: 'four proxy', account_count: 4, latency_ms: 50 }),
      proxyFixture({ id: 48, name: 'five proxy', account_count: 5, latency_ms: 50 }),
      proxyFixture({ id: 49, name: 'six proxy', account_count: 6, latency_ms: 50 }),
    ])
    const wrapper = mountWizard()
    await flushPromises()

    const visibleCards = wrapper.findAll('button[data-testid^="proxy-card-"]')
    expect(visibleCards).toHaveLength(8)
    expect(visibleCards[0].attributes('data-testid')).toBe('proxy-card-44')
    expect(visibleCards[1].attributes('data-testid')).toBe('proxy-card-42')
    expect(wrapper.find('[data-testid="proxy-card-41"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="proxy-list-toggle"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="proxy-list-summary"]').text()).toContain('优先显示未绑定/低绑定量代理')

    await wrapper.find('[data-testid="proxy-list-toggle"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('button[data-testid^="proxy-card-"]')).toHaveLength(9)
    expect(wrapper.find('[data-testid="proxy-card-41"]').exists()).toBe(true)
  })

  it('shows proxy and group load errors with reload buttons when loading fails', async () => {
    adminApiMock.proxies.getAllWithCount.mockRejectedValueOnce(new Error('count endpoint unavailable'))
    adminApiMock.proxies.getAll.mockRejectedValueOnce(new Error('proxy load failed'))
    adminApiMock.groups.getAll.mockRejectedValueOnce(new Error('group load failed'))
    const wrapper = mountWizard()
    await flushPromises()

    const proxyError = wrapper.find('[data-testid="proxy-list-error"]')
    const groupError = wrapper.find('[data-testid="group-list-error"]')

    expect(proxyError.exists()).toBe(true)
    expect(proxyError.text()).toContain('proxy load failed')
    expect(wrapper.find('[data-testid="reload-proxies"]').exists()).toBe(true)
    expect(groupError.exists()).toBe(true)
    expect(groupError.text()).toContain('group load failed')
    expect(wrapper.find('[data-testid="reload-groups"]').exists()).toBe(true)
  })

  it('shows dynamic missing items while the create session button is disabled', async () => {
    const wrapper = mountWizard()
    await flushPromises()

    const startButton = wrapper.find('[data-testid="start-session"]')
    expect(startButton.attributes('disabled')).toBeDefined()

    const missing = wrapper.find('[data-testid="start-session-missing-items"]')
    expect(missing.exists()).toBe(true)
    expect(missing.text()).toContain('账号名称')
    expect(missing.text()).toContain('代理')
    expect(missing.text()).toContain('分组')

    await wrapper.find('[data-testid="account-name-input"]').setValue('claude-safe-name')
    await wrapper.find('[data-testid="proxy-card-7"]').trigger('click')
    await wrapper.find('[data-testid="group-card-9"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="start-session-missing-items"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="start-session"]').attributes('disabled')).toBeUndefined()
  })

  it('shows create-proxy missing host and port with Chinese proxy field labels', async () => {
    const wrapper = mountWizard()
    await flushPromises()

    await wrapper.find('[data-testid="proxy-mode-select"]').setValue('create')
    await wrapper.find('[data-testid="account-name-input"]').setValue('claude-new-proxy')
    await wrapper.find('[data-testid="group-card-9"]').trigger('click')
    await wrapper.find('[data-testid="create-proxy-port-input"]').setValue('')
    await flushPromises()

    const missing = wrapper.find('[data-testid="start-session-missing-items"]')
    expect(missing.text()).toContain('创建代理地址')
    expect(missing.text()).toContain('创建代理端口')
    expect(wrapper.text()).toContain('代理地址')
    expect(wrapper.text()).toContain('示例：proxy.example.com')
    expect(wrapper.text()).toContain('代理端口')
    expect(wrapper.text()).toContain('示例：1080')
    expect(wrapper.text()).toContain('代理用户名')
    expect(wrapper.text()).toContain('没有账号密码可留空')
    expect(wrapper.text()).toContain('代理密码')
  })

  it('reload buttons call list APIs again', async () => {
    const wrapper = mountWizard()
    await flushPromises()

    adminApiMock.proxies.getAllWithCount.mockClear()
    adminApiMock.proxies.getAll.mockClear()
    adminApiMock.groups.getAll.mockClear()
    adminApiMock.groups.getCapacitySummary.mockClear()

    await wrapper.find('[data-testid="refresh-proxies"]').trigger('click')
    await wrapper.find('[data-testid="refresh-groups"]').trigger('click')
    await flushPromises()

    expect(adminApiMock.proxies.getAllWithCount).toHaveBeenCalledTimes(1)
    expect(adminApiMock.proxies.getAll).not.toHaveBeenCalled()
    expect(adminApiMock.groups.getAll).toHaveBeenCalledTimes(1)
    expect(adminApiMock.groups.getCapacitySummary).toHaveBeenCalledTimes(1)
  })

  it('shows proxy host endpoints on proxy cards and hides sensitive proxy fields', async () => {
    adminApiMock.proxies.getAllWithCount.mockResolvedValueOnce([
      proxyFixture({
        id: 21,
        name: 'IPv4 proxy',
        protocol: 'http',
        host: '203.0.113.25',
        port: 8080,
        username: 'raw-user',
        password: 'raw-pass',
        ip_address: '198.51.100.7',
        token: 'raw-token',
        nonce: 'raw-nonce',
      }),
      proxyFixture({
        id: 22,
        name: 'Domain proxy',
        protocol: 'socks5',
        host: 'edge.secret.example.net',
        port: 1080,
      }),
      proxyFixture({
        id: 23,
        name: 'IPv6 proxy',
        protocol: 'https',
        host: '2001:0db8:85a3::8a2e:0370:7334',
        port: 443,
      }),
      proxyFixture({
        id: 24,
        name: 'Missing host proxy',
        protocol: 'socks5h',
        host: '',
        port: 1081,
      }),
    ])
    const wrapper = mountWizard()
    await flushPromises()

    const text = wrapper.text()

    expect(text).toContain('http://203.0.113.25:8080')
    expect(text).toContain('socks5://edge.secret.example.net:1080')
    expect(text).toContain('https://[2001:0db8:85a3::8a2e:0370:7334]:443')
    expect(text).toContain('socks5h://host 未配置:1081')
    expect(text).not.toContain('IPv4 已隐藏')
    expect(text).not.toContain('IPv6 已隐藏')
    expect(text).not.toContain('域名已隐藏')
    expect(text).not.toContain('raw-user')
    expect(text).not.toContain('raw-pass')
    expect(text).not.toContain('198.51.100.7')
    expect(text).not.toContain('raw-token')
    expect(text).not.toContain('raw-nonce')
  })

  it('extracts proxy hostnames from malformed host fields without leaking userinfo', async () => {
    adminApiMock.proxies.getAllWithCount.mockResolvedValueOnce([
      proxyFixture({
        id: 31,
        protocol: 'http',
        host: 'http://url-user:url-pass@proxy.example.net',
        port: 8080,
      }),
      proxyFixture({
        id: 32,
        protocol: 'socks5',
        host: 'socks5://socks-user:socks-pass@203.0.113.25:1080',
        port: 1080,
      }),
      proxyFixture({
        id: 33,
        protocol: 'https',
        host: 'plain-user:plain-pass@edge.example.net',
        port: 443,
      }),
    ])
    const wrapper = mountWizard()
    await flushPromises()

    const text = wrapper.text()

    expect(text).toContain('http://proxy.example.net:8080')
    expect(text).toContain('socks5://203.0.113.25:1080')
    expect(text).toContain('https://edge.example.net:443')
    expect(text).not.toContain('url-user')
    expect(text).not.toContain('url-pass')
    expect(text).not.toContain('socks-user')
    expect(text).not.toContain('socks-pass')
    expect(text).not.toContain('plain-user')
    expect(text).not.toContain('plain-pass')
  })

  it('strips path query and fragment from dirty proxy host fields without leaking tokens', async () => {
    adminApiMock.proxies.getAllWithCount.mockResolvedValueOnce([
      proxyFixture({
        id: 35,
        protocol: 'http',
        host: 'proxy.example.net/path?token=abc#frag',
        port: 8080,
      }),
      proxyFixture({
        id: 36,
        protocol: 'socks5',
        host: 'proxy.example.net:1080/path?password=abc',
        port: 18080,
      }),
    ])
    const wrapper = mountWizard()
    await flushPromises()

    const firstCardText = wrapper.find('[data-testid="proxy-card-35"]').text()
    const secondCardText = wrapper.find('[data-testid="proxy-card-36"]').text()
    const text = wrapper.text()

    expect(firstCardText).toContain('http://proxy.example.net:8080')
    expect(secondCardText).toContain('socks5://proxy.example.net:18080')
    expect(text).not.toContain('/path')
    expect(text).not.toContain('token=abc')
    expect(text).not.toContain('password=abc')
    expect(text).not.toContain('#frag')
  })

  it('does not double-wrap already bracketed IPv6 proxy hosts', async () => {
    adminApiMock.proxies.getAllWithCount.mockResolvedValueOnce([
      proxyFixture({
        id: 34,
        protocol: 'https',
        host: '[2001:db8::1]',
        port: 443,
      }),
    ])
    const wrapper = mountWizard()
    await flushPromises()

    const text = wrapper.find('[data-testid="proxy-card-34"]').text()

    expect(text).toContain('https://[2001:db8::1]:443')
    expect(text).not.toContain('https://[[2001:db8::1]]:443')
  })

  it('keeps create-proxy mode usable and submits a proxy object', async () => {
    const wrapper = mountWizard()
    await flushPromises()

    await wrapper.find('[data-testid="proxy-card-7"]').trigger('click')
    await wrapper.find('[data-testid="proxy-mode-select"]').setValue('create')
    onboardingApi.createSession.mockResolvedValueOnce(sessionFixture())
    await wrapper.find('[data-testid="account-name-input"]').setValue('claude-new-proxy')
    await wrapper.find('[data-testid="group-card-9"]').trigger('click')
    await wrapper.find('input[placeholder="代理备注名称"]').setValue('New managed proxy')
    await wrapper.find('input[placeholder="示例：proxy.example.com"]').setValue('proxy.example.net')
    await wrapper.find('[data-testid="create-proxy-port-input"]').setValue(18080)
    await wrapper.find('[data-testid="start-session"]').trigger('click')
    await flushPromises()

    const payload = onboardingApi.createSession.mock.calls[0][0]
    expect(payload).not.toHaveProperty('proxy_id')
    expect(onboardingApi.createSession).toHaveBeenCalledWith(expect.objectContaining({
      proxy_mode: 'create',
      group_id: 9,
      account_name: 'claude-new-proxy',
      proxy: expect.objectContaining({
        name: 'New managed proxy',
        protocol: 'socks5',
        host: 'proxy.example.net',
        port: 18080,
      }),
    }), expect.any(String))
  })

  it('does not render undefined capacity fragments when group capacity fields are incomplete', async () => {
    adminApiMock.groups.getCapacitySummary.mockResolvedValueOnce([
      { group_id: 9, concurrency_used: 2, concurrency_max: undefined, sessions_used: null, sessions_max: null },
    ])
    const wrapper = mountWizard()
    await flushPromises()

    const groupCardText = wrapper.find('[data-testid="group-card-9"]').text()

    expect(groupCardText).not.toContain('undefined/undefined')
    expect(groupCardText).not.toContain('undefined')
    expect(groupCardText).not.toContain('null/null')
    expect(groupCardText).not.toContain('并发 2/undefined')
  })

  it('shows idle and no browser egress check URL immediately after StartSession', async () => {
    const wrapper = mountWizard()

    await startSession(wrapper)

    expect(wrapper.find('[data-testid="browser-egress-status"]').text()).toContain('未开始')
    expect(wrapper.find('[data-testid="browser-egress-status"]').text()).not.toContain('idle')
    expect(wrapper.find('[data-testid="browser-egress-check-url"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('/browser-egress-check/')
  })

  it('does not render the raw browser egress nonce while copy still uses the real URL', async () => {
    const rawNonce = 'raw-nonce-DO-NOT-LEAK-12345'
    const realUrl = `https://safe.example/api/v1/claude-onboarding/browser-egress-check/${rawNonce}`
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const wrapper = mountWizard()
    await startSession(wrapper)

    onboardingApi.testProxy.mockResolvedValueOnce(sessionFixture({
      status: 'proxy_tested',
      browser_egress_check_status: 'waiting',
      browser_egress_check_url: realUrl,
    }))
    await wrapper.find('[data-testid="test-proxy"]').trigger('click')
    await flushPromises()

    expect(wrapper.html()).not.toContain(rawNonce)
    expect(wrapper.text()).not.toContain(rawNonce)
    expect(wrapper.html()).not.toContain(realUrl)
    expect(wrapper.text()).toContain('已生成一次性校验链接')

    await wrapper.find('[data-testid="copy-browser-egress-check-url"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(realUrl)
    vi.unstubAllGlobals()
  })

  it('falls back to execCommand copy for browser egress links without permanently rendering the raw nonce', async () => {
    const rawNonce = 'raw-nonce-FALLBACK-DO-NOT-LEAK-12345'
    const realUrl = `https://safe.example/api/v1/claude-onboarding/browser-egress-check/${rawNonce}`
    vi.stubGlobal('navigator', {})
    const originalExecCommand = document.execCommand
    const execCommand = vi.fn().mockReturnValue(true)
    Object.defineProperty(document, 'execCommand', { configurable: true, value: execCommand })
    const wrapper = mountWizard()
    await startSession(wrapper)

    onboardingApi.testProxy.mockResolvedValueOnce(sessionFixture({
      status: 'proxy_tested',
      browser_egress_check_status: 'waiting',
      browser_egress_check_url: realUrl,
    }))
    await wrapper.find('[data-testid="test-proxy"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="browser-egress-check-url-display"]').text()).toContain('[一次性 nonce 已隐藏]')
    expect(wrapper.html()).not.toContain(rawNonce)
    expect(wrapper.html()).not.toContain(realUrl)

    await wrapper.find('[data-testid="copy-browser-egress-check-url"]').trigger('click')
    await flushPromises()

    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(wrapper.find('[data-testid="browser-egress-copy-status"]').text()).toContain('已复制校验链接')
    expect(wrapper.html()).not.toContain(rawNonce)
    expect(wrapper.html()).not.toContain(realUrl)
    Object.defineProperty(document, 'execCommand', { configurable: true, value: originalExecCommand })
    vi.unstubAllGlobals()
  })

  it('shows the OAuth 1/2/3 human flow, copies the generated authorization link, and keeps the raw URL hidden', async () => {
    const rawCode = 'oauth-code-DO-NOT-LEAK-12345'
    const realUrl = `https://claude.ai/oauth/authorize?code=${rawCode}`
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const wrapper = mountWizard()
    await startSession(wrapper, { browser_egress_verified: true, browser_egress_check_status: 'verified' })
    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')

    expect(wrapper.find('[data-testid="oauth-human-flow"]').text()).toContain('1')
    expect(wrapper.find('[data-testid="oauth-human-flow"]').text()).toContain('复制授权链接')
    expect(wrapper.find('[data-testid="oauth-human-flow"]').text()).toContain('同出口浏览器')
    expect(wrapper.find('[data-testid="oauth-human-flow"]').text()).toContain('粘贴授权码并创建账号')
    expect(wrapper.text()).toContain('生成授权链接')
    expect(wrapper.text()).not.toContain('生成 OAuth URL')

    onboardingApi.generateAuthUrl.mockResolvedValueOnce(sessionFixture({
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      auth_url: realUrl,
    }))
    await wrapper.find('[data-testid="generate-oauth-url"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('授权链接已生成')
    expect(wrapper.html()).not.toContain(rawCode)
    expect(wrapper.html()).not.toContain(realUrl)

    await wrapper.find('[data-testid="copy-oauth-url"]').trigger('click')
    await flushPromises()

    expect(writeText).toHaveBeenCalledWith(realUrl)
    expect(wrapper.find('[data-testid="oauth-copy-status"]').text()).toContain('已复制授权链接')
    vi.unstubAllGlobals()
  })

  it('shows a retryable message when copying the OAuth authorization link fails', async () => {
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    const wrapper = mountWizard()
    await startSession(wrapper, {
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      auth_url: 'https://claude.ai/oauth/authorize?code=copy-fails',
    })
    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')

    await wrapper.find('[data-testid="copy-oauth-url"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="oauth-copy-status"]').text()).toContain('复制失败，请重试')
    vi.unstubAllGlobals()
  })

  it('uses Chinese primary action text and avoids raw engineering words in the onboarding UX', async () => {
    const wrapper = mountWizard()
    await flushPromises()

    const proxyStepText = wrapper.text()
    expect(proxyStepText).toContain('标准消耗：约 7 天平滑使用（推荐）')
    expect(proxyStepText).toContain('加速消耗：请求更积极，但仍需通过健康门禁')
    expect(proxyStepText).not.toContain('Exchange code')
    expect(proxyStepText).not.toContain('OAuth URL')
    expect(proxyStepText).not.toContain('Host')
    expect(proxyStepText).not.toContain('Port')
    expect(proxyStepText).not.toContain('Username')
    expect(proxyStepText).not.toContain('Password')

    await startSession(wrapper, { browser_egress_verified: true, browser_egress_check_status: 'verified' })
    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')

    const authStepText = wrapper.text()
    expect(authStepText).toContain('复制授权链接')
    expect(authStepText).toContain('提交授权码并创建账号')
    expect(authStepText).not.toContain('Exchange code')
    expect(authStepText).not.toContain('OAuth URL')
  })



  it('uses safe session ref in the badge instead of raw session id', async () => {
    const rawSessionId = '987654321'
    const wrapper = mountWizard()

    await startSession(wrapper, {
      id: rawSessionId,
      safe_summary: { session_ref: 'session_bucket_safe_abc' },
    })

    const badge = wrapper.find('[data-testid="session-ref"]')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toContain('session_bucket_safe_abc')
    expect(wrapper.html()).not.toContain(rawSessionId)
    expect(wrapper.text()).not.toContain(rawSessionId)
  })

  it('does not render a current-browser open button or attestation code input', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      browser_egress_check_url: 'https://safe.example/check',
      browser_egress_check_status: 'waiting',
    })

    expect(wrapper.text().toLowerCase()).not.toContain('open current browser')
    expect(wrapper.text()).not.toContain('打开当前浏览器')
    expect(wrapper.find('input[placeholder*="attestation"]').exists()).toBe(false)
    expect(wrapper.find('input[placeholder*="校验码"]').exists()).toBe(false)
  })

  it('monotonically consumes the shared poller finalization result', async () => {
    const proof = `nonce_${'a'.repeat(32)}`
    const wrapper = mountWizard()
    await startSession(wrapper)
    const pending = sessionFixture({
      version: 2,
      status: 'proxy_tested',
      browser_egress_check_status: 'verified_pending_finalize',
      browser_egress_verified: false,
      browser_egress_check_url: `https://safe.example/browser-egress-check/${proof}`,
    })
    onboardingApi.testProxy.mockResolvedValueOnce(pending)

    await wrapper.find('[data-testid="test-proxy"]').trigger('click')
    await flushPromises()
    expect(egressPollingMock.start).toHaveBeenCalledWith('session-1')

    egressPollingSession.value = sessionFixture({
      version: 3,
      status: 'proxy_verified',
      browser_egress_check_status: 'verified',
      browser_egress_verified: true,
    })
    await flushPromises()
    egressPollingSession.value = pending
    await flushPromises()

    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('available')
    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')
    expect(wrapper.find('[data-testid="generate-oauth-url"]').attributes('disabled')).toBeUndefined()
  })

  it('labels expired nonce recovery as starting a new onboarding session and triggers createSession', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      browser_egress_check_status: 'expired',
      browser_egress_last_error_code: 'nonce_expired',
    })

    onboardingApi.createSession.mockResolvedValueOnce(sessionFixture({ id: 'session-2' }))
    const cta = wrapper.find('[data-testid="expired-start-new-session"]')
    expect(cta.exists()).toBe(true)
    expect(cta.text()).toContain('重新开一个上号会话')
    expect(wrapper.text()).not.toContain('重新生成校验链接')
    expect(wrapper.text().toLowerCase()).not.toContain('regenerate nonce')

    await cta.trigger('click')
    await flushPromises()

    expect(onboardingApi.createSession).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('session-2')
    expect(wrapper.get('[data-testid="session-ref"]').text()).toContain('会话编号暂不可用')
  })

  it('shows mismatch buckets or generic copy and never raw IP addresses', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      browser_egress_check_status: 'mismatch',
      browser_egress_last_error_code: 'mismatch',
      browser_egress_browser_ip_bucket: 'browser_bucket_A',
      browser_egress_proxy_ip_bucket: 'proxy_bucket_B',
      safe_summary: {
        detail: 'browser 203.0.113.10 did not match proxy 198.51.100.7',
      },
    })

    const mismatch = wrapper.find('[data-testid="browser-egress-mismatch"]')
    expect(mismatch.exists()).toBe(true)
    expect(mismatch.text()).toContain('浏览器出口分组')
    expect(mismatch.text()).toContain('代理出口分组')
    expect(mismatch.text()).toContain('browser_bucket_A')
    expect(mismatch.text()).toContain('proxy_bucket_B')
    expect(mismatch.text()).toContain('出口不一致')
    expect(mismatch.text()).not.toContain('Browser bucket')
    expect(mismatch.text()).not.toContain('Proxy bucket')
    expect(mismatch.text().toLowerCase()).not.toContain('mismatch')
    expect(wrapper.text()).not.toContain('203.0.113.10')
    expect(wrapper.text()).not.toContain('198.51.100.7')
  })

  it('uses a non-token-shaped Setup Token placeholder', async () => {
    const wrapper = mountWizard()
    // Auth step is locked until browser egress is verified.
    await startSession(wrapper, { browser_egress_verified: true, browser_egress_check_status: 'verified' })
    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')
    await wrapper.find('input[value="setup-token-cookie"]').setValue()

    const input = wrapper.find('[data-testid="setup-token-input"]')
    expect(input.exists()).toBe(true)
    expect(input.attributes('placeholder')).toBe('粘贴 Setup Token')
    expect(input.attributes('placeholder')).not.toMatch(/sk-ant-sid/i)
  })

  it('lets Setup Token flow skip browser egress verification after proxy health passes', async () => {
    const wrapper = mountWizard()
    await flushPromises()

    await wrapper.find('[data-testid="auth-mode-setup-token"]').setValue()
    await startSession(wrapper)

    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('locked')
    expect(wrapper.find('[data-testid="stepper-lock-reason-auth"]').text()).toContain('代理健康检查')
    expect(wrapper.find('[data-testid="setup-token-proxy-not-ready"]').text()).toContain('代理健康检查未通过')
    expect(wrapper.find('[data-testid="test-proxy"]').text()).toContain('测试代理健康')

    onboardingApi.testProxy.mockResolvedValueOnce(sessionFixture({
      status: 'proxy_verified',
      browser_egress_check_status: 'waiting',
      browser_egress_verified: false,
      browser_egress_check_url: 'https://safe.example/api/v1/claude-onboarding/browser-egress-check/nonce-bucket',
    }))
    await wrapper.find('[data-testid="test-proxy"]').trigger('click')
    await flushPromises()

    expect(egressPollingMock.start).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.find('[data-testid="browser-egress-check-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="setup-token-input"]').exists()).toBe(true)

    await wrapper.find('[data-testid="setup-token-input"]').setValue('safe-test-token')
    const createButton = wrapper.find('[data-testid="setup-token-create"]')
    expect(createButton.attributes('disabled')).toBeUndefined()

    onboardingApi.setupTokenCookieAuthAndCreate.mockResolvedValueOnce(sessionFixture({
      status: 'imported',
      account_id: 88,
      browser_egress_verified: false,
    }))
    await createButton.trigger('click')
    await flushPromises()

    expect(onboardingApi.setupTokenCookieAuthAndCreate).toHaveBeenCalledWith(expect.objectContaining({ id: 'session-1' }), 'safe-test-token')
    expect(wrapper.find('[data-testid="stepper-gates"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.text()).toContain('完成上号检查，进入预热期')
  })

  it('does not let Setup Token continue when proxy health did not pass', async () => {
    const wrapper = mountWizard()
    await flushPromises()

    await wrapper.find('[data-testid="auth-mode-setup-token"]').setValue()
    await startSession(wrapper)

    onboardingApi.testProxy.mockResolvedValueOnce(sessionFixture({
      status: 'idle',
      browser_egress_check_status: 'mismatch',
      browser_egress_verified: false,
    }))
    await wrapper.find('[data-testid="test-proxy"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('locked')
    expect(wrapper.find('[data-testid="setup-token-input"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('代理健康检查未通过')
  })

  it('shows one operator-friendly recommended gate action and hides manual engineering actions by default', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      status: 'refreshed',
      account_id: 42,
      cc_gateway_runtime_registered: false,
      healthcheck_passed: false,
    })

    await wrapper.find('[data-testid="stepper-gates"]').trigger('click')

    expect(wrapper.text()).toContain('完成上号检查，进入预热期')
    expect(wrapper.text()).not.toContain('Refresh / Runtime / Healthcheck / Warming / Production')
    expect(wrapper.find('[data-testid="recommended-gate-action"]').text()).toContain('继续下一步：接入调度器')
    expect(wrapper.text()).toContain('让调度器识别这个账号，之后才能做健康检查')
    expect(wrapper.text()).toContain('登录态确认')
    expect(wrapper.text()).toContain('调度器接入')
    expect(wrapper.text()).toContain('上游可用性检查')
    expect(wrapper.text()).not.toContain('运行映射')
    expect(wrapper.find('[data-testid="refresh-only"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="runtime-register"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="healthcheck"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="start-warming"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="promote-production"]').exists()).toBe(false)

    await wrapper.find('[data-testid="advanced-manual-toggle"]').trigger('click')

    expect(wrapper.find('[data-testid="refresh-only"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="runtime-register"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="healthcheck"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="start-warming"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="promote-production"]').exists()).toBe(true)
  })

  it('starts the recommended gate flow with credential refresh before runtime registration', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      status: 'imported',
      account_id: 42,
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      cc_gateway_runtime_registered: false,
      healthcheck_passed: false,
    })

    await wrapper.find('[data-testid="stepper-gates"]').trigger('click')

    const recommendedButton = wrapper.find('[data-testid="recommended-gate-action"]')
    expect(recommendedButton.text()).toContain('继续下一步：刷新登录状态')
    expect(wrapper.text()).toContain('先确认登录态仍可用')

    onboardingApi.refreshOnly.mockResolvedValueOnce(sessionFixture({
      status: 'refreshed',
      account_id: 42,
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      cc_gateway_runtime_registered: false,
      healthcheck_passed: false,
    }))
    await recommendedButton.trigger('click')
    await flushPromises()

    expect(onboardingApi.refreshOnly).toHaveBeenCalledWith(expect.objectContaining({ id: 'session-1' }))
    expect(onboardingApi.runtimeRegister).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="recommended-gate-action"]').text()).toContain('继续下一步：接入调度器')
  })

  it('advances the recommended gate action through health check and warming with Chinese labels', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      status: 'runtime_registered',
      account_id: 42,
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      cc_gateway_runtime_registered: true,
      healthcheck_passed: false,
    })

    await wrapper.find('[data-testid="stepper-gates"]').trigger('click')

    const recommendedButton = () => wrapper.find('[data-testid="recommended-gate-action"]')
    expect(recommendedButton().text()).toContain('继续下一步：做一次上游可用性检查')

    onboardingApi.healthcheck.mockResolvedValueOnce(acceptanceFixture())
    await recommendedButton().trigger('click')
    await flushPromises()
    expect(onboardingApi.healthcheck).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="healthcheck-confirm-dialog"]').attributes('data-message')).toContain('真实上游请求')
    await wrapper.find('[data-testid="confirm-dialog-stub-confirm"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-healthcheck_passed"]').classes()).toContain('is-active')
    expect(wrapper.find('[data-testid="stage-healthcheck_passed"]').text()).toContain('上游可用性已通过')
    expect(recommendedButton().text()).toContain('继续下一步：进入低权重预热')

		onboardingApi.startWarming.mockResolvedValueOnce(sessionFixture({ version: 3, status: 'warming', account_id: 42, healthcheck_passed: true }))
		await recommendedButton().trigger('click')
		await flushPromises()

		expect(onboardingApi.startWarming.mock.calls[0][0].version).toBe(2)
		expect(recommendedButton().text()).toContain('切换到生产调度')
    expect(wrapper.text()).toContain('账号已在低权重预热期，可按策略切换到生产调度')
  })

  it('does not run the manual health check until the operator confirms', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      status: 'runtime_registered',
      account_id: 42,
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      cc_gateway_runtime_registered: true,
      healthcheck_passed: false,
    })

    await wrapper.find('[data-testid="stepper-gates"]').trigger('click')
    await wrapper.find('[data-testid="advanced-manual-toggle"]').trigger('click')
    await wrapper.find('[data-testid="healthcheck"]').trigger('click')
    await flushPromises()

    expect(onboardingApi.healthcheck).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="healthcheck-confirm-dialog"]').exists()).toBe(true)

    await wrapper.find('[data-testid="confirm-dialog-stub-cancel"]').trigger('click')
    expect(wrapper.find('[data-testid="healthcheck-confirm-dialog"]').exists()).toBe(false)
    expect(onboardingApi.healthcheck).not.toHaveBeenCalled()
  })

  it('renders Chinese stage labels instead of raw engineering statuses', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      status: 'production',
      account_id: 42,
      cc_gateway_runtime_registered: true,
      healthcheck_passed: true,
    })

    await wrapper.find('[data-testid="stepper-gates"]').trigger('click')

    expect(wrapper.find('[data-testid="stage-runtime_registered"]').text()).toContain('已接入调度器')
    expect(wrapper.find('[data-testid="stage-healthcheck_passed"]').text()).toContain('上游可用性已通过')
    expect(wrapper.find('[data-testid="stage-warming"]').text()).toContain('预热期')
    expect(wrapper.find('[data-testid="stage-production"]').text()).toContain('生产调度中')
    expect(wrapper.find('[data-testid="recommended-gate-action"]').text()).toContain('查看诊断状态')
    expect(wrapper.text()).not.toContain('runtime_registered')
    expect(wrapper.text()).not.toContain('healthcheck_passed')
    expect(wrapper.text()).not.toContain('new号 low weight')
    expect(wrapper.text()).not.toContain('Promote production')
  })

  it('keeps production promotion in the recommended flow when the account is warming', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      status: 'warming',
      account_id: 42,
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      cc_gateway_runtime_registered: true,
      healthcheck_passed: true,
    })

    await wrapper.find('[data-testid="stepper-gates"]').trigger('click')

    const recommendedButton = wrapper.find('[data-testid="recommended-gate-action"]')
    expect(recommendedButton.text()).toContain('切换到生产调度')

    onboardingApi.promoteProduction.mockResolvedValueOnce(sessionFixture({
      status: 'production',
      account_id: 42,
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      cc_gateway_runtime_registered: true,
      healthcheck_passed: true,
    }))
    await recommendedButton.trigger('click')
    await flushPromises()

    expect(onboardingApi.promoteProduction).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="promote-production-confirm-dialog"]').exists()).toBe(true)

    await wrapper.find('[data-testid="confirm-dialog-stub-cancel"]').trigger('click')
    expect(onboardingApi.promoteProduction).not.toHaveBeenCalled()

    await recommendedButton.trigger('click')
    await wrapper.find('[data-testid="confirm-dialog-stub-confirm"]').trigger('click')
    await flushPromises()

    expect(onboardingApi.promoteProduction).toHaveBeenCalledWith(expect.objectContaining({ id: 'session-1' }), expect.any(String))
    expect(wrapper.find('[data-testid="recommended-gate-action"]').text()).toContain('查看诊断状态')
  })


  it('confirms manual production promotion before calling the API', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      status: 'warming',
      account_id: 42,
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      cc_gateway_runtime_registered: true,
      healthcheck_passed: true,
    })

    await wrapper.find('[data-testid="stepper-gates"]').trigger('click')
    await wrapper.find('[data-testid="advanced-manual-toggle"]').trigger('click')

    onboardingApi.promoteProduction.mockResolvedValueOnce(sessionFixture({
      status: 'production',
      account_id: 42,
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      cc_gateway_runtime_registered: true,
      healthcheck_passed: true,
    }))
    await wrapper.find('[data-testid="promote-production"]').trigger('click')
    await flushPromises()

    expect(onboardingApi.promoteProduction).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="promote-production-confirm-dialog"]').exists()).toBe(true)

    await wrapper.find('[data-testid="confirm-dialog-stub-confirm"]').trigger('click')
    await flushPromises()

    expect(onboardingApi.promoteProduction).toHaveBeenCalledWith(expect.objectContaining({ id: 'session-1' }), expect.any(String))
  })

  it('renders browser egress statuses in Chinese without raw status codes', async () => {
    const cases = [
      ['idle', '未开始'],
      ['verified', '已通过'],
      ['mismatch', '出口不一致'],
      ['expired', '已过期'],
    ] as const

    for (const [rawStatus, label] of cases) {
      const wrapper = mountWizard()
      await startSession(wrapper, { browser_egress_check_status: rawStatus, browser_egress_verified: rawStatus === 'verified' })
      const statusText = wrapper.find('[data-testid="browser-egress-status"]').text()
      expect(statusText).toContain(label)
      expect(statusText).not.toContain(rawStatus)
    }

    const wrapper = mountWizard()
    await flushPromises()
    await wrapper.find('[data-testid="auth-mode-setup-token"]').setValue()
    await startSession(wrapper, {
      status: 'proxy_verified',
      browser_egress_check_status: 'waiting',
      browser_egress_verified: false,
    })

    const setupTokenStatusText = wrapper.find('[data-testid="browser-egress-status"]').text()
    expect(setupTokenStatusText).toContain('代理健康已通过')
    expect(setupTokenStatusText).not.toContain('waiting')
  })

  it('keeps raw diagnostic JSON folded on the evidence page and shows operator labels', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      status: 'production',
      account_id: 42,
      proxy_ref: 'proxy_bucket_safe',
      egress_bucket: 'egress_bucket_safe',
      cc_gateway_runtime_registered: true,
      healthcheck_passed: true,
    })

    await wrapper.find('[data-testid="stepper-evidence"]').trigger('click')

    expect(wrapper.text()).toContain('代理标识')
    expect(wrapper.text()).toContain('出口分组')
    expect(wrapper.text()).not.toContain('Proxy ref')
    expect(wrapper.text()).not.toContain('Egress bucket')
    expect(wrapper.text()).not.toContain('cc_gateway_runtime_registered')
    expect(wrapper.text()).not.toContain('healthcheck_passed')
    expect(wrapper.find('[data-testid="advanced-safe-session-json"]').exists()).toBe(false)

    await wrapper.find('[data-testid="advanced-safe-session-toggle"]').trigger('click')

    expect(wrapper.find('[data-testid="advanced-safe-session-json"]').exists()).toBe(true)
  })

  it('scrubs sensitive backend and account-source display text before it reaches the DOM', async () => {
    const wrapper = mountWizard()
    const sensitiveFragments = [
      'sk-ant-sid-raw-secret-DO-NOT-LEAK',
      'plain prompt text without token shape',
      'plain body text without token shape',
      'plain telemetry payload without token shape',
      'plain cch payload without token shape',
      'operator@example.com',
      '123e4567-e89b-12d3-a456-426614174000',
      'http://proxyUser:proxyPass@example-proxy.local:8080',
      'plain proxy ref without token shape',
      'plain proxy value without token shape',
      'prompt check name without token shape',
      'message check text without token shape',
    ]

    await startSession(wrapper, {
      status: 'quarantined',
      account_id: 42,
      account_name: sensitiveFragments[5],
      proxy_ref: sensitiveFragments[8],
      account_ref: sensitiveFragments[6],
      safe_summary: {
        token: sensitiveFragments[0],
        prompt: sensitiveFragments[1],
        body: sensitiveFragments[2],
        telemetry: sensitiveFragments[3],
        cch: sensitiveFragments[4],
        proxy: sensitiveFragments[9],
        nested: { raw_capture: 'nested safe-looking raw capture text' },
        status: 'quarantined',
        boolean_gate: true,
      },
      checks: [
        { name: sensitiveFragments[10], status: 'fail', message: sensitiveFragments[11] },
        { name: 'safe_bucket', status: 'pass', message: 'bucket_ok' },
      ],
    })

    await wrapper.find('[data-testid="stepper-evidence"]').trigger('click')

    const html = wrapper.html()
    for (const fragment of sensitiveFragments) {
      expect(html).not.toContain(fragment)
    }
    expect(html).not.toContain('nested safe-looking raw capture text')
    expect(html).toContain('[redacted]')
    expect(html).toContain('quarantined')
    expect(html).toContain('bucket_ok')
  })

  it('scrubs obvious IPv6 raw IP addresses before display', async () => {
    const wrapper = mountWizard()

    await startSession(wrapper, {
      status: 'quarantined',
      safe_summary: {
        detail: 'browser 2001:0db8:85a3:0000:0000:8a2e:0370:7334 did not match proxy fe80::1ff:fe23:4567:890a',
      },
      checks: [{ name: 'ipv6_check', status: 'fail', message: 'raw ipv6 ::ffff:192.0.2.128 was present' }],
    })

    await wrapper.find('[data-testid="stepper-evidence"]').trigger('click')

    expect(wrapper.html()).not.toContain('2001:0db8:85a3:0000:0000:8a2e:0370:7334')
    expect(wrapper.html()).not.toContain('fe80::1ff:fe23:4567:890a')
    expect(wrapper.html()).not.toContain('::ffff:192.0.2.128')
  })

  it('starts with proxy as the active step and auth/gates/evidence all locked', async () => {
    const wrapper = mountWizard()

    expect(wrapper.find('[data-testid="stepper-proxy"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('locked')
    expect(wrapper.find('[data-testid="stepper-gates"]').attributes('data-step-status')).toBe('locked')
    expect(wrapper.find('[data-testid="stepper-evidence"]').attributes('data-step-status')).toBe('locked')

    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('aria-disabled')).toBe('true')
    expect(wrapper.find('[data-testid="stepper-gates"]').attributes('aria-disabled')).toBe('true')
    expect(wrapper.find('[data-testid="stepper-evidence"]').attributes('aria-disabled')).toBe('true')

    // Locked steps expose a visible prerequisite hint and a hover-tooltip title.
    const authBtn = wrapper.find('[data-testid="stepper-auth"]')
    expect(authBtn.attributes('title')).toContain('需先在第 1 步')
    expect(wrapper.find('[data-testid="stepper-lock-reason-auth"]').text()).toContain('需先在第 1 步')
    expect(wrapper.find('[data-testid="stepper-lock-reason-gates"]').text()).toContain('需先在第 2 步')
    expect(wrapper.find('[data-testid="stepper-lock-reason-evidence"]').text()).toContain('需先在第 1 步')
  })

  it('locked stepper buttons refuse click and do NOT change the active step', async () => {
    const wrapper = mountWizard()

    // proxy is active by default; clicking a locked step must not move there.
    await wrapper.find('[data-testid="stepper-gates"]').trigger('click')
    expect(wrapper.find('[data-testid="stepper-proxy"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.find('[data-testid="stepper-gates"]').attributes('data-step-status')).toBe('locked')

    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')
    expect(wrapper.find('[data-testid="stepper-proxy"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('locked')

    await wrapper.find('[data-testid="stepper-evidence"]').trigger('click')
    expect(wrapper.find('[data-testid="stepper-proxy"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.find('[data-testid="stepper-evidence"]').attributes('data-step-status')).toBe('locked')
  })

  it('creating a session keeps auth/gates locked until browser egress and account gates are satisfied', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper)

    // After StartSession, proxy is still the active step. Auth stays locked
    // until browser egress is verified, so operators cannot jump into a disabled
    // authorization form before completing the same-egress gate.
    expect(wrapper.find('[data-testid="stepper-proxy"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('locked')
    expect(wrapper.find('[data-testid="stepper-gates"]').attributes('data-step-status')).toBe('locked')
    expect(wrapper.find('[data-testid="stepper-evidence"]').attributes('data-step-status')).toBe('available')
    expect(wrapper.find('[data-testid="stepper-lock-reason-auth"]').text()).toContain('同出口校验')
    expect(wrapper.find('[data-testid="stepper-lock-reason-gates"]').text()).toContain('授权')

    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')
    expect(wrapper.find('[data-testid="stepper-proxy"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('locked')
  })

  it('verified browser egress unlocks auth while gates stay locked until an account exists', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, { browser_egress_verified: true, browser_egress_check_status: 'verified' })

    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('available')
    expect(wrapper.find('[data-testid="stepper-gates"]').attributes('data-step-status')).toBe('locked')

    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')
    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.find('[data-testid="stepper-proxy"]').attributes('data-step-status')).toBe('done')
  })

  it('falls back from auth when browser egress is no longer verified', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, { browser_egress_verified: true, browser_egress_check_status: 'verified' })
    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')
    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('active')

    onboardingApi.generateAuthUrl.mockResolvedValueOnce(sessionFixture({
      browser_egress_verified: false,
      browser_egress_check_status: 'mismatch',
    }))
    const generateAuthButton = wrapper.find('[data-testid="generate-oauth-url"]')
    expect(generateAuthButton.exists()).toBe(true)
    await generateAuthButton.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="stepper-proxy"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('locked')
    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('aria-current')).toBeUndefined()
    expect(wrapper.text()).toContain('代理与出口设置')
    expect(wrapper.text()).not.toContain('授权与创建不可调度账号')
  })

  it('falls back from gates when the account gate is no longer satisfied', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      account_id: 42,
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
    })
    await wrapper.find('[data-testid="stepper-gates"]').trigger('click')
    expect(wrapper.find('[data-testid="stepper-gates"]').attributes('data-step-status')).toBe('active')

    onboardingApi.refreshOnly.mockResolvedValueOnce(sessionFixture({
      browser_egress_verified: false,
      browser_egress_check_status: 'mismatch',
    }))
    await wrapper.find('[data-testid="advanced-manual-toggle"]').trigger('click')
    await wrapper.find('[data-testid="refresh-only"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="stepper-proxy"]').attributes('data-step-status')).toBe('active')
    expect(wrapper.find('[data-testid="stepper-gates"]').attributes('data-step-status')).toBe('locked')
    expect(wrapper.find('[data-testid="stepper-gates"]').attributes('aria-current')).toBeUndefined()
    expect(wrapper.text()).toContain('代理与出口设置')
    expect(wrapper.find('[data-testid="recommended-gate-action"]').exists()).toBe(false)
  })

  it('once an account is created and healthcheck passes, every reachable step shows done/active visuals', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper, {
      status: 'healthcheck_passed',
      account_id: 42,
      browser_egress_verified: true,
      browser_egress_check_status: 'verified',
      healthcheck_passed: true,
    })
    // Move focus to evidence so the other three settle into "done".
    await wrapper.find('[data-testid="stepper-evidence"]').trigger('click')

    expect(wrapper.find('[data-testid="stepper-proxy"]').attributes('data-step-status')).toBe('done')
    expect(wrapper.find('[data-testid="stepper-auth"]').attributes('data-step-status')).toBe('done')
    expect(wrapper.find('[data-testid="stepper-gates"]').attributes('data-step-status')).toBe('done')
    expect(wrapper.find('[data-testid="stepper-evidence"]').attributes('data-step-status')).toBe('active')

    // Done steps render a checkmark icon; locked icons no longer appear.
    expect(wrapper.find('[data-testid="stepper-icon-proxy"] [data-testid="stepper-icon-done"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="stepper-icon-auth"] [data-testid="stepper-icon-done"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="stepper-icon-gates"] [data-testid="stepper-icon-done"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="stepper-icon-locked"]')).toHaveLength(0)
  })

  it('stops the active browser egress poller when changing steps', async () => {
    const wrapper = mountWizard()
    await startSession(wrapper)

    onboardingApi.testProxy.mockResolvedValueOnce(sessionFixture({
      status: 'proxy_tested',
      browser_egress_check_status: 'waiting',
      browser_egress_check_url: 'https://safe.example/api/v1/claude-onboarding/browser-egress-check/nonce-bucket',
    }))
    await wrapper.find('[data-testid="test-proxy"]').trigger('click')
    await flushPromises()

    expect(egressPollingMock.start).toHaveBeenCalledWith('session-1')
    const stopCallsBeforeStepChange = egressPollingMock.stop.mock.calls.length

    await wrapper.find('[data-testid="stepper-evidence"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="stepper-evidence"]').attributes('data-step-status')).toBe('active')
    expect(egressPollingMock.stop.mock.calls.length).toBeGreaterThan(stopCallsBeforeStepChange)
  })
})
