import { defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { serverProofFromBrowserURL, useEgressCheckPolling } from '../useEgressCheckPolling'
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

function mountHarness(
  fetchSession: (id: string, signal: AbortSignal) => Promise<FormalPoolSession>,
  attestBrowserEgress?: (session: FormalPoolSession, proof: string) => Promise<FormalPoolSession>,
) {
  let exposed!: ReturnType<typeof useEgressCheckPolling>
  const wrapper = mount(defineComponent({
    setup() {
      exposed = useEgressCheckPolling({
        fetchSession,
        intervalMs: 1000,
        attestBrowserEgress,
      })
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

  it('auto-finalizes a server-observed proof once and keeps the newer returned session', async () => {
    const proof = `nonce_${'a'.repeat(32)}`
    const pending = sessionFixture({
      version: 2,
      browser_egress_check_status: 'verified_pending_finalize',
      browser_egress_verified: false,
      browser_egress_check_url: `https://safe.example/browser-egress-check/${proof}`,
    })
    const finalized = sessionFixture({
      version: 3,
      browser_egress_check_status: 'verified',
      browser_egress_verified: true,
    })
    const fetchSession = vi.fn().mockResolvedValue(pending)
    const attestBrowserEgress = vi.fn().mockResolvedValue(finalized)
    const { poller } = mountHarness(fetchSession, attestBrowserEgress)

    poller.start('session-1')
    await flush()

    expect(attestBrowserEgress).toHaveBeenCalledTimes(1)
    expect(attestBrowserEgress).toHaveBeenCalledWith(pending, proof, expect.any(AbortSignal))
    expect(poller.session.value).toEqual(finalized)

    await vi.advanceTimersByTimeAsync(1000)
    await flush()

    expect(attestBrowserEgress).toHaveBeenCalledTimes(1)
    expect(poller.session.value).toEqual(finalized)
  })

  it.each([
    ['', 'empty'],
    ['https://safe.example/browser-egress-check/', 'empty final segment'],
    [`https://safe.example/browser-egress-check/nonce_${'a'.repeat(31)}`, 'short proof'],
    [`https://safe.example/browser-egress-check/nonce_${'a'.repeat(33)}`, 'long proof'],
    [`https://safe.example/browser-egress-check/NONCE_${'a'.repeat(32)}`, 'uppercase prefix'],
    [`https://safe.example/browser-egress-check/nonce_${'A'.repeat(32)}`, 'uppercase hex'],
    [`https://safe.example/browser-egress-check/proof_${'a'.repeat(32)}`, 'wrong prefix'],
    [`https://safe.example/browser-egress-check/nonce_${'a'.repeat(32)}/extra`, 'extra path'],
    [`https://safe.example/browser-egress-check?proof=nonce_${'a'.repeat(32)}`, 'query-only proof'],
    [`https://safe.example/browser-egress-check#nonce_${'a'.repeat(32)}`, 'fragment-only proof'],
    [`https://safe.example/browser-egress-check/nonce_${'a'.repeat(32)}?source=query`, 'query ambiguity'],
    [`https://safe.example/browser-egress-check/nonce_${'a'.repeat(32)}#fragment`, 'fragment ambiguity'],
    [`javascript:nonce_${'a'.repeat(32)}`, 'non-HTTP scheme'],
    ['not a valid URL %', 'invalid URL'],
  ])('rejects %s as a server proof (%s)', (raw) => {
    expect(serverProofFromBrowserURL(raw)).toBe('')
  })

  it('accepts only an exact nonce in the final URL path segment', () => {
    const proof = `nonce_${'b'.repeat(32)}`
    expect(serverProofFromBrowserURL(`https://safe.example/api/browser-egress-check/${proof}`)).toBe(proof)
    expect(serverProofFromBrowserURL(`/api/browser-egress-check/${proof}`)).toBe(proof)
  })

  it('does not retry a failed tuple but allows a new version and proof', async () => {
    const firstProof = `nonce_${'c'.repeat(32)}`
    const secondProof = `nonce_${'d'.repeat(32)}`
    const first = sessionFixture({
      version: 2,
      browser_egress_check_status: 'verified_pending_finalize',
      browser_egress_check_url: `/check/${firstProof}`,
    })
    const second = sessionFixture({
      version: 3,
      browser_egress_check_status: 'verified_pending_finalize',
      browser_egress_check_url: `/check/${secondProof}`,
    })
    const finalized = sessionFixture({
      version: 4,
      browser_egress_check_status: 'verified',
      browser_egress_verified: true,
    })
    const fetchSession = vi.fn()
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce(second)
    const attestBrowserEgress = vi.fn()
      .mockRejectedValueOnce(new Error('finalization failed'))
      .mockResolvedValueOnce(finalized)
    const { poller } = mountHarness(fetchSession, attestBrowserEgress)

    poller.start('session-1')
    await flush()
    expect(attestBrowserEgress).toHaveBeenCalledTimes(1)
    expect(poller.error.value).toBe('finalization failed')

    await vi.advanceTimersByTimeAsync(1000)
    await flush()
    expect(attestBrowserEgress).toHaveBeenCalledTimes(1)
    expect(poller.error.value).toBe('finalization failed')

    await vi.advanceTimersByTimeAsync(1000)
    await flush()
    expect(attestBrowserEgress).toHaveBeenCalledTimes(2)
    expect(attestBrowserEgress).toHaveBeenLastCalledWith(second, secondProof, expect.any(AbortSignal))
    expect(poller.session.value).toEqual(finalized)
    expect(poller.error.value).toBe('')
  })

  it('does not replace a pending session with a stale finalization response', async () => {
    const proof = `nonce_${'e'.repeat(32)}`
    const pending = sessionFixture({
      version: 5,
      browser_egress_check_status: 'verified_pending_finalize',
      browser_egress_check_url: `/check/${proof}`,
    })
    const stale = sessionFixture({
      version: 4,
      browser_egress_check_status: 'verified',
      browser_egress_verified: true,
    })
    const { poller } = mountHarness(
      vi.fn().mockResolvedValue(pending),
      vi.fn().mockResolvedValue(stale),
    )

    poller.start('session-1')
    await flush()

    expect(poller.session.value).toEqual(pending)
    expect(poller.status.value).toBe('verified_pending_finalize')
  })

  it('clears the one-shot guard on an explicit restart', async () => {
    const proof = `nonce_${'f'.repeat(32)}`
    const pending = sessionFixture({
      version: 2,
      browser_egress_check_status: 'verified_pending_finalize',
      browser_egress_check_url: `/check/${proof}`,
    })
    const attestBrowserEgress = vi.fn().mockRejectedValue(new Error('retry after restart'))
    const { poller } = mountHarness(vi.fn().mockResolvedValue(pending), attestBrowserEgress)

    poller.start('session-1')
    await flush()
    poller.start('session-1')
    await flush()

    expect(attestBrowserEgress).toHaveBeenCalledTimes(2)
  })

  it('ignores a delayed finalization result after stop or unmount', async () => {
    const proof = `nonce_${'0'.repeat(32)}`
    const pending = sessionFixture({
      version: 2,
      browser_egress_check_status: 'verified_pending_finalize',
      browser_egress_check_url: `/check/${proof}`,
    })
    let resolveFinalization!: (value: FormalPoolSession) => void
    const finalization = new Promise<FormalPoolSession>((resolve) => {
      resolveFinalization = resolve
    })
    const { wrapper, poller } = mountHarness(
      vi.fn().mockResolvedValue(pending),
      vi.fn().mockReturnValue(finalization),
    )

    poller.start('session-1')
    await flush()
    wrapper.unmount()
    resolveFinalization(sessionFixture({
      version: 3,
      browser_egress_check_status: 'verified',
      browser_egress_verified: true,
    }))
    await flush()

    expect(poller.session.value).toEqual(pending)
    expect(poller.session.value?.browser_egress_verified).not.toBe(true)
  })
})
