import { describe, expect, it } from 'vitest'
import {
  emptyTurnStateHunter, emptyTurnStateRecovery, normalizeTurnStateHunter,
  readTurnStateHunter, readTurnStateRecovery, validateTurnStateHunter, writeTurnStateHunterExtra
} from '../turnStateHunter'

describe('turn-state hunter configuration', () => {
  it('keeps account proxy and unrelated fields while excluding stale scheduler runtime', () => {
    const extra: Record<string, unknown> = { custom: true, openai_turn_state_hunt: { hour_count: 1 }, openai_turn_state_recovery_state: { streak: 2 } }
    const account = { proxy_id: 8, extra }
    const hunter = { ...emptyTurnStateHunter(), enabled: true, models: ['gpt-6-astra'], proxy_ids: [12], rotating_proxy_ids: [12], idle_minutes: -1 }
    writeTurnStateHunterExtra(extra, hunter, emptyTurnStateRecovery(), true)
    expect(account.proxy_id).toBe(8)
    expect(extra).toEqual({ custom: true, openai_turn_state_hunter: { enabled: true, models: ['gpt-6-astra'], proxy_ids: [12], rotating_proxy_ids: [12], idle_minutes: -1 } })
  })

  it('explicitly disables saved background work when leaving reuse mode and retains config', () => {
    const hunter = { ...emptyTurnStateHunter(), enabled: true, auto_models: true, proxy_ids: [3], retry_minutes: 7 }
    const recovery = { ...emptyTurnStateRecovery(), enabled: true, streak_target: 6 }
    const extra: Record<string, unknown> = {}
    writeTurnStateHunterExtra(extra, hunter, recovery, false)
    expect(extra.openai_turn_state_hunter).toMatchObject({ enabled: false, auto_models: true, proxy_ids: [3], retry_minutes: 7 })
    expect(extra.openai_turn_state_recovery).toEqual({ enabled: false, streak_target: 6 })
  })

  it('loads defaults without adding opt-in fields and preserves all supported settings on save', () => {
    expect(normalizeTurnStateHunter(readTurnStateHunter(null))).toBeNull()
    const hunter = { enabled: true, models: ['gpt-6-astra'], auto_models: false, proxy_ids: [1, 2], rotating_proxy_ids: [2, 99], max_per_hour: 30, gap_seconds: 20, lead_minutes: 10, retry_minutes: 15, idle_minutes: -1, reasoning_effort: 'high', hold_when_degraded: true, usage_api_key_id: 101 }
    expect(normalizeTurnStateHunter(readTurnStateHunter(hunter))).toEqual({ ...hunter, auto_models: undefined, rotating_proxy_ids: [2] })
    const recovery = { enabled: true, model: 'gpt-6-astra', streak_target: 5, min_minutes: 30, max_minutes: 90, cooldown_hours: 16, reasoning_effort: 'high', usage_api_key_id: 102 }
    expect(readTurnStateRecovery(recovery)).toEqual(recovery)
  })

  it('keeps future server config fields and runs recovery independently in observation mode', () => {
    const extra: Record<string, unknown> = { openai_turn_state_hunter: { enabled: true, future_option: 'retained', proxy_ids: [3] } }
    const hunter = { ...emptyTurnStateHunter(), enabled: true, auto_models: true, proxy_ids: [3] }
    const recovery = { ...emptyTurnStateRecovery(), enabled: true, model: 'gpt-6-astra' }
    writeTurnStateHunterExtra(extra, hunter, recovery, false, true)
    expect(extra.openai_turn_state_hunter).toMatchObject({ enabled: false, future_option: 'retained', proxy_ids: [3] })
    expect(extra.openai_turn_state_recovery).toEqual({ enabled: true, model: 'gpt-6-astra' })
  })

  it('accepts Team lengths, requires an actionable configuration and rejects malformed numbers', () => {
    const cfg = { ...emptyTurnStateHunter(), enabled: true, auto_models: true, proxy_ids: [1] }
    expect(validateTurnStateHunter(cfg, emptyTurnStateRecovery(), [332])).toBeNull()
    expect(validateTurnStateHunter(cfg, emptyTurnStateRecovery(), [])).toBe('lengthsRequired')
    expect(validateTurnStateHunter({ ...cfg, proxy_ids: [] }, emptyTurnStateRecovery(), [332])).toBe('selectionRequired')
    expect(validateTurnStateHunter({ ...cfg, max_per_hour: 1.5 }, emptyTurnStateRecovery(), [332])).toBe('invalidNumber')
    expect(validateTurnStateHunter({ ...cfg, idle_minutes: -2 }, emptyTurnStateRecovery(), [332])).toBe('invalidNumber')
  })
})
