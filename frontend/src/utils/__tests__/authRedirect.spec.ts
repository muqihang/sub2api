import { describe, expect, it } from 'vitest'
import { sanitizeInternalRedirect } from '@/utils/authRedirect'

describe('sanitizeInternalRedirect', () => {
  it('keeps plugin quick login redirects with query params intact', () => {
    expect(
      sanitizeInternalRedirect('/plugin/augment/quick-login?code=abc&state=xyz')
    ).toBe('/plugin/augment/quick-login?code=abc&state=xyz')
  })

  it('falls back for external redirects', () => {
    expect(sanitizeInternalRedirect('https://example.com')).toBe('/dashboard')
    expect(sanitizeInternalRedirect('//example.com/plugin/augment/quick-login')).toBe('/dashboard')
  })

  it('falls back for missing redirects', () => {
    expect(sanitizeInternalRedirect(undefined)).toBe('/dashboard')
    expect(sanitizeInternalRedirect('')).toBe('/dashboard')
  })
})
