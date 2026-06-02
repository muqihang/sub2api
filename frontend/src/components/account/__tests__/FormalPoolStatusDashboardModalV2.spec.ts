import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import FormalPoolStatusDashboardModalV2 from '../FormalPoolStatusDashboardModalV2.vue'
import type {
  FormalPoolDashboardState,
  FormalPoolStatusDashboard,
  FormalPoolStatusDashboardAccount,
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
    },
  }
}

async function mountWithFixture(
  accounts: FormalPoolStatusDashboardAccount[],
  show = true,
) {
  getFormalPoolStatusDashboard.mockResolvedValue(buildDashboard(accounts))
  const wrapper = mount(FormalPoolStatusDashboardModalV2, {
    props: { show },
  })
  await flushPromises()
  return wrapper
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

  it('uses exactly 5 primary table columns', async () => {
    const wrapper = await mountWithFixture([accountFixture()])
    const columnHeaders = wrapper.findAll('[data-testid^="column-"]')
    expect(columnHeaders).toHaveLength(5)
  })

  it('pins needs-intervention rows above active/paused/inactive and gives them a rose rail', async () => {
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
    expect(rows[0].classes().join(' ')).toContain('rose')
  })

  it('renders the warming row with a sky rail and the "预热中 · low weight" copy', async () => {
    const wrapper = await mountWithFixture([
      accountFixture({ account_id: 21, state: 'warming', state_label: '预热中' }),
    ])

    const warmingRow = wrapper.find('tr[data-bucket="active"]')
    expect(warmingRow.exists()).toBe(true)
    expect(warmingRow.attributes('data-warming')).toBe('true')
    expect(warmingRow.classes().join(' ')).toContain('sky')

    const label = warmingRow.find('[data-testid="warming-presentation-label"]')
    expect(label.exists()).toBe(true)
    expect(label.text()).toContain('预热中')
    expect(label.text()).toContain('low weight')
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
