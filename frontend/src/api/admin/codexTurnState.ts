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
  candidate?: CodexTurnStateCandidate
}

export interface CodexTurnStateStatus {
  mode: 'off' | 'observe' | 'reuse'
  identity_version: string
  candidate_lengths: number[]
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
