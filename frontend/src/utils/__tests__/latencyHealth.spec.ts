import { describe, expect, it } from 'vitest'
import { durationSeverity, firstTokenSeverity } from '../latencyHealth'

describe('latency health', () => {
  it('classifies first-token and full-request latency with independent thresholds', () => {
    expect(firstTokenSeverity(9_999)).toBe('good')
    expect(firstTokenSeverity(10_000)).toBe('warn')
    expect(firstTokenSeverity(60_000)).toBe('critical')
    expect(durationSeverity(59_999)).toBe('good')
    expect(durationSeverity(60_000)).toBe('warn')
    expect(durationSeverity(300_000)).toBe('critical')
  })
})
