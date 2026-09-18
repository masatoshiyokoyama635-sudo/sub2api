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
    expect(wrapper.text()).toContain('admin.accounts.codexTurnStateStatus.notReused')
    expect(wrapper.text()).not.toMatch(/healthy|degraded|满血|降智/i)
    expect(clearCodexTurnState).not.toHaveBeenCalled()
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
