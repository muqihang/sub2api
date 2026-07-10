import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import type { ApiKey } from '@/types'
import KeysView from '../KeysView.vue'

const {
  listKeys,
  getPublicSettings,
  getDashboardApiKeysUsage,
  getAvailableGroups,
  getUserGroupRates,
  showError,
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.refresh': 'Refresh',
  'common.status': 'Status',
  'keys.apiKey': 'API Key',
  'keys.allGroups': 'All Groups',
  'keys.allStatus': 'All Status',
  'keys.columnSettings': 'Column Settings',
  'keys.createKey': 'Create API Key',
  'keys.created': 'Created',
  'keys.expiresAt': 'Expires',
  'keys.group': 'Group',
  'keys.currentConcurrency': 'Current Concurrency',
  'keys.lastUsedAt': 'Last Used',
  'keys.lastUsedIP': 'Last Used IP',
  'keys.rateLimitColumn': 'Rate Limit',
  'keys.searchPlaceholder': 'Search keys',
  'keys.status.active': 'Active',
  'keys.status.expired': 'Expired',
  'keys.status.inactive': 'Inactive',
  'keys.status.quota_exhausted': 'Quota exhausted',
  'keys.usage': 'Usage',
}

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    toggleStatus: vi.fn(),
  },
  authAPI: { getPublicSettings },
  usageAPI: { getDashboardApiKeysUsage },
  userGroupsAPI: { getAvailable: getAvailableGroups, getUserGroupRates },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({ isCurrentStep, nextStep }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard }),
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/keys', query: {} }),
  useRouter: () => ({ replace: vi.fn() }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => messages[key] ?? key }),
  }
})

const createApiKey = (): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test-key',
  name: 'test-key',
  group_id: null,
  augment_only: false,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  current_concurrency: 3,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
})

const DataTableStub = {
  props: ['columns', 'data'],
  emits: ['sort'],
  template: '<div><div data-test="columns">{{ columns.map((col) => col.key).join(",") }}</div><div data-test="sortable-columns">{{ columns.filter((col) => col.sortable).map((col) => col.key).join(",") }}</div><div v-for="row in data" :key="row.id"><slot name="cell-name" :value="row.name" :row="row" /><div data-test="current-concurrency"><slot name="cell-current_concurrency" :value="row.current_concurrency" :row="row" /></div></div></div>',
}

const mountView = async () => {
  const wrapper = mount(KeysView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="actions" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: { props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"></select>' },
        SearchInput: { props: ['modelValue'], emits: ['update:modelValue', 'search'], template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' },
        Icon: { props: ['name'], template: '<span>{{ name }}</span>' },
        UseKeyModal: true,
        EndpointPopover: true,
        GroupBadge: true,
        GroupOptionItem: true,
        Teleport: true,
      },
    },
  })
  await flushPromises()
  await nextTick()
  return wrapper
}

const visibleColumns = (wrapper: VueWrapper) => wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const clickButtonContaining = async (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  expect(button, `button containing ${text}`).toBeTruthy()
  await button!.trigger('click')
  await flushPromises()
}

describe('user KeysView column settings', () => {
  beforeEach(() => {
    localStorage.clear()
    listKeys.mockResolvedValue({ items: [createApiKey()], total: 1, page: 1, page_size: 20, pages: 1 })
    getPublicSettings.mockResolvedValue({})
    getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    getAvailableGroups.mockResolvedValue([])
    getUserGroupRates.mockResolvedValue({})
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    isCurrentStep.mockReturnValue(false)
  })

  it('hides low-frequency API key columns by default', async () => {
    const wrapper = await mountView()

    expect(visibleColumns(wrapper)).toEqual([
      'name',
      'key',
      'group',
      'current_concurrency',
      'usage',
      'expires_at',
      'status',
      'created_at',
      'actions',
    ])
  })

  it('lets users show a hidden API key column and persists the selection', async () => {
    const wrapper = await mountView()

    await clickButtonContaining(wrapper, 'Column Settings')
    await clickButtonContaining(wrapper, 'Rate Limit')

    expect(visibleColumns(wrapper)).toContain('rate_limit')
    expect(JSON.parse(localStorage.getItem('api-key-hidden-columns') || '[]')).toEqual(['last_used_at', 'last_used_ip'])
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('2')
  })

  it('hides the new last-used IP column for saved column preferences', async () => {
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['last_used_at']))
    localStorage.setItem('api-key-column-settings-version', '1')

    await mountView()

    expect(JSON.parse(localStorage.getItem('api-key-hidden-columns') || '[]')).toEqual(['last_used_at', 'last_used_ip'])
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('2')
  })

  it('renders current concurrency for each API key', async () => {
    const wrapper = await mountView()

    expect(visibleColumns(wrapper)).toContain('current_concurrency')
    expect(wrapper.get('[data-test="current-concurrency"]').text()).toContain('3')
  })

  it('allows sorting API keys by current concurrency', async () => {
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="sortable-columns"]').text().split(',')).toContain('current_concurrency')
  })
})
