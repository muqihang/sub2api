import { describe, expect, it } from 'vitest'
import {
  buildAugmentQuickLoginGrantPayload,
  isAugmentLocalCompatGateEnabled,
  resolveAugmentQuickLoginDeeplink,
  summarizeAugmentQuickLoginDiagnostics,
} from '@/utils/augmentQuickLogin'

describe('buildAugmentQuickLoginGrantPayload', () => {
  it('flattens route query values into string payload fields', () => {
    expect(
      buildAugmentQuickLoginGrantPayload({
        code: 'auth-code',
        state: ['state-1', 'state-2'],
        empty: null,
      })
    ).toEqual({
      code: 'auth-code',
      state: 'state-1',
      mode: 'official_passthrough',
    })
  })

  it('defaults production users to official passthrough mode', () => {
    expect(buildAugmentQuickLoginGrantPayload({})).toEqual({
      mode: 'official_passthrough',
    })
  })

  it('defaults to official passthrough mode when official session context is present', () => {
    expect(
      buildAugmentQuickLoginGrantPayload({
        official_session_bundle: '{"access_token":"official"}',
      })
    ).toEqual({
      official_session_bundle: '{"access_token":"official"}',
      mode: 'official_passthrough',
    })
  })

  it('keeps generic session fields in local compat mode when no official fields are present', () => {
    expect(
      buildAugmentQuickLoginGrantPayload({
        session_bundle: '{"access_token":"generic"}',
        access_token: 'generic-access',
      })
    ).toEqual({
      session_bundle: '{"access_token":"generic"}',
      access_token: 'generic-access',
      mode: 'official_passthrough',
    })
  })
})

describe('summarizeAugmentQuickLoginDiagnostics', () => {
  it('never returns secret field names in diagnostics', () => {
    expect(
      summarizeAugmentQuickLoginDiagnostics({
        mode: 'official_passthrough',
        tenant_url: 'https://tenant.local',
        access_token: 'raw-access-token',
        refresh_token: 'raw-refresh-token',
        official_session_bundle: '{"access_token":"bundle-secret"}',
        bind_token: 'bind-secret',
      })
    ).toEqual([
      ['mode', 'official_passthrough'],
      ['tenant_url', 'https://tenant.local'],
    ])
  })
})

describe('isAugmentLocalCompatGateEnabled', () => {
  it('requires both admin access and an explicit emergency flag', () => {
    expect(
      isAugmentLocalCompatGateEnabled({
        isAdmin: false,
        query: { emergency_local_compat: '1' },
      })
    ).toBe(false)

    expect(
      isAugmentLocalCompatGateEnabled({
        isAdmin: true,
        query: {},
      })
    ).toBe(false)

    expect(
      isAugmentLocalCompatGateEnabled({
        isAdmin: true,
        query: { emergency_local_compat: '1' },
      })
    ).toBe(true)
  })
})

describe('resolveAugmentQuickLoginDeeplink', () => {
  it('prefers explicit VS Code deeplink fields from the grant response', () => {
    expect(
      resolveAugmentQuickLoginDeeplink({
        vscode_deeplink: 'vscode://augment/quick-login?token=abc',
        deeplink: 'vscode://augment/fallback',
      })
    ).toBe('vscode://augment/quick-login?token=abc')
  })

  it('falls back to other deeplink keys when needed', () => {
    expect(
      resolveAugmentQuickLoginDeeplink({
        deeplink_url: 'vscode://augment/quick-login?token=xyz',
      })
    ).toBe('vscode://augment/quick-login?token=xyz')
  })
})
