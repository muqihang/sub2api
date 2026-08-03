import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AugmentGatewayView from '../AugmentGatewayView.vue'

const mockGetSummary = vi.fn()
const mockGetProviderGroups = vi.fn()
const mockGetSourcePriority = vi.fn()
const mockUpdateSourcePriority = vi.fn()
const mockGetModels = vi.fn()
const mockUpdateModel = vi.fn()
const mockListPoolSessions = vi.fn()
const mockCreatePoolBindIntent = vi.fn()
const mockBindPoolSession = vi.fn()
const mockRevokePoolSessionAdmin = vi.fn()
const mockDisablePoolSessionAdmin = vi.fn()
const mockRequireReloginAdmin = vi.fn()
const mockGetDiagnostics = vi.fn()
const mockGetUsage = vi.fn()
const mockShowError = vi.fn()
const mockShowSuccess = vi.fn()
let mockRouteQuery: Record<string, unknown> = {}

vi.mock('vue-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-router')>()
  return {
    ...actual,
    useRoute: () => ({ query: mockRouteQuery }),
  }
})

vi.mock('@/api/admin/augmentGateway', () => ({
  getAugmentGatewaySummary: (...args: any[]) => mockGetSummary(...args),
  getAugmentProviderGroups: (...args: any[]) => mockGetProviderGroups(...args),
  getAugmentGatewaySourcePriority: (...args: any[]) => mockGetSourcePriority(...args),
  updateAugmentGatewaySourcePriority: (...args: any[]) => mockUpdateSourcePriority(...args),
  getAugmentGatewayModels: (...args: any[]) => mockGetModels(...args),
  updateAugmentGatewayModel: (...args: any[]) => mockUpdateModel(...args),
  listAugmentPoolSessions: (...args: any[]) => mockListPoolSessions(...args),
  createAugmentPoolSessionBindIntent: (...args: any[]) => mockCreatePoolBindIntent(...args),
  bindAugmentPoolSession: (...args: any[]) => mockBindPoolSession(...args),
  revokeAugmentPoolSessionAdmin: (...args: any[]) => mockRevokePoolSessionAdmin(...args),
  disableAugmentPoolSessionAdmin: (...args: any[]) => mockDisablePoolSessionAdmin(...args),
  requireAugmentPoolSessionReloginAdmin: (...args: any[]) => mockRequireReloginAdmin(...args),
  getAugmentPoolSessionDiagnosticsAdmin: (...args: any[]) => mockGetDiagnostics(...args),
  getAugmentGatewayAdminUsage: (...args: any[]) => mockGetUsage(...args),
  default: {
    getAugmentGatewaySummary: (...args: any[]) => mockGetSummary(...args),
    getAugmentProviderGroups: (...args: any[]) => mockGetProviderGroups(...args),
    getAugmentGatewaySourcePriority: (...args: any[]) => mockGetSourcePriority(...args),
    updateAugmentGatewaySourcePriority: (...args: any[]) => mockUpdateSourcePriority(...args),
    getAugmentGatewayModels: (...args: any[]) => mockGetModels(...args),
    updateAugmentGatewayModel: (...args: any[]) => mockUpdateModel(...args),
    listAugmentPoolSessions: (...args: any[]) => mockListPoolSessions(...args),
    createAugmentPoolSessionBindIntent: (...args: any[]) => mockCreatePoolBindIntent(...args),
    bindAugmentPoolSession: (...args: any[]) => mockBindPoolSession(...args),
    revokeAugmentPoolSessionAdmin: (...args: any[]) => mockRevokePoolSessionAdmin(...args),
    disableAugmentPoolSessionAdmin: (...args: any[]) => mockDisablePoolSessionAdmin(...args),
    requireAugmentPoolSessionReloginAdmin: (...args: any[]) => mockRequireReloginAdmin(...args),
    getAugmentPoolSessionDiagnosticsAdmin: (...args: any[]) => mockGetDiagnostics(...args),
    getAugmentGatewayAdminUsage: (...args: any[]) => mockGetUsage(...args),
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: (...args: any[]) => mockShowError(...args),
    showSuccess: (...args: any[]) => mockShowSuccess(...args),
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

describe('AugmentGateway admin', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockRouteQuery = {}
    mockGetSummary.mockResolvedValue({
      entitlement_groups: {
        total_count: 1,
        rows: [
          {
            id: 201,
            name: 'Augment Users',
            status: 'active',
            total_accounts: 3,
            active_accounts: 2,
          },
        ],
      },
      provider_routing_groups: {
        total_count: 1,
        configured_route_policy_version: '2026-05-08',
        route_policy_version: '2026-05-08',
        source_priority: ['official_quick_login', 'wukong_quick_login'],
        rows: [
          { provider: 'openai', group_id: 1001, healthy: true, active_accounts: 2, total_accounts: 2, version: 1 },
        ],
      },
      official_session_pool: {
        total_count: 1,
        active_count: 1,
        healthy_count: 1,
        source_counts: {
          official_quick_login: 1,
        },
      },
      usage: {
        estimated_cost: 12.3,
        settled_cost: 10.1,
        free_quota: 1.2,
        paid_balance: 8.9,
        cache_hit_ratio: 0.42,
        currency: 'USD',
      },
      models: [
        {
          model: { id: 'gpt-5.4', provider: 'openai', upstream_model: 'gpt-5.4' },
          enabled: true,
          visible: true,
          explicit_pricing: true,
          smoke_status: 'passed',
          provider_healthy: true,
          settings_version: 4,
          settings_namespace: 'gateway.augment.enabled_models',
        },
        {
          model: { id: 'claude-sonnet-4-5', provider: 'anthropic', upstream_model: 'claude-sonnet-4-5' },
          enabled: false,
          visible: false,
          explicit_pricing: false,
          smoke_status: 'pending',
          provider_healthy: false,
          settings_version: 4,
          settings_namespace: 'gateway.augment.enabled_models',
        },
      ],
    })
    mockGetProviderGroups.mockResolvedValue({
      rows: [
        { provider: 'openai', group_id: 1001, healthy: true, active_accounts: 2, total_accounts: 2, version: 1 },
      ],
    })
    mockGetSourcePriority.mockResolvedValue({ sources: ['official_quick_login', 'wukong_quick_login'] })
    mockGetModels.mockResolvedValue({
      rows: [
        {
          model: { id: 'gpt-5.4', provider: 'openai', upstream_model: 'gpt-5.4' },
          enabled: true,
          visible: true,
          explicit_pricing: true,
          smoke_status: 'passed',
          provider_healthy: true,
          settings_version: 4,
          settings_namespace: 'gateway.augment.enabled_models',
        },
        {
          model: { id: 'claude-sonnet-4-5', provider: 'anthropic', upstream_model: 'claude-sonnet-4-5' },
          enabled: false,
          visible: false,
          explicit_pricing: false,
          smoke_status: 'pending',
          provider_healthy: false,
          settings_version: 4,
          settings_namespace: 'gateway.augment.enabled_models',
        },
      ],
    })
    mockListPoolSessions.mockResolvedValue({
      rows: [
        {
          id: 42,
          source: 'official_quick_login',
          tenant_origin: 'https://tenant.example.com',
          status: 'active',
          fingerprint_prefix: 'fp-1',
          has_credential_payload: true,
          health_score: 100,
          created_by_admin_id: 7,
          access_token: 'should-not-render',
        },
      ],
    })
    mockGetUsage.mockResolvedValue({
      rows: [
        {
          model: 'gpt-5.4',
          upstream_model: 'gpt-5.4-mini',
          request_scope: 'augment_gateway',
          feature_scope: 'context_engine',
          group_id: 201,
          route_policy_version: '2026-05-08',
          augment_session_id: 'augment-session-1',
          request_id: 'req-1',
          estimated_cost: 1.2,
          settled_cost: 1.1,
          cache_read_tokens: 10,
          cache_creation_tokens: 5,
        },
      ],
      page: { page: 1, page_size: 20, pages: 1, total: 1 },
    })
    mockGetDiagnostics.mockResolvedValue({
      id: 42,
      tenant_host: 'tenant.example.com',
      fingerprint_prefix: 'fp-1',
      access_token: 'should-not-render',
    })
  })

  function mountView() {
    return mount(AugmentGatewayView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          RouterLink: { props: ['to'], template: '<a :href="typeof to === \'string\' ? to : to.path"><slot /></a>' },
        },
      },
    })
  }

  it('renders entitlement groups, routing groups, and pool sessions without secrets', async () => {
    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('Augment Users')
    expect(text).toContain('openai')
    expect(text).toContain('1001')
    expect(text).toContain('2026-05-08')
    expect(text).toContain('official_quick_login')
    expect(text).toContain('wukong_quick_login')
    expect(text).toContain('tenant.example.com')
    expect(text).not.toContain('access_token')
    expect(text).not.toContain('should-not-render')
  })

  it('updates source priority order', async () => {
    mockUpdateSourcePriority.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="source-priority-down-official_quick_login"]').trigger('click')
    await wrapper.get('[data-test="save-source-priority"]').trigger('click')
    await flushPromises()

    expect(mockUpdateSourcePriority).toHaveBeenCalledWith({
      sources: ['wukong_quick_login', 'official_quick_login'],
    })
    expect(mockShowSuccess).toHaveBeenCalledWith('admin.augmentGateway.saved')
    expect(mockGetSummary).toHaveBeenCalledTimes(1)
    expect(mockGetUsage).toHaveBeenCalledTimes(1)
  })

  it('prevents model visible toggle when smoke status is not ok', async () => {
    const wrapper = mountView()
    await flushPromises()

    const blocked = wrapper.get('[data-test="model-toggle-claude-sonnet-4-5"]')
    expect(blocked.attributes('disabled')).toBeDefined()
  })

  it('refreshes only model data after a model toggle', async () => {
    mockUpdateModel.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="model-toggle-gpt-5.4"]').trigger('click')
    await flushPromises()

    expect(mockUpdateModel).toHaveBeenCalledWith('gpt-5.4', {
      enabled: false,
      smoke_status: 'passed',
      expected_version: 4,
    })
    expect(mockGetSummary).toHaveBeenCalledTimes(1)
    expect(mockGetUsage).toHaveBeenCalledTimes(1)
    expect(mockGetModels).toHaveBeenCalledTimes(2)
  })

  it('uses enabled state for model action semantics and shows visible as a separate derived state', async () => {
    mockGetSummary.mockResolvedValueOnce({
      entitlement_groups: { total_count: 0, rows: [] },
      provider_routing_groups: {
        total_count: 1,
        configured_route_policy_version: '2026-05-08',
        route_policy_version: '2026-05-08',
        source_priority: ['official_quick_login'],
        rows: [
          { provider: 'openai', group_id: 1001, healthy: true, active_accounts: 2, total_accounts: 2, version: 1 },
        ],
      },
      official_session_pool: {
        total_count: 1,
        active_count: 1,
        healthy_count: 1,
        source_counts: { official_quick_login: 1 },
      },
      usage: {
        estimated_cost: 0,
        settled_cost: 0,
        free_quota: 0,
        paid_balance: 0,
        cache_hit_ratio: 0,
        currency: 'USD',
      },
      models: [
        {
          model: { id: 'gpt-5.4', provider: 'openai', upstream_model: 'gpt-5.4' },
          enabled: true,
          visible: false,
          explicit_pricing: true,
          smoke_status: 'passed',
          provider_healthy: true,
          settings_version: 4,
          settings_namespace: 'gateway.augment.enabled_models',
        },
      ],
    })
    mockGetModels.mockResolvedValueOnce({
      rows: [
        {
          model: { id: 'gpt-5.4', provider: 'openai', upstream_model: 'gpt-5.4' },
          enabled: true,
          visible: false,
          smoke_status: 'passed',
          provider_healthy: true,
          settings_version: 4,
          settings_namespace: 'gateway.augment.enabled_models',
        },
      ],
    })

    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.get('[data-test="model-toggle-gpt-5.4"]')
    expect(button.text()).toBe('admin.augmentGateway.disableModel')
    expect(wrapper.text()).toContain('admin.augmentGateway.enabledState')
    expect(wrapper.text()).toContain('admin.augmentGateway.notVisibleState')
  })

  it('calls pool revoke endpoint and refreshes session list', async () => {
    mockRevokePoolSessionAdmin.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="revoke-session-42"]').trigger('click')
    await flushPromises()

    expect(mockRevokePoolSessionAdmin).toHaveBeenCalledWith(42)
    expect(mockListPoolSessions).toHaveBeenCalledTimes(2)
    expect(mockGetSummary).toHaveBeenCalledTimes(1)
    expect(mockGetUsage).toHaveBeenCalledTimes(1)
  })

  it('calls pool disable and require-relogin actions', async () => {
    mockDisablePoolSessionAdmin.mockResolvedValue({})
    mockRequireReloginAdmin.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="disable-session-42"]').trigger('click')
    await wrapper.get('[data-test="require-relogin-session-42"]').trigger('click')
    await flushPromises()

    expect(mockDisablePoolSessionAdmin).toHaveBeenCalledWith(42)
    expect(mockRequireReloginAdmin).toHaveBeenCalledWith(42)
    expect(mockGetSummary).toHaveBeenCalledTimes(1)
    expect(mockGetUsage).toHaveBeenCalledTimes(1)
  })

  it('captures callback payload into a pool session using admin pool bind APIs', async () => {
    mockRouteQuery = {
      official_tenant_url: 'https://capture.augment.local',
      official_access_token: 'capture-access-token',
      official_refresh_token: 'capture-refresh-token',
      official_expires_at: '2026-05-08T16:00:00Z',
      official_scopes: 'augment:session',
    }
    mockCreatePoolBindIntent.mockResolvedValue({
      bind_intent_id: 'pool-bind-intent-2',
      state: 'pool-bind-state-2',
      expires_at: '2026-05-08T15:30:00Z',
      bind_token: 'pool-bind-token-2',
    })
    mockBindPoolSession.mockResolvedValue({
      id: 77,
      source: 'official_quick_login',
      tenant_origin: 'https://capture.augment.local',
      status: 'active',
    })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="capture-pool-session"]').trigger('click')
    await flushPromises()

    expect(mockCreatePoolBindIntent).toHaveBeenCalledWith({
      mode: 'official_passthrough',
      source: 'official_quick_login',
      tenant_allowlist: ['https://capture.augment.local'],
    })
    expect(mockBindPoolSession).toHaveBeenCalledWith({
      bind_token: 'pool-bind-token-2',
      bind_intent_id: 'pool-bind-intent-2',
      state: 'pool-bind-state-2',
      mode: 'official_passthrough',
      source: 'official_quick_login',
      payload: {
        tenant_url: 'https://capture.augment.local',
        access_token: 'capture-access-token',
        refresh_token: 'capture-refresh-token',
        expires_at: '2026-05-08T16:00:00Z',
        scopes: ['augment:session'],
      },
    })
    expect(mockListPoolSessions).toHaveBeenCalledTimes(2)
    expect(mockGetSummary).toHaveBeenCalledTimes(1)
    expect(mockGetUsage).toHaveBeenCalledTimes(1)
  })

  it('renders cache hit ratio and cost summary', async () => {
    mockGetSummary.mockResolvedValue({
      entitlement_groups: { total_count: 0, rows: [] },
      provider_routing_groups: {
        total_count: 0,
        configured_route_policy_version: '2026-05-08',
        route_policy_version: '2026-05-08',
        source_priority: ['official_quick_login', 'wukong_quick_login'],
        rows: [],
      },
      official_session_pool: {
        total_count: 1,
        active_count: 1,
        healthy_count: 1,
        source_counts: { official_quick_login: 1 },
      },
      usage: {
        cache_hit_ratio: 0.42,
        estimated_cost: 12.3,
        settled_cost: 10.1,
        free_quota: 1.2,
        paid_balance: 8.9,
        currency: 'USD',
      },
      models: [],
    })

    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('42.0%')
    expect(text).toContain('12.30')
    expect(text).toContain('10.10')
    expect(text).toContain('1.20')
    expect(text).toContain('8.90')
  })

  it('renders usage routing metadata and admin surface links', async () => {
    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('context_engine')
    expect(text).toContain('augment_gateway')
    expect(text).toContain('augment-session-1')
    expect(text).toContain('admin.augmentGateway.configuredRoutePolicyVersion')
    expect(text).toContain('admin.augmentGateway.executedRoutePolicyVersion')
    expect(wrapper.html()).toContain('/admin/groups')
    expect(wrapper.html()).toContain('/admin/accounts')
    expect(wrapper.html()).toContain('/plugin/augment/quick-login')
  })

  it('shows shared-wallet and dedicated-key guidance on the admin surface', async () => {
    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('admin.augmentGateway.description')
    expect(text).toContain('admin.augmentGateway.operationalGuidance')
    expect(text).toContain('admin.augmentGateway.sharedWalletDescription')
    expect(text).toContain('admin.augmentGateway.singleActiveKey')
  })
})

describe('AugmentGateway admin route', () => {
  it('requires admin route meta', async () => {
    const { routes } = await import('@/router')
    const route = routes.find((item) => item.path === '/admin/augment-gateway')
    expect(route?.meta?.requiresAdmin).toBe(true)
  })
})
