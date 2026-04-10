import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildImportedAccountName,
  findExistingOpenAIAccountByRefreshToken,
  findExistingOpenAIAccountByAccessToken,
  normalizeOpenAIRTInput,
  parseOpenAIAccessTokenInput
} from '../openaiRTImport.ts'

test('normalizeOpenAIRTInput deduplicates while preserving order', () => {
  const raw = '\nrt_1\nrt_2\nrt_1\n  rt_3  \n'
  assert.deepEqual(normalizeOpenAIRTInput(raw), ['rt_1', 'rt_2', 'rt_3'])
})

test('findExistingOpenAIAccountByRefreshToken finds matching account', () => {
  const account = findExistingOpenAIAccountByRefreshToken(
    [
      { id: 1, credentials: { refresh_token: 'rt_old' } },
      { id: 2, credentials: { refresh_token: 'rt_target' } }
    ],
    'rt_target'
  )
  assert.equal(account?.id, 2)
})

test('findExistingOpenAIAccountByAccessToken finds matching account', () => {
  const account = findExistingOpenAIAccountByAccessToken(
    [
      { id: 1, credentials: { access_token: 'at_old' } },
      { id: 2, credentials: { access_token: 'at_target' } }
    ],
    'at_target'
  )
  assert.equal(account?.id, 2)
})

test('parseOpenAIAccessTokenInput supports AT-only and AT+RT lines', () => {
  const raw = '\nat_1\nat_2----rt_2\nat_1\n  at_3 ---- rt_3  \n'
  assert.deepEqual(parseOpenAIAccessTokenInput(raw), [
    { accessToken: 'at_1', refreshToken: '' },
    { accessToken: 'at_2', refreshToken: 'rt_2' },
    { accessToken: 'at_3', refreshToken: 'rt_3' }
  ])
})

test('buildImportedAccountName prefers email', () => {
  const name = buildImportedAccountName(
    { email: 'alice@example.com', chatgpt_account_id: 'acct_1' },
    3,
    new Date('2026-04-04T12:30:00Z')
  )
  assert.equal(name, 'alice@example.com')
})

test('buildImportedAccountName falls back to deterministic timestamped name', () => {
  const name = buildImportedAccountName({}, 3, new Date('2026-04-04T12:30:00Z'))
  assert.equal(name, 'openai-oauth-20260404-123000-03')
})
