import { mount, type VueWrapper } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import TurnStateHunterSettings from '../TurnStateHunterSettings.vue'
import { emptyTurnStateHunter, emptyTurnStateRecovery } from '@/utils/turnStateHunter'
import type { Proxy } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const proxy = (id: number, username = 'private-user'): Proxy => ({
  id, name: `Proxy ${id}`, protocol: 'socks5', host: id === 2 ? 'p.webshare.io' : 'proxy.example', port: 1080,
  username, password: 'private-password', status: 'active'
} as Proxy)

function mountSettings(hunter = emptyTurnStateHunter(), eligible = true, recoveryEligible = eligible) {
  const wrapper: VueWrapper<InstanceType<typeof TurnStateHunterSettings>> = mount(TurnStateHunterSettings, { props: {
    hunter, recovery: emptyTurnStateRecovery(), eligible, recoveryEligible, lengths: '332',
    idPrefix: 'test', proxies: [proxy(1), proxy(2, 'private-user-rotate')],
    'onUpdate:hunter': (hunter) => { void wrapper.setProps({ hunter }) },
    'onUpdate:recovery': (recovery) => { void wrapper.setProps({ recovery }) }
  } })
  return wrapper
}

describe('TurnStateHunterSettings', () => {
  it('requires reuse mode and never enables a scheduler implicitly', async () => {
    const wrapper = mountSettings(emptyTurnStateHunter(), false)
    expect(wrapper.get<HTMLInputElement>('[data-testid="test-hunter-enabled"]').element.disabled).toBe(true)
    expect(wrapper.get<HTMLInputElement>('[data-testid="test-recovery-enabled"]').element.disabled).toBe(true)
    expect(wrapper.find('[data-testid="test-hunter-needs-reuse"]').exists()).toBe(true)
    expect(wrapper.emitted('update:hunter')).toBeUndefined()
    wrapper.unmount()
  })

  it('selects managed proxy IDs, explicitly marks rotation, and omits credentials from labels', async () => {
    const wrapper = mountSettings({ ...emptyTurnStateHunter(), enabled: true, models: ['gpt-6-astra'] })
    await wrapper.get('[data-testid="test-hunter-proxies"]').setValue(['1', '2'])
    await wrapper.get('[data-testid="test-hunter-rotating-1"]').setValue(true)
    expect(wrapper.props('hunter').proxy_ids).toEqual([1, 2])
    expect(wrapper.props('hunter').rotating_proxy_ids).toEqual([1])
    expect(wrapper.get<HTMLInputElement>('[data-testid="test-hunter-rotating-2"]').element.checked).toBe(true)
    expect(wrapper.get<HTMLInputElement>('[data-testid="test-hunter-rotating-2"]').element.disabled).toBe(true)
    expect(wrapper.html()).not.toContain('private-user')
    expect(wrapper.html()).not.toContain('private-password')
    await wrapper.get('[data-testid="test-hunter-proxies"]').setValue(['2'])
    expect(wrapper.props('hunter').rotating_proxy_ids).toEqual([])
    expect(wrapper.emitted()).not.toHaveProperty('update:proxy_id')
    wrapper.unmount()
  })

  it('keeps unavailable saved IDs visible and validates manual versus automatic models', async () => {
    const wrapper = mountSettings({ ...emptyTurnStateHunter(), enabled: true, proxy_ids: [99] })
    expect(wrapper.text()).toContain('unavailableProxy #99')
    expect(wrapper.get<HTMLSelectElement>('[data-testid="test-hunter-proxies"]').element.selectedOptions[0]?.value).toBe('99')
    expect(wrapper.get('[role="alert"]').text()).toContain('selectionRequired')
    await wrapper.get('[data-testid="test-hunter-auto-models"]').setValue(true)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="test-hunter-models"]').element.disabled).toBe(true)
    await wrapper.get('[data-testid="test-hunter-idle_minutes"]').setValue('-1')
    expect(wrapper.props('hunter').idle_minutes).toBe(-1)
    wrapper.unmount()
  })

  it('supports independent recovery and reports reversed intervals', async () => {
    const wrapper = mountSettings(emptyTurnStateHunter(), false, true)
    await wrapper.get('[data-testid="test-recovery-enabled"]').setValue(true)
    await wrapper.get('[data-testid="test-recovery-min_minutes"]').setValue('100')
    await wrapper.get('[data-testid="test-recovery-max_minutes"]').setValue('50')
    expect(wrapper.props('hunter').enabled).toBe(false)
    expect(wrapper.props('recovery').enabled).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).toContain('invalidInterval')
    await wrapper.setProps({ lengths: '' })
    expect(wrapper.get('[role="alert"]').text()).toContain('lengthsRequired')
    wrapper.unmount()
  })
})
