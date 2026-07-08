import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { axiosCreate, setupClient } = vi.hoisted(() => ({
  axiosCreate: vi.fn(),
  setupClient: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

vi.mock('axios', () => ({
  default: {
    create: axiosCreate,
  },
}))

describe('setup API client', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.example.com/api/v1')
    axiosCreate.mockReset()
    setupClient.get.mockReset()
    setupClient.post.mockReset()
    axiosCreate.mockReturnValue(setupClient)
  })

  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('uses the configured API origin for root setup endpoints', async () => {
    await import('@/api/setup')

    expect(axiosCreate).toHaveBeenCalledWith(expect.objectContaining({
      baseURL: 'https://api.example.com',
    }))
  })
})
