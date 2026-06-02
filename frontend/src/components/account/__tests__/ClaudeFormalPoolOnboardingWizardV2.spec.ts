import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ClaudeFormalPoolOnboardingWizardV2 from '../ClaudeFormalPoolOnboardingWizardV2.vue'
import type { FormalPoolAcceptanceResult, FormalPoolSession } from '@/api/admin/claudeOnboarding'

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

const egressPollingMock = vi.hoisted(() => {
  const { ref, readonly } = require('vue') as typeof import('vue')
  return {
    session: readonly(ref(null)),
    status: readonly(ref('idle')),
    running: readonly(ref(false)),
    error: readonly(ref('')),
    start: vi.fn(),
    stop: vi.fn(),
    abort: vi.fn(),
  }
})

vi.mock('@/api/admin/claudeOnboarding', () => ({
  default: onboardingApi,
  ...onboardingApi,
}))

vi.mock('@/composables/useEgressCheckPolling', () => ({
  useEgressCheckPolling: () => egressPollingMock,
}))

function sessionFixture(overrides: Partial<FormalPoolSession> = {}): FormalPoolSession {
  return {
    id: 'session-1',
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
    status: 'healthcheck_passed',
    account_id: 42,
    account_ref: 'acct_bucket_42',
    proxy_ref: 'proxy_bucket_1',
    egress_bucket: 'egress_bucket_1',
    checks: [],
    no_real_messages_request_performed: false,
    activation_required: false,
    cc_gateway_seen: true,
    raw_capture_present: true,
    ...overrides,
  }
}

async function startSession(wrapper: ReturnType<typeof mount>, overrides: Partial<FormalPoolSession> = {}) {
  onboardingApi.createSession.mockResolvedValueOnce(sessionFixture(overrides))
  await wrapper.find('[data-testid="account-name-input"]').setValue('claude-safe-name')
  await wrapper.find('[data-testid="group-id-input"]').setValue(9)
  await wrapper.find('[data-testid="proxy-id-input"]').setValue(7)
  await wrapper.find('[data-testid="start-session"]').trigger('click')
  await flushPromises()
}

describe('ClaudeFormalPoolOnboardingWizardV2', () => {
  beforeEach(() => {
    Object.values(onboardingApi).forEach((mock) => mock.mockReset())
    egressPollingMock.start.mockReset()
    egressPollingMock.stop.mockReset()
    egressPollingMock.abort.mockReset()
    onboardingApi.getSession.mockResolvedValue(sessionFixture())
  })

  it('shows idle and no browser egress check URL immediately after StartSession', async () => {
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)

    await startSession(wrapper)

    expect(wrapper.find('[data-testid="browser-egress-status"]').text()).toContain('idle')
    expect(wrapper.find('[data-testid="browser-egress-check-url"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('/browser-egress-check/')
  })

  it('does not render the raw browser egress nonce while copy still uses the real URL', async () => {
    const rawNonce = 'raw-nonce-DO-NOT-LEAK-12345'
    const realUrl = `https://safe.example/api/v1/claude-onboarding/browser-egress-check/${rawNonce}`
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)
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



  it('uses safe session ref in the badge instead of raw session id', async () => {
    const rawSessionId = '987654321'
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)

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
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)
    await startSession(wrapper, {
      browser_egress_check_url: 'https://safe.example/check',
      browser_egress_check_status: 'waiting',
    })

    expect(wrapper.text().toLowerCase()).not.toContain('open current browser')
    expect(wrapper.text()).not.toContain('打开当前浏览器')
    expect(wrapper.find('input[placeholder*="attestation"]').exists()).toBe(false)
    expect(wrapper.find('input[placeholder*="校验码"]').exists()).toBe(false)
  })

  it('labels expired nonce recovery as starting a new onboarding session and triggers createSession', async () => {
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)
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
    expect(wrapper.get('[data-testid="session-ref"]').text()).toContain('session ref unavailable')
  })

  it('shows mismatch buckets or generic copy and never raw IP addresses', async () => {
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)
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
    expect(mismatch.text()).toContain('browser_bucket_A')
    expect(mismatch.text()).toContain('proxy_bucket_B')
    expect(wrapper.text()).not.toContain('203.0.113.10')
    expect(wrapper.text()).not.toContain('198.51.100.7')
  })

  it('uses a non-token-shaped Setup Token placeholder', async () => {
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)
    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')
    await wrapper.find('input[value="setup-token-cookie"]').setValue()

    const input = wrapper.find('[data-testid="setup-token-input"]')
    expect(input.exists()).toBe(true)
    expect(input.attributes('placeholder')).toBe('粘贴 Setup Token')
    expect(input.attributes('placeholder')).not.toMatch(/sk-ant-sid/i)
  })

  it('gates runtime healthcheck, warming, and production with clear real directed healthcheck copy', async () => {
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)
    await startSession(wrapper, {
      status: 'runtime_registered',
      account_id: 42,
      cc_gateway_runtime_registered: true,
      healthcheck_passed: false,
    })

    await wrapper.find('[data-testid="stepper-gates"]').trigger('click')

    const healthcheckButton = wrapper.find('[data-testid="healthcheck"]')
    expect(healthcheckButton.text()).toContain('一次真实 directed healthcheck/上游请求')
    expect(healthcheckButton.attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-testid="start-warming"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="promote-production"]').attributes('disabled')).toBeDefined()

    onboardingApi.healthcheck.mockResolvedValueOnce(acceptanceFixture())
    await healthcheckButton.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-healthcheck_passed"]').classes()).toContain('is-active')
    expect(wrapper.find('[data-testid="start-warming"]').attributes('disabled')).toBeUndefined()

    onboardingApi.startWarming.mockResolvedValueOnce(sessionFixture({ status: 'warming', account_id: 42, healthcheck_passed: true }))
    await wrapper.find('[data-testid="start-warming"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('新号 low weight')
    expect(wrapper.find('[data-testid="promote-production"]').attributes('disabled')).toBeUndefined()
  })

  it('scrubs sensitive backend and account-source display text before it reaches the DOM', async () => {
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)
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
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)

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

  it('stops the active browser egress poller when changing steps', async () => {
    const wrapper = mount(ClaudeFormalPoolOnboardingWizardV2)
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

    await wrapper.find('[data-testid="stepper-auth"]').trigger('click')
    await flushPromises()

    expect(egressPollingMock.stop.mock.calls.length).toBeGreaterThan(stopCallsBeforeStepChange)
  })
})
