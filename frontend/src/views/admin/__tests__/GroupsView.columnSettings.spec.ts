import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

import type { AdminGroup } from '@/types'
import GroupsView from '../GroupsView.vue'

const {
  listGroups,
  getAllGroups,
  getModelsListCandidates,
  getUsageSummary,
  getCapacitySummary,
  listAccounts,
  showError,
  showSuccess,
  isCurrentStep,
  nextStep,
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  getAllGroups: vi.fn(),
  getModelsListCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  listAccounts: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
}))

const messages: Record<string, string> = {
  'admin.groups.allGroups': 'All Groups',
  'admin.groups.allPlatforms': 'All Platforms',
  'admin.groups.allStatus': 'All Status',
  'admin.groups.columnSettings': 'Column Settings',
  'admin.groups.columns.accounts': 'Accounts',
  'admin.groups.columns.actions': 'Actions',
  'admin.groups.columns.billingType': 'Billing Type',
  'admin.groups.columns.capacity': 'Capacity',
  'admin.groups.columns.name': 'Name',
  'admin.groups.columns.platform': 'Platform',
  'admin.groups.columns.rateMultiplier': 'Rate Multiplier',
  'admin.groups.columns.status': 'Status',
  'admin.groups.columns.type': 'Type',
  'admin.groups.columns.usage': 'Usage',
  'admin.groups.createGroup': 'Create Group',
  'admin.groups.exclusive': 'Exclusive',
  'admin.groups.nonExclusive': 'Shared',
  'admin.groups.searchGroups': 'Search groups',
  'admin.groups.sortOrder': 'Sort',
  'admin.accounts.status.active': 'Active',
  'admin.accounts.status.inactive': 'Inactive',
  'common.refresh': 'Refresh',
}

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      getAll: getAllGroups,
      getModelsListCandidates,
      getUsageSummary,
      getCapacitySummary,
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      updateSortOrder: vi.fn(),
      getRPMOverrides: vi.fn(),
    },
    accounts: {
      list: listAccounts,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({ isCurrentStep, nextStep }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => messages[key] ?? key }),
  }
})

const createGroup = (overrides: Partial<AdminGroup> = {}): AdminGroup => ({
  id: 1,
  name: 'Core Group',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  models_list_config: undefined,
  sort_order: 1,
  ...overrides,
})

const DataTableStub = {
  props: ['columns', 'data'],
  emits: ['sort'],
  template: '<div><div data-test="columns">{{ columns.map((col) => col.key).join(",") }}</div><div data-test="rows">{{ data.map((row) => row.name).join(",") }}</div></div>',
}

const SelectStub = {
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value); $emit(\'change\')"><option v-for="option in options" :key="String(option.value)" :value="option.value">{{ option.label }}</option></select>',
}

const mountView = async () => {
  const wrapper = mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        PlatformIcon: true,
        Icon: { props: ['name'], template: '<span>{{ name }}</span>' },
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: { template: '<div><slot /></div>' },
      },
    },
  })
  await flushPromises()
  return wrapper
}

const visibleColumns = (wrapper: VueWrapper) => wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const clickButtonContaining = async (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  expect(button, `button containing ${text}`).toBeTruthy()
  await button!.trigger('click')
  await flushPromises()
}

describe('admin GroupsView column settings', () => {
  beforeEach(() => {
    localStorage.clear()
    listGroups.mockReset()
    getAllGroups.mockReset()
    getModelsListCandidates.mockReset()
    getUsageSummary.mockReset()
    getCapacitySummary.mockReset()
    listAccounts.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()

    listGroups.mockResolvedValue({ items: [createGroup()], total: 1, page: 1, page_size: 20, pages: 1 })
    getAllGroups.mockResolvedValue([])
    getModelsListCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    isCurrentStep.mockReturnValue(false)
  })

  afterEach(() => {
    localStorage.clear()
  })

  it('lets admins hide optional group columns and persists the selection', async () => {
    const wrapper = await mountView()

    expect(visibleColumns(wrapper)).toContain('usage')

    await clickButtonContaining(wrapper, 'Column Settings')
    await clickButtonContaining(wrapper, 'Usage')

    expect(visibleColumns(wrapper)).not.toContain('usage')
    expect(JSON.parse(localStorage.getItem('group-hidden-columns') || '[]')).toEqual(['usage'])

    wrapper.unmount()
    const remounted = await mountView()
    expect(visibleColumns(remounted)).not.toContain('usage')
    expect(visibleColumns(remounted)).toContain('name')
    expect(visibleColumns(remounted)).toContain('actions')
  })

  it('skips usage and capacity summary fetches without visible consumers', async () => {
    localStorage.setItem('group-hidden-columns', JSON.stringify(['billing_type', 'usage', 'capacity']))

    const wrapper = await mountView()

    expect(visibleColumns(wrapper)).not.toContain('usage')
    expect(visibleColumns(wrapper)).not.toContain('capacity')
    expect(getUsageSummary).not.toHaveBeenCalled()
    expect(getCapacitySummary).not.toHaveBeenCalled()
  })

  it('loads usage when subscription quota cells remain visible', async () => {
    localStorage.setItem('group-hidden-columns', JSON.stringify(['usage']))

    await mountView()

    expect(getUsageSummary).toHaveBeenCalledTimes(1)
  })
})
