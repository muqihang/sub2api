export type LatencySeverity = 'good' | 'warn' | 'slow' | 'critical'

export const FIRST_TOKEN_THRESHOLDS_MS = { warn: 10_000, slow: 30_000, critical: 60_000 } as const
export const DURATION_THRESHOLDS_MS = { warn: 60_000, slow: 180_000, critical: 300_000 } as const

interface Thresholds { warn: number; slow: number; critical: number }

const classify = (milliseconds: number, thresholds: Thresholds): LatencySeverity => {
  if (milliseconds >= thresholds.critical) return 'critical'
  if (milliseconds >= thresholds.slow) return 'slow'
  if (milliseconds >= thresholds.warn) return 'warn'
  return 'good'
}

export const firstTokenSeverity = (milliseconds: number): LatencySeverity => classify(milliseconds, FIRST_TOKEN_THRESHOLDS_MS)
export const durationSeverity = (milliseconds: number): LatencySeverity => classify(milliseconds, DURATION_THRESHOLDS_MS)

export const LATENCY_TEXT_CLASSES: Record<LatencySeverity, string> = {
  good: 'text-emerald-600 dark:text-emerald-400',
  warn: 'text-amber-600 dark:text-amber-400',
  slow: 'text-orange-600 dark:text-orange-400',
  critical: 'text-red-600 dark:text-red-400',
}

export const LATENCY_BAR_CLASSES: Record<LatencySeverity, string> = {
  good: 'bg-emerald-500',
  warn: 'bg-amber-400',
  slow: 'bg-orange-500',
  critical: 'bg-red-500',
}
