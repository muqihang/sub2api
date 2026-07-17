import { onBeforeUnmount, readonly, ref } from 'vue'

import claudeOnboarding, {
  type BrowserEgressCheckStatus,
  type FormalPoolSession,
} from '@/api/admin/claudeOnboarding'

export interface UseEgressCheckPollingOptions {
  intervalMs?: number
  fetchSession?: (id: string, signal: AbortSignal) => Promise<FormalPoolSession>
  attestBrowserEgress?: (
    session: FormalPoolSession,
    proof: string,
    signal: AbortSignal,
  ) => Promise<FormalPoolSession>
}

const SERVER_BROWSER_EGRESS_PROOF = /^nonce_[0-9a-f]{32}$/

export function serverProofFromBrowserURL(raw: string | undefined): string {
  if (!raw) return ''
  try {
    const base = globalThis.location?.origin || 'http://localhost'
    const parsed = new URL(raw, base)
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return ''
    if (parsed.search || parsed.hash) return ''
    const proof = parsed.pathname.slice(parsed.pathname.lastIndexOf('/') + 1)
    return SERVER_BROWSER_EGRESS_PROOF.test(proof) ? proof : ''
  } catch {
    return ''
  }
}

export function useEgressCheckPolling(options: UseEgressCheckPollingOptions = {}) {
  const intervalMs = options.intervalMs ?? 3000
  const fetchSession = options.fetchSession ?? ((id: string, signal: AbortSignal) => claudeOnboarding.getSession(id, signal))
  const attestBrowserEgress = options.attestBrowserEgress
    ?? ((next: FormalPoolSession, proof: string) => claudeOnboarding.attestBrowserEgress(next, proof))

  const session = ref<FormalPoolSession | null>(null)
  const status = ref<BrowserEgressCheckStatus>('idle')
  const running = ref(false)
  const error = ref('')

  let timer: ReturnType<typeof setTimeout> | null = null
  let controller: AbortController | null = null
  let activeSessionId: string | null = null
  let generation = 0
  const attemptedProofKeys = new Set<string>()
  const finalizationErrors = new Map<string, string>()

  function clearTimer() {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
  }

  function abortCurrent() {
    controller?.abort()
    controller = null
  }

  function stop() {
    running.value = false
    activeSessionId = null
    generation += 1
    clearTimer()
    abortCurrent()
    attemptedProofKeys.clear()
    finalizationErrors.clear()
  }

  function mutationErrorMessage(err: any): string {
    return err?.response?.data?.message || err?.message || '自动确认 browser egress 失败'
  }

  async function finalizeObservedBrowserEgress(
    nextSession: FormalPoolSession,
    currentGeneration: number,
    id: string,
    signal: AbortSignal,
  ) {
    if (
      nextSession.browser_egress_check_status !== 'verified_pending_finalize'
      || nextSession.browser_egress_verified
    ) return

    const proof = serverProofFromBrowserURL(nextSession.browser_egress_check_url)
    if (!proof) return
    const key = `${nextSession.id}:${nextSession.version}:${proof}`
    if (attemptedProofKeys.has(key)) {
      const previousError = finalizationErrors.get(key)
      if (previousError) error.value = previousError
      return
    }

    attemptedProofKeys.add(key)
    try {
      const finalized = await attestBrowserEgress(nextSession, proof, signal)
      if (
        signal.aborted
        || currentGeneration !== generation
        || activeSessionId !== id
        || finalized.id !== id
      ) return
      if (session.value?.id === finalized.id && finalized.version < session.value.version) return
      session.value = finalized
      status.value = finalized.browser_egress_check_status ?? 'idle'
      finalizationErrors.delete(key)
      error.value = ''
    } catch (err: any) {
      if (signal.aborted || currentGeneration !== generation || activeSessionId !== id) return
      const message = mutationErrorMessage(err)
      finalizationErrors.set(key, message)
      error.value = message
    }
  }

  async function poll(currentGeneration: number, id: string) {
    clearTimer()
    abortCurrent()

    const localController = new AbortController()
    controller = localController

    try {
      const nextSession = await fetchSession(id, localController.signal)
      if (
        localController.signal.aborted ||
        currentGeneration !== generation ||
        activeSessionId !== id
      ) return
      if (session.value?.id === nextSession.id && nextSession.version < session.value.version) return

      session.value = nextSession
      status.value = nextSession.browser_egress_check_status ?? 'idle'
      error.value = ''
      await finalizeObservedBrowserEgress(nextSession, currentGeneration, id, localController.signal)
    } catch (err: any) {
      if (localController.signal.aborted || currentGeneration !== generation) return
      error.value = err?.response?.data?.message || err?.message || '轮询 browser egress 状态失败'
    } finally {
      if (running.value && currentGeneration === generation && activeSessionId === id) {
        timer = setTimeout(() => {
          void poll(currentGeneration, id)
        }, intervalMs)
      }
    }
  }

  function start(sessionId: string) {
    if (!sessionId) return
    stop()
    running.value = true
    activeSessionId = sessionId
    generation += 1
    const currentGeneration = generation
    void poll(currentGeneration, sessionId)
  }

  onBeforeUnmount(() => stop())

  return {
    session: readonly(session),
    status: readonly(status),
    running: readonly(running),
    error: readonly(error),
    start,
    stop,
    abort: stop,
  }
}
