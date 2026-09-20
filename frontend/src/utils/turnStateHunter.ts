export interface TurnStateHunterConfig {
  enabled: boolean
  models: string[]
  auto_models: boolean
  proxy_ids: number[]
  rotating_proxy_ids: number[]
  max_per_hour: number | null
  lead_minutes: number | null
  retry_minutes: number | null
  idle_minutes: number | null
  gap_seconds: number | null
  reasoning_effort: string
  hold_when_degraded: boolean
  usage_api_key_id: number | null
}

export interface TurnStateRecoveryConfig {
  enabled: boolean
  model: string
  streak_target: number | null
  min_minutes: number | null
  max_minutes: number | null
  cooldown_hours: number | null
  reasoning_effort: string
  usage_api_key_id: number | null
}

export const HUNTER_REASONING_EFFORTS = ['minimal', 'low', 'medium', 'high', 'xhigh']
export const emptyTurnStateHunter = (): TurnStateHunterConfig => ({
  enabled: false, models: [], auto_models: false, proxy_ids: [], rotating_proxy_ids: [],
  max_per_hour: null, lead_minutes: null, retry_minutes: null, idle_minutes: null,
  gap_seconds: null, reasoning_effort: '', hold_when_degraded: false, usage_api_key_id: null
})
export const emptyTurnStateRecovery = (): TurnStateRecoveryConfig => ({
  enabled: false, model: '', streak_target: null, min_minutes: null, max_minutes: null,
  cooldown_hours: null, reasoning_effort: '', usage_api_key_id: null
})

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}
function positiveIDs(value: unknown): number[] {
  return Array.isArray(value) ? [...new Set(value.filter((id): id is number => typeof id === 'number' && Number.isSafeInteger(id) && id > 0))] : []
}
export function parseHunterModels(value: string): string[] {
  return [...new Set(value.split(/[,，\s]+/).map((model) => model.trim()).filter(Boolean))]
}
export function readTurnStateHunter(value: unknown): TurnStateHunterConfig {
  const raw = record(value)
  const cfg = emptyTurnStateHunter()
  cfg.enabled = raw.enabled === true
  cfg.auto_models = raw.auto_models === true
  cfg.hold_when_degraded = raw.hold_when_degraded === true
  cfg.models = Array.isArray(raw.models) ? [...new Set(raw.models.filter((model): model is string => typeof model === 'string').map((model) => model.trim()).filter(Boolean))] : []
  cfg.proxy_ids = positiveIDs(raw.proxy_ids)
  cfg.rotating_proxy_ids = positiveIDs(raw.rotating_proxy_ids).filter((id) => cfg.proxy_ids.includes(id))
  cfg.reasoning_effort = typeof raw.reasoning_effort === 'string' ? raw.reasoning_effort : ''
  for (const key of ['max_per_hour', 'lead_minutes', 'retry_minutes', 'idle_minutes', 'gap_seconds', 'usage_api_key_id'] as const) {
    cfg[key] = typeof raw[key] === 'number' ? raw[key] : null
  }
  return cfg
}
export function readTurnStateRecovery(value: unknown): TurnStateRecoveryConfig {
  const raw = record(value)
  const cfg = emptyTurnStateRecovery()
  cfg.enabled = raw.enabled === true
  cfg.model = typeof raw.model === 'string' ? raw.model : ''
  cfg.reasoning_effort = typeof raw.reasoning_effort === 'string' ? raw.reasoning_effort : ''
  for (const key of ['streak_target', 'min_minutes', 'max_minutes', 'cooldown_hours', 'usage_api_key_id'] as const) {
    cfg[key] = typeof raw[key] === 'number' ? raw[key] : null
  }
  return cfg
}
export function normalizeTurnStateHunter(cfg: TurnStateHunterConfig): Record<string, unknown> | null {
  const out: Record<string, unknown> = { enabled: cfg.enabled }
  const models = [...new Set(cfg.models.map((model) => model.trim()).filter(Boolean))]
  const proxies = positiveIDs(cfg.proxy_ids)
  if (models.length) out.models = models
  if (proxies.length) out.proxy_ids = proxies
  const rotating = positiveIDs(cfg.rotating_proxy_ids).filter((id) => proxies.includes(id))
  if (rotating.length) out.rotating_proxy_ids = rotating
  for (const key of ['max_per_hour', 'lead_minutes', 'retry_minutes', 'idle_minutes', 'gap_seconds', 'usage_api_key_id'] as const) {
    if (typeof cfg[key] === 'number' && cfg[key] !== 0) out[key] = cfg[key]
  }
  if (cfg.auto_models) out.auto_models = true
  if (cfg.hold_when_degraded) out.hold_when_degraded = true
  if (cfg.reasoning_effort) out.reasoning_effort = cfg.reasoning_effort
  return !cfg.enabled && Object.keys(out).length === 1 ? null : out
}
export function normalizeTurnStateRecovery(cfg: TurnStateRecoveryConfig): Record<string, unknown> | null {
  const out: Record<string, unknown> = { enabled: cfg.enabled }
  if (cfg.model.trim()) out.model = cfg.model.trim()
  for (const key of ['streak_target', 'min_minutes', 'max_minutes', 'cooldown_hours', 'usage_api_key_id'] as const) {
    if (typeof cfg[key] === 'number' && cfg[key] !== 0) out[key] = cfg[key]
  }
  if (cfg.reasoning_effort) out.reasoning_effort = cfg.reasoning_effort
  return !cfg.enabled && Object.keys(out).length === 1 ? null : out
}

// Return a localized message key. Numeric input is validated before serialization;
// malformed values must not silently disappear and turn into backend defaults.
export function validateTurnStateHunter(hunter: TurnStateHunterConfig, recovery: TurnStateRecoveryConfig, lengths: number[] | null): string | null {
  if ((hunter.enabled || recovery.enabled) && !lengths?.length) return 'lengthsRequired'
  if (hunter.enabled && (!hunter.proxy_ids.length || (!hunter.auto_models && !hunter.models.length))) return 'selectionRequired'
  if (hunter.models.length > 8 || hunter.proxy_ids.length > 64) return 'selectionLimit'
  const ranges: Array<[number | null, number, number]> = [
    [hunter.max_per_hour, 0, 600], [hunter.lead_minutes, 0, 55],
    [hunter.retry_minutes, 0, 1440], [hunter.idle_minutes, -1, 1440], [hunter.gap_seconds, 0, 600],
    [hunter.usage_api_key_id, 0, 2147483647], [recovery.streak_target, 0, 50],
    [recovery.min_minutes, 0, 1440], [recovery.max_minutes, 0, 1440],
    [recovery.cooldown_hours, 0, 168], [recovery.usage_api_key_id, 0, 2147483647]
  ]
  if (ranges.some(([value, min, max]) => value !== null && (!Number.isSafeInteger(value) || value < min || value > max))) return 'invalidNumber'
  if ((recovery.min_minutes || 30) > (recovery.max_minutes || 90)) return 'invalidInterval'
  if ([hunter.reasoning_effort, recovery.reasoning_effort].some((effort) => effort && !HUNTER_REASONING_EFFORTS.includes(effort))) return 'invalidEffort'
  return null
}

// Write configuration only. Runtime state belongs to the scheduler and may have
// changed since the dialog was opened. A disabled setting is explicit for merge APIs.
export function writeTurnStateHunterExtra(extra: Record<string, unknown>, hunter: TurnStateHunterConfig, recovery: TurnStateRecoveryConfig, eligible: boolean, recoveryEligible = eligible): void {
  delete extra.openai_turn_state_hunt
  delete extra.openai_turn_state_recovery_state
  const configs = [
    ['openai_turn_state_hunter', normalizeTurnStateHunter({ ...hunter, enabled: eligible && hunter.enabled })],
    ['openai_turn_state_recovery', normalizeTurnStateRecovery({ ...recovery, enabled: recoveryEligible && recovery.enabled })]
  ] as const
  for (const [key, config] of configs) {
    // Keep fields added by newer server versions, but remove cleared known fields.
    const retained = { ...record(extra[key]) }
    const known = key === 'openai_turn_state_hunter' ? emptyTurnStateHunter() : emptyTurnStateRecovery()
    for (const field of Object.keys(known)) delete retained[field]
    if (config) extra[key] = { ...retained, ...config }
    else if (extra[key] !== undefined) extra[key] = { ...retained, enabled: false }
  }
}
