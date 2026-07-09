import { describe, expect, it, vi } from 'vitest'

const post = vi.fn()

vi.mock('../../client', () => ({
  apiClient: {
    post
  }
}))

describe('admin accounts api', () => {
  it('scopes a 120s timeout to Codex session imports', async () => {
    post.mockResolvedValueOnce({ data: { total: 1, created: 1, updated: 0, skipped: 0, failed: 0 } })
    const { importCodexSession } = await import('../accounts')

    await importCodexSession({ content: 'access-token' })

    expect(post).toHaveBeenCalledWith('/admin/accounts/import/codex-session', { content: 'access-token' }, { timeout: 120000 })
  })
})
