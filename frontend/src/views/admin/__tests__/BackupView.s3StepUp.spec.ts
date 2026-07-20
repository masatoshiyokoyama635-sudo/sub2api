import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, type PropType } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { StepUpController } from '@/composables/useStepUp'
import BackupView from '../BackupView.vue'

const {
  getS3Config,
  getSchedule,
  listBackups,
  updateS3Config,
  showError,
  showSuccess
} = vi.hoisted(() => ({
  getS3Config: vi.fn(),
  getSchedule: vi.fn(),
  listBackups: vi.fn(),
  updateS3Config: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api', () => ({
  adminAPI: {
    backup: {
      getS3Config,
      getSchedule,
      listBackups,
      updateS3Config,
      testS3Connection: vi.fn(),
      updateSchedule: vi.fn(),
      createBackup: vi.fn(),
      getBackup: vi.fn(),
      getDownloadURL: vi.fn(),
      restoreBackup: vi.fn(),
      deleteBackup: vi.fn()
    }
  }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showWarning: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const TotpStepUpDialogStub = defineComponent({
  props: {
    controller: {
      type: Object as PropType<StepUpController>,
      required: true
    }
  },
  template: '<div data-test="step-up-dialog" />'
})

describe('BackupView S3 step-up', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getS3Config.mockResolvedValue({
      endpoint: '',
      region: 'auto',
      bucket: '',
      access_key_id: '',
      secret_access_key: '',
      prefix: 'backups/',
      force_path_style: false
    })
    getSchedule.mockResolvedValue({
      enabled: false,
      cron_expr: '0 2 * * *',
      retain_days: 14,
      retain_count: 10
    })
    listBackups.mockResolvedValue({ items: [] })
    updateS3Config.mockRejectedValue({ status: 403, code: 'STEP_UP_REQUIRED' })
  })

  it('does not retry or show a network error when S3 step-up is cancelled', async () => {
    const wrapper = mount(BackupView, {
      global: {
        stubs: {
          TotpStepUpDialog: TotpStepUpDialogStub
        }
      }
    })
    await flushPromises()

    const saveButton = wrapper
      .find('.card')
      .findAll('button')
      .find(button => button.text() === 'common.save')
    expect(saveButton).toBeDefined()

    await saveButton!.trigger('click')
    await vi.waitFor(() => expect(updateS3Config).toHaveBeenCalledOnce())

    const dialog = wrapper.findComponent(TotpStepUpDialogStub)
    const controller = dialog.props('controller') as StepUpController
    expect(controller.visible.value).toBe(true)
    controller.onCancel()
    await flushPromises()

    expect(updateS3Config).toHaveBeenCalledOnce()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
  })
})
