import { apiClient } from '../client'

export interface CodexTurnStateCandidate {
  hash_prefix: string
  length: number
  issued_at: string
  expires_at: string
  last_observed_at: string
  observed_count: number
  reuse_count: number
  last_reused_at?: string
}

export interface CodexTurnStateDiagnostic {
  reason: string
  at: string
}

export interface CodexTurnStateRequestSnapshot {
  at: string
  request_id?: string
  state_source: 'none' | 'client' | 'candidate'
  outbound_state_length: number
  upstream_response_model?: string
  response_model_observed: boolean
  model_mismatch: boolean
  selection_reason: string
  failed: boolean
}

export interface CodexTurnStateCollectionSnapshot {
  attempt_count: number
  in_flight: boolean
  last_attempt_at: string
  last_finished_at?: string
  next_eligible_at?: string
  last_reason: string
  last_http_status: number
  last_observed_length: number
  last_response_model?: string
}

export interface CodexTurnStateModelSnapshot {
  model: string
  observed_count: number
  last_observed_at: string
  lengths: Array<{ length: number; count: number }>
  other_length_count: number
  // Optional for compatibility with snapshots produced before bucket diagnostics.
  reuse_attempt_count?: number
  last_reuse_attempt_at?: string
  last_selection?: CodexTurnStateDiagnostic
  last_candidate_observation?: CodexTurnStateDiagnostic
  last_candidate_rejection?: CodexTurnStateDiagnostic
  last_candidate_invalidation?: CodexTurnStateDiagnostic
  last_request?: CodexTurnStateRequestSnapshot
  collection?: CodexTurnStateCollectionSnapshot
  candidate?: CodexTurnStateCandidate
}

export interface TurnStateHunterAttempt {
  at: string
  model: string
  proxy_id: number
  proxy?: string
  status: number
  chars: number
  healthy: boolean
  latency_ms: number
  exit?: string
  error?: string
  response_model?: string
}

export interface TurnStateHunterRuntime {
  next_at: string
  hour_start: string
  hour_count: number
  cursor: number
  last: TurnStateHunterAttempt[]
  exits?: Array<{ proxy_id: number; ip: string; at: string; healthy: boolean; response_model?: string }>
  last_error?: string
  cap_wait?: boolean
  gate?: string
  updated_at: string
  auth_blocked_credential?: string
  rate_limit_until?: string
}

export interface TurnStateRecoveryRuntime {
  streak?: number
  fail_streak?: number
  next_at: string
  recovered_at?: string
  cooling_until?: string
  last?: TurnStateHunterAttempt[]
  last_error?: string
  updated_at: string
  auth_blocked_credential?: string
  rate_limit_until?: string
}

export interface CodexTurnStateStatus {
  mode: 'off' | 'observe' | 'reuse'
  identity_version: string
  candidate_lengths: number[]
  active_collection_enabled?: boolean
  hunter_enabled?: boolean
  hunter?: TurnStateHunterRuntime
  hunter_models?: CodexTurnStateModelSnapshot[]
  hunter_shared_cache?: boolean
  recovery_enabled?: boolean
  recovery?: TurnStateRecoveryRuntime
  process_local: boolean
  models: CodexTurnStateModelSnapshot[]
}

export async function getCodexTurnState(accountId: number): Promise<CodexTurnStateStatus> {
  const { data } = await apiClient.get<CodexTurnStateStatus>(`/admin/accounts/${accountId}/codex-turn-state`)
  return data
}

export async function clearCodexTurnState(accountId: number): Promise<CodexTurnStateStatus> {
  const { data } = await apiClient.delete<CodexTurnStateStatus>(`/admin/accounts/${accountId}/codex-turn-state`)
  return data
}
