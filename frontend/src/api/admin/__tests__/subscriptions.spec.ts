import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, del } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  del: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
    delete: del,
  },
}))

import { revoke } from '@/api/admin/subscriptions'

describe('admin subscriptions api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    del.mockReset()
    post.mockResolvedValue({ data: { message: 'ok' } })
  })

  it('uses explicit POST revoke endpoint while backend keeps DELETE compatibility', async () => {
    await expect(revoke(42)).resolves.toEqual({ message: 'ok' })

    expect(post).toHaveBeenCalledWith('/admin/subscriptions/42/revoke')
    expect(del).not.toHaveBeenCalled()
  })
})
