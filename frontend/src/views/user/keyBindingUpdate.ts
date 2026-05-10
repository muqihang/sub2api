import type { UpdateApiKeyRequest } from '@/types'

export interface KeyBindingState {
  group_id: number | null
  augment_only: boolean
}

export function applyChangedKeyBindingFields(
  payload: UpdateApiKeyRequest,
  current: KeyBindingState,
  next: KeyBindingState,
): UpdateApiKeyRequest {
  if (current.group_id !== next.group_id) {
    payload.group_id = next.group_id
  }
  if (current.augment_only !== next.augment_only) {
    payload.augment_only = next.augment_only
  }
  return payload
}
