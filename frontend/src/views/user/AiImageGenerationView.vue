<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="grid gap-4 lg:grid-cols-4">
        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
              <Icon name="sparkles" size="md" />
            </div>
            <div>
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('aiImages.selectedGroup') }}</p>
              <p class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ selectedGroup?.name || t('aiImages.notSelected') }}</p>
            </div>
          </div>
        </div>
        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-100 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300">
              <Icon name="key" size="md" />
            </div>
            <div>
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('aiImages.selectedKey') }}</p>
              <p class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ selectedKey?.name || t('aiImages.notSelected') }}</p>
            </div>
          </div>
        </div>
        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-purple-100 text-purple-600 dark:bg-purple-900/30 dark:text-purple-300">
              <Icon name="brain" size="md" />
            </div>
            <div>
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('aiImages.model') }}</p>
              <p class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ model }}</p>
            </div>
          </div>
        </div>
        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-amber-100 text-amber-600 dark:bg-amber-900/30 dark:text-amber-300">
              <Icon name="clock" size="md" />
            </div>
            <div>
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('aiImages.lastLatency') }}</p>
              <p class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ lastLatency ? `${lastLatency}ms` : '-' }}</p>
            </div>
          </div>
        </div>
      </div>

      <div class="grid gap-6 xl:grid-cols-[380px_1fr]">
        <aside class="space-y-4">
          <div class="card p-5">
            <h2 class="mb-4 text-base font-semibold text-gray-900 dark:text-white">{{ t('aiImages.generateSettings') }}</h2>
            <form class="space-y-4" @submit.prevent="generateImages">
              <div>
                <label class="input-label">{{ t('aiImages.group') }}</label>
                <Select
                  v-model="selectedGroupId"
                  :options="groupOptions"
                  :placeholder="t('aiImages.selectGroup')"
                  :disabled="loading || groupOptions.length === 0"
                  searchable
                />
              </div>
              <div>
                <label class="input-label">{{ t('aiImages.apiKey') }}</label>
                <Select
                  v-model="selectedKeyId"
                  :options="keyOptions"
                  :placeholder="t('aiImages.selectApiKey')"
                  :disabled="loading || keyOptions.length === 0"
                  searchable
                />
              </div>
              <div>
                <label for="image-model" class="input-label">{{ t('aiImages.model') }}</label>
                <input id="image-model" v-model.trim="model" class="input" placeholder="gpt-image-2" />
              </div>
              <div>
                <label class="input-label">{{ t('aiImages.size') }}</label>
                <Select v-model="size" :options="sizeOptions" />
              </div>
              <div>
                <label class="input-label">{{ t('aiImages.count') }}</label>
                <Select v-model="count" :options="countOptions" />
              </div>
              <div>
                <label for="image-prompt" class="input-label">{{ t('aiImages.prompt') }}</label>
                <textarea
                  id="image-prompt"
                  v-model="prompt"
                  class="input min-h-[150px] resize-none"
                  :placeholder="t('aiImages.promptPlaceholder')"
                  :disabled="submitting"
                ></textarea>
              </div>
              <div v-if="credentialWarning" class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-800/50 dark:bg-amber-900/20 dark:text-amber-200">
                {{ credentialWarning }}
              </div>
              <div class="flex gap-2">
                <button v-if="submitting" type="button" class="btn btn-secondary flex-1" @click="abortRequest">
                  <Icon name="x" size="sm" class="mr-2" />
                  {{ t('aiImages.stop') }}
                </button>
                <button type="submit" class="btn btn-primary flex-1" :disabled="!canGenerate">
                  <Icon name="sparkles" size="sm" class="mr-2" />
                  {{ submitting ? t('aiImages.generating') : t('aiImages.generate') }}
                </button>
              </div>
            </form>
          </div>

          <div class="card p-5">
            <h2 class="mb-3 text-base font-semibold text-gray-900 dark:text-white">{{ t('aiImages.tipsTitle') }}</h2>
            <ul class="space-y-2 text-sm text-gray-600 dark:text-dark-300">
              <li>{{ t('aiImages.tipOpenAI') }}</li>
              <li>{{ t('aiImages.tipBilling') }}</li>
              <li>{{ t('aiImages.tipPrompt') }}</li>
            </ul>
          </div>
        </aside>

        <section class="card min-h-[680px] overflow-hidden">
          <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('aiImages.results') }}</h2>
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('aiImages.resultsHint') }}</p>
            </div>
            <button class="btn btn-ghost" :disabled="submitting || images.length === 0" @click="clearImages">
              <Icon name="trash" size="sm" class="mr-2" />
              {{ t('aiImages.clear') }}
            </button>
          </div>

          <div class="bg-gradient-to-br from-primary-50/40 to-cyan-50/30 p-6 dark:from-dark-900 dark:to-dark-800">
            <div v-if="images.length === 0" class="empty-state py-24">
              <Icon name="sparkles" size="xl" class="mx-auto mb-4 text-primary-500" />
              <h3 class="empty-state-title">{{ t('aiImages.emptyTitle') }}</h3>
              <p class="empty-state-description">{{ t('aiImages.emptyDescription') }}</p>
            </div>

            <div v-else class="grid gap-5 md:grid-cols-2 xl:grid-cols-3">
              <article
                v-for="image in images"
                :key="image.id"
                class="overflow-hidden rounded-2xl border border-gray-100 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800"
              >
                <div class="aspect-square bg-gray-100 dark:bg-dark-700">
                  <img :src="image.src" :alt="image.prompt" referrerpolicy="no-referrer" class="h-full w-full object-cover" />
                </div>
                <div class="space-y-3 p-4">
                  <p class="line-clamp-2 text-sm text-gray-600 dark:text-dark-300">{{ image.prompt }}</p>
                  <div class="flex gap-2">
                    <button class="btn btn-secondary flex-1" @click="copyImageSource(image.src)">
                      <Icon name="copy" size="sm" class="mr-2" />
                      {{ t('aiImages.copy') }}
                    </button>
                    <a class="btn btn-primary flex-1" :href="image.src" :download="`image-${image.id}.png`" target="_blank" rel="noopener noreferrer">
                      <Icon name="download" size="sm" class="mr-2" />
                      {{ t('aiImages.download') }}
                    </a>
                  </div>
                </div>
              </article>
            </div>
          </div>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import { gatewayAPI } from '@/api/gateway'
import { useGatewayCredentials } from '@/composables/useGatewayCredentials'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'

interface GeneratedImage {
  id: string
  src: string
  prompt: string
}

const { t } = useI18n()
const appStore = useAppStore()
const {
  loading,
  error: credentialError,
  groups,
  activeKeysForSelectedGroup,
  selectedGroupId,
  selectedKeyId,
  selectedGroup,
  selectedKey
} = useGatewayCredentials({ groupFilter: (group) => group.platform === 'openai' })

const model = ref('gpt-image-2')
const size = ref<string | number | boolean | null>('1024x1024')
const count = ref<string | number | boolean | null>(1)
const prompt = ref('')
const submitting = ref(false)
const images = ref<GeneratedImage[]>([])
const lastLatency = ref<number | null>(null)
const abortController = ref<AbortController | null>(null)

const sizeOptions = [
  { value: '1024x1024', label: '1024 × 1024' },
  { value: '1024x1536', label: '1024 × 1536' },
  { value: '1536x1024', label: '1536 × 1024' }
]

const countOptions = [1, 2, 3, 4].map((value) => ({ value, label: String(value) }))
const groupOptions = computed(() => groups.value.map((group) => ({ value: group.id, label: `${group.name} · ${group.platform}` })))
const keyOptions = computed(() => activeKeysForSelectedGroup.value.map((key) => ({ value: key.id, label: key.name })))

const credentialWarning = computed(() => {
  if (credentialError.value) return extractApiErrorMessage(credentialError.value, t('aiImages.credentialsFailed'))
  if (!loading.value && groups.value.length === 0) return t('aiImages.noOpenAIGroups')
  if (selectedGroupId.value !== null && activeKeysForSelectedGroup.value.length === 0) return t('aiImages.noKeysInGroup')
  return ''
})

const canGenerate = computed(() => {
  return !submitting.value && !!selectedKey.value?.key && !!model.value.trim() && !!prompt.value.trim()
})

function createSafeImageSource(item: { url?: string; b64_json?: string }): string {
  if (item.b64_json) {
    return `data:image/png;base64,${item.b64_json}`
  }

  if (!item.url) return ''

  try {
    const url = new URL(item.url)
    return url.protocol === 'https:' ? url.toString() : ''
  } catch {
    return ''
  }
}

async function generateImages() {
  if (!canGenerate.value || !selectedKey.value) return

  submitting.value = true
  abortController.value = new AbortController()
  const startedAt = performance.now()
  const currentPrompt = prompt.value.trim()

  try {
    const response = await gatewayAPI.createImageGeneration(
      selectedKey.value.key,
      {
        model: model.value.trim(),
        prompt: currentPrompt,
        size: String(size.value || '1024x1024'),
        n: Number(count.value || 1)
      },
      abortController.value.signal
    )

    lastLatency.value = Math.round(performance.now() - startedAt)
    images.value = (response.data || [])
      .map((item): GeneratedImage | null => {
        const src = createSafeImageSource(item)
        return src ? { id: crypto.randomUUID(), src, prompt: item.revised_prompt || currentPrompt } : null
      })
      .filter((item): item is GeneratedImage => item !== null)

    if (images.value.length === 0) {
      appStore.showError(t('aiImages.emptyResponse'))
    }
  } catch (error) {
    if ((error as Error).name !== 'AbortError') {
      appStore.showError(extractApiErrorMessage(error, t('aiImages.requestFailed')))
    }
  } finally {
    submitting.value = false
    abortController.value = null
  }
}

async function copyImageSource(src: string) {
  try {
    await navigator.clipboard.writeText(src)
    appStore.showSuccess(t('aiImages.copied'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('aiImages.copyFailed')))
  }
}

function abortRequest() {
  abortController.value?.abort()
}

function clearImages() {
  images.value = []
  lastLatency.value = null
}

onBeforeUnmount(abortRequest)
</script>
