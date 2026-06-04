import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import FormalPoolStatusDashboardModalV2 from '../FormalPoolStatusDashboardModalV2.vue'
import type {
  FormalPoolDashboardState,
  FormalPoolStatusDashboard,
  FormalPoolStatusDashboardAccount,
  FormalPoolStatusSummary,
} from '@/types'

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

function accountFixture(
  overrides: Partial<FormalPoolStatusDashboardAccount> = {},
): FormalPoolStatusDashboardAccount {
  return {
    account_id: 1,
    account_label: 'claude-oauth-01',
    platform: 'anthropic',
    type: 'oauth',
    stage: 'production',
    state: 'production',
    state_label: '生产中',
    state_severity: 'success',
    schedulable: true,
    effective_schedulable: true,
    production_ready: true,
    five_hour_window: {
      used: 1,
      limit: 5,
      remaining: 4,
      utilization: 0.2,
      reset_at: '2026-06-01T18:30:00Z',
      status: 'allowed',
      available: true,
    },
    rpm: { current: 10, limit: 60, utilization: 0.16, available: true },
    concurrency: { current: 1, limit: 5, utilization: 0.2, available: true },
    sessions: { current: 1, limit: 3, utilization: 0.33, available: true },
    last_used_at: null,
    last_success_hint: '14:32:01',
    last_failure_code: '',
    last_failure_bucket: '',
    recommendation: { label: '保持观察', detail: '账号证据完整且可调度。', action_kind: 'none' },
    ...overrides,
  }
}

function buildDashboard(
  accounts: FormalPoolStatusDashboardAccount[],
  summaryOverrides: Partial<FormalPoolStatusSummary> = {},
): FormalPoolStatusDashboard {
  return {
    accounts,
    summary: {
      total: accounts.length,
      normal: 0,
      warming: accounts.filter((a) => a.state === 'warming').length,
      production: accounts.filter((a) => a.state === 'production').length,
      rate_limited: accounts.filter((a) => a.state === 'rate_limited').length,
      manual_risk: 0,
      error: accounts.filter((a) => a.state === 'error').length,
      quarantined: accounts.filter((a) => a.state === 'quarantined').length,
      inactive: accounts.filter((a) => a.state === 'inactive').length,
      not_schedulable: 0,
      evidence_missing: 0,
      data_missing: 0,
      schedulable: accounts.filter((a) => a.effective_schedulable).length,
      total_current_rpm: 0,
      total_rpm_limit: 0,
      rpm_available: false,
      five_hour_remaining_ratio: null,
      five_hour_window_available: false,
      generated_at: '2026-06-01T14:32:08Z',
      ...summaryOverrides,
    },
  }
}

async function mountWithFixture(
  accounts: FormalPoolStatusDashboardAccount[],
  show = true,
  summaryOverrides: Partial<FormalPoolStatusSummary> = {},
) {
  getFormalPoolStatusDashboard.mockResolvedValue(buildDashboard(accounts, summaryOverrides))
  const wrapper = mount(FormalPoolStatusDashboardModalV2, {
    props: { show },
  })
  await flushPromises()
  return wrapper
}

function expectNoRailBorderClasses(classes: string[]) {
  const classText = classes.join(' ')
  expect(classes).not.toContain('border-l-4')
  expect(classText).not.toMatch(/(?:^|\s)(?:dark:)?border-(?:rose|sky|amber|emerald|slate)-/)
}

describe('FormalPoolStatusDashboardModalV2', () => {
  beforeEach(() => {
    getFormalPoolStatusDashboard.mockReset()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('does not render when show is false', () => {
    const wrapper = mount(FormalPoolStatusDashboardModalV2, {
      props: { show: false },
    })
    expect(wrapper.find('[data-testid="formal-pool-dashboard-v2"]').exists()).toBe(false)
    expect(getFormalPoolStatusDashboard).not.toHaveBeenCalled()
  })

  it('renders the health distribution bar with four segments and three command metrics', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: 1, state: 'production' }),
      accountFixture({ account_id: 2, state: 'warming' }),
      accountFixture({ account_id: 3, state: 'rate_limited' }),
      accountFixture({ account_id: 4, state: 'quarantined' }),
    ])

    expect(wrapper.find('[data-testid="dashboard-v2-health-distribution"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="distribution-segment-active"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="distribution-segment-paused"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="distribution-segment-needs-intervention"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="distribution-segment-inactive"]').exists()).toBe(true)

    expect(wrapper.find('[data-testid="dashboard-v2-command-metrics"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="command-metric-usable-capacity"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="command-metric-cooling-window"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="command-metric-intervention-queue"]').exists()).toBe(true)
  })

  it('renders total RPM capacity in the top command metrics when summary RPM is available', async () => {
    const wrapper = await mountWithFixture(
      [accountFixture()],
      true,
      {
        total_current_rpm: 0,
        total_rpm_limit: 170,
        rpm_available: true,
      },
    )

    const metrics = wrapper.find('[data-testid="dashboard-v2-command-metrics"]')
    expect(metrics.text()).toContain('总 RPM 可供调用')
    expect(metrics.text()).toContain('当前 0 / 总 170 RPM')
  })

  it('still renders total RPM capacity when runtime RPM data is unavailable', async () => {
    const wrapper = await mountWithFixture(
      [accountFixture()],
      true,
      {
        total_current_rpm: 0,
        total_rpm_limit: 170,
        rpm_available: false,
      },
    )

    expect(wrapper.find('[data-testid="dashboard-v2-command-metrics"]').text()).toContain('当前数据不足 / 总 170 RPM')
  })

  it('renders an unconfigured RPM fallback only when total RPM capacity is not configured', async () => {
    const wrapper = await mountWithFixture(
      [accountFixture()],
      true,
      {
        total_current_rpm: 0,
        total_rpm_limit: 0,
        rpm_available: false,
      },
    )

    expect(wrapper.find('[data-testid="dashboard-v2-command-metrics"]').text()).toContain('未配置 RPM 容量')
  })


  it('shows 5h and 7d quota summaries in the main table and drawer', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({
        five_hour_window: {
          used: 1,
          limit: 5,
          remaining: 4,
          utilization: 0.2,
          reset_at: '2026-06-01T18:30:00Z',
          status: 'allowed',
          available: true,
        },
        passive_usage_5h: {
          utilization: 0.42,
          remaining_ratio: 0.58,
          reset_at: '2026-06-01T17:00:00Z',
          sampled_at: '2026-06-01T12:34:56Z',
          status: 'allowed',
          available: true,
        },
        passive_usage_7d: {
          utilization: 0.91,
          remaining_ratio: 0.09,
          reset_at: '2026-06-08T12:00:00Z',
          sampled_at: '2026-06-01T12:34:56Z',
          status: 'sampled',
          available: true,
        },
      } as any),
    ])

    expect(wrapper.find('[data-testid="column-five-hour"]').text()).toContain('限额余量')
    const row = wrapper.find('tr[data-account-row]')
    expect(row.text()).toContain('5h 剩余 58.0%，已用 42.0%')
    expect(row.text()).toContain('7d 剩余 9.0%，已用 91.0%')
    expect(row.text()).not.toContain('20.0%')

    await wrapper.find('[data-testid^="expand-"]').trigger('click')
    const drawer = wrapper.find('[data-testid^="drawer-"]')
    expect(drawer.text()).toContain('5h 限额')
    expect(drawer.text()).toContain('剩余 58.0%')
    expect(drawer.text()).toContain('已用 42.0%')
    expect(drawer.text()).toContain('周/7d 限额')
    expect(drawer.text()).toContain('剩余 9.0%')
    expect(drawer.text()).toContain('已用 91.0%')
  })

  it('falls back to legacy five_hour_window when passive 5h is unavailable', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({
        five_hour_window: {
          used: 1,
          limit: 5,
          remaining: 4,
          utilization: 0.2,
          reset_at: '2026-06-01T18:30:00Z',
          status: 'allowed',
          available: true,
        },
        passive_usage_5h: {
          utilization: null,
          remaining_ratio: null,
          reset_at: null,
          sampled_at: null,
          status: 'not_sampled',
          available: false,
        },
        passive_usage_7d: {
          utilization: null,
          remaining_ratio: null,
          reset_at: null,
          sampled_at: null,
          status: 'not_sampled',
          available: false,
        },
      } as any),
    ])

    const row = wrapper.find('tr[data-account-row]')
    expect(row.text()).toContain('5h 剩余 80.0%，已用 20.0%')
    expect(row.text()).toContain('7d 数据不足/未采样')

    await wrapper.find('[data-testid^="expand-"]').trigger('click')
    const drawer = wrapper.find('[data-testid^="drawer-"]')
    expect(drawer.text()).toContain('5h 限额')
    expect(drawer.text()).toContain('剩余 80.0%')
    expect(drawer.text()).toContain('已用 20.0%')
    expect(drawer.text()).toContain('周/7d 限额')
    expect(drawer.text()).toContain('数据不足/未采样')
  })


  it('shows per-account RPM, concurrency, and sessions in the main table without expanding', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({
        rpm: { current: 7, limit: 60, utilization: 0.116, available: true },
        concurrency: { current: 2, limit: 5, utilization: 0.4, available: true },
        sessions: { current: 1, limit: 3, utilization: 0.333, available: true },
      }),
      accountFixture({
        account_id: 2,
        rpm: { current: 0, limit: 120, utilization: null, available: false },
        concurrency: { current: 0, limit: 0, utilization: null, available: true },
        sessions: { current: 0, limit: 4, utilization: null, available: false },
      }),
    ])

    const rows = wrapper.findAll('tr[data-account-row]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('RPM 7 / 60')
    expect(rows[0].text()).toContain('并发 2 / 5')
    expect(rows[0].text()).toContain('会话 1 / 3')
    expect(rows[1].text()).toContain('RPM 数据不足 / 配置 120')
    expect(rows[1].text()).toContain('并发 未配置')
    expect(rows[1].text()).toContain('会话 数据不足 / 配置 4')
  })

  it('renders four segmented lanes plus the "全部" lane', async () => {
    const wrapper = await mountWithFixture([accountFixture()])
    const lanes = wrapper.find('[data-testid="dashboard-v2-lanes"]')
    expect(lanes.exists()).toBe(true)
    expect(lanes.find('[data-testid="lane-all"]').exists()).toBe(true)
    expect(lanes.find('[data-testid="lane-needs_intervention"]').exists()).toBe(true)
    expect(lanes.find('[data-testid="lane-paused"]').exists()).toBe(true)
    expect(lanes.find('[data-testid="lane-active"]').exists()).toBe(true)
    expect(lanes.find('[data-testid="lane-inactive"]').exists()).toBe(true)
  })

  it('includes a primary table column for per-account runtime metrics', async () => {
    const wrapper = await mountWithFixture([accountFixture()])
    const columnHeaders = wrapper.findAll('[data-testid^="column-"]')
    expect(columnHeaders).toHaveLength(6)
    expect(wrapper.find('[data-testid="column-runtime"]').text()).toContain('调用指标')
  })

  it('pins needs-intervention rows above active/paused/inactive and gives them a rose rail element', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: 10, state: 'production' }),
      accountFixture({ account_id: 11, state: 'rate_limited' }),
      accountFixture({ account_id: 12, state: 'quarantined' }),
      accountFixture({ account_id: 13, state: 'inactive' }),
    ])

    const rows = wrapper.findAll('tr[data-bucket]')
    expect(rows.length).toBeGreaterThanOrEqual(4)
    // The first row should be the needs_intervention one (quarantined).
    expect(rows[0].attributes('data-account-row')).not.toContain('12')
    expect(rows[0].attributes('data-bucket')).toBe('needs_intervention')
    // Visible rail element lives inside the first <td>, not as a <tr> border
    // class (which is unreliable under border-collapse).
    const rail = rows[0].find('[data-testid^="row-rail-"]')
    expect(rail.exists()).toBe(true)
    expect(rail.attributes('data-rail-tone')).toBe('rose')
    expect(rail.attributes('data-rail-warming')).toBe('false')
    expect(rail.classes().join(' ')).toContain('rose-500')
    expect(rail.classes().join(' ')).toContain('absolute')
    expect(rail.classes()).toContain('w-1')
    expectNoRailBorderClasses(rows[0].classes())
  })

  it('renders the warming row with a sky rail element and the "预热中 · low weight" copy', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: 21, state: 'warming', state_label: '预热中' }),
    ])

    const warmingRow = wrapper.find('tr[data-bucket="active"]')
    expect(warmingRow.exists()).toBe(true)
    expect(warmingRow.attributes('data-warming')).toBe('true')
    const rail = warmingRow.find('[data-testid^="row-rail-"]')
    expect(rail.exists()).toBe(true)
    expect(rail.attributes('data-rail-tone')).toBe('sky')
    expect(rail.attributes('data-rail-warming')).toBe('true')
    expect(rail.classes().join(' ')).toContain('sky-500')
    expect(rail.classes()).toContain('w-1')
    expectNoRailBorderClasses(warmingRow.classes())

    const label = warmingRow.find('[data-testid="warming-presentation-label"]')
    expect(label.exists()).toBe(true)
    expect(label.text()).toContain('预热中')
    expect(label.text()).toContain('low weight')
  })

  it('renders a visible row rail element per row whose tone matches the bucket', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: 71, state: 'production' }),
      accountFixture({ account_id: 72, state: 'rate_limited' }),
      accountFixture({ account_id: 73, state: 'quarantined' }),
      accountFixture({ account_id: 74, state: 'inactive' }),
      accountFixture({ account_id: 75, state: 'warming' }),
    ])

    const rows = wrapper.findAll('tr[data-bucket]')
    expect(rows.length).toBe(5)
    for (const row of rows) {
      const rail = row.find('[data-testid^="row-rail-"]')
      expect(rail.exists()).toBe(true)
      // Rails are positioned absolutely inside the first <td>; this guarantees
      // they paint regardless of <table> border-collapse behavior.
      expect(rail.classes().join(' ')).toContain('absolute')
      expect(rail.classes()).toContain('w-1')
      expectNoRailBorderClasses(row.classes())
      const tone = rail.attributes('data-rail-tone')
      expect(['rose', 'sky', 'amber', 'emerald', 'slate']).toContain(tone)
      const warming = row.attributes('data-warming') === 'true'
      if (warming) {
        expect(tone).toBe('sky')
      } else {
        const bucket = row.attributes('data-bucket')
        if (bucket === 'needs_intervention') expect(tone).toBe('rose')
        if (bucket === 'paused') expect(tone).toBe('amber')
        if (bucket === 'active') expect(tone).toBe('emerald')
        if (bucket === 'inactive') expect(tone).toBe('slate')
      }
    }
  })

  it('also renders a rail element inside the expanded drawer row', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: 81, state: 'quarantined' }),
    ])

    const row = wrapper.find('tr[data-bucket="needs_intervention"]')
    expect(row.exists()).toBe(true)
    await row.find('[data-testid^="expand-"]').trigger('click')

    const drawerRail = wrapper.find('[data-testid^="drawer-rail-"]')
    expect(drawerRail.exists()).toBe(true)
    expect(drawerRail.attributes('data-rail-tone')).toBe('rose')
    expect(drawerRail.classes().join(' ')).toContain('absolute')
    expect(drawerRail.classes().join(' ')).toContain('rose-500')
    expect(drawerRail.classes()).toContain('w-1')
    expectNoRailBorderClasses(wrapper.find('[data-testid^="drawer-"]').classes())
  })

  it('filters the table when a lane is clicked', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: 31, state: 'production' }),
      accountFixture({ account_id: 32, state: 'quarantined' }),
      accountFixture({ account_id: 33, state: 'rate_limited' }),
    ])

    await wrapper.find('[data-testid="lane-needs_intervention"]').trigger('click')
    const rows = wrapper.findAll('tr[data-account-row]')
    expect(rows).toHaveLength(1)
    expect(rows[0].attributes('data-account-row')).not.toContain('32')
    expect(rows[0].attributes('data-bucket')).toBe('needs_intervention')
  })

  it('toggles the row detail drawer when the action button is clicked', async () => {
    const wrapper = await mountWithFixture([accountFixture({ account_id: 41 })])

    expect(wrapper.find('[data-testid^="drawer-"]').exists()).toBe(false)
    const expand = wrapper.find('[data-testid^="expand-"]')
    expect(expand.attributes('data-testid')).not.toContain('41')
    await expand.trigger('click')
    expect(wrapper.find('[data-testid^="drawer-"]').exists()).toBe(true)
    await expand.trigger('click')
    expect(wrapper.find('[data-testid^="drawer-"]').exists()).toBe(false)
  })

  it('jump-to-needs-intervention shortcut activates the rose lane filter', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: 51, state: 'production' }),
      accountFixture({ account_id: 52, state: 'error', state_label: '错误' }),
    ])

    const jumpBtn = wrapper.find('[data-testid="jump-needs-intervention"]')
    expect(jumpBtn.exists()).toBe(true)
    await jumpBtn.trigger('click')

    const rows = wrapper.findAll('tr[data-account-row]')
    expect(rows).toHaveLength(1)
    expect(rows[0].attributes('data-account-row')).not.toContain('52')
    expect(rows[0].attributes('data-bucket')).toBe('needs_intervention')
  })

  it('shows ordinary operator labels with IP suffixes but hides mixed secret labels and raw numeric fallbacks', async () => {
    const rawAccountId = 87654321
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: 81, account_label: 'ops-user@example.com' }),
      accountFixture({ account_id: 82, account_label: 'ops-user@example.com sk-ant-secret-token' }),
      accountFixture({ account_id: 83, account_label: 'anthropic-setup-204.1.108.104' }),
      accountFixture({ account_id: 84, account_label: '疑似限额-anthropic-setup-207.97.155.20' }),
      accountFixture({ account_id: rawAccountId, account_label: `账号 #${rawAccountId}` }),
    ])

    const text = wrapper.text()
    expect(text).toContain('ops-user@example.com')
    expect(text).toContain('anthropic-setup-204.1.108.104')
    expect(text).toContain('疑似限额-anthropic-setup-207.97.155.20')
    expect(text).not.toContain('sk-ant-secret-token')
    expect(text).not.toContain(`账号 #${rawAccountId}`)
    expect(text).toContain('账号（未命名）')
  })

  it('renders recent request copy and failure diagnostics in Chinese', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: 91, last_success_hint: '14:32:01', last_failure_code: 'auth', last_failure_bucket: 'rate_limited' }),
      accountFixture({ account_id: 92, last_success_hint: '', last_used_at: '2026-06-01T14:32:08Z', last_failure_code: 'formal_pool_healthcheck_failed' }),
      accountFixture({ account_id: 93, last_success_hint: '', last_used_at: null, last_failure_code: '', last_failure_bucket: 'status_429' }),
      accountFixture({ account_id: 94, last_success_hint: '', last_used_at: null, last_failure_code: 'unknown sk-ant-secret-token' }),
    ])

    const text = wrapper.text()
    expect(text).toContain('最近成功：14:32:01')
    expect(text).toContain('最近调度：')
    expect(text).toContain('从未调度')
    expect(text).toContain('授权/登录失败')
    expect(text).toContain('健康检查未通过')
    expect(text).toContain('限流')
    expect(text).toContain('诊断：unknown [redacted]')
    expect(text).not.toContain('sk-ant-secret-token')
  })

  it('scrubs raw sensitive backend display fields before rendering them in the DOM', async () => {
    const sensitiveFragments = [
      'sk-ant-raw-secret-DO-NOT-LEAK',
      'sk-ant-sid-raw-secret-DO-NOT-LEAK',
      'operator@example.com',
      '123e4567-e89b-12d3-a456-426614174000',
      'raw_prompt',
      'raw_body',
      'raw_cch',
      'raw_telemetry',
      'user:proxy-pass@',
      'secret-password',
      'abcdef0123456789abcdef0123456789',
    ]
    const wrapper = await mountWithFixture([
      accountFixture({
        account_id: 61,
        account_label: 'label sk-ant-raw-secret-DO-NOT-LEAK operator@example.com',
        state: 'unknown_state' as FormalPoolDashboardState,
        state_label: '状态 raw_prompt 123e4567-e89b-12d3-a456-426614174000',
        last_failure_code: 'failure raw_body sk-ant-sid-raw-secret-DO-NOT-LEAK',
        last_failure_bucket: 'bucket raw_cch http://user:proxy-pass@example.net:8080',
        recommendation: {
          label: '建议 raw_telemetry password=secret-password',
          detail: '详情 Bearer abcdef0123456789abcdef0123456789',
          action_kind: 'none',
        },
      }),
    ])

    await wrapper.find('[data-testid^="expand-"]').trigger('click')
    const html = wrapper.html()
    for (const fragment of sensitiveFragments) {
      expect(html).not.toContain(fragment)
    }
    expect(html).toContain('[redacted]')
  })



  it('uses safe row DOM refs and labels without raw account IDs', async () => {
    const rawAccountId = 987654321
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: rawAccountId, account_label: `账号 #${rawAccountId}` }),
    ])

    const html = wrapper.html()
    const rows = wrapper.findAll('tr[data-bucket]')
    expect(rows).toHaveLength(1)
    expect(rows[0].attributes('data-account-row')).not.toContain(String(rawAccountId))
    expect(rows[0].attributes('data-testid')).not.toContain(String(rawAccountId))
    expect(html).not.toContain(`row-${rawAccountId}`)
    expect(html).not.toContain(`expand-${rawAccountId}`)
    expect(html).not.toContain(`drawer-${rawAccountId}`)
    expect(html).not.toContain(`账号 #${rawAccountId}`)
    expect(wrapper.text()).not.toContain(`账号 #${rawAccountId}`)
    expect(wrapper.text()).toContain('账号（未命名）')
  })

  it('emits close when the close button is clicked', async () => {
    const wrapper = await mountWithFixture([accountFixture()])
    await wrapper.find('[data-testid="dashboard-v2-close"]').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})
