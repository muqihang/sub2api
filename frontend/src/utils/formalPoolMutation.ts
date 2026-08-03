import type { FormalPoolMutationResult, FormalPoolSession } from '@/api/admin/claudeOnboarding'

export interface FormalPoolMutationErrorClassification {
  status: number
  reconcile: boolean
  retainOperationKey: boolean
}

export function classifyFormalPoolMutationError(error: unknown): FormalPoolMutationErrorClassification {
  const candidate = error as { status?: unknown; code?: unknown; response?: { status?: unknown } } | null
  const status = Number(candidate?.response?.status ?? candidate?.status ?? 0)
  const canceled = candidate?.code === 'ERR_CANCELED'
  const ambiguous = !canceled && (status === 0 || status >= 500)
  return {
    status,
    reconcile: status === 409 || ambiguous,
    retainOperationKey: ambiguous,
  }
}

export function monotonicFormalPoolSession(
  current: FormalPoolSession | null,
  next: FormalPoolSession,
): FormalPoolSession {
  if (current && current.id === next.id && next.version < current.version) return current
  return next
}

export function mergeFormalPoolMutationResult(
  current: FormalPoolSession,
  result: FormalPoolMutationResult,
): FormalPoolSession {
  if (result.version < current.version) return current
  return {
    ...current,
    version: result.version,
    status: result.status,
    healthcheck_passed: result.status === 'healthcheck_passed' || current.healthcheck_passed,
  }
}
