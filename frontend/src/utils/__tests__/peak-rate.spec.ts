import { describe, expect, it } from 'vitest'

import { formatPeakRateWindow, peakRateMultiplierLabel } from '@/utils/peak-rate'

describe('peak-rate display helpers', () => {
  it('formats enabled peak windows with server UTC offset', () => {
    expect(formatPeakRateWindow({
      peak_rate_enabled: true,
      peak_start: '09:30',
      peak_end: '11:00',
      peak_rate_multiplier: 1.5,
    }, '+08:00')).toBe('09:30-11:00 (UTC+08:00)')
  })

  it('does not format disabled or incomplete peak windows', () => {
    expect(formatPeakRateWindow({ peak_rate_enabled: false, peak_start: '09:30', peak_end: '11:00', peak_rate_multiplier: 1.5 }, '+08:00')).toBe('')
    expect(formatPeakRateWindow({ peak_rate_enabled: true, peak_start: '', peak_end: '11:00', peak_rate_multiplier: 1.5 }, '+08:00')).toBe('')
  })

  it('formats multiplier without applying locale-specific raw data leakage', () => {
    expect(peakRateMultiplierLabel(1.5)).toBe('×1.5')
    expect(peakRateMultiplierLabel(2)).toBe('×2')
  })
})
