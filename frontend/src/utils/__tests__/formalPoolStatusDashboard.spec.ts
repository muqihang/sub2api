import { describe, expect, it } from 'vitest'
import type { FormalPoolDashboardState, FormalPoolStatusDashboardAccount } from '@/types'
import {
  DASHBOARD_BUCKET_ORDER,
  KNOWN_STATE_BUCKET,
  WARMING_PRESENTATION_LABEL,
  WARMING_RAIL_CLASS,
  formatConcurrencyText,
  formatDashboardPercent,
  formatFiveHourWindow,
  formatFormalPoolRecentSuccessHint,
  formatFormalPoolRecentFailureText,
  formatRpmText,
  getBucketLanePresentation,
  getDashboardBucket,
  getDashboardBucketSortKey,
  getDashboardRecommendationText,
  getDashboardStateClass,
  isWarmingState,
  safeFormalPoolOperatorLabel,
  scrubFormalPoolDisplayText,
  summarizeBuckets,
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
      utilization: 0.25,
      reset_at: '2026-06-01T18:30:00Z',
      status: 'allowed',
      available: true
    },
    rpm: {
      current: 12,
      limit: 60,
      utilization: 0.2,
      available: true
    },
    concurrency: {
      current: 2,
      limit: 5,
      utilization: 0.4,
      available: true
    },
    sessions: {
      current: 1,
      limit: 3,
      utilization: 0.333333,
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
    expect(formatDashboardPercent(0.12345)).toBe('12.3%')

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
          utilization: 0.4,
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
        state_label: '需人工介入',
        recommendation: {
          label: '人工介入',
          detail: '需人工介入：查看具体失败分类后处理。',
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
      utilization: 0.25,
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
    expect(formatRpmText({ current: 12, limit: 60, utilization: 0.2, available: true })).toBe('12 / 60 RPM (20.0%)')
    expect(formatConcurrencyText({ current: 2, limit: 5, utilization: 0.4, available: true })).toBe('2 / 5 并发 (40.0%)')
    expect(formatRpmText({ current: 0, limit: 60, utilization: null, available: false })).toBe('数据不足')
    expect(formatConcurrencyText({ current: 0, limit: 5, utilization: null, available: false })).toBe('数据不足')
  })

  it('maps state classes without reclassifying backend state', () => {
    expect(getDashboardStateClass('production')).toContain('success')
    expect(getDashboardStateClass('rate_limited')).toContain('warning')
    expect(getDashboardStateClass('manual_risk')).toContain('danger')
    expect(getDashboardStateClass('unknown')).toContain('unknown')
  })

  it('does not emit synthetic sensitive strings from derived helpers', () => {
    const outputs = [
      formatDashboardPercent(null),
      formatFiveHourWindow({ used: 0, limit: 0, remaining: 0, utilization: null, reset_at: null, status: 'unavailable', available: false }),
      formatRpmText({ current: 0, limit: 0, utilization: null, available: false }),
      formatConcurrencyText({ current: 0, limit: 0, utilization: null, available: false }),
      getDashboardStateClass('unknown'),
      getDashboardRecommendationText(accountFixture({ state: 'unknown', state_label: '' }))
    ].join('\n')

    expect(outputs).not.toContain('sk-ant-')
    expect(outputs).not.toContain('raw_prompt')
  })

  it('keeps ordinary operator labels including IP suffixes while failing closed on mixed secret content', () => {
    const fallback = '账号（未命名）'
    expect(safeFormalPoolOperatorLabel('ops-user@example.com', fallback)).toBe('ops-user@example.com')
    expect(safeFormalPoolOperatorLabel('Claude Ops Main', fallback)).toBe('Claude Ops Main')
    expect(safeFormalPoolOperatorLabel('anthropic-setup-204.1.108.104', fallback)).toBe('anthropic-setup-204.1.108.104')
    expect(safeFormalPoolOperatorLabel('疑似限额-anthropic-setup-207.97.155.20', fallback)).toBe('疑似限额-anthropic-setup-207.97.155.20')
    expect(safeFormalPoolOperatorLabel('ops-ipv6-2001:db8::1', fallback)).toBe('ops-ipv6-2001:db8::1')

    const mixed = safeFormalPoolOperatorLabel('ops-user@example.com sk-ant-secret-token', fallback)
    expect(mixed).toBe(fallback)
    expect(mixed).not.toContain('ops-user@example.com')
  })

  it('fails closed when operator labels mix token, URL, user-pass, or host-port proxy shapes', () => {
    const fallback = '账号（未命名）'
    const highRiskLabels = [
      'ops-user@example.com token abcdef0123456789abcdef0123456789',
      'ops-user@example.com socks5://proxy-host:1080',
      'ops-user@example.com https://proxy-host/path',
      'ops-user@example.com proxy-user:proxy-pass@proxy-host',
      'ops-user@example.com proxy-user:proxy-pass@proxy-host:8080',
      'ops-user@example.com proxy-host:1080',
    ]

    for (const label of highRiskLabels) {
      expect(safeFormalPoolOperatorLabel(label, fallback)).toBe(fallback)
    }
  })

  it('formats recent failure codes as Chinese diagnostics with code priority and scrubbing', () => {
    expect(formatFormalPoolRecentFailureText('auth', 'rate_limited')).toContain('授权/登录失败')
    expect(formatFormalPoolRecentFailureText('formal_pool_healthcheck_failed', '')).toContain('健康检查未通过')
    expect(formatFormalPoolRecentFailureText('rate_limited', '')).toContain('限流')
    expect(formatFormalPoolRecentFailureText('status_429', '')).toContain('限流')
    expect(formatFormalPoolRecentFailureText('', 'status_429')).toContain('限流')

    const unknown = formatFormalPoolRecentFailureText('', 'custom sk-ant-secret-token')
    expect(unknown).toBe('未知错误，需查看诊断')
    expect(unknown).not.toContain('sk-ant-secret-token')

    const bareCredential = formatFormalPoolRecentFailureText('', 'user:pass@proxy-host:8080')
    expect(bareCredential).toBe('未知错误，需查看诊断')
    expect(bareCredential).not.toContain('user:pass')
  })

  it('maps common recent failure codes to operator Chinese without leaking raw codes', () => {
    const cases: Array<[string, string]> = [
      ['5h', '5 小时额度'],
      ['7d', '7 天额度'],
      ['fallback', '降级兜底'],
      ['fallback_detected', '降级兜底'],
      ['raw_capture_missing', '采集证据缺失'],
      ['cc_gateway_not_seen', '网关运行证据缺失'],
      ['invalid_grant', '授权已失效'],
      ['refresh_token_invalid', '刷新令牌失效'],
      ['setup_token_expired', 'Setup Token 已过期'],
      ['session_expired', '登录会话已过期'],
      ['status_401', '授权/登录失败'],
      ['401', '授权/登录失败'],
      ['status_429', '限流中'],
      ['429', '限流中'],
      ['status_403', '上游风控或权限异常'],
      ['403', '上游风控或权限异常'],
      ['runtime_register', '调度器接入'],
      ['runtime_registered', '调度器接入'],
      ['not_schedulable', '调度门禁'],
      ['gate', '调度门禁'],
    ]

    for (const [code, expected] of cases) {
      const text = formatFormalPoolRecentFailureText(code, '')
      expect(text).toContain(expected)
      expect(text).not.toBe('未知错误，需查看诊断')
      expect(text.toLowerCase()).not.toContain(code.toLowerCase())
    }
  })

  it('formats parseable recent success hints as zh-CN local time and scrubs non-date hints', () => {
    const iso = '2026-06-01T14:32:08Z'
    const expected = new Date(iso).toLocaleString('zh-CN', { hour12: false })

    expect(formatFormalPoolRecentSuccessHint(iso)).toBe(expected)
    expect(formatFormalPoolRecentSuccessHint('14:32:01')).toBe('14:32:01')
    expect(formatFormalPoolRecentSuccessHint('raw_body sk-ant-secret-token')).toBe('敏感信息已隐藏 敏感信息已隐藏')
  })

  it('scrubs formal pool display text fail-closed while preserving safe operator copy', () => {
    expect(scrubFormalPoolDisplayText('保持观察：账号证据完整且可调度。')).toBe('保持观察：账号证据完整且可调度。')
    expect(scrubFormalPoolDisplayText('')).toBe('—')
    expect(scrubFormalPoolDisplayText(null, '未知')).toBe('未知')

    const rawInputs = [
      'account sk-ant-secret-token',
      'session sk-ant-sid-secret-token',
      'email operator@example.com',
      'uuid 123e4567-e89b-12d3-a456-426614174000',
      'payload raw_prompt raw_body raw_cch raw_telemetry',
      'proxy http://user:proxy-pass@example.net:8080',
      'proxy password=secret-password',
      'Bearer abcdef0123456789abcdef0123456789'
    ]

    for (const raw of rawInputs) {
      const scrubbed = scrubFormalPoolDisplayText(raw)
      expect(scrubbed).toContain('[redacted]')
      expect(scrubbed).not.toContain('sk-ant-secret-token')
      expect(scrubbed).not.toContain('sk-ant-sid-secret-token')
      expect(scrubbed).not.toContain('operator@example.com')
      expect(scrubbed).not.toContain('123e4567-e89b-12d3-a456-426614174000')
      expect(scrubbed).not.toContain('raw_prompt')
      expect(scrubbed).not.toContain('raw_body')
      expect(scrubbed).not.toContain('raw_cch')
      expect(scrubbed).not.toContain('raw_telemetry')
      expect(scrubbed).not.toContain('user:proxy-pass@')
      expect(scrubbed).not.toContain('secret-password')
      expect(scrubbed).not.toContain('abcdef0123456789abcdef0123456789')
    }
  })
})

describe('V2 four-bucket mapping', () => {
  it('warming maps to active bucket but is still warming-presented', () => {
    expect(getDashboardBucket('warming')).toBe('active')
    expect(isWarmingState('warming')).toBe(true)
    // warming gets the sky rail / "预热中 · 低权重" treatment overlay on
    // top of the active bucket — never demoted to needs_intervention.
    expect(WARMING_RAIL_CLASS).toContain('sky')
    expect(WARMING_PRESENTATION_LABEL).toContain('预热中')
    expect(WARMING_PRESENTATION_LABEL).toContain('低权重')
  })

  it('normal and production are active', () => {
    expect(getDashboardBucket('normal')).toBe('active')
    expect(getDashboardBucket('production')).toBe('active')
    expect(isWarmingState('normal')).toBe(false)
    expect(isWarmingState('production')).toBe(false)
  })

  it('rate_limited maps to paused', () => {
    expect(getDashboardBucket('rate_limited')).toBe('paused')
  })

  it('manual_risk / error / quarantined / not_schedulable / evidence_missing / data_missing all map to needs_intervention', () => {
    const needsIntervention: ReadonlyArray<FormalPoolDashboardState> = [
      'manual_risk',
      'error',
      'quarantined',
      'not_schedulable',
      'evidence_missing',
      'data_missing',
    ]
    for (const state of needsIntervention) {
      expect(getDashboardBucket(state)).toBe('needs_intervention')
    }
  })

  it('inactive maps to inactive (never active)', () => {
    expect(getDashboardBucket('inactive')).toBe('inactive')
  })

  it('unknown / empty / null states fall back to needs_intervention, never active', () => {
    const fallbacks: ReadonlyArray<FormalPoolDashboardState | null | undefined> = [
      'definitely_not_a_real_state',
      'some_future_backend_state',
      '',
      null,
      undefined,
    ]
    for (const state of fallbacks) {
      expect(getDashboardBucket(state)).toBe('needs_intervention')
      expect(getDashboardBucket(state)).not.toBe('active')
      expect(getDashboardBucket(state)).not.toBe('inactive')
    }
  })

  it('KNOWN_STATE_BUCKET never assigns quarantined/error to active', () => {
    expect(KNOWN_STATE_BUCKET.quarantined).toBe('needs_intervention')
    expect(KNOWN_STATE_BUCKET.error).toBe('needs_intervention')
    expect(KNOWN_STATE_BUCKET.manual_risk).toBe('needs_intervention')
    expect(KNOWN_STATE_BUCKET.quarantined).not.toBe('active')
    expect(KNOWN_STATE_BUCKET.error).not.toBe('active')
  })

  it('getDashboardBucketSortKey pins needs_intervention first, inactive last', () => {
    const key = (s: FormalPoolDashboardState) => getDashboardBucketSortKey(s)
    expect(key('quarantined')).toBeLessThan(key('rate_limited'))
    expect(key('rate_limited')).toBeLessThan(key('production'))
    expect(key('production')).toBeLessThan(key('inactive'))
    // unknown defers to needs_intervention so it bubbles to the top, too.
    expect(key('definitely_not_a_real_state')).toBe(key('quarantined'))
  })

  it('summarizeBuckets accumulates counts including warming separately', () => {
    const fixtures: FormalPoolStatusDashboardAccount[] = [
      accountFixture({ account_id: 1, state: 'production' }),
      accountFixture({ account_id: 2, state: 'warming' }),
      accountFixture({ account_id: 3, state: 'warming' }),
      accountFixture({ account_id: 4, state: 'rate_limited' }),
      accountFixture({ account_id: 5, state: 'quarantined' }),
      accountFixture({ account_id: 6, state: 'error' }),
      accountFixture({ account_id: 7, state: 'inactive' }),
      accountFixture({ account_id: 8, state: 'definitely_not_a_real_state' }),
    ]

    const counts = summarizeBuckets(fixtures)
    expect(counts.total).toBe(8)
    expect(counts.active).toBe(3) // production + 2 warming
    expect(counts.warming).toBe(2)
    expect(counts.paused).toBe(1) // rate_limited
    expect(counts.needs_intervention).toBe(3) // quarantined + error + unknown
    expect(counts.inactive).toBe(1)
  })

  it('DASHBOARD_BUCKET_ORDER pins needs_intervention first and inactive last', () => {
    expect(DASHBOARD_BUCKET_ORDER[0]).toBe('needs_intervention')
    expect(DASHBOARD_BUCKET_ORDER[DASHBOARD_BUCKET_ORDER.length - 1]).toBe('inactive')
  })

  it('getBucketLanePresentation gives needs_intervention a rose rail and dot', () => {
    const preset = getBucketLanePresentation('needs_intervention')
    expect(preset.dotClass).toContain('rose')
    expect(preset.rowRailClass).toContain('rose')
    expect(preset.label).toBe('待介入')
  })

  it('getBucketLanePresentation does not leak sensitive strings from input', () => {
    for (const bucket of DASHBOARD_BUCKET_ORDER) {
      const preset = getBucketLanePresentation(bucket)
      expect(preset.label).not.toContain('sk-ant-')
      expect(preset.dotClass).not.toContain('sk-ant-')
      expect(preset.rowRailClass).not.toContain('sk-ant-')
    }
  })
})
