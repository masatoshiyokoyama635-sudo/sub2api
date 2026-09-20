<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  clearCodexTurnState,
  getCodexTurnState,
  type CodexTurnStateRequestSnapshot,
  type CodexTurnStateStatus
} from '@/api/admin/codexTurnState'
import { formatDateTime } from '@/utils/format'
import TurnStateHunterStatus from './TurnStateHunterStatus.vue'

const props = defineProps<{ accountId: number }>()
const { t } = useI18n()
const state = ref<CodexTurnStateStatus | null>(null)
const busy = ref(false)
const errorKey = ref('')
let requestVersion = 0

const diagnosticReasons = new Set([
  'reused', 'no_candidate', 'expired', 'length_not_allowed', 'observe_mode',
  'client_state', 'client_continuation', 'client_metadata_state',
  'collection_cooldown', 'collection_rejected', 'collection_pending',
  'accepted', 'refreshed', 'unchanged', 'invalid_format', 'future_timestamp',
  'stale', 'state_echo', 'upstream_rejected'
])
const collectionReasons = new Set([
  'in_progress', 'accepted', 'missing_state', 'length_not_allowed', 'invalid_format',
  'future_timestamp', 'expired', 'stale', 'model_mismatch', 'model_unobserved',
  'incomplete_response', 'response_failed', 'body_too_large', 'transport_error',
  'timeout', 'unauthorized', 'forbidden', 'rate_limited', 'upstream_error',
  'configuration_changed', 'collection_failed'
])

function reasonLabel(reason: string) {
  return t(`admin.accounts.codexTurnStateStatus.reasons.${diagnosticReasons.has(reason) ? reason : 'unknown'}`)
}

function collectionReasonLabel(reason: string) {
  return t(`admin.accounts.codexTurnStateStatus.collectionReasons.${collectionReasons.has(reason) ? reason : 'unknown'}`)
}

function modeLabel(mode: CodexTurnStateStatus['mode']) {
  switch (mode) {
    case 'observe': return t('admin.accounts.openai.codexTurnStateObserve')
    case 'reuse': return t('admin.accounts.openai.codexTurnStateReuse')
    default: return t('admin.accounts.openai.codexTurnStateOff')
  }
}

function sourceLabel(source: CodexTurnStateRequestSnapshot['state_source']) {
  switch (source) {
    case 'client': return t('admin.accounts.codexTurnStateStatus.sourceClient')
    case 'candidate': return t('admin.accounts.codexTurnStateStatus.sourceCandidate')
    default: return t('admin.accounts.codexTurnStateStatus.sourceNone')
  }
}

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
        <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || !(state?.models?.length || state?.hunter_models?.length)" data-testid="codex-turn-state-clear" @click="load(true)">
          {{ t('admin.accounts.codexTurnStateStatus.clear') }}
        </button>
      </div>
    </div>
    <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.savedSettings') }}</p>
    <dl v-if="state" class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs text-gray-700 dark:text-dark-200" data-testid="codex-turn-state-effective-settings">
      <dt>{{ t('admin.accounts.codexTurnStateStatus.effectiveMode') }}</dt><dd data-testid="codex-turn-state-effective-mode">{{ modeLabel(state.mode) }}</dd>
      <dt>{{ t('admin.accounts.codexTurnStateStatus.effectiveIdentity') }}</dt><dd class="font-mono">{{ state.identity_version }}</dd>
      <dt>{{ t('admin.accounts.codexTurnStateStatus.effectiveCollection') }}</dt><dd data-testid="codex-turn-state-effective-collection">{{ state.active_collection_enabled ? t('admin.accounts.openai.codexTurnStateCollectionActive') : t('admin.accounts.openai.codexTurnStateCollectionPassive') }}</dd>
      <dt>{{ t('admin.accounts.codexTurnStateStatus.effectiveLengths') }}</dt>
      <dd class="font-mono">{{ state.candidate_lengths.length ? state.candidate_lengths.join(', ') : t('admin.accounts.codexTurnStateStatus.noSelectedLengths') }}</dd>
    </dl>
    <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.localOnly') }}</p>
    <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.attemptsOnly') }}</p>
    <p v-if="errorKey" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(`admin.accounts.codexTurnStateStatus.${errorKey}`) }}</p>
    <TurnStateHunterStatus v-if="state" :state="state" />
    <p v-if="busy && !state" aria-live="polite" class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.loading') }}</p>
    <p v-else-if="state && !state.models?.length" class="text-xs text-gray-500 dark:text-dark-400" data-testid="codex-turn-state-empty">{{ t('admin.accounts.codexTurnStateStatus.empty') }}</p>
    <ul v-if="state?.models?.length" class="max-h-96 space-y-3 overflow-y-auto">
      <li v-for="entry in state.models" :key="entry.model" class="space-y-2 rounded border border-gray-200 bg-white p-3 text-xs dark:border-dark-600 dark:bg-dark-800">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="break-all text-gray-800 dark:text-gray-100">{{ t('admin.accounts.codexTurnStateStatus.outboundModel') }}: <span class="font-mono font-medium">{{ entry.model }}</span></span>
          <span class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.observed', { count: entry.observed_count }) }}</span>
        </div>
        <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-gray-600 dark:text-dark-300">
          <dt>{{ t('admin.accounts.codexTurnStateStatus.totalReuseAttempts') }}</dt>
          <dd data-testid="codex-turn-state-total-attempts">{{ entry.reuse_attempt_count ?? t('admin.accounts.codexTurnStateStatus.unavailable') }}</dd>
          <template v-if="entry.last_reuse_attempt_at">
            <dt>{{ t('admin.accounts.codexTurnStateStatus.lastReused') }}</dt><dd>{{ formatDateTime(entry.last_reuse_attempt_at) }}</dd>
          </template>
          <template v-if="entry.last_selection">
            <dt>{{ t('admin.accounts.codexTurnStateStatus.lastSelection') }}</dt>
            <dd data-testid="codex-turn-state-last-selection">{{ reasonLabel(entry.last_selection.reason) }} · {{ formatDateTime(entry.last_selection.at) }}</dd>
          </template>
          <template v-if="entry.last_candidate_rejection">
            <dt>{{ t('admin.accounts.codexTurnStateStatus.lastRejection') }}</dt>
            <dd data-testid="codex-turn-state-last-rejection">{{ reasonLabel(entry.last_candidate_rejection.reason) }} · {{ formatDateTime(entry.last_candidate_rejection.at) }}</dd>
          </template>
          <template v-if="entry.last_candidate_invalidation">
            <dt>{{ t('admin.accounts.codexTurnStateStatus.lastInvalidation') }}</dt>
            <dd data-testid="codex-turn-state-last-invalidation">{{ reasonLabel(entry.last_candidate_invalidation.reason) }} · {{ formatDateTime(entry.last_candidate_invalidation.at) }}</dd>
          </template>
        </dl>
        <section v-if="entry.collection && entry.collection.attempt_count > 0" class="space-y-1 rounded bg-gray-50 p-2 dark:bg-dark-700/60" data-testid="codex-turn-state-collection-result">
          <h5 class="font-medium">{{ t('admin.accounts.codexTurnStateStatus.collectionTitle') }}</h5>
          <p class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.collectionActualOnly') }}</p>
          <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-gray-600 dark:text-dark-300">
            <dt>{{ t('admin.accounts.codexTurnStateStatus.collectionAttempts') }}</dt><dd>{{ entry.collection.attempt_count }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.collectionResult') }}</dt><dd>{{ collectionReasonLabel(entry.collection.in_flight ? 'in_progress' : entry.collection.last_reason) }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.collectionStarted') }}</dt><dd>{{ formatDateTime(entry.collection.last_attempt_at) }}</dd>
            <template v-if="entry.collection.last_finished_at">
              <dt>{{ t('admin.accounts.codexTurnStateStatus.collectionFinished') }}</dt><dd>{{ formatDateTime(entry.collection.last_finished_at) }}</dd>
            </template>
            <template v-if="entry.collection.next_eligible_at">
              <dt>{{ t('admin.accounts.codexTurnStateStatus.collectionNextEligible') }}</dt><dd>{{ formatDateTime(entry.collection.next_eligible_at) }}</dd>
            </template>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.collectionHTTPStatus') }}</dt><dd>{{ entry.collection.last_http_status || t('admin.accounts.codexTurnStateStatus.unavailable') }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.collectionStateLength') }}</dt><dd>{{ entry.collection.last_observed_length }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.responseModel') }}</dt><dd class="break-all font-mono">{{ entry.collection.last_response_model || t('admin.accounts.codexTurnStateStatus.responseModelUnknown') }}</dd>
          </dl>
        </section>
        <section v-if="entry.last_request" class="space-y-1 rounded bg-gray-50 p-2 dark:bg-dark-700/60" data-testid="codex-turn-state-last-request">
          <h5 class="font-medium">{{ t('admin.accounts.codexTurnStateStatus.lastRequest') }}</h5>
          <p class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.lastRequestOnly') }}</p>
          <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-gray-600 dark:text-dark-300">
            <dt>{{ t('admin.accounts.codexTurnStateStatus.requestTime') }}</dt><dd>{{ formatDateTime(entry.last_request.at) }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.requestId') }}</dt><dd class="break-all font-mono">{{ entry.last_request.request_id || t('admin.accounts.codexTurnStateStatus.unavailable') }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.requestOutcome') }}</dt><dd>{{ entry.last_request.failed ? t('admin.accounts.codexTurnStateStatus.requestFailed') : t('admin.accounts.codexTurnStateStatus.requestCompleted') }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.outboundState') }}</dt><dd>{{ entry.last_request.outbound_state_length }} · {{ sourceLabel(entry.last_request.state_source) }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.selection') }}</dt><dd>{{ reasonLabel(entry.last_request.selection_reason) }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.responseModel') }}</dt>
            <dd class="break-all font-mono">{{ entry.last_request.response_model_observed && entry.last_request.upstream_response_model ? entry.last_request.upstream_response_model : t('admin.accounts.codexTurnStateStatus.responseModelUnknown') }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.modelComparison') }}</dt>
            <dd data-testid="codex-turn-state-model-comparison" :class="entry.last_request.response_model_observed && entry.last_request.model_mismatch ? 'text-amber-700 dark:text-amber-400' : ''">
              {{ !entry.last_request.response_model_observed ? t('admin.accounts.codexTurnStateStatus.responseModelUnknown') : entry.last_request.model_mismatch ? t('admin.accounts.codexTurnStateStatus.modelMismatch') : t('admin.accounts.codexTurnStateStatus.modelMatches') }}
            </dd>
          </dl>
        </section>
        <h5 class="font-medium text-gray-700 dark:text-dark-200">{{ t('admin.accounts.codexTurnStateStatus.observationHistory') }}</h5>
        <div v-if="entry.lengths.length" class="flex flex-wrap gap-2 text-gray-600 dark:text-dark-300">
          <span v-for="length in entry.lengths" :key="length.length" class="rounded bg-gray-100 px-2 py-1 font-mono dark:bg-dark-700">{{ length.length }} × {{ length.count }}</span>
          <span v-if="entry.other_length_count">{{ t('admin.accounts.codexTurnStateStatus.otherLengths', { count: entry.other_length_count }) }}</span>
        </div>
        <p v-if="entry.observed_count > 0" class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.lastObserved') }}: {{ formatDateTime(entry.last_observed_at) }}</p>
        <p v-else class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.noObservations') }}</p>
        <h5 class="font-medium text-gray-700 dark:text-dark-200">{{ t('admin.accounts.codexTurnStateStatus.currentCandidate') }}</h5>
        <p v-if="entry.candidate && state.mode !== 'reuse'" class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.reuseNotEnabled') }}</p>
        <p v-else-if="entry.candidate && !state.candidate_lengths.includes(entry.candidate.length)" class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.candidateLengthExcluded') }}</p>
        <dl v-if="entry.candidate" class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-gray-600 dark:text-dark-300" data-testid="codex-turn-state-current-candidate">
          <dt>{{ t('admin.accounts.codexTurnStateStatus.length') }}</dt><dd class="font-mono">{{ entry.candidate.length }}</dd>
          <dt>{{ t('admin.accounts.codexTurnStateStatus.digest') }}</dt><dd class="break-all font-mono">{{ entry.candidate.hash_prefix }}</dd>
          <dt>{{ t('admin.accounts.codexTurnStateStatus.issued') }}</dt><dd>{{ formatDateTime(entry.candidate.issued_at) }}</dd>
          <dt>{{ t('admin.accounts.codexTurnStateStatus.expires') }}</dt><dd>{{ formatDateTime(entry.candidate.expires_at) }}</dd>
          <dt>{{ t('admin.accounts.codexTurnStateStatus.candidateReuseAttempts') }}</dt><dd>{{ entry.candidate.reuse_count }}</dd>
        </dl>
        <p v-else class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.codexTurnStateStatus.noCandidate') }}</p>
      </li>
    </ul>
  </section>
</template>
