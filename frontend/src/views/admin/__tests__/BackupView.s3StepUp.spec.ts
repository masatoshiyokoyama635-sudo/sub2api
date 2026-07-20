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
  getImageStorageConfig,
  updateImageStorageConfig,
  testImageStorageConnection,
  showError,
  showSuccess
} = vi.hoisted(() => ({
  getS3Config: vi.fn(),
  getSchedule: vi.fn(),
  listBackups: vi.fn(),
  updateS3Config: vi.fn(),
  getImageStorageConfig: vi.fn(),
  updateImageStorageConfig: vi.fn(),
  testImageStorageConnection: vi.fn(),
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
      getImageStorageConfig,
      updateImageStorageConfig,
      testImageStorageConnection,
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
    getImageStorageConfig.mockResolvedValue({
      config: {
        enabled: false,
        reuse_backup_s3: true,
        bucket: '',
        prefix: 'images/',
        public_base_url: '',
        presign_expiry_hours: 24,
        max_download_bytes: 20 * 1024 * 1024,
        endpoint: '',
        region: 'auto',
        access_key_id: '',
        secret_access_key: '',
        force_path_style: false
      },
      secret_configured: false
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

  it('loads image storage and preserves an existing secret with a blank save value', async () => {
    getImageStorageConfig.mockResolvedValue({
      config: {
        enabled: true,
        reuse_backup_s3: false,
        bucket: 'images-bucket',
        prefix: 'generated/',
        public_base_url: 'https://cdn.example.test',
        presign_expiry_hours: 12,
        max_download_bytes: 20 * 1024 * 1024,
        endpoint: 'https://s3.example.test',
        region: 'auto',
        access_key_id: 'AKID',
        secret_access_key: 'must-not-enter-the-form',
        force_path_style: true
      },
      secret_configured: true
    })
    updateImageStorageConfig.mockResolvedValue({})
    const wrapper = mount(BackupView, {
      global: { stubs: { TotpStepUpDialog: TotpStepUpDialogStub } }
    })
    await flushPromises()

    const card = wrapper.findAll('.card').find(item => item.text().includes('admin.backup.imageStorage.title'))
    expect(card).toBeDefined()
    const secretInput = card!.find('input[type="password"]')
    expect((secretInput.element as HTMLInputElement).value).toBe('')
    const saveButton = card!.findAll('button').find(button => button.text() === 'common.save')
    await saveButton!.trigger('click')
    await vi.waitFor(() => expect(updateImageStorageConfig).toHaveBeenCalledOnce())

    expect(updateImageStorageConfig).toHaveBeenCalledWith(expect.objectContaining({
      enabled: true,
      reuse_backup_s3: false,
      bucket: 'images-bucket',
      prefix: 'generated/',
      access_key_id: 'AKID',
      secret_access_key: ''
    }))
    expect(showSuccess).toHaveBeenCalledWith('admin.backup.imageStorage.saved')
  })

  it('does not retry or show a network error when image storage step-up is cancelled', async () => {
    updateImageStorageConfig.mockRejectedValue({ status: 403, code: 'STEP_UP_REQUIRED' })
    const wrapper = mount(BackupView, {
      global: { stubs: { TotpStepUpDialog: TotpStepUpDialogStub } }
    })
    await flushPromises()

    const card = wrapper.findAll('.card').find(item => item.text().includes('admin.backup.imageStorage.title'))
    expect(card).toBeDefined()
    const saveButton = card!.findAll('button').find(button => button.text() === 'common.save')
    await saveButton!.trigger('click')
    await vi.waitFor(() => expect(updateImageStorageConfig).toHaveBeenCalledOnce())

    const controller = wrapper.findComponent(TotpStepUpDialogStub).props('controller') as StepUpController
    expect(controller.visible.value).toBe(true)
    controller.onCancel()
    await flushPromises()

    expect(updateImageStorageConfig).toHaveBeenCalledOnce()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
  })

  it.each([
    { result: { ok: true, message: 'connected' }, success: true },
    { result: { ok: false, message: 'connection failed' }, success: false }
  ])('reports image storage connection result $result.ok', async ({ result, success }) => {
    testImageStorageConnection.mockResolvedValue(result)
    const wrapper = mount(BackupView, {
      global: { stubs: { TotpStepUpDialog: TotpStepUpDialogStub } }
    })
    await flushPromises()

    const card = wrapper.findAll('.card').find(item => item.text().includes('admin.backup.imageStorage.title'))
    expect(card).toBeDefined()
    const testButton = card!.findAll('button').find(button => button.text() === 'admin.backup.s3.testConnection')
    await testButton!.trigger('click')
    await vi.waitFor(() => expect(testImageStorageConnection).toHaveBeenCalledOnce())

    expect(testImageStorageConnection).toHaveBeenCalledWith(expect.objectContaining({
      reuse_backup_s3: true,
      prefix: 'images/',
      secret_access_key: ''
    }))
    if (success) {
      expect(showSuccess).toHaveBeenCalledWith(result.message)
      expect(showError).not.toHaveBeenCalled()
    } else {
      expect(showError).toHaveBeenCalledWith(result.message)
      expect(showSuccess).not.toHaveBeenCalled()
    }
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
