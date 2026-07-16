import { defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useEgressCheckPolling } from '../useEgressCheckPolling'
import type { FormalPoolSession } from '@/api/admin/claudeOnboarding'

function sessionFixture(overrides: Partial<FormalPoolSession> = {}): FormalPoolSession {
  return {
    id: 'session-1',
    version: 1,
    status: 'proxy_tested',
    pool_profile: 'normal',
    group_id: 1,
    account_name: 'claude-test',
    concurrency: 1,
    browser_egress_check_status: 'waiting',
    ...overrides,
  }
}

function mountHarness(fetchSession: (id: string, signal: AbortSignal) => Promise<FormalPoolSession>) {
  let exposed!: ReturnType<typeof useEgressCheckPolling>
  const wrapper = mount(defineComponent({
    setup() {
      exposed = useEgressCheckPolling({ fetchSession, intervalMs: 1000 })
      return () => null
    },
  }))
  return { wrapper, poller: exposed }
}

async function flush() {
  await Promise.resolve()
  await nextTick()
}

describe('useEgressCheckPolling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('fetches immediately on start and then again on each interval', async () => {
    const fetchSession = vi.fn().mockResolvedValue(sessionFixture())
    const { poller } = mountHarness(fetchSession)

    poller.start('session-1')
    await flush()

    expect(fetchSession).toHaveBeenCalledTimes(1)
    expect(fetchSession.mock.calls[0][0]).toBe('session-1')

    await vi.advanceTimersByTimeAsync(1000)
    await flush()

    expect(fetchSession).toHaveBeenCalledTimes(2)
  })

  it('stops polling when stop is called or the owner unmounts', async () => {
    const fetchSession = vi.fn().mockResolvedValue(sessionFixture())
    const { wrapper, poller } = mountHarness(fetchSession)

    poller.start('session-1')
    await flush()
    poller.stop()

    await vi.advanceTimersByTimeAsync(3000)
    expect(fetchSession).toHaveBeenCalledTimes(1)

    poller.start('session-1')
    await flush()
    wrapper.unmount()

    await vi.advanceTimersByTimeAsync(3000)
    expect(fetchSession).toHaveBeenCalledTimes(2)
  })

  it('aborts the previous request and timer when a new session starts', async () => {
    const signals: AbortSignal[] = []
    const fetchSession = vi.fn((id: string, signal: AbortSignal) => {
      signals.push(signal)
      return Promise.resolve(sessionFixture({ id }))
    })
    const { poller } = mountHarness(fetchSession)

    poller.start('session-old')
    await flush()
    expect(signals[0].aborted).toBe(false)

    poller.start('session-new')
    await flush()

    expect(signals[0].aborted).toBe(true)
    expect(fetchSession.mock.calls.map((call) => call[0])).toEqual(['session-old', 'session-new'])

    await vi.advanceTimersByTimeAsync(1000)
    await flush()

    expect(fetchSession.mock.calls.map((call) => call[0])).toEqual(['session-old', 'session-new', 'session-new'])
  })

  it('updates browser egress status for expired, mismatch, and verified sessions', async () => {
    const fetchSession = vi.fn()
      .mockResolvedValueOnce(sessionFixture({ browser_egress_check_status: 'expired' }))
      .mockResolvedValueOnce(sessionFixture({ browser_egress_check_status: 'mismatch' }))
      .mockResolvedValueOnce(sessionFixture({ browser_egress_check_status: 'verified', browser_egress_verified: true }))
    const { poller } = mountHarness(fetchSession)

    poller.start('session-1')
    await flush()
    expect(poller.status.value).toBe('expired')

    await vi.advanceTimersByTimeAsync(1000)
    await flush()
    expect(poller.status.value).toBe('mismatch')

    await vi.advanceTimersByTimeAsync(1000)
    await flush()
    expect(poller.status.value).toBe('verified')
    expect(poller.session.value?.browser_egress_verified).toBe(true)
  })

  it('does not replace a newer session with an older polling version', async () => {
    const fetchSession = vi.fn()
      .mockResolvedValueOnce(sessionFixture({ version: 5, browser_egress_check_status: 'verified' }))
      .mockResolvedValueOnce(sessionFixture({ version: 4, browser_egress_check_status: 'waiting' }))
    const { poller } = mountHarness(fetchSession)

    poller.start('session-1')
    await flush()
    expect(poller.session.value?.version).toBe(5)

    await vi.advanceTimersByTimeAsync(1000)
    await flush()

    expect(poller.session.value?.version).toBe(5)
    expect(poller.status.value).toBe('verified')
  })
})
