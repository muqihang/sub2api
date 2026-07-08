import { describe, expect, it } from 'vitest'
import {
  HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY,
  HEADER_OVERRIDES_CREDENTIAL_KEY,
  applyHeaderOverride,
  buildHeaderOverridesObject,
  getHeaderOverrideTemplate,
  isHeaderOverridePlatform,
  splitHeaderOverridesObject,
  validateHeaderOverrideRows
} from '../credentialsBuilder'

describe('header override credential helpers', () => {
  it('only enables Anthropic and OpenAI API-key platforms in UI helpers', () => {
    expect(isHeaderOverridePlatform('anthropic')).toBe(true)
    expect(isHeaderOverridePlatform('openai')).toBe(true)
    expect(isHeaderOverridePlatform('gemini')).toBe(false)
    expect(isHeaderOverridePlatform('grok')).toBe(false)
    expect(isHeaderOverridePlatform('antigravity')).toBe(false)
  })

  it('validates names, blocked security/session headers, duplicates and value limits', () => {
    expect(validateHeaderOverrideRows([{ name: 'user-agent', value: 'ua' }])).toBeNull()
    expect(validateHeaderOverrideRows([{ name: '', value: 'v' }])).toBe('invalidName')
    expect(validateHeaderOverrideRows([{ name: 'bad name', value: '' }])).toBe('invalidName')
    expect(validateHeaderOverrideRows([{ name: 'Authorization', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'Content-Type', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'Cookie', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'x-goog-api-key', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'Sec-WebSocket-Key', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'X-Claude-Code-Session-Id', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'x-client-request-id', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'x-app', value: 'bad\nvalue' }])).toBe('invalidValue')
    expect(validateHeaderOverrideRows([{ name: 'x-app', value: '测'.repeat(3000) }])).toBe('invalidValue')
    expect(
      validateHeaderOverrideRows([
        { name: 'User-Agent', value: 'a' },
        { name: 'user-agent', value: 'b' }
      ])
    ).toBe('duplicateName')
    expect(validateHeaderOverrideRows(Array.from({ length: 65 }, (_, i) => ({ name: `x-h-${i}`, value: 'v' })))).toBe('tooManyEntries')
  })

  it('builds, splits and applies credential objects with normalized names', () => {
    const rows = [
      { name: ' User-Agent ', value: ' ua ' },
      { name: 'X-App', value: '' },
      { name: '', value: '' }
    ]
    expect(buildHeaderOverridesObject(rows)).toEqual({ 'user-agent': 'ua', 'x-app': '' })
    expect(splitHeaderOverridesObject({ 'x-app': 'cli', 'user-agent': 'ua', ignored: 42 })).toEqual([
      { name: 'user-agent', value: 'ua' },
      { name: 'x-app', value: 'cli' }
    ])

    const creds: Record<string, unknown> = { api_key: 'sk' }
    applyHeaderOverride(creds, true, rows, 'create')
    expect(creds[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY]).toBe(true)
    expect(creds[HEADER_OVERRIDES_CREDENTIAL_KEY]).toEqual({ 'user-agent': 'ua', 'x-app': '' })
    applyHeaderOverride(creds, false, [], 'edit')
    expect(HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY in creds).toBe(false)
    expect(HEADER_OVERRIDES_CREDENTIAL_KEY in creds).toBe(false)
  })

  it('provides safe empty templates for Anthropic and OpenAI', () => {
    const anthropicNames = getHeaderOverrideTemplate('anthropic').map((row) => row.name)
    expect(anthropicNames).toContain('anthropic-beta')
    expect(anthropicNames).toContain('x-stainless-lang')
    expect(validateHeaderOverrideRows(getHeaderOverrideTemplate('anthropic'))).toBeNull()

    const openAINames = getHeaderOverrideTemplate('openai').map((row) => row.name)
    expect(openAINames).toContain('openai-beta')
    expect(openAINames).toContain('originator')
    expect(validateHeaderOverrideRows(getHeaderOverrideTemplate('openai'))).toBeNull()
  })
})
