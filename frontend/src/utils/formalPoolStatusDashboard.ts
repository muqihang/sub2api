import type {
  FormalPoolDashboardState,
  FormalPoolStatusDashboardAccount,
  FormalPoolStatusRuntime,
  FormalPoolStatusWindow
} from '@/types'

const INSUFFICIENT_DATA_TEXT = '数据不足'
const UNCONFIGURED_TEXT = '未配置'

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
