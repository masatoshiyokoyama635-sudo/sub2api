import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AmountInput from '../AmountInput.vue'
import SubscriptionPlanCard from '../SubscriptionPlanCard.vue'
import type { SubscriptionPlan } from '@/types/payment'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'payment.perMonth': 'month',
        'payment.perYear': 'year',
        'payment.days': '天',
        'payment.planCard.rate': '费率',
        'payment.planCard.dailyLimit': '每日额度',
        'payment.planCard.weeklyLimit': '每周额度',
        'payment.planCard.monthlyLimit': '每月额度',
        'payment.planCard.quota': '额度',
        'payment.planCard.unlimited': '无限',
        'payment.subscribeNow': '立即订阅',
        'payment.renewNow': '立即续费',
      }[key] ?? key),
    }),
  }
})

const basePlan: SubscriptionPlan = {
  id: 1,
  group_id: 1,
  name: 'Pro',
  description: '',
  price: 128,
  original_price: 168,
  validity_days: 30,
  validity_unit: 'day',
  rate_multiplier: 1,
  daily_limit_usd: 10,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  features: [],
  group_platform: 'openai',
  sort_order: 1,
  for_sale: true,
  group_name: 'OpenAI',
}

describe('Payment currency display', () => {
  it('uses RMB symbol for custom recharge amount input', () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
      },
    })

    expect(wrapper.text()).toContain('¥')
    expect(wrapper.text()).not.toContain('$')
  })

  it('uses RMB symbol for subscription price while keeping USD quota symbols', () => {
    const wrapper = mount(SubscriptionPlanCard, {
      props: {
        plan: basePlan,
      },
    })

    expect(wrapper.text()).toContain('¥128')
    expect(wrapper.text()).toContain('¥168')
    expect(wrapper.text()).toContain('$10')
  })
})
