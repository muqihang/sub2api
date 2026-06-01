import { describe, expect, it } from 'vitest'
import type { FormalPoolStatusDashboardAccount } from '@/types'
import {
  formatConcurrencyText,
  formatDashboardPercent,
  formatFiveHourWindow,
  formatRpmText,
  getDashboardRecommendationText,
  getDashboardStateClass
} from '../formalPoolStatusDashboard'

function accountFixture(
  overrides: Partial<FormalPoolStatusDashboardAccount> = {}
): FormalPoolStatusDashboardAccount {
  return {
    account_id: 1,
    account_label: '账号 #1',
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
      used: 1.25,
      limit: 5,
      remaining: 3.75,
      utilization: 25,
      reset_at: '2026-06-01T18:30:00Z',
      status: 'allowed',
      available: true
    },
    rpm: {
      current: 12,
      limit: 60,
      utilization: 20,
      available: true
    },
    concurrency: {
      current: 2,
      limit: 5,
      utilization: 40,
      available: true
    },
    sessions: {
      current: 1,
      limit: 3,
      utilization: 33.3333,
      available: true
    },
    last_used_at: null,
    last_success_hint: null,
    last_failure_code: '',
    last_failure_bucket: '',
    recommendation: {
      label: '保持观察',
      detail: '账号证据完整且可调度。',
      action_kind: 'none'
    },
    ...overrides
  }
}

describe('formalPoolStatusDashboard formatting helpers', () => {
  it('formats null and unknown percentages as insufficient data, never normal', () => {
    expect(formatDashboardPercent(null)).toBe('数据不足')
    expect(formatDashboardPercent(undefined)).toBe('数据不足')
    expect(formatDashboardPercent(Number.NaN)).toBe('数据不足')
    expect(formatDashboardPercent(12.345)).toBe('12.3%')

    const unknownText = getDashboardRecommendationText(
      accountFixture({
        state: 'unexpected_state',
        state_label: '',
        recommendation: { label: '', detail: '', action_kind: '' }
      })
    )
    expect(unknownText).toContain('数据不足')
    expect(unknownText).not.toContain('正常')
  })

  it('formats rate_limited as cooldown/waiting recovery, not quarantine', () => {
    const text = getDashboardRecommendationText(
      accountFixture({
        state: 'rate_limited',
        state_label: '限流冷却中',
        five_hour_window: {
          used: 2,
          limit: 5,
          remaining: 3,
          utilization: 40,
          reset_at: '2026-06-01T18:30:00Z',
          status: 'allowed',
          available: true
        },
        recommendation: {
          label: '等待恢复',
          detail: '限流冷却中，等待 reset 后复查。',
          action_kind: 'wait_rate_limit'
        }
      })
    )

    expect(text).toContain('限流')
    expect(text).toMatch(/等待|恢复|冷却/)
    expect(text).not.toContain('隔离')
  })

  it('formats manual_risk as requiring manual intervention', () => {
    const text = getDashboardRecommendationText(
      accountFixture({
        state: 'manual_risk',
        state_label: '账号风险，需人工介入',
        recommendation: {
          label: '人工介入',
          detail: '账号存在风险信号，请人工检查账号状态。',
          action_kind: 'manual_review'
        }
      })
    )

    expect(text).toContain('需人工介入')
  })

  it('formats data_missing as insufficient data', () => {
    const text = getDashboardRecommendationText(
      accountFixture({
        state: 'data_missing',
        state_label: '数据不足',
        recommendation: {
          label: '补齐数据',
          detail: '运行指标未读到，不能判断正常。',
          action_kind: 'recover_runtime_metrics'
        }
      })
    )

    expect(text).toContain('数据不足')
    expect(text).not.toBe('正常')
  })

  it('does not render inactive or not_schedulable as normal', () => {
    const inactive = getDashboardRecommendationText(
      accountFixture({
        state: 'inactive',
        state_label: '已停用',
        recommendation: { label: '确认停用', detail: '账号已停用，不参与调度。', action_kind: 'confirm_inactive' }
      })
    )
    const notSchedulable = getDashboardRecommendationText(
      accountFixture({
        state: 'not_schedulable',
        state_label: '不可调度',
        effective_schedulable: false,
        recommendation: { label: '查看 gate', detail: '不可调度，检查调度 gate。', action_kind: 'inspect_gate' }
      })
    )

    expect(inactive).toContain('已停用')
    expect(notSchedulable).toContain('不可调度')
    expect(inactive).not.toContain('正常')
    expect(notSchedulable).not.toContain('正常')
  })

  it('formats five-hour used, remaining, and reset time', () => {
    const text = formatFiveHourWindow({
      used: 1.25,
      limit: 5,
      remaining: 3.75,
      utilization: 25,
      reset_at: '2026-06-01T18:30:00Z',
      status: 'allowed',
      available: true
    })

    expect(text).toContain('已用 1.25')
    expect(text).toContain('剩余 3.75')
    expect(text).toContain('25.0%')
    expect(text).toContain('重置')
  })

  it('formats runtime counters defensively when unavailable', () => {
    expect(formatRpmText({ current: 12, limit: 60, utilization: 20, available: true })).toBe('12 / 60 RPM (20.0%)')
    expect(formatConcurrencyText({ current: 2, limit: 5, utilization: 40, available: true })).toBe('2 / 5 并发 (40.0%)')
    expect(formatRpmText({ current: 0, limit: 60, utilization: null, available: false })).toBe('数据不足')
    expect(formatConcurrencyText({ current: 0, limit: 5, utilization: null, available: false })).toBe('数据不足')
  })

  it('maps state classes without reclassifying backend state', () => {
    expect(getDashboardStateClass('production')).toContain('success')
    expect(getDashboardStateClass('rate_limited')).toContain('warning')
    expect(getDashboardStateClass('manual_risk')).toContain('danger')
    expect(getDashboardStateClass('unknown')).toContain('unknown')
  })

  it('does not emit sensitive fixture strings from helpers', () => {
    const sensitive = 'sk-ant-secret user@example.com proxy-password raw_prompt raw_body raw_cch 123e4567-e89b-12d3-a456-426614174000'
    const outputs = [
      formatDashboardPercent(null),
      formatFiveHourWindow({ used: 0, limit: 0, remaining: 0, utilization: null, reset_at: null, status: sensitive, available: false }),
      formatRpmText({ current: 0, limit: 0, utilization: null, available: false }),
      formatConcurrencyText({ current: 0, limit: 0, utilization: null, available: false }),
      getDashboardStateClass(sensitive),
      getDashboardRecommendationText(
        accountFixture({
          account_label: sensitive,
          last_failure_code: sensitive,
          last_failure_bucket: sensitive,
          state: 'unknown',
          state_label: sensitive,
          recommendation: { label: sensitive, detail: sensitive, action_kind: sensitive }
        })
      )
    ].join('\n')

    for (const fragment of ['sk-ant-secret', 'user@example.com', 'proxy-password', 'raw_prompt', 'raw_body', 'raw_cch', '123e4567-e89b-12d3-a456-426614174000']) {
      expect(outputs).not.toContain(fragment)
    }
  })
})
