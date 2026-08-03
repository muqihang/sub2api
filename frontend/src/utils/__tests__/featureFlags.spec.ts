import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

import { useAppStore } from '@/stores/app'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import type { PublicSettings } from '@/types'

// Match app.spec.ts to keep the store importable without hitting real APIs.
vi.mock('@/api/admin/system', () => ({
  checkUpdates: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  getPublicSettings: vi.fn(),
}))

function setCachedSettings(partial: Partial<PublicSettings>): void {
  const store = useAppStore()
  store.cachedPublicSettings = partial as PublicSettings
}

describe('FeatureFlags.newAccountManagement', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.unstubAllEnvs()
  })

  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('is registered as opt-in with use_new_account_management_ux as its key', () => {
    expect(FeatureFlags.newAccountManagement.key).toBe(
      'use_new_account_management_ux',
    )
    expect(FeatureFlags.newAccountManagement.mode).toBe('opt-in')
    expect(FeatureFlags.newAccountManagement.devOverrideEnvKey).toBe(
      'VITE_NEW_ACCOUNT_UX',
    )
  })

  it('returns false when public settings have not loaded (opt-in fallback)', () => {
    useAppStore() // activate empty pinia store
    expect(isFeatureFlagEnabled(FeatureFlags.newAccountManagement)).toBe(false)
  })

  it('returns true when cachedPublicSettings.use_new_account_management_ux is true', () => {
    setCachedSettings({ use_new_account_management_ux: true })
    expect(isFeatureFlagEnabled(FeatureFlags.newAccountManagement)).toBe(true)
  })

  it('returns false when cachedPublicSettings.use_new_account_management_ux is false', () => {
    setCachedSettings({ use_new_account_management_ux: false })
    expect(isFeatureFlagEnabled(FeatureFlags.newAccountManagement)).toBe(false)
  })

  it("DEV override VITE_NEW_ACCOUNT_UX='true' force-enables regardless of settings", () => {
    // Vitest runs in dev mode by default (import.meta.env.DEV === true), so the
    // override branch is reachable. In a production build it is statically
    // tree-shaken out.
    vi.stubEnv('VITE_NEW_ACCOUNT_UX', 'true')
    setCachedSettings({ use_new_account_management_ux: false })

    expect(isFeatureFlagEnabled(FeatureFlags.newAccountManagement)).toBe(true)
  })

  it("DEV override VITE_NEW_ACCOUNT_UX='false' force-disables regardless of settings", () => {
    vi.stubEnv('VITE_NEW_ACCOUNT_UX', 'false')
    setCachedSettings({ use_new_account_management_ux: true })

    expect(isFeatureFlagEnabled(FeatureFlags.newAccountManagement)).toBe(false)
  })

  it("DEV override with an unrelated value (e.g. '1') falls through to settings", () => {
    vi.stubEnv('VITE_NEW_ACCOUNT_UX', '1')
    setCachedSettings({ use_new_account_management_ux: true })

    expect(isFeatureFlagEnabled(FeatureFlags.newAccountManagement)).toBe(true)
  })
})
