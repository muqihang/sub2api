import { describe, expect, it } from 'vitest'

import {
  buildImportedAccountName,
  findExistingOpenAIAccountByRefreshToken,
  findExistingOpenAIAccountByAccessToken,
  normalizeOpenAIRTInput,
  parseOpenAIAccessTokenInput
} from '../openaiRTImport.ts'

describe('openaiRTImport', () => {
  it('normalizeOpenAIRTInput deduplicates while preserving order', () => {
    const raw = '\nrt_1\nrt_2\nrt_1\n  rt_3  \n'
    expect(normalizeOpenAIRTInput(raw)).toEqual(['rt_1', 'rt_2', 'rt_3'])
  })

  it('findExistingOpenAIAccountByRefreshToken finds matching account', () => {
    const account = findExistingOpenAIAccountByRefreshToken(
      [
        { id: 1, credentials: { refresh_token: 'rt_old' } },
        { id: 2, credentials: { refresh_token: 'rt_target' } }
      ],
      'rt_target'
    )
    expect(account?.id).toBe(2)
  })

  it('findExistingOpenAIAccountByAccessToken finds matching account', () => {
    const account = findExistingOpenAIAccountByAccessToken(
      [
        { id: 1, credentials: { access_token: 'at_old' } },
        { id: 2, credentials: { access_token: 'at_target' } }
      ],
      'at_target'
    )
    expect(account?.id).toBe(2)
  })

  it('parseOpenAIAccessTokenInput supports AT-only and AT+RT lines', () => {
    const raw = '\nat_1\nat_2----rt_2\nat_1\n  at_3 ---- rt_3  \n'
    expect(parseOpenAIAccessTokenInput(raw)).toEqual([
      { accessToken: 'at_1', refreshToken: '' },
      { accessToken: 'at_2', refreshToken: 'rt_2' },
      { accessToken: 'at_3', refreshToken: 'rt_3' }
    ])
  })

  it('buildImportedAccountName prefers email', () => {
    const name = buildImportedAccountName(
      { email: 'alice@example.com', chatgpt_account_id: 'acct_1' },
      3,
      new Date('2026-04-04T12:30:00Z')
    )
    expect(name).toBe('alice@example.com')
  })

  it('buildImportedAccountName falls back to deterministic timestamped name', () => {
    const name = buildImportedAccountName({}, 3, new Date('2026-04-04T12:30:00Z'))
    expect(name).toBe('openai-oauth-20260404-123000-03')
  })
})
