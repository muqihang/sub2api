import { describe, expect, it } from 'vitest'
import {
  buildAugmentQuickLoginGrantPayload,
  resolveAugmentQuickLoginDeeplink,
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
      mode: 'local_compat',
    })
  })

  it('defaults to local compat mode when no official session context is present', () => {
    expect(buildAugmentQuickLoginGrantPayload({})).toEqual({
      mode: 'local_compat',
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
      mode: 'local_compat',
    })
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
