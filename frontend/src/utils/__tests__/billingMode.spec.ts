import { describe, expect, it } from 'vitest'

import {
  BILLING_MODE_VIDEO,
  getBillingModeLabel,
  getDisplayBillingMode,
  isImageUsage,
} from '../billingMode'

describe('billing mode video handling', () => {
  it('keeps video rows out of the legacy image fallback', () => {
    const row = { image_count: 1, billing_mode: BILLING_MODE_VIDEO }

    expect(isImageUsage(row)).toBe(false)
    expect(getDisplayBillingMode(row)).toBe(BILLING_MODE_VIDEO)
    expect(getBillingModeLabel(BILLING_MODE_VIDEO, (key) => key)).toBe(
      'admin.usage.billingModeVideo',
    )
  })
})
