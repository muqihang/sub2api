import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  getBatchTodayStats,
  getAllProxies,
  getAllGroups,
  setPrivacy,
  showErrorMock,
  showSuccessMock,
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
  setPrivacy: vi.fn(),
  showErrorMock: vi.fn(),
  showSuccessMock: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      setPrivacy,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn(),
    },
    proxies: { getAll: getAllProxies },
    groups: { getAll: getAllGroups },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: (...args: any[]) => showErrorMock(...args),
    showSuccess: (...args: any[]) => showSuccessMock(...args),
    showInfo: vi.fn(),
    useNewAccountManagementUx: false,
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
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const DataTableStub = {
  props: ['columns', 'data'],
  template: '<div><slot v-for="row in data" name="cell-actions" :row="row" /></div>',
}

const AccountActionMenuStub = {
  props: ['account'],
  emits: ['set-privacy'],
  template: '<button data-test="set-privacy" @click="$emit(\'set-privacy\', account)">privacy</button>',
}

async function mountAccountsView() {
  const wrapper = mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: DataTableStub,
        Pagination: true,
        ConfirmDialog: true,
        AccountTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
        AccountTableFilters: { template: '<div></div>' },
        AccountBulkActionsBar: true,
        AccountActionMenu: AccountActionMenuStub,
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
        FormalPoolDiagnosticsModal: true,
        FormalPoolDiagnosticsModalV2: true,
        FormalPoolStatusDashboardModal: true,
        FormalPoolStatusDashboardModalV2: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        Icon: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('admin AccountsView privacy result messaging', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockReset()
    listWithEtag.mockReset()
    getBatchTodayStats.mockReset()
    getAllProxies.mockReset()
    getAllGroups.mockReset()
    setPrivacy.mockReset()
    showErrorMock.mockReset()
    showSuccessMock.mockReset()

    listAccounts.mockResolvedValue({
      items: [{ id: 1, name: 'openai', platform: 'openai', type: 'oauth', status: 'active', schedulable: true, extra: {} }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    listWithEtag.mockResolvedValue({ notModified: true, etag: null, data: null })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
  })

  it('shows an error when OpenAI privacy setting is Cloudflare blocked', async () => {
    setPrivacy.mockResolvedValue({ id: 1, platform: 'openai', extra: { privacy_mode: 'training_set_cf_blocked' } })
    const wrapper = await mountAccountsView()

    await (wrapper.vm as any).handleSetPrivacy({ id: 1, platform: 'openai', extra: {} })
    await flushPromises()

    expect(showSuccessMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.privacyCfBlocked')
  })

  it('shows success when Antigravity privacy mode was set', async () => {
    setPrivacy.mockResolvedValue({ id: 2, platform: 'antigravity', extra: { privacy_mode: 'privacy_set' } })
    const wrapper = await mountAccountsView()

    await (wrapper.vm as any).handleSetPrivacy({ id: 2, platform: 'antigravity', extra: {} })
    await flushPromises()

    expect(showErrorMock).not.toHaveBeenCalled()
    expect(showSuccessMock).toHaveBeenCalledWith('admin.accounts.privacyAntigravitySet')
  })
})
