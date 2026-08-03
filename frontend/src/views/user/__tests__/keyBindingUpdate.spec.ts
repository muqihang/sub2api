import { describe, expect, it } from 'vitest'
import { applyChangedKeyBindingFields } from '@/views/user/keyBindingUpdate'

describe('applyChangedKeyBindingFields', () => {
  it('does not over-post unchanged binding fields', () => {
    const payload = applyChangedKeyBindingFields(
      { name: 'updated' },
      { group_id: 10, augment_only: true },
      { group_id: 10, augment_only: true },
    )

    expect(payload).toEqual({ name: 'updated' })
  })

  it('includes only the binding fields that changed', () => {
    const payload = applyChangedKeyBindingFields(
      { name: 'updated' },
      { group_id: 10, augment_only: false },
      { group_id: 11, augment_only: true },
    )

    expect(payload).toEqual({
      name: 'updated',
      group_id: 11,
      augment_only: true,
    })
  })
})
