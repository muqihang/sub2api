import { describe, expect, it } from 'vitest'

import type { FormalPoolSession } from '@/api/admin/claudeOnboarding'
import {
  classifyFormalPoolMutationError,
  mergeFormalPoolMutationResult,
  monotonicFormalPoolSession,
} from '../formalPoolMutation'

function session(version: number, id = 'session-1'): FormalPoolSession {
  return {
    id,
    version,
    status: `status-${version}`,
    pool_profile: 'normal',
    group_id: 9,
    account_name: 'test',
    concurrency: 1,
    browser_egress_check_status: 'idle',
    browser_egress_verified: false,
  }
}

describe('formal pool mutation state', () => {
  it('retains operation keys only for network and 5xx ambiguity', () => {
    expect(classifyFormalPoolMutationError(new Error('network')).retainOperationKey).toBe(true)
    expect(classifyFormalPoolMutationError({ status: 503 }).retainOperationKey).toBe(true)
    expect(classifyFormalPoolMutationError({ response: { status: 502 } }).retainOperationKey).toBe(true)
    expect(classifyFormalPoolMutationError({ status: 409 }).retainOperationKey).toBe(false)
    expect(classifyFormalPoolMutationError({ response: { status: 400 } }).retainOperationKey).toBe(false)
  })

  it('reconciles conflicts and ambiguous failures but not definitive 4xx failures', () => {
    expect(classifyFormalPoolMutationError({ status: 409 }).reconcile).toBe(true)
    expect(classifyFormalPoolMutationError({ status: 500 }).reconcile).toBe(true)
    expect(classifyFormalPoolMutationError(new Error('network')).reconcile).toBe(true)
    expect(classifyFormalPoolMutationError({ status: 422 }).reconcile).toBe(false)
  })

  it('rejects stale same-session snapshots while accepting equal/newer and replacement sessions', () => {
    const current = session(5)
    expect(monotonicFormalPoolSession(current, session(4))).toBe(current)
    expect(monotonicFormalPoolSession(current, session(5)).version).toBe(5)
    expect(monotonicFormalPoolSession(current, session(6)).version).toBe(6)
    expect(monotonicFormalPoolSession(current, session(1, 'session-2')).id).toBe('session-2')
  })

  it('does not roll a session backward while merging acceptance results', () => {
    const current = session(5)
    expect(mergeFormalPoolMutationResult(current, { version: 4, status: 'old' })).toBe(current)
    const merged = mergeFormalPoolMutationResult(current, { version: 6, status: 'healthcheck_passed' })
    expect(merged.version).toBe(6)
    expect(merged.status).toBe('healthcheck_passed')
    expect(merged.healthcheck_passed).toBe(true)
  })
})
