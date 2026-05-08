import type { LocationQuery, LocationQueryValue } from 'vue-router'

type AugmentQuickLoginGrantResponseLike = Record<string, unknown>
type AugmentQuickLoginDiagnosticsPayload = Record<string, string>

function toSingleQueryValue(
  value: LocationQueryValue | LocationQueryValue[] | undefined
): string | undefined {
  if (Array.isArray(value)) {
    return value.find((item): item is string => typeof item === 'string' && item.length > 0)
  }

  return typeof value === 'string' && value.length > 0 ? value : undefined
}

export function buildAugmentQuickLoginGrantPayload(query: LocationQuery): Record<string, string> {
  const payload: Record<string, string> = {}

  Object.entries(query).forEach(([key, value]) => {
    const normalizedValue = toSingleQueryValue(value)
    if (normalizedValue) {
      payload[key] = normalizedValue
    }
  })

  if (!payload.mode) {
    payload.mode = 'official_passthrough'
  }

  return payload
}

function payloadHasOfficialSessionContext(payload: Record<string, string>): boolean {
  return Boolean(
    payload.official_session_bundle ||
      payload.official_access_token
  )
}

export function resolveAugmentQuickLoginDeeplink(
  response: AugmentQuickLoginGrantResponseLike
): string {
  const candidates = [
    response.vscode_deeplink,
    response.deeplink,
    response.deeplink_url,
    response.url,
  ]

  const deeplink = candidates.find(
    (candidate): candidate is string => typeof candidate === 'string' && candidate.trim().length > 0
  )

  return deeplink?.trim() || ''
}

export function summarizeAugmentQuickLoginDiagnostics(
  payload: AugmentQuickLoginDiagnosticsPayload
): Array<[string, string]> {
  const safeDiagnosticKeys = new Set([
    'mode',
    'source',
    'tenant_url',
    'official_tenant_url',
    'tenant_origin',
    'expires_at',
    'official_expires_at',
    'status',
  ])

  return Object.entries(payload)
    .filter(([key, value]) => safeDiagnosticKeys.has(key) && typeof value === 'string' && value.length > 0)
    .slice(0, 8)
}

export function isAugmentLocalCompatGateEnabled(input: {
  isAdmin: boolean
  query: LocationQuery
}): boolean {
  if (!input.isAdmin) {
    return false
  }
  return toSingleQueryValue(input.query.emergency_local_compat) === '1'
}
