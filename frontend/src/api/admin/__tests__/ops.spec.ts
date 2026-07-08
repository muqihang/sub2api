import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

describe('admin ops WebSocket URL', () => {
  const originalWebSocket = globalThis.WebSocket

  beforeEach(() => {
    vi.resetModules()
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.example.com/api/v1')
    vi.stubEnv('VITE_WS_BASE_URL', '')
    localStorage.setItem('auth_token', 'jwt-test-token')
  })

  afterEach(() => {
    globalThis.WebSocket = originalWebSocket
    localStorage.clear()
    vi.unstubAllEnvs()
    vi.restoreAllMocks()
  })

  it('uses the configured API origin when no explicit websocket base is set', async () => {
    const created: Array<{ url: string; protocols?: string | string[] }> = []
    class FakeWebSocket {
      static readonly OPEN = 1
      static readonly CONNECTING = 0
      readyState = FakeWebSocket.OPEN
      onopen: (() => void) | null = null
      onmessage: ((event: MessageEvent) => void) | null = null
      onerror: ((event: Event) => void) | null = null
      onclose: ((event: CloseEvent) => void) | null = null
      constructor(url: string, protocols?: string | string[]) {
        created.push({ url, protocols })
      }
      close() {}
    }
    globalThis.WebSocket = FakeWebSocket as any

    const { subscribeQPS } = await import('@/api/admin/ops')
    const unsubscribe = subscribeQPS(vi.fn(), { maxReconnectAttempts: 0 })

    expect(created[0]?.url).toBe('wss://api.example.com/api/v1/admin/ops/ws/qps')
    unsubscribe()
  })
})
