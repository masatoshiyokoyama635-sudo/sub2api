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

export interface CodexTurnStateModelSnapshot {
  model: string
  observed_count: number
  last_observed_at: string
  lengths: Array<{ length: number; count: number }>
  other_length_count: number
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
