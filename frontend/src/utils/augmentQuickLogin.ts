import type { LocationQuery, LocationQueryValue } from 'vue-router'
import {
  AUGMENT_QUICK_LOGIN_EDITOR_TARGET_STORAGE_KEY,
  isAugmentQuickLoginEditorTarget,
  type AugmentQuickLoginEditorTarget,
} from '@/utils/augmentIdeTargets'

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

export function resolveAugmentQuickLoginEditorTarget(input: {
  mode: string
  query: LocationQuery
  backendDefaultTarget?: AugmentQuickLoginEditorTarget
}): AugmentQuickLoginEditorTarget {
  if (input.mode !== 'official_passthrough') {
    return 'vscode'
  }

  const queryTarget = toSingleQueryValue(input.query.editor_target)
  if (isAugmentQuickLoginEditorTarget(queryTarget)) {
    return queryTarget
  }

  const persistedTarget = getPersistedAugmentQuickLoginEditorTarget()
  if (persistedTarget) {
    return persistedTarget
  }

  return input.backendDefaultTarget ?? 'vscode'
}

export function persistAugmentQuickLoginEditorTarget(target: AugmentQuickLoginEditorTarget): void {
  if (typeof window === 'undefined') {
    return
  }
  window.localStorage.setItem(AUGMENT_QUICK_LOGIN_EDITOR_TARGET_STORAGE_KEY, target)
}

export function getPersistedAugmentQuickLoginEditorTarget(): AugmentQuickLoginEditorTarget | null {
  if (typeof window === 'undefined') {
    return null
  }
  const value = window.localStorage.getItem(AUGMENT_QUICK_LOGIN_EDITOR_TARGET_STORAGE_KEY)
  return isAugmentQuickLoginEditorTarget(value) ? value : null
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

export function shouldAutoLaunchAugmentQuickLogin(
  response: AugmentQuickLoginGrantResponseLike
): boolean {
  if (!resolveAugmentQuickLoginDeeplink(response)) {
    return false
  }
  if (extractAugmentQuickLoginTargetWarning(response)) {
    return false
  }
  if (typeof response.target_verified === 'boolean') {
    return response.target_verified
  }
  return true
}

export function extractAugmentQuickLoginTargetWarning(
  response: AugmentQuickLoginGrantResponseLike
): string {
  const warning = response.target_warning
  return typeof warning === 'string' ? warning.trim() : ''
}

export function summarizeAugmentQuickLoginDiagnostics(
  payload: AugmentQuickLoginDiagnosticsPayload
): Array<[string, string]> {
  const safeDiagnosticKeys = new Set([
    'mode',
    'source',
    'editor_target',
    'target_scheme',
    'target_warning',
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

export function extractAugmentOfficialTenantAllowlist(query: LocationQuery): string[] {
  const tenant = toSingleQueryValue(query.official_tenant_url) || extractTenantURLFromBundle(query)
  return tenant ? [tenant] : []
}

export function buildAugmentOfficialBindPayload(query: LocationQuery): Record<string, unknown> | null {
  const bundled = parseBundleQuery(query)
  if (bundled) {
    return bundled
  }

  const tenantURL = toSingleQueryValue(query.official_tenant_url)
  const accessToken = toSingleQueryValue(query.official_access_token)
  if (!tenantURL || !accessToken) {
    return null
  }

  return {
    tenant_url: tenantURL,
    access_token: accessToken,
    refresh_token: toSingleQueryValue(query.official_refresh_token) || '',
    expires_at: toSingleQueryValue(query.official_expires_at) || '',
    scopes: splitScopes(toSingleQueryValue(query.official_scopes)),
  }
}

function parseBundleQuery(query: LocationQuery): Record<string, unknown> | null {
  const raw = toSingleQueryValue(query.official_session_bundle)
  if (!raw) {
    return null
  }
  try {
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? (parsed as Record<string, unknown>) : null
  } catch {
    return null
  }
}

function extractTenantURLFromBundle(query: LocationQuery): string | undefined {
  const bundled = parseBundleQuery(query)
  const tenant = bundled?.tenant_url
  return typeof tenant === 'string' && tenant.length > 0 ? tenant : undefined
}

function splitScopes(raw: string | undefined): string[] {
  if (!raw) {
    return []
  }
  return raw
    .split(/[,\s]+/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}
