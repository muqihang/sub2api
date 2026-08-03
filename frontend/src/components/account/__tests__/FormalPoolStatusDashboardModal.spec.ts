import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { FormalPoolStatusDashboard, FormalPoolStatusDashboardAccount } from '@/types'

const { getFormalPoolStatusDashboard } = vi.hoisted(() => ({
  getFormalPoolStatusDashboard: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getFormalPoolStatusDashboard,
    },
  },
}))

import FormalPoolStatusDashboardModal from '@/components/account/FormalPoolStatusDashboardModal.vue'

const runtime = (overrides: Partial<FormalPoolStatusDashboardAccount['rpm']> = {}) => ({
  current: 1,
  limit: 10,
  utilization: 0.1,
  available: true,
  ...overrides,
})

const window5h = (overrides: Partial<FormalPoolStatusDashboardAccount['five_hour_window']> = {}) => ({
  used: 100,
  limit: 1000,
  remaining: 900,
  utilization: 0.1,
  reset_at: '2026-06-01T12:00:00Z',
  status: 'active',
  available: true,
  ...overrides,
})

const account = (overrides: Partial<FormalPoolStatusDashboardAccount> = {}): FormalPoolStatusDashboardAccount => ({
  account_id: 1,
  account_label: '账号 #1',
  platform: 'anthropic',
  type: 'setup-token',
  stage: 'production',
  state: 'production',
  state_label: '生产中',
  state_severity: 'success',
  schedulable: true,
  effective_schedulable: true,
  production_ready: true,
  five_hour_window: window5h(),
  rpm: runtime({ current: 2, limit: 20 }),
  concurrency: runtime({ current: 1, limit: 4, utilization: 0.25 }),
  sessions: runtime({ current: 1, limit: 3, utilization: 0.333 }),
  last_used_at: '2026-06-01T11:55:00Z',
  last_success_hint: '最近成功',
  last_failure_code: '',
  last_failure_bucket: '',
  recommendation: {
    label: '继续观测',
    detail: '生产中：正常可调度。',
    action_kind: 'monitor',
  },
  ...overrides,
})

const dashboardFixture = (): FormalPoolStatusDashboard => {
  const rows = [
    account({ account_id: 1, account_label: '生产账号', state: 'production', state_label: '生产中' }),
    account({
      account_id: 2,
      account_label: '限流账号',
      state: 'rate_limited',
      state_label: '限流冷却中',
      recommendation: { label: '等待恢复', detail: '限流冷却中：等待 reset/cooldown 后复查。', action_kind: 'wait' },
    }),
    account({
      account_id: 3,
      account_label: '风险账号',
      state: 'manual_risk',
      state_label: '需人工介入',
      recommendation: { label: '人工介入', detail: '需人工介入：查看具体失败分类后处理。', action_kind: 'manual' },
    }),
    account({
      account_id: 4,
      account_label: '数据缺失账号',
      state: 'data_missing',
      state_label: '数据不足',
      rpm: runtime({ available: false, current: 0, limit: 0, utilization: null }),
      recommendation: { label: '补齐数据', detail: '数据不足：不能判断正常。', action_kind: 'repair' },
    }),
    account({
      account_id: 5,
      account_label: '证据缺失账号',
      state: 'evidence_missing',
      state_label: '证据不足',
      recommendation: { label: '补齐证据', detail: '证据不足：需要补齐健康检查证据。', action_kind: 'repair' },
    }),
    account({
      account_id: 6,
      account_label: '停用账号',
      state: 'inactive',
      state_label: '已停用',
      schedulable: false,
      effective_schedulable: false,
      recommendation: { label: '保持停用', detail: '已停用：不参与调度。', action_kind: 'none' },
    }),
    account({
      account_id: 7,
      account_label: '不可调度账号',
      state: 'not_schedulable',
      state_label: '不可调度',
      effective_schedulable: false,
      recommendation: { label: '查看 gate', detail: '不可调度：需查看 gate 原因。', action_kind: 'diagnose' },
    }),
  ]

  return {
    summary: {
      total: rows.length,
      normal: 1,
      warming: 0,
      production: 1,
      rate_limited: 1,
      manual_risk: 1,
      error: 0,
      quarantined: 0,
      inactive: 1,
      not_schedulable: 1,
      evidence_missing: 1,
      data_missing: 1,
      schedulable: 4,
      total_current_rpm: 12,
      total_rpm_limit: 80,
      rpm_available: true,
      five_hour_remaining_ratio: 0.725,
      five_hour_window_available: true,
      generated_at: '2026-06-01T12:00:00Z',
    },
    accounts: rows,
  }
}

const mountModal = async (fixture = dashboardFixture()) => {
  getFormalPoolStatusDashboard.mockResolvedValue(fixture)
  const wrapper = mount(FormalPoolStatusDashboardModal, {
    props: { show: true },
    attachTo: document.body,
  })
  await flushPromises()
  return wrapper
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('FormalPoolStatusDashboardModal', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    getFormalPoolStatusDashboard.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('renders summary cards from fixture data', async () => {
    const wrapper = await mountModal()

    expect(wrapper.text()).toContain('可正常调度')
    expect(wrapper.text()).toContain('生产中')
    expect(wrapper.text()).toContain('限流冷却')
    expect(wrapper.text()).toContain('需人工介入')
    expect(wrapper.text()).toContain('当前总 RPM')
    expect(wrapper.text()).toContain('5 小时总体余量')
    expect(wrapper.text()).toContain('72.5%')
  })

  it('rate_limited row displays limit/cooldown guidance, not quarantine guidance', async () => {
    const wrapper = await mountModal()

    const text = wrapper.get('[data-account-row="2"]').text()
    expect(text).toContain('限流冷却中')
    expect(text).toContain('等待')
    expect(text).not.toContain('隔离')
  })

  it('manual_risk row displays manual intervention guidance', async () => {
    const wrapper = await mountModal()

    const text = wrapper.get('[data-account-row="3"]').text()
    expect(text).toContain('需人工介入')
  })

  it('data_missing and evidence_missing display 数据不足 and 证据不足', async () => {
    const wrapper = await mountModal()

    expect(wrapper.get('[data-account-row="4"]').text()).toContain('数据不足')
    expect(wrapper.get('[data-account-row="5"]').text()).toContain('证据不足')
  })

  it('inactive and not_schedulable rows do not display as normal and are filterable', async () => {
    const wrapper = await mountModal()

    expect(wrapper.get('[data-account-row="6"]').text()).toContain('已停用')
    expect(wrapper.get('[data-account-row="6"]').text()).not.toContain('正常')
    expect(wrapper.get('[data-account-row="7"]').text()).toContain('不可调度')
    expect(wrapper.get('[data-account-row="7"]').text()).not.toContain('正常')

    await wrapper.get('[data-filter="inactive"]').trigger('click')
    expect(wrapper.text()).toContain('停用账号')
    expect(wrapper.find('[data-account-row="7"]').exists()).toBe(false)

    await wrapper.get('[data-filter="not_schedulable"]').trigger('click')
    expect(wrapper.text()).toContain('不可调度账号')
    expect(wrapper.find('[data-account-row="6"]').exists()).toBe(false)
  })


  it('sorts rows by operational state priority before account id', async () => {
    const wrapper = await mountModal()
    const labels = wrapper.findAll('[data-account-row]').map(row => row.text())

    expect(labels[0]).toContain('停用账号')
    expect(labels[1]).toContain('风险账号')
    expect(labels[2]).toContain('限流账号')
    expect(labels[3]).toContain('不可调度账号')
    expect(labels[4]).toContain('证据缺失账号')
    expect(labels[5]).toContain('数据缺失账号')
    expect(labels[6]).toContain('生产账号')
  })

  it('auto-refresh calls API when open and stops after close/unmount', async () => {
    const wrapper = await mountModal()
    expect(getFormalPoolStatusDashboard).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(getFormalPoolStatusDashboard).toHaveBeenCalledTimes(2)

    await wrapper.setProps({ show: false })
    await vi.advanceTimersByTimeAsync(10000)
    await flushPromises()
    expect(getFormalPoolStatusDashboard).toHaveBeenCalledTimes(2)

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(10000)
    expect(getFormalPoolStatusDashboard).toHaveBeenCalledTimes(2)
  })

  it('aborts in-flight refresh and does not update rows after close', async () => {
    const pending = deferred<FormalPoolStatusDashboard>()
    getFormalPoolStatusDashboard.mockReturnValueOnce(pending.promise)

    const wrapper = mount(FormalPoolStatusDashboardModal, {
      props: { show: true },
      attachTo: document.body,
    })
    await flushPromises()

    const firstCallOptions = getFormalPoolStatusDashboard.mock.calls[0]?.[0]
    expect(firstCallOptions?.signal).toBeInstanceOf(AbortSignal)
    expect(firstCallOptions.signal.aborted).toBe(false)

    await wrapper.setProps({ show: false })
    expect(firstCallOptions.signal.aborted).toBe(true)

    pending.resolve(dashboardFixture())
    await flushPromises()

    expect(wrapper.text()).not.toContain('生产账号')
    wrapper.unmount()
  })

  it('sensitive fixture strings are not rendered', async () => {
    const fixture = dashboardFixture()
    fixture.accounts.push(account({
      account_id: 8,
      account_label: '账号 #8',
      recommendation: {
        label: '检查',
        detail: '安全摘要，不包含 secret。',
        action_kind: 'monitor',
      },
      // These extra keys simulate accidental sensitive payload from an unsafe fixture.
      access_token: 'sk-ant-sensitive-token',
      raw_body: 'raw-body-secret',
      email: 'secret@example.com',
      proxy_password: 'proxy-password-secret',
      raw_cch: 'raw-cch-secret',
    } as unknown as Partial<FormalPoolStatusDashboardAccount>))

    const wrapper = await mountModal(fixture)
    const text = wrapper.text()

    expect(text).not.toContain('sk-ant-sensitive-token')
    expect(text).not.toContain('raw-body-secret')
    expect(text).not.toContain('secret@example.com')
    expect(text).not.toContain('proxy-password-secret')
    expect(text).not.toContain('raw-cch-secret')
  })
})
