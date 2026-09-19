import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CodexTurnStateStatusPanel from '../CodexTurnStateStatusPanel.vue'
import type { CodexTurnStateStatus } from '@/api/admin/codexTurnState'

const { getCodexTurnState, clearCodexTurnState } = vi.hoisted(() => ({
  getCodexTurnState: vi.fn(),
  clearCodexTurnState: vi.fn()
}))

vi.mock('@/api/admin/codexTurnState', () => ({ getCodexTurnState, clearCodexTurnState }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))

const observed = (model = 'gpt-team'): CodexTurnStateStatus => ({
  mode: 'observe',
  identity_version: 'v2',
  candidate_lengths: [292, 332],
  process_local: true,
  models: [{
    model,
    observed_count: 3,
    last_observed_at: '2026-09-18T12:00:00Z',
    lengths: [{ length: 332, count: 2 }, { length: 312, count: 1 }],
    other_length_count: 0,
    reuse_attempt_count: 0,
    last_selection: { reason: 'observe_mode', at: '2026-09-18T12:00:00Z' },
    candidate: {
      hash_prefix: 'abcdef012345',
      length: 332,
      issued_at: '2026-09-18T11:55:00Z',
      expires_at: '2026-09-18T12:55:00Z',
      last_observed_at: '2026-09-18T12:00:00Z',
      observed_count: 2,
      reuse_count: 0
    }
  }]
})

describe('CodexTurnStateStatusPanel', () => {
  beforeEach(() => {
    getCodexTurnState.mockReset()
    clearCodexTurnState.mockReset()
  })

  it('shows Team length observations and candidate metadata without quality labels', async () => {
    getCodexTurnState.mockResolvedValueOnce(observed())
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await flushPromises()

    expect(getCodexTurnState).toHaveBeenCalledWith(7)
    expect(wrapper.text()).toContain('332 × 2')
    expect(wrapper.text()).toContain('312 × 1')
    expect(wrapper.text()).toContain('abcdef012345')
    expect(wrapper.text()).toContain('2026-09-18T12:55:00Z')
    expect(wrapper.get('[data-testid="codex-turn-state-effective-mode"]').text()).toBe('admin.accounts.openai.codexTurnStateObserve')
    expect(wrapper.get('[data-testid="codex-turn-state-effective-settings"]').text()).toContain('292, 332')
    expect(wrapper.text()).toContain('admin.accounts.codexTurnStateStatus.reuseNotEnabled')
    expect(wrapper.get('[data-testid="codex-turn-state-total-attempts"]').text()).toBe('0')
    expect(wrapper.text()).not.toMatch(/healthy|degraded|满血|降智/i)
    expect(clearCodexTurnState).not.toHaveBeenCalled()
  })

  it('keeps model totals visible when the current candidate has never been selected', async () => {
    const snapshot = observed()
    snapshot.mode = 'reuse'
    snapshot.models[0].reuse_attempt_count = 7
    snapshot.models[0].last_reuse_attempt_at = '2026-09-18T11:59:00Z'
    snapshot.models[0].last_selection = { reason: 'reused', at: '2026-09-18T11:59:00Z' }
    getCodexTurnState.mockResolvedValueOnce(snapshot)
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await flushPromises()

    expect(wrapper.get('[data-testid="codex-turn-state-total-attempts"]').text()).toBe('7')
    expect(wrapper.get('[data-testid="codex-turn-state-current-candidate"]').text()).toContain('admin.accounts.codexTurnStateStatus.candidateReuseAttempts0')
    expect(wrapper.text()).toContain('2026-09-18T11:59:00Z')
    expect(wrapper.text()).not.toContain('admin.accounts.codexTurnStateStatus.notReused')
    expect(clearCodexTurnState).not.toHaveBeenCalled()
  })

  it('separates historical lengths from an absent candidate and explains rejection and expiry', async () => {
    const snapshot = observed()
    delete snapshot.models[0].candidate
    snapshot.models[0].last_selection = { reason: 'no_candidate', at: '2026-09-18T13:00:00Z' }
    snapshot.models[0].last_candidate_rejection = { reason: 'length_not_allowed', at: '2026-09-18T12:58:00Z' }
    snapshot.models[0].last_candidate_invalidation = { reason: 'expired', at: '2026-09-18T12:55:00Z' }
    getCodexTurnState.mockResolvedValueOnce(snapshot)
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await flushPromises()

    expect(wrapper.text()).toContain('332 × 2')
    expect(wrapper.text()).toContain('admin.accounts.codexTurnStateStatus.observationHistory')
    expect(wrapper.text()).toContain('admin.accounts.codexTurnStateStatus.noCandidate')
    expect(wrapper.find('[data-testid="codex-turn-state-current-candidate"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="codex-turn-state-last-selection"]').text()).toContain('reasons.no_candidate')
    expect(wrapper.get('[data-testid="codex-turn-state-last-rejection"]').text()).toContain('reasons.length_not_allowed')
    expect(wrapper.get('[data-testid="codex-turn-state-last-invalidation"]').text()).toContain('reasons.expired')
  })

  it('shows only refreshed server settings and marks candidates excluded by the saved allowlist', async () => {
    getCodexTurnState.mockResolvedValueOnce(observed())
    const next = { ...observed(), mode: 'reuse' as const, candidate_lengths: [] }
    getCodexTurnState.mockResolvedValueOnce(next)
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-turn-state-effective-mode"]').text()).toBe('admin.accounts.openai.codexTurnStateObserve')
    await wrapper.get('[data-testid="codex-turn-state-refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="codex-turn-state-effective-mode"]').text()).toBe('admin.accounts.openai.codexTurnStateReuse')
    expect(wrapper.get('[data-testid="codex-turn-state-effective-settings"]').text()).toContain('admin.accounts.codexTurnStateStatus.noSelectedLengths')
    expect(wrapper.text()).toContain('admin.accounts.codexTurnStateStatus.candidateLengthExcluded')
    expect(clearCodexTurnState).not.toHaveBeenCalled()
  })

  it('can show outbound state length 332 alongside a different upstream model declaration', async () => {
    const snapshot = observed('gpt-6-astra')
    snapshot.mode = 'reuse'
    snapshot.models[0].last_request = {
      at: '2026-09-18T12:00:00Z', request_id: 'req-completed-1',
      state_source: 'candidate', outbound_state_length: 332,
      upstream_response_model: 'gpt-5.6-luna', response_model_observed: true,
      model_mismatch: true, selection_reason: 'reused', failed: false
    }
    getCodexTurnState.mockResolvedValueOnce(snapshot)
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await flushPromises()

    const request = wrapper.get('[data-testid="codex-turn-state-last-request"]')
    expect(wrapper.text()).toContain('gpt-6-astra')
    expect(request.text()).toContain('332 · admin.accounts.codexTurnStateStatus.sourceCandidate')
    expect(request.text()).toContain('gpt-5.6-luna')
    expect(request.text()).toContain('req-completed-1')
    expect(request.text()).toContain('admin.accounts.codexTurnStateStatus.lastRequestOnly')
    expect(wrapper.get('[data-testid="codex-turn-state-model-comparison"]').text()).toBe('admin.accounts.codexTurnStateStatus.modelMismatch')
  })

  it('does not report a matching model when no response model was observed', async () => {
    const snapshot = observed()
    snapshot.models[0].last_request = {
      at: '2026-09-18T12:00:00Z', state_source: 'client', outbound_state_length: 332,
      response_model_observed: false, model_mismatch: false,
      selection_reason: 'client_state', failed: true
    }
    getCodexTurnState.mockResolvedValueOnce(snapshot)
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await flushPromises()

    expect(wrapper.get('[data-testid="codex-turn-state-model-comparison"]').text()).toBe('admin.accounts.codexTurnStateStatus.responseModelUnknown')
    expect(wrapper.text()).not.toContain('admin.accounts.codexTurnStateStatus.modelMatches')
    expect(wrapper.get('[data-testid="codex-turn-state-last-request"]').text()).toContain('admin.accounts.codexTurnStateStatus.requestFailed')
    expect(wrapper.get('[data-testid="codex-turn-state-last-request"]').text()).toContain('admin.accounts.codexTurnStateStatus.sourceClient')
  })

  it('renders a selection without an observation and hides arbitrary reason text', async () => {
    const snapshot = observed()
    snapshot.models[0].observed_count = 0
    snapshot.models[0].last_observed_at = '0001-01-01T00:00:00Z'
    snapshot.models[0].lengths = []
    snapshot.models[0].last_selection = { reason: 'private-unexpected-value', at: '2026-09-18T12:00:00Z' }
    delete snapshot.models[0].candidate
    getCodexTurnState.mockResolvedValueOnce(snapshot)
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.codexTurnStateStatus.noObservations')
    expect(wrapper.text()).not.toContain('0001-01-01')
    expect(wrapper.text()).not.toContain('private-unexpected-value')
    expect(wrapper.get('[data-testid="codex-turn-state-last-selection"]').text()).toContain('reasons.unknown')
  })

  it('does not substitute the per-candidate count for unavailable cumulative data', async () => {
    const snapshot = observed()
    delete snapshot.models[0].reuse_attempt_count
    getCodexTurnState.mockResolvedValueOnce(snapshot)
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await flushPromises()

    expect(wrapper.get('[data-testid="codex-turn-state-total-attempts"]').text()).toBe('admin.accounts.codexTurnStateStatus.unavailable')
  })

  it('ignores old account responses after switching accounts', async () => {
    let resolveOld: (value: CodexTurnStateStatus) => void = () => {}
    getCodexTurnState.mockImplementationOnce(() => new Promise<CodexTurnStateStatus>((resolve) => { resolveOld = resolve }))
    getCodexTurnState.mockResolvedValueOnce(observed('new-account-model'))
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await wrapper.setProps({ accountId: 8 })
    await flushPromises()

    expect(wrapper.text()).toContain('new-account-model')
    resolveOld(observed('old-account-model'))
    await flushPromises()
    expect(wrapper.text()).toContain('new-account-model')
    expect(wrapper.text()).not.toContain('old-account-model')
    expect(getCodexTurnState.mock.calls).toEqual([[7], [8]])
  })

  it('clears only the selected account and renders the returned empty snapshot', async () => {
    getCodexTurnState.mockResolvedValueOnce(observed())
    clearCodexTurnState.mockResolvedValueOnce({ ...observed(), models: [] })
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await flushPromises()
    await wrapper.get('[data-testid="codex-turn-state-clear"]').trigger('click')
    await flushPromises()

    expect(clearCodexTurnState).toHaveBeenCalledWith(7)
    expect(wrapper.find('[data-testid="codex-turn-state-empty"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('abcdef012345')
    expect(wrapper.get('[data-testid="codex-turn-state-clear"]').attributes('disabled')).toBeDefined()
  })

  it('keeps metadata on failure and displays no raw server error', async () => {
    getCodexTurnState.mockResolvedValueOnce(observed())
    clearCodexTurnState.mockRejectedValueOnce(new Error('private-raw-server-response'))
    const wrapper = mount(CodexTurnStateStatusPanel, { props: { accountId: 7 } })
    await flushPromises()
    await wrapper.get('[data-testid="codex-turn-state-clear"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toBe('admin.accounts.codexTurnStateStatus.clearFailed')
    expect(wrapper.text()).toContain('abcdef012345')
    expect(wrapper.text()).not.toContain('private-raw-server-response')
  })
})
