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

describe('augment plugin routes', () => {
  it('registers the quick login, account, and billing routes for the standard plugin UI', async () => {
    const { routes } = await import('@/router')

    const pluginRoutePaths = routes.map((route) => route.path)

    expect(pluginRoutePaths).toContain('/plugin/augment/quick-login')
    expect(pluginRoutePaths).toContain('/plugin/augment/account')
    expect(pluginRoutePaths).toContain('/plugin/augment/billing')
  })

  it('requires authentication for the quick login route', async () => {
    const { routes } = await import('@/router')

    const quickLoginRoute = routes.find((route) => route.path === '/plugin/augment/quick-login')

    expect(quickLoginRoute?.meta?.requiresAuth).toBe(true)
  })

  it('keeps backend-mode plugin exposure limited to quick login', async () => {
    const { BACKEND_MODE_ALLOWED_PATHS } = await import('@/router')

    const backendModePluginPaths = BACKEND_MODE_ALLOWED_PATHS.filter((path) =>
      path.startsWith('/plugin/augment/')
    )

    expect(backendModePluginPaths).toEqual(['/plugin/augment/quick-login'])
  })
})
