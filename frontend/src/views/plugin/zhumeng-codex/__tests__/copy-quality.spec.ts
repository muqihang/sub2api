import { describe, it, expect } from 'vitest'

/**
 * Centralized copy-quality test.
 * This is the ONLY place where we scan for forbidden terms.
 * Individual component tests should NOT duplicate this.
 *
 * We import the locale files directly (they're TS modules) and check their content.
 */

// Import locale modules
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

const FORBIDDEN_TERMS = [
  '/codex/v1',
  'internal/',
  'gateway:',
  'manager_version',
  '评审注',
  '底层路由',
  'OpenAI API key',
]

function flattenObject(obj: any, prefix = ''): Record<string, string> {
  const result: Record<string, string> = {}
  for (const key of Object.keys(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key
    if (typeof obj[key] === 'object' && obj[key] !== null) {
      Object.assign(result, flattenObject(obj[key], fullKey))
    } else if (typeof obj[key] === 'string') {
      result[fullKey] = obj[key]
    }
  }
  return result
}

describe('Copy quality — forbidden terms in locale values', () => {
  const zhFlat = flattenObject(zh)
  const enFlat = flattenObject(en)

  for (const term of FORBIDDEN_TERMS) {
    it(`zh locale values do not contain "${term}"`, () => {
      for (const [key, value] of Object.entries(zhFlat)) {
        expect(value, `Key "${key}" contains forbidden term`).not.toContain(term)
      }
    })

    it(`en locale values do not contain "${term}"`, () => {
      for (const [key, value] of Object.entries(enFlat)) {
        expect(value, `Key "${key}" contains forbidden term`).not.toContain(term)
      }
    })
  }
})

describe('Copy quality — i18n parity', () => {
  it('zh and en both have codex section', () => {
    expect(zh).toHaveProperty('codex')
    expect(en).toHaveProperty('codex')
  })

  it('zh and en codex sections have matching top-level keys', () => {
    const zhKeys = Object.keys((zh as any).codex)
    const enKeys = Object.keys((en as any).codex)
    expect(zhKeys.sort()).toEqual(enKeys.sort())
  })

  it('zh and en both have zhumengAgent section in keys', () => {
    expect((zh as any).keys).toHaveProperty('zhumengAgent')
    expect((en as any).keys).toHaveProperty('zhumengAgent')
  })
})
