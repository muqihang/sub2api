import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { useCodexEntryStore } from '@/stores/codexEntry'
import CodexEntryView from '../CodexEntryView.vue'

const mockGetCodexSummary = vi.fn()
const mockCreateCodexSetupSession = vi.fn()

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
  createCodexSetupSession: (...args: any[]) => mockCreateCodexSetupSession(...args),
  regenerateCodexSetupSession: vi.fn().mockResolvedValue({}),
  diagnoseCodex: vi.fn().mockResolvedValue({ ok: true, target_kind: 'setup_session', checks: [] }),
  resyncCodexDevice: vi.fn().mockResolvedValue({ device_id: 1, accepted: true }),
  repairCodexDevice: vi.fn().mockResolvedValue({ device_id: 1, accepted: true }),
  reattachCodexDevice: vi.fn().mockResolvedValue({ device_id: 1, accepted: true }),
  revokeCodexAttachment: vi.fn().mockResolvedValue({ device_id: 1, accepted: true }),
  removeCodexDevice: vi.fn().mockResolvedValue({ device_id: 1, accepted: true }),
}))

describe('CodexEntryView + CodexWizard', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.useRealTimers()
  })

  it('renders inside the main app layout instead of as a standalone page', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'onboarding_credential',
      wizard_step: 1,
      attachment_mode: null,
      setup_session_presentation: null,
      setup_session: null,
      focus_device_id: null,
      devices: [],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    expect(wrapper.find('[data-testid="app-layout"]').exists()).toBe(true)
  })

  it('renders wizard when page_state is onboarding_credential', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'onboarding_credential',
      wizard_step: 1,
      attachment_mode: null,
      setup_session_presentation: null,
      setup_session: null,
      focus_device_id: null,
      devices: [],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    expect(wrapper.find('[data-testid="codex-wizard"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="wizard-step-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="codex-console"]').exists()).toBe(false)
  })

  it('renders step 2 when page_state is onboarding_attach', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'onboarding_attach',
      wizard_step: 2,
      attachment_mode: 'reused_key',
      setup_session_presentation: 'wizard',
      setup_session: {
        id: 'sess-1',
        credential_label: 'Key',
        attachment_mode: 'reused_key',
        reuse_api_key_id: 42,
        launch_url: 'zhumeng-agent://setup?code=abc',
        cli_command: 'codex auth --code abc --server https://example.com',
        expires_at: '2026-01-01T00:00:00Z',
        first_seen_at: null,
        first_catalog_synced_at: null,
      },
      focus_device_id: null,
      devices: [],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    expect(wrapper.find('[data-testid="wizard-step-2"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="open-local-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="copy-cli-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="diagnose-session-btn"]').exists()).toBe(true)
  })

  it('renders step 3 when page_state is onboarding_verify', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'onboarding_verify',
      wizard_step: 3,
      attachment_mode: 'independent_credential',
      setup_session_presentation: 'wizard',
      setup_session: {
        id: 'sess-1',
        credential_label: 'Codex',
        attachment_mode: 'independent_credential',
        reuse_api_key_id: null,
        launch_url: null,
        cli_command: null,
        expires_at: '2026-01-01T00:00:00Z',
        first_seen_at: '2026-01-01T00:01:00Z',
        first_catalog_synced_at: null,
      },
      focus_device_id: null,
      devices: [],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    expect(wrapper.find('[data-testid="wizard-step-3"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="refresh-verify-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="diagnose-verify-btn"]').exists()).toBe(true)
  })

  it('renders console when page_state is console', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'console',
      wizard_step: null,
      attachment_mode: null,
      setup_session_presentation: null,
      setup_session: null,
      focus_device_id: 1,
      devices: [
        { device_id: 1, device_name: 'MacBook', device_state: 'healthy', attachment_mode: 'reused_key', last_seen_at: '2026-01-01T00:00:00Z', client_version: '1.0.0', min_supported_client_version: '0.1.0', catalog_synced_at: null, catalog_last_error_kind: 'none', revoked_at: null },
      ],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    expect(wrapper.find('[data-testid="codex-console"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="codex-wizard"]').exists()).toBe(false)
  })

  it('does NOT switch to wizard when user has devices and creates new session', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'console',
      wizard_step: null,
      attachment_mode: null,
      setup_session_presentation: 'console_banner',
      setup_session: { id: 'new-sess', credential_label: 'New', attachment_mode: 'reused_key', reuse_api_key_id: 42, launch_url: null, cli_command: null, expires_at: '2026-01-01T00:00:00Z', first_seen_at: null, first_catalog_synced_at: null },
      focus_device_id: 1,
      devices: [
        { device_id: 1, device_name: 'MacBook', device_state: 'healthy', attachment_mode: 'reused_key', last_seen_at: '2026-01-01T00:00:00Z', client_version: '1.0.0', min_supported_client_version: '0.1.0', catalog_synced_at: null, catalog_last_error_kind: 'none', revoked_at: null },
      ],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    // Page stays as console, not wizard.
    expect(wrapper.find('[data-testid="codex-console"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="codex-wizard"]').exists()).toBe(false)
    // Banner is shown.
    expect(wrapper.find('[data-testid="console-setup-banner"]').exists()).toBe(true)
  })


  it('renders page shell, hero copy, and side guidance cards for step 1', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'onboarding_credential',
      wizard_step: 1,
      attachment_mode: null,
      setup_session_presentation: null,
      setup_session: null,
      focus_device_id: null,
      devices: [],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    expect(wrapper.find('[data-testid="codex-page-hero"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="codex-page-shell"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="wizard-side-card-what"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="wizard-side-card-nochange"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="wizard-side-card-env"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('codex.hero.title')
    expect(wrapper.text()).toContain('codex.hero.setupDescription')
  })

  it('shows step 1 option descriptions and footer hint instead of bare radio labels', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'onboarding_credential',
      wizard_step: 1,
      attachment_mode: null,
      setup_session_presentation: null,
      setup_session: null,
      focus_device_id: null,
      devices: [],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    expect(wrapper.find('[data-testid="mode-independent-description"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="mode-reused-description"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="wizard-footer-hint"]').exists()).toBe(true)
  })

  it('shows attach step as a product card with launch panel and help panel', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'onboarding_attach',
      wizard_step: 2,
      attachment_mode: 'reused_key',
      setup_session_presentation: 'wizard',
      setup_session: {
        id: 'sess-1',
        credential_label: 'Key',
        attachment_mode: 'reused_key',
        reuse_api_key_id: 42,
        launch_url: 'zhumeng-agent://setup?code=abc',
        cli_command: 'codex auth --code abc --server https://example.com',
        expires_at: '2026-01-01T00:00:00Z',
        first_seen_at: null,
        first_catalog_synced_at: null,
      },
      focus_device_id: null,
      devices: [],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    expect(wrapper.find('[data-testid="attach-launch-panel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="attach-help-panel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="attach-cli-block"]').exists()).toBe(true)
  })

  it('shows verify step as a real waiting panel around first catalog sync', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'onboarding_verify',
      wizard_step: 3,
      attachment_mode: 'independent_credential',
      setup_session_presentation: 'wizard',
      setup_session: {
        id: 'sess-1',
        credential_label: 'Codex',
        attachment_mode: 'independent_credential',
        reuse_api_key_id: null,
        launch_url: null,
        cli_command: null,
        expires_at: '2026-01-01T00:00:00Z',
        first_seen_at: '2026-01-01T00:01:00Z',
        first_catalog_synced_at: null,
      },
      focus_device_id: null,
      devices: [],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    expect(wrapper.find('[data-testid="verify-sync-panel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="verify-exit-condition"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('codex.wizard.step3.exitDescription')
  })

  it('step 1 has both attachment mode options', async () => {
    mockGetCodexSummary.mockResolvedValue({
      page_state: 'onboarding_credential',
      wizard_step: 1,
      attachment_mode: null,
      setup_session_presentation: null,
      setup_session: null,
      focus_device_id: null,
      devices: [],
    })

    const store = useCodexEntryStore()
    await store.loadSummary()

    const wrapper = mount(CodexEntryView)
    expect(wrapper.find('[data-testid="mode-independent"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="mode-reused"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="start-setup-btn"]').exists()).toBe(true)
  })

  it('polls while waiting for local confirmation and advances when the device appears', async () => {
    vi.useFakeTimers()
    mockGetCodexSummary
      .mockResolvedValueOnce({
        page_state: 'onboarding_attach',
        wizard_step: 2,
        attachment_mode: 'independent_credential',
        setup_session_presentation: 'wizard',
        setup_session: {
          id: 'sess-1',
          credential_label: 'Codex',
          attachment_mode: 'independent_credential',
          reuse_api_key_id: null,
          launch_url: 'zhumeng-agent://setup?code=abc',
          cli_command: 'codex auth --code abc --server https://example.com',
          expires_at: '2026-01-01T00:00:00Z',
          first_seen_at: null,
          first_catalog_synced_at: null,
        },
        focus_device_id: null,
        devices: [],
      })
      .mockResolvedValueOnce({
        page_state: 'console',
        wizard_step: null,
        attachment_mode: null,
        setup_session_presentation: null,
        setup_session: null,
        focus_device_id: 1,
        devices: [
          { device_id: 1, device_name: 'MacBook', device_state: 'healthy', attachment_mode: 'reused_key', last_seen_at: '2026-01-01T00:00:00Z', client_version: '1.0.0', min_supported_client_version: '0.1.0', catalog_synced_at: null, catalog_last_error_kind: 'none', revoked_at: null },
        ],
      })

    const wrapper = mount(CodexEntryView)
    await flushPromises()

    expect(wrapper.find('[data-testid="wizard-step-2"]').exists()).toBe(true)

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    expect(mockGetCodexSummary).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="codex-console"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="codex-wizard"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
