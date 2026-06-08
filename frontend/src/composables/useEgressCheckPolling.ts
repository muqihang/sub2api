import { onBeforeUnmount, readonly, ref } from 'vue'

import claudeOnboarding, {
  type BrowserEgressCheckStatus,
  type FormalPoolSession,
} from '@/api/admin/claudeOnboarding'

export interface UseEgressCheckPollingOptions {
  intervalMs?: number
  fetchSession?: (id: string, signal: AbortSignal) => Promise<FormalPoolSession>
}

export function useEgressCheckPolling(options: UseEgressCheckPollingOptions = {}) {
  const intervalMs = options.intervalMs ?? 3000
  const fetchSession = options.fetchSession ?? ((id: string, signal: AbortSignal) => claudeOnboarding.getSession(id, signal))

  const session = ref<FormalPoolSession | null>(null)
  const status = ref<BrowserEgressCheckStatus>('idle')
  const running = ref(false)
  const error = ref('')

  let timer: ReturnType<typeof setTimeout> | null = null
  let controller: AbortController | null = null
  let activeSessionId: string | null = null
  let generation = 0

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

      session.value = nextSession
      status.value = nextSession.browser_egress_check_status ?? 'idle'
      error.value = ''
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
