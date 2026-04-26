import type { LocationQuery, LocationQueryValue } from 'vue-router'

type AugmentQuickLoginGrantResponseLike = Record<string, unknown>

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
    payload.mode = payloadHasOfficialSessionContext(payload)
      ? 'official_passthrough'
      : 'local_compat'
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
