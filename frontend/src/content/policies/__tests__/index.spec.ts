import { describe, expect, it } from 'vitest'
import {
  DEFAULT_POLICY_LOCALE,
  getPolicyDocument,
  listPolicyDocuments,
  normalizePolicyLocale,
  policyDocumentRegistry
} from '@/content/policies'

describe('policy document registry', () => {
  it('normalizes zh and en aliases to canonical locales', () => {
    expect(normalizePolicyLocale('zh')).toBe('zh-CN')
    expect(normalizePolicyLocale('zh-Hant')).toBe('zh-CN')
    expect(normalizePolicyLocale('en')).toBe('en-US')
    expect(normalizePolicyLocale('EN-GB')).toBe('en-US')
  })

  it('falls back to zh-CN for unsupported locales', () => {
    expect(normalizePolicyLocale('fr-FR')).toBe(DEFAULT_POLICY_LOCALE)

    const document = getPolicyDocument('fr-FR', 'terms')
    expect(document.locale).toBe(DEFAULT_POLICY_LOCALE)
    expect(document.content).toContain('协议')
  })

  it('loads markdown content from the registry', () => {
    const document = getPolicyDocument('en-US', 'terms')

    expect(document.locale).toBe('en-US')
    expect(document.key).toBe('terms')
    expect(document.content).toContain('Terms')
    expect(document.isEmpty).toBe(false)
    expect(document.hidden).toBe(false)
  })

  it('marks blank markdown documents as empty and hidden', () => {
    const document = getPolicyDocument('en-US', 'notice')

    expect(document.locale).toBe('en-US')
    expect(document.key).toBe('notice')
    expect(document.content.trim()).toBe('')
    expect(document.isEmpty).toBe(true)
    expect(document.hidden).toBe(true)
  })

  it('lists all policy documents for a locale in a stable order', () => {
    const documents = listPolicyDocuments('zh')

    expect(documents.map((doc) => doc.key)).toEqual(['terms', 'privacy', 'notice'])
    expect(documents.every((doc) => doc.locale === 'zh-CN')).toBe(true)
    expect(documents.every((doc) => doc === policyDocumentRegistry['zh-CN'][doc.key])).toBe(true)
  })
})
