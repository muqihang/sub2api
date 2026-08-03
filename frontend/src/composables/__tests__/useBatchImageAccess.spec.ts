import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { keysAPI } from '@/api/keys'
import { useAuthStore } from '@/stores/auth'
import { loadBatchImageAccess } from '../useBatchImageAccess'

vi.mock('@/api/keys', () => ({
  keysAPI: {
    list: vi.fn(),
  },
}))

function authenticate(userId: number): void {
  const auth = useAuthStore()
  auth.$patch({
    token: `token-${userId}`,
    user: { id: userId, role: 'user' },
  } as any)
}

describe('useBatchImageAccess', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(keysAPI.list).mockReset()
  })

  it('reloads entitlement when the authenticated user changes', async () => {
    vi.mocked(keysAPI.list)
      .mockResolvedValueOnce({
        items: [{ status: 'active', group: { platform: 'gemini', allow_batch_image_generation: true } }],
        pages: 1,
      } as any)
      .mockResolvedValueOnce({ items: [], pages: 1 } as any)

    authenticate(101)
    await expect(loadBatchImageAccess()).resolves.toBe(true)

    authenticate(202)
    await expect(loadBatchImageAccess()).resolves.toBe(false)
    expect(keysAPI.list).toHaveBeenCalledTimes(2)
  })
})
