import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  getBatchTodayStats,
  getAllProxies,
  getAllGroups,
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn(),
      getFormalPoolStatusDashboard: vi.fn().mockResolvedValue({
        accounts: [],
        summary: {
          total: 0,
          normal: 0,
          warming: 0,
          production: 0,
          rate_limited: 0,
          manual_risk: 0,
          error: 0,
          quarantined: 0,
          inactive: 0,
          not_schedulable: 0,
          evidence_missing: 0,
          data_missing: 0,
          schedulable: 0,
          total_current_rpm: 0,
          total_rpm_limit: 0,
          rpm_available: false,
          five_hour_remaining_ratio: null,
          five_hour_window_available: false,
          generated_at: '2026-06-01T00:00:00Z',
        },
      }),
    },
    proxies: { getAll: getAllProxies },
    groups: { getAll: getAllGroups },
  },
}))

const useNewAccountManagementUx = { value: false }

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
    get useNewAccountManagementUx() {
      return useNewAccountManagementUx.value
    },
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ token: 'test-token' }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'admin.accounts.formalPool.effectiveBlocked': 'DB 调度已开，但正式号池调度门禁仍阻止调度',
  }
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => messages[key] ?? key }),
  }
})

const LegacyDashboardStub = {
  props: ['show'],
  emits: ['close'],
  template:
    '<div data-test="dashboard-legacy" :data-show="String(show)"></div>',
}

const V2DashboardStub = {
  props: ['show'],
  emits: ['close', 'diagnose'],
  template: `
    <div data-test="dashboard-v2" :data-show="String(show)">
      <button v-if="show" data-test="dashboard-v2-diagnose" @click="$emit('diagnose', 42)">diagnose</button>
    </div>
  `,
}

const LegacyDiagnosticsStub = {
  props: ['show', 'account'],
  emits: ['close', 'updated'],
  template: '<div data-test="diagnostics-legacy" :data-show="String(show)"></div>',
}

const V2DiagnosticsStub = {
  props: ['show', 'account'],
  emits: ['close', 'updated'],
  template: '<div data-test="diagnostics-v2" :data-show="String(show)" :data-account-id="account?.id ?? \'\'"></div>',
}

const baseStubs = {
  AppLayout: { template: '<div><slot /></div>' },
  TablePageLayout: {
    template:
      '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>',
  },
  DataTable: {
    props: ['columns', 'data', 'density'],
    template: `
      <div data-test="data-table">
        <div v-for="row in data" :key="row.id" data-test="data-row">
          <slot name="cell-schedulable" :row="row" :value="row.schedulable" />
        </div>
      </div>
    `,
  },
  Pagination: true,
  ConfirmDialog: true,
  AccountTableActions: {
    template: '<div><slot name="beforeCreate" /><slot name="after" /></div>',
  },
  AccountTableFilters: { template: '<div></div>' },
  AccountBulkActionsBar: true,
  AccountActionMenu: true,
  ImportDataModal: true,
  ReAuthAccountModal: true,
  AccountTestModal: true,
  AccountStatsModal: true,
  ScheduledTestsPanel: true,
  SyncFromCrsModal: true,
  TempUnschedStatusModal: true,
  ErrorPassthroughRulesModal: true,
  TLSFingerprintProfilesModal: true,
  CreateAccountModal: true,
  EditAccountModal: true,
  BulkEditAccountModal: true,
  FormalPoolDiagnosticsModal: LegacyDiagnosticsStub,
  FormalPoolDiagnosticsModalV2: V2DiagnosticsStub,
  FormalPoolStatusDashboardModal: LegacyDashboardStub,
  FormalPoolStatusDashboardModalV2: V2DashboardStub,
  PlatformTypeBadge: true,
  AccountCapacityCell: true,
  AccountStatusIndicator: true,
  AccountTodayStatsCell: true,
  AccountGroupsCell: true,
  AccountUsageCell: true,
  Icon: true,
}

async function mountAccountsView() {
  const wrapper = mount(AccountsView, {
    global: { stubs: baseStubs },
  })
  await flushPromises()
  return wrapper
}

describe('admin AccountsView dashboard flag switch', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockReset()
    listWithEtag.mockReset()
    getBatchTodayStats.mockReset()
    getAllProxies.mockReset()
    getAllGroups.mockReset()
    useNewAccountManagementUx.value = false

    listAccounts.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 0,
    })
    listWithEtag.mockResolvedValue({ notModified: true, etag: null, data: null })
    getBatchTodayStats.mockResolvedValue({})
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
  })

  it('renders the legacy dashboard when use_new_account_management_ux is false', async () => {
    useNewAccountManagementUx.value = false
    const wrapper = await mountAccountsView()

    expect(wrapper.find('[data-test="dashboard-legacy"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="dashboard-v2"]').exists()).toBe(false)
  })

  it('renders the V2 dashboard when use_new_account_management_ux is true', async () => {
    useNewAccountManagementUx.value = true
    const wrapper = await mountAccountsView()

    expect(wrapper.find('[data-test="dashboard-v2"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="dashboard-legacy"]').exists()).toBe(false)
  })

  it('renders the legacy diagnostics modal when use_new_account_management_ux is false', async () => {
    useNewAccountManagementUx.value = false
    const wrapper = await mountAccountsView()

    expect(wrapper.find('[data-test="diagnostics-legacy"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="diagnostics-v2"]').exists()).toBe(false)
  })

  it('renders the V2 diagnostics modal when use_new_account_management_ux is true', async () => {
    useNewAccountManagementUx.value = true
    const wrapper = await mountAccountsView()

    expect(wrapper.find('[data-test="diagnostics-v2"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="diagnostics-legacy"]').exists()).toBe(false)
  })

  it('opens V2 diagnostics for the account emitted by the V2 dashboard diagnose CTA', async () => {
    useNewAccountManagementUx.value = true
    listAccounts.mockResolvedValue({
      items: [
        {
          id: 42,
          name: 'claude-oauth-42',
          platform: 'anthropic',
          type: 'oauth',
          is_formal_pool: true,
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })

    const wrapper = await mountAccountsView()
    await wrapper.find('button[title="号池实时看板"]').trigger('click')
    expect(wrapper.find('[data-test="dashboard-v2"]').attributes('data-show')).toBe('true')

    await wrapper.find('[data-test="dashboard-v2-diagnose"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="diagnostics-v2"]').attributes('data-show')).toBe('true')
    expect(wrapper.find('[data-test="diagnostics-v2"]').attributes('data-account-id')).toBe('42')
  })

  it('shows Chinese scheduling gate copy in the visible table UI without English Gate', async () => {
    listAccounts.mockResolvedValue({
      items: [
        {
          id: 77,
          name: 'claude-oauth-77',
          platform: 'anthropic',
          type: 'oauth',
          is_formal_pool: true,
          schedulable: true,
          effective_schedulable: false,
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })

    const wrapper = await mountAccountsView()
    const text = wrapper.text()
    expect(text).toContain('门禁')
    expect(text).not.toMatch(/\bGate\b/)

    const gateButton = wrapper.find('button[title*="调度门禁"]')
    expect(gateButton.exists()).toBe(true)
    expect(gateButton.text()).toBe('门禁')
    expect(gateButton.attributes('title')).not.toMatch(/\bGate\b/i)
  })
})
