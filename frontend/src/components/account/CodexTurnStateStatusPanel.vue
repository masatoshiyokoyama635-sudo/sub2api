<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  clearCodexTurnState,
  getCodexTurnState,
  type CodexTurnStateStatus
} from '@/api/admin/codexTurnState'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ accountId: number }>()
const { t } = useI18n()
const state = ref<CodexTurnStateStatus | null>(null)
const busy = ref(false)
const errorKey = ref('')
let requestVersion = 0

async function load(clear = false) {
  const version = ++requestVersion
  const accountId = props.accountId
  busy.value = true
  errorKey.value = ''
  try {
    const next = await (clear ? clearCodexTurnState(accountId) : getCodexTurnState(accountId))
    if (version === requestVersion) state.value = next
  } catch {
    if (version === requestVersion) {
      errorKey.value = clear ? 'clearFailed' : 'loadFailed'
    }
  } finally {
    if (version === requestVersion) busy.value = false
  }
}

watch(() => props.accountId, () => {
  state.value = null
  void load()
}, { immediate: true })

onBeforeUnmount(() => { requestVersion++ })
</script>

<template>
  <section class="space-y-3 rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-800/60" data-testid="codex-turn-state-status">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h4 class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ t('admin.accounts.codexTurnStateStatus.title') }}</h4>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" data-testid="codex-turn-state-refresh" @click="load()">
          {{ t('admin.accounts.codexTurnStateStatus.refresh') }}
        </button>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || !state?.models?.length" data-testid="codex-turn-state-clear" @click="load(true)">
          {{ t('admin.accounts.codexTurnStateStatus.clear') }}
        </button>
      </div>
    </div>
    <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.savedSettings') }}</p>
    <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.localOnly') }}</p>
    <p v-if="errorKey" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(`admin.accounts.codexTurnStateStatus.${errorKey}`) }}</p>
    <p v-if="busy && !state" aria-live="polite" class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.loading') }}</p>
    <p v-else-if="state && !state.models?.length" class="text-xs text-gray-500 dark:text-dark-400" data-testid="codex-turn-state-empty">{{ t('admin.accounts.codexTurnStateStatus.empty') }}</p>
    <ul v-if="state?.models?.length" class="max-h-96 space-y-3 overflow-y-auto">
      <li v-for="entry in state.models" :key="entry.model" class="space-y-2 rounded border border-gray-200 bg-white p-3 text-xs dark:border-dark-600 dark:bg-dark-800">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="break-all font-mono font-medium text-gray-800 dark:text-gray-100">{{ entry.model }}</span>
          <span class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.observed', { count: entry.observed_count }) }}</span>
        </div>
        <div class="flex flex-wrap gap-2 text-gray-600 dark:text-dark-300">
          <span v-for="length in entry.lengths" :key="length.length" class="rounded bg-gray-100 px-2 py-1 font-mono dark:bg-dark-700">{{ length.length }} × {{ length.count }}</span>
          <span v-if="entry.other_length_count">{{ t('admin.accounts.codexTurnStateStatus.otherLengths', { count: entry.other_length_count }) }}</span>
        </div>
        <p class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.lastObserved') }}: {{ formatDateTime(entry.last_observed_at) }}</p>
        <dl v-if="entry.candidate" class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-gray-600 dark:text-dark-300">
          <dt>{{ t('admin.accounts.codexTurnStateStatus.length') }}</dt><dd class="font-mono">{{ entry.candidate.length }}</dd>
          <dt>{{ t('admin.accounts.codexTurnStateStatus.digest') }}</dt><dd class="break-all font-mono">{{ entry.candidate.hash_prefix }}</dd>
          <dt>{{ t('admin.accounts.codexTurnStateStatus.issued') }}</dt><dd>{{ formatDateTime(entry.candidate.issued_at) }}</dd>
          <dt>{{ t('admin.accounts.codexTurnStateStatus.expires') }}</dt><dd>{{ formatDateTime(entry.candidate.expires_at) }}</dd>
          <dt>{{ t('admin.accounts.codexTurnStateStatus.lastReused') }}</dt>
          <dd>{{ entry.candidate.last_reused_at ? formatDateTime(entry.candidate.last_reused_at) : t('admin.accounts.codexTurnStateStatus.notReused') }} · {{ t('admin.accounts.codexTurnStateStatus.reused', { count: entry.candidate.reuse_count }) }}</dd>
        </dl>
        <p v-else class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.noCandidate') }}</p>
      </li>
    </ul>
  </section>
</template>
