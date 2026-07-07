import { describe, expect, it, vi } from 'vitest'

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

describe('marketing routes', () => {
  it('registers the Codex Gateway product page as a public route', async () => {
    const { routes } = await import('@/router')

    const codexGatewayRoute = routes.find((route) => route.path === '/codex-gateway')

    expect(codexGatewayRoute?.meta?.requiresAuth).toBe(false)
    expect(codexGatewayRoute?.meta?.title).toBe('Codex Gateway')
  })

  it('allows public marketing pages in backend mode', async () => {
    const { BACKEND_MODE_ALLOWED_PATHS } = await import('@/router')

    expect(BACKEND_MODE_ALLOWED_PATHS).toContain('/home')
    expect(BACKEND_MODE_ALLOWED_PATHS).toContain('/codex-gateway')
  })
})
