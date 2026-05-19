import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useCodexEntryStore } from '@/stores/codexEntry'

const mockGetCodexSummary = vi.fn()
const mockCreateCodexSetupSession = vi.fn()
const mockDiagnoseCodex = vi.fn()



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
  regenerateCodexSetupSession: vi.fn(),
  diagnoseCodex: (...args: any[]) => mockDiagnoseCodex(...args),
  resyncCodexDevice: vi.fn(),
  repairCodexDevice: vi.fn(),
  reattachCodexDevice: vi.fn(),
  revokeCodexAttachment: vi.fn(),
  removeCodexDevice: vi.fn(),
}))

describe('useCodexEntryStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('three-layer state model', () => {
    it('defaults to onboarding_credential when no summary loaded', () => {
      const store = useCodexEntryStore()
      expect(store.pageState).toBe('onboarding_credential')
      expect(store.wizardStep).toBeNull()
      expect(store.devices).toEqual([])
    })

    it('reflects page_state from summary', async () => {
      mockGetCodexSummary.mockResolvedValue({
        page_state: 'onboarding_attach',
        wizard_step: 2,
        attachment_mode: 'reused_key',
        setup_session_presentation: 'wizard',
        setup_session: {
          id: 'sess-1',
          credential_label: 'My Key',
          attachment_mode: 'reused_key',
          reuse_api_key_id: 42,
          launch_url: 'zhumeng-agent://setup?code=abc',
          cli_command: 'codex auth --code abc',
          expires_at: '2026-01-01T00:00:00Z',
          first_seen_at: null,
          first_catalog_synced_at: null,
        },
        focus_device_id: null,
        devices: [],
      })

      const store = useCodexEntryStore()
      await store.loadSummary()

      expect(store.pageState).toBe('onboarding_attach')
      expect(store.wizardStep).toBe(2)
      expect(store.setupSession).not.toBeNull()
      expect(store.setupSession!.id).toBe('sess-1')
      expect(store.setupSessionPresentation).toBe('wizard')
    })

    it('separates device_state per device in console mode', async () => {
      mockGetCodexSummary.mockResolvedValue({
        page_state: 'console',
        wizard_step: null,
        attachment_mode: null,
        setup_session_presentation: null,
        setup_session: null,
        focus_device_id: 2,
        devices: [
          { device_id: 1, device_name: 'MacBook', device_state: 'healthy', attachment_mode: 'reused_key', last_seen_at: '2026-01-01T00:00:00Z', client_version: '1.0.0', min_supported_client_version: '0.1.0', catalog_synced_at: null, catalog_last_error_kind: 'none', revoked_at: null },
          { device_id: 2, device_name: 'Linux Box', device_state: 'device_offline', attachment_mode: 'independent_credential', last_seen_at: '2025-12-01T00:00:00Z', client_version: '0.9.0', min_supported_client_version: '0.1.0', catalog_synced_at: null, catalog_last_error_kind: 'none', revoked_at: null },
        ],
      })

      const store = useCodexEntryStore()
      await store.loadSummary()

      expect(store.pageState).toBe('console')
      expect(store.wizardStep).toBeNull()
      expect(store.devices).toHaveLength(2)
      expect(store.devices[0].device_state).toBe('healthy')
      expect(store.devices[1].device_state).toBe('device_offline')
      expect(store.focusDeviceId).toBe(2)
      expect(store.focusDevice?.device_name).toBe('Linux Box')
    })
  })

  describe('setupSessionPresentation', () => {
    it('is console_banner when user has devices and a pending session', async () => {
      mockGetCodexSummary.mockResolvedValue({
        page_state: 'console',
        wizard_step: null,
        attachment_mode: null,
        setup_session_presentation: 'console_banner',
        setup_session: { id: 'sess-2', credential_label: 'New', attachment_mode: 'reused_key', reuse_api_key_id: 42, launch_url: null, cli_command: null, expires_at: '2026-01-01T00:00:00Z', first_seen_at: null, first_catalog_synced_at: null },
        focus_device_id: 1,
        devices: [
          { device_id: 1, device_name: 'MacBook', device_state: 'healthy', attachment_mode: 'reused_key', last_seen_at: '2026-01-01T00:00:00Z', client_version: '1.0.0', min_supported_client_version: '0.1.0', catalog_synced_at: null, catalog_last_error_kind: 'none', revoked_at: null },
        ],
      })

      const store = useCodexEntryStore()
      await store.loadSummary()

      expect(store.pageState).toBe('console')
      expect(store.setupSessionPresentation).toBe('console_banner')
      expect(store.setupSession).not.toBeNull()
    })
  })

  describe('actions', () => {
    it('setAttachmentMode updates local state', () => {
      const store = useCodexEntryStore()
      expect(store.selectedAttachmentMode).toBe('independent_credential')
      store.setAttachmentMode('reused_key')
      expect(store.selectedAttachmentMode).toBe('reused_key')
    })

    it('startSetup calls createCodexSetupSession then reloads summary', async () => {
      mockCreateCodexSetupSession.mockResolvedValue({
        setup_session: { id: 'new-sess' },
        page_state: 'onboarding_attach',
        setup_session_presentation: 'wizard',
      })
      mockGetCodexSummary.mockResolvedValue({
        page_state: 'onboarding_attach',
        wizard_step: 2,
        attachment_mode: 'independent_credential',
        setup_session_presentation: 'wizard',
        setup_session: { id: 'new-sess', credential_label: '我的 MacBook', attachment_mode: 'independent_credential', reuse_api_key_id: null, launch_url: 'zhumeng-agent://setup?code=x', cli_command: 'codex auth --code x', expires_at: '2026-01-01T00:00:00Z', first_seen_at: null, first_catalog_synced_at: null },
        focus_device_id: null,
        devices: [],
      })

      const store = useCodexEntryStore()
      await store.startSetup()

      expect(mockCreateCodexSetupSession).toHaveBeenCalledWith({
        attachment_mode: 'independent_credential',
        credential_label: '我的 MacBook',
        reuse_api_key_id: undefined,
      })
      expect(mockGetCodexSummary).toHaveBeenCalled()
      expect(store.pageState).toBe('onboarding_attach')
    })

    it('diagnoseSetupSession calls diagnoseCodex with setup_session_id', async () => {
      mockGetCodexSummary.mockResolvedValue({
        page_state: 'onboarding_attach',
        wizard_step: 2,
        attachment_mode: 'reused_key',
        setup_session_presentation: 'wizard',
        setup_session: { id: 'sess-1', credential_label: 'Key', attachment_mode: 'reused_key', reuse_api_key_id: 42, launch_url: null, cli_command: null, expires_at: '2026-01-01T00:00:00Z', first_seen_at: null, first_catalog_synced_at: null },
        focus_device_id: null,
        devices: [],
      })
      mockDiagnoseCodex.mockResolvedValue({
        ok: false,
        target_kind: 'setup_session',
        checks: [{ name: 'credential', status: 'ok', hint: 'Valid' }],
      })

      const store = useCodexEntryStore()
      await store.loadSummary()
      await store.diagnoseSetupSession()

      expect(mockDiagnoseCodex).toHaveBeenCalledWith({ setup_session_id: 'sess-1' })
      expect(store.lastDiagnose?.target_kind).toBe('setup_session')
    })
  })

    it('startSetup rejects reused_key mode without a selected key', async () => {
      const store = useCodexEntryStore()
      store.setAttachmentMode('reused_key')
      // Do NOT select a key.
      await store.startSetup()

      expect(mockCreateCodexSetupSession).not.toHaveBeenCalled()
      expect(store.error).toBeTruthy()
    })

  })