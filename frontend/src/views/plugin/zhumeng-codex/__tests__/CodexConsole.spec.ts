import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { useCodexEntryStore } from '@/stores/codexEntry'
import CodexEntryView from '../CodexEntryView.vue'

const mockGetCodexSummary = vi.fn()

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: {
    template: '<div data-testid="app-layout"><slot /></div>',
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))




vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    docUrl: '',
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { balance: 0 },
  }),
}))

vi.mock('@/api/keys', () => ({
  keysAPI: {
    list: vi.fn().mockResolvedValue({ items: [] }),
  },
}))

vi.mock('@/api/channels', () => ({
  userChannelsAPI: {
    getAvailable: vi.fn().mockResolvedValue([]),
  },
}))

vi.mock('@/api/zhumengAgent', () => ({
  getCodexSummary: (...args: any[]) => mockGetCodexSummary(...args),
  createCodexSetupSession: vi.fn().mockResolvedValue({}),
  regenerateCodexSetupSession: vi.fn().mockResolvedValue({}),
  diagnoseCodex: vi.fn().mockResolvedValue({ ok: true, target_kind: 'device', checks: [] }),
  resyncCodexDevice: vi.fn().mockResolvedValue({ device_id: 1, accepted: true }),
  repairCodexDevice: vi.fn().mockResolvedValue({ device_id: 1, accepted: true }),
  reattachCodexDevice: vi.fn().mockResolvedValue({ device_id: 1, accepted: true }),
  revokeCodexAttachment: vi.fn().mockResolvedValue({ device_id: 1, accepted: true }),
  removeCodexDevice: vi.fn().mockResolvedValue({ device_id: 1, accepted: true }),
}))

function makeDevice(overrides: Record<string, any> = {}) {
  return {
    device_id: 1,
    device_name: 'MacBook',
    attachment_mode: 'reused_key',
    device_state: 'healthy',
    last_seen_at: '2026-01-01T00:00:00Z',
    client_version: '1.0.0',
    min_supported_client_version: '0.1.0',
    catalog_synced_at: null,
    catalog_last_error_kind: 'none',
    revoked_at: null,
    ...overrides,
  }
}

function makeConsoleSummary(overrides: Record<string, any> = {}) {
  return {
    page_state: 'console',
    wizard_step: null,
    attachment_mode: null,
    setup_session_presentation: null,
    setup_session: null,
    focus_device_id: 1,
    devices: [makeDevice()],
    ...overrides,
  }
}

describe('CodexConsole', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('renders hero with add-device button', async () => {
    mockGetCodexSummary.mockResolvedValue(makeConsoleSummary())
    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    await flushPromises()
    expect(wrapper.find('[data-testid="console-hero"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="hero-add-device-btn"]').exists()).toBe(true)
  })

  it('shows status bar for focused device', async () => {
    mockGetCodexSummary.mockResolvedValue(makeConsoleSummary())
    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    await flushPromises()
    // StatusBar renders inside CodexConsole; check for its action buttons
    expect(wrapper.find('[data-testid="status-bar-resync-btn"]').exists()).toBe(true)
  })

  it('repair and diagnose are always separate buttons in troubleshoot card', async () => {
    mockGetCodexSummary.mockResolvedValue(makeConsoleSummary({
      devices: [makeDevice({ device_state: 'device_offline' })],
    }))
    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    await flushPromises()
    expect(wrapper.find('[data-testid="troubleshoot-repair-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="troubleshoot-diagnose-btn"]').exists()).toBe(true)
  })

  it('reused_key device shows disconnect, NOT revoke-credential', async () => {
    mockGetCodexSummary.mockResolvedValue(makeConsoleSummary({
      devices: [makeDevice({ attachment_mode: 'reused_key' })],
    }))
    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    await flushPromises()
    expect(wrapper.find('[data-testid="device-disconnect-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="device-revoke-btn"]').exists()).toBe(false)
  })

  it('independent_credential device shows revoke-credential', async () => {
    mockGetCodexSummary.mockResolvedValue(makeConsoleSummary({
      devices: [makeDevice({ attachment_mode: 'independent_credential' })],
    }))
    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    await flushPromises()
    expect(wrapper.find('[data-testid="device-revoke-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="device-disconnect-btn"]').exists()).toBe(false)
  })

  it('credential_revoked shows reattach as primary, not repair', async () => {
    mockGetCodexSummary.mockResolvedValue(makeConsoleSummary({
      devices: [makeDevice({ device_state: 'credential_revoked' })],
    }))
    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    await flushPromises()
    expect(wrapper.find('[data-testid="status-bar-reattach-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="status-bar-repair-btn"]').exists()).toBe(false)
  })

  it('device_offline shows repair as primary, not reattach', async () => {
    mockGetCodexSummary.mockResolvedValue(makeConsoleSummary({
      devices: [makeDevice({ device_state: 'device_offline' })],
    }))
    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    await flushPromises()
    expect(wrapper.find('[data-testid="status-bar-repair-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="status-bar-reattach-btn"]').exists()).toBe(false)
  })

  it('console_banner shows when page_state=console and setup_session_presentation=console_banner', async () => {
    mockGetCodexSummary.mockResolvedValue(makeConsoleSummary({
      setup_session_presentation: 'console_banner',
      setup_session: { id: 'new-sess', credential_label: 'New', attachment_mode: 'reused_key', reuse_api_key_id: 42, launch_url: 'zhumeng-agent://setup?code=x', cli_command: 'codex auth --code x', expires_at: '2026-01-01T00:00:00Z', first_seen_at: null, first_catalog_synced_at: null },
    }))
    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    await flushPromises()
    expect(wrapper.find('[data-testid="console-setup-banner"]').exists()).toBe(true)
  })

  it('shows Codex gateway models from summary even when channel models are empty', async () => {
    mockGetCodexSummary.mockResolvedValue(makeConsoleSummary({
      model_catalog: [
        { name: 'gpt-5.5', display_name: 'GPT-5.5', platform: 'openai' },
        { name: 'deepseek-v4-pro', display_name: 'DeepSeek V4 Pro', platform: 'deepseek' },
        { name: 'claude-opus-4-7', display_name: 'Claude Opus 4.7', platform: 'anthropic' },
      ],
    }))
    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    await flushPromises()

    expect(wrapper.text()).toContain('GPT-5.5')
    expect(wrapper.text()).toContain('DeepSeek V4 Pro')
    expect(wrapper.text()).toContain('Claude Opus 4.7')
    expect(wrapper.text()).not.toContain('codex.console.modelCatalogEmpty')
  })

  it('shows a scrollable model list and hover pricing details from summary pricing', async () => {
    mockGetCodexSummary.mockResolvedValue(makeConsoleSummary({
      model_catalog: [
        {
          name: 'gpt-5.5',
          display_name: 'GPT-5.5',
          platform: 'openai',
          pricing: {
            billing_mode: 'token',
            input_price: 0.0000025,
            output_price: 0.000015,
            cache_write_price: 0.0000025,
            cache_read_price: 0.00000025,
            image_output_price: null,
            per_request_price: null,
            intervals: [],
            source: 'fallback',
          },
        },
      ],
    }))
    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    await flushPromises()

    expect(wrapper.find('[data-testid="model-list"]').classes()).toContain('model-list--scrollable')

    const trigger = wrapper.find('[data-testid="model-pricing-trigger-gpt-5.5"]')
    expect(trigger.exists()).toBe(true)
    await trigger.trigger('mouseenter')
    await flushPromises()

    expect(document.body.textContent).toContain('usage.inputTokenPrice')
    expect(document.body.textContent).toContain('usage.outputTokenPrice')
    expect(document.body.textContent).toContain('codex.console.cacheWritePrice')
    expect(document.body.textContent).toContain('codex.console.cacheReadPrice')
    expect(document.body.textContent).toContain('$2.5')
    expect(document.body.textContent).toContain('$15')
    expect(document.body.textContent).toContain('$0.25')

    wrapper.unmount()
  })
})
