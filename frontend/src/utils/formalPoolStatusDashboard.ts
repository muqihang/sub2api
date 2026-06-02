import type {
  FormalPoolDashboardState,
  FormalPoolStatusDashboardAccount,
  FormalPoolStatusRuntime,
  FormalPoolStatusWindow
} from '@/types'

// ─── V2 four-bucket model ────────────────────────────────────────────────────
//
// V2 collapses the 11 backend `FormalPoolDashboardState` values into four
// operator-facing lanes. The mapping is exhaustive over the *known* literals
// via `satisfies Record<KnownDashboardState, FormalPoolFourBucket>`, so adding
// a new known state without an explicit bucket assignment is a compile error.
//
// `FormalPoolDashboardState` is an open string union (`| string`) at the type
// level because the backend may ship intermediate / experimental states ahead
// of frontend updates. For unknown values we deliberately fall back to
// `needs_intervention` rather than `active`, so:
//   - A new "danger-ish" state never gets silently shown as healthy.
//   - Operators see an unfamiliar account in the rose lane and investigate.
//   - V2 visually flags the unknown so we update the literal union next sprint.

export type FormalPoolFourBucket = 'active' | 'paused' | 'needs_intervention' | 'inactive'

export type KnownDashboardState =
  | 'normal' | 'production' | 'warming'
  | 'rate_limited'
  | 'manual_risk' | 'error' | 'quarantined' | 'not_schedulable'
  | 'evidence_missing' | 'data_missing'
  | 'inactive'

export const KNOWN_STATE_BUCKET = {
  normal: 'active',
  production: 'active',
  warming: 'active',
  rate_limited: 'paused',
  manual_risk: 'needs_intervention',
  error: 'needs_intervention',
  quarantined: 'needs_intervention',
  not_schedulable: 'needs_intervention',
  evidence_missing: 'needs_intervention',
  data_missing: 'needs_intervention',
  inactive: 'inactive',
} as const satisfies Record<KnownDashboardState, FormalPoolFourBucket>

const UNKNOWN_STATE_BUCKET: FormalPoolFourBucket = 'needs_intervention'

export function getDashboardBucket(
  state: FormalPoolDashboardState | null | undefined,
): FormalPoolFourBucket {
  if (!state) return UNKNOWN_STATE_BUCKET
  // The literal-union cast is a runtime read; type-level exhaustiveness is
  // already enforced by the `satisfies` above.
  const map = KNOWN_STATE_BUCKET as Record<string, FormalPoolFourBucket | undefined>
  return map[state] ?? UNKNOWN_STATE_BUCKET
}

export function isWarmingState(
  state: FormalPoolDashboardState | null | undefined,
): boolean {
  return state === 'warming'
}

export interface DashboardBucketCounts {
  active: number
  paused: number
  needs_intervention: number
  inactive: number
  total: number
  warming: number
}

export function summarizeBuckets(
  accounts: ReadonlyArray<FormalPoolStatusDashboardAccount>,
): DashboardBucketCounts {
  const counts: DashboardBucketCounts = {
    active: 0,
    paused: 0,
    needs_intervention: 0,
    inactive: 0,
    total: 0,
    warming: 0,
  }
  for (const account of accounts) {
    counts[getDashboardBucket(account.state)] += 1
    counts.total += 1
    if (isWarmingState(account.state)) counts.warming += 1
  }
  return counts
}

const BUCKET_SORT_PRIORITY = {
  needs_intervention: 0,
  paused: 1,
  active: 2,
  inactive: 3,
} as const satisfies Record<FormalPoolFourBucket, number>

export function getDashboardBucketSortKey(
  state: FormalPoolDashboardState | null | undefined,
): number {
  return BUCKET_SORT_PRIORITY[getDashboardBucket(state)]
}

export interface BucketLanePresentation {
  bucket: FormalPoolFourBucket
  label: string
  dotClass: string
  /**
   * Tailwind classes for an inactive segmented lane button.
   */
  laneInactiveClass: string
  /**
   * Tailwind classes for the currently-selected segmented lane button.
   */
  laneActiveClass: string
  /**
   * Left rail visual marker applied to every table row whose bucket matches.
   * Rose for needs_intervention, sky for warming-active, amber for paused,
   * slate-muted for inactive. The warming refinement is applied separately by
   * the V2 modal because warming sits inside the `active` bucket.
   */
  rowRailClass: string
}

const BUCKET_PRESENTATION = {
  needs_intervention: {
    bucket: 'needs_intervention',
    label: '待介入',
    dotClass: 'bg-rose-500',
    laneInactiveClass: 'text-rose-700 hover:bg-rose-50 dark:text-rose-300 dark:hover:bg-rose-900/20',
    laneActiveClass: 'bg-rose-600 text-white shadow-sm dark:bg-rose-500',
    rowRailClass: 'border-l-4 border-rose-500 bg-rose-50/40 dark:border-rose-400 dark:bg-rose-900/10',
  },
  paused: {
    bucket: 'paused',
    label: '暂停',
    dotClass: 'bg-amber-400',
    laneInactiveClass: 'text-amber-700 hover:bg-amber-50 dark:text-amber-300 dark:hover:bg-amber-900/20',
    laneActiveClass: 'bg-amber-500 text-white shadow-sm dark:bg-amber-500',
    rowRailClass: 'border-l-4 border-amber-400 dark:border-amber-500/70',
  },
  active: {
    bucket: 'active',
    label: '能用',
    dotClass: 'bg-emerald-500',
    laneInactiveClass: 'text-emerald-700 hover:bg-emerald-50 dark:text-emerald-300 dark:hover:bg-emerald-900/20',
    laneActiveClass: 'bg-emerald-600 text-white shadow-sm dark:bg-emerald-500',
    rowRailClass: 'border-l-4 border-emerald-500/70 dark:border-emerald-500/60',
  },
  inactive: {
    bucket: 'inactive',
    label: '已停用',
    dotClass: 'bg-slate-400',
    laneInactiveClass: 'text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-dark-800',
    laneActiveClass: 'bg-slate-700 text-white shadow-sm dark:bg-slate-600',
    rowRailClass: 'border-l-4 border-slate-300 dark:border-dark-600',
  },
} as const satisfies Record<FormalPoolFourBucket, BucketLanePresentation>

export const DASHBOARD_BUCKET_ORDER: ReadonlyArray<FormalPoolFourBucket> = [
  'needs_intervention',
  'paused',
  'active',
  'inactive',
]

export function getBucketLanePresentation(
  bucket: FormalPoolFourBucket,
): BucketLanePresentation {
  return BUCKET_PRESENTATION[bucket]
}

/**
 * Warming-specific rail override. Warming sits inside the `active` bucket so
 * it shares the bucket's sort priority, but the V2 design wants warming rows
 * to wear a sky rail and a "预热中 · low weight" label to distinguish from
 * fully-promoted production accounts.
 */
export const WARMING_RAIL_CLASS = 'border-l-4 border-sky-500 bg-sky-50/40 dark:border-sky-400 dark:bg-sky-900/10'
export const WARMING_PRESENTATION_LABEL = '预热中 · low weight'

const INSUFFICIENT_DATA_TEXT = '数据不足'
const UNCONFIGURED_TEXT = '未配置'

const REDACTED_TEXT = '[redacted]'
const DEFAULT_DISPLAY_FALLBACK = '—'

const SCRUB_PATTERNS: ReadonlyArray<RegExp> = [
  /sk-ant(?:-sid)?-[A-Za-z0-9][A-Za-z0-9_-]*/gi,
  /\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b/gi,
  /\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b/gi,
  /\braw_[A-Za-z0-9_:-]*\b/gi,
  /\b(?:bearer|token)\s+[A-Za-z0-9._~+/=-]{16,}\b/gi,
  /\b(?:api[_-]?key|access[_-]?token|refresh[_-]?token|auth[_-]?token|session[_-]?token|password|passwd|pwd|proxy[_-]?password|cch)\s*[:=]\s*[^\s,;]+/gi,
  /\bproxy[-_]?password\b/gi,
  /\b(?:https?|socks5?):\/\/[^\s/@:]+:[^\s/@]+@/gi,
  /\b(?=[A-Za-z0-9_-]{32,}\b)(?=[A-Za-z0-9_-]*\d)(?=[A-Za-z0-9_-]*[A-Za-z])[A-Za-z0-9_-]+\b/g,
]
/**
 * V2 dashboard displays backend-provided diagnostic labels. Treat them as
 * untrusted and redact obvious secrets fail-closed before they reach the DOM.
 */
export function scrubFormalPoolDisplayText(
  value: string | null | undefined,
  fallback = DEFAULT_DISPLAY_FALLBACK,
): string {
  const fallbackText = fallback.trim() || DEFAULT_DISPLAY_FALLBACK
  if (value === null || value === undefined) return fallbackText

  let text = String(value).trim()
  if (!text) return fallbackText

  for (const pattern of SCRUB_PATTERNS) {
    text = text.replace(pattern, REDACTED_TEXT)
  }

  return text.trim() || fallbackText
}


type StatePresentation = {
  className: string
  recommendation: string
}

const STATE_PRESENTATION: Record<string, StatePresentation> = {
  normal: {
    className: 'formal-pool-state formal-pool-state--success',
    recommendation: '正常：账号证据完整且可调度。'
  },
  production: {
    className: 'formal-pool-state formal-pool-state--success',
    recommendation: '生产中：账号证据完整且可调度。'
  },
  warming: {
    className: 'formal-pool-state formal-pool-state--info',
    recommendation: '预热中：继续观察预热进度。'
  },
  rate_limited: {
    className: 'formal-pool-state formal-pool-state--warning',
    recommendation: '限流冷却中：等待恢复后复查。'
  },
  manual_risk: {
    className: 'formal-pool-state formal-pool-state--danger',
    recommendation: '需人工介入：查看具体失败分类后处理。'
  },
  error: {
    className: 'formal-pool-state formal-pool-state--danger',
    recommendation: '错误：需诊断安全失败码和运行证据。'
  },
  quarantined: {
    className: 'formal-pool-state formal-pool-state--danger',
    recommendation: '已隔离：查看隔离原因。'
  },
  inactive: {
    className: 'formal-pool-state formal-pool-state--muted',
    recommendation: '已停用：不参与调度。'
  },
  not_schedulable: {
    className: 'formal-pool-state formal-pool-state--warning',
    recommendation: '不可调度：需查看 gate 原因。'
  },
  evidence_missing: {
    className: 'formal-pool-state formal-pool-state--warning',
    recommendation: '证据不足：需补齐运行注册或健康检查证据。'
  },
  data_missing: {
    className: 'formal-pool-state formal-pool-state--warning',
    recommendation: '数据不足：需补齐运行指标。'
  }
}

function isFiniteNumber(value: number | null | undefined): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function formatNumber(value: number): string {
  if (Number.isInteger(value)) {
    return String(value)
  }
  return value.toFixed(2).replace(/0+$/, '').replace(/\.$/, '')
}

function formatResetTime(value: string | null): string {
  if (!value) {
    return '重置时间未知'
  }

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '重置时间未知'
  }

  return `重置 ${date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })}`
}

export function dashboardRatioToPercent(value: number | null | undefined): number | null {
  if (!isFiniteNumber(value)) {
    return null
  }
  return Math.max(0, Math.min(100, value * 100))
}

export function formatDashboardPercent(value: number | null | undefined): string {
  const percent = dashboardRatioToPercent(value)
  if (percent === null) {
    return INSUFFICIENT_DATA_TEXT
  }
  return `${percent.toFixed(1)}%`
}

export function formatFiveHourWindow(window: FormalPoolStatusWindow | null | undefined): string {
  if (!window?.available) {
    return INSUFFICIENT_DATA_TEXT
  }

  const percent = formatDashboardPercent(window.utilization)
  return `已用 ${formatNumber(window.used)} / ${formatNumber(window.limit)}，剩余 ${formatNumber(window.remaining)}（${percent}），${formatResetTime(window.reset_at)}`
}

export function formatRpmText(runtime: FormalPoolStatusRuntime | null | undefined): string {
  if (!runtime?.available) {
    return INSUFFICIENT_DATA_TEXT
  }
  if (runtime.limit <= 0) {
    return `${UNCONFIGURED_TEXT} RPM`
  }

  return `${runtime.current} / ${runtime.limit} RPM (${formatDashboardPercent(runtime.utilization)})`
}

export function formatConcurrencyText(runtime: FormalPoolStatusRuntime | null | undefined): string {
  if (!runtime?.available) {
    return INSUFFICIENT_DATA_TEXT
  }
  if (runtime.limit <= 0) {
    return `${UNCONFIGURED_TEXT} 并发`
  }

  return `${runtime.current} / ${runtime.limit} 并发 (${formatDashboardPercent(runtime.utilization)})`
}

export function getDashboardStateClass(state: FormalPoolDashboardState | null | undefined): string {
  if (!state) {
    return 'formal-pool-state formal-pool-state--unknown'
  }
  return STATE_PRESENTATION[state]?.className ?? 'formal-pool-state formal-pool-state--unknown'
}

export function getDashboardRecommendationText(account: FormalPoolStatusDashboardAccount): string {
  return STATE_PRESENTATION[account.state]?.recommendation ?? '数据不足：状态未知，需补齐数据。'
}
