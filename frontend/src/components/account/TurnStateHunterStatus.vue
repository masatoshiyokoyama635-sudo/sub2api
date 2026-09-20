<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CodexTurnStateStatus } from '@/api/admin/codexTurnState'
import { formatDateTime } from '@/utils/format'

defineProps<{ state: CodexTurnStateStatus }>()
const { t } = useI18n()
const label = (key: string) => t(`admin.accounts.turnStateHunter.${key}`)
const validDate = (value?: string) => !!value && !value.startsWith('0001-')
function gateLabel(gate?: string, capWait?: boolean) {
  if (capWait) return label('capWait')
  if (!gate) return label('running')
  return label(gate === 'idle' || gate === 'fresh' ? gate : 'unknownGate')
}
</script>

<template>
  <div class="space-y-3">
    <section v-if="state.hunter_models?.length" class="space-y-2 rounded border border-gray-200 bg-white p-3 text-xs dark:border-dark-600 dark:bg-dark-800" data-testid="codex-hunter-candidates">
      <h5 class="font-medium">{{ label('candidatesTitle') }}</h5>
      <p v-if="state.hunter_shared_cache !== undefined" class="text-gray-500 dark:text-gray-400" data-testid="codex-hunter-cache-scope">{{ label(state.hunter_shared_cache ? 'sharedCache' : 'localCache') }}</p>
      <ul class="space-y-3">
        <li v-for="entry in state.hunter_models" :key="entry.model" class="space-y-1 rounded bg-gray-50 p-2 dark:bg-dark-700/60" :data-testid="`codex-hunter-candidate-${entry.model}`">
          <p class="break-all font-mono font-medium">{{ entry.model }}</p>
          <dl v-if="entry.candidate" class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
            <dt>{{ t('admin.accounts.codexTurnStateStatus.length') }}</dt><dd>{{ entry.candidate.length }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.expires') }}</dt><dd>{{ formatDateTime(entry.candidate.expires_at) }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.digest') }}</dt><dd class="break-all font-mono">{{ entry.candidate.hash_prefix }}</dd>
            <dt>{{ t('admin.accounts.codexTurnStateStatus.candidateReuseAttempts') }}</dt><dd>{{ entry.candidate.reuse_count }}</dd>
          </dl>
          <p v-else class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.codexTurnStateStatus.noCandidate') }}</p>
        </li>
      </ul>
    </section>
    <section v-if="state.hunter_enabled !== undefined || state.hunter" class="space-y-2 rounded border border-gray-200 bg-white p-3 text-xs dark:border-dark-600 dark:bg-dark-800" data-testid="codex-hunter-status">
      <h5 class="font-medium">{{ label('runtimeTitle') }} · {{ state.hunter_enabled ? label('enabled') : label('disabled') }}</h5>
      <p v-if="!state.hunter">{{ label('noRuntime') }}</p>
      <dl v-else class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
        <dt>{{ label('gate') }}</dt><dd data-testid="codex-hunter-gate">{{ gateLabel(state.hunter.gate, state.hunter.cap_wait) }}</dd>
        <dt>{{ label('hourCount') }}</dt><dd>{{ state.hunter.hour_count }}</dd>
        <template v-if="validDate(state.hunter.next_at)"><dt>{{ label('nextAt') }}</dt><dd>{{ formatDateTime(state.hunter.next_at) }}</dd></template>
        <template v-if="validDate(state.hunter.updated_at)"><dt>{{ label('updatedAt') }}</dt><dd>{{ formatDateTime(state.hunter.updated_at) }}</dd></template>
        <template v-if="state.hunter.exits?.length"><dt>{{ label('exitCount') }}</dt><dd>{{ state.hunter.exits.length }}</dd></template>
        <template v-if="validDate(state.hunter.rate_limit_until)"><dt>{{ label('rateLimitUntil') }}</dt><dd>{{ formatDateTime(state.hunter.rate_limit_until!) }}</dd></template>
      </dl>
      <p v-if="state.hunter?.auth_blocked_credential" class="text-amber-700 dark:text-amber-400">{{ label('authBlocked') }}</p>
      <p v-if="state.hunter?.last_error" class="text-amber-700 dark:text-amber-400">{{ label('runtimeError') }}</p>
    </section>
    <section v-if="state.recovery_enabled !== undefined || state.recovery" class="space-y-2 rounded border border-gray-200 bg-white p-3 text-xs dark:border-dark-600 dark:bg-dark-800" data-testid="codex-recovery-status">
      <h5 class="font-medium">{{ label('recoveryStatusTitle') }} · {{ state.recovery_enabled ? label('enabled') : label('disabled') }}</h5>
      <p v-if="!state.recovery">{{ label('noRuntime') }}</p>
      <dl v-else class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
        <dt>{{ label('streak') }}</dt><dd>{{ state.recovery.streak ?? 0 }}</dd>
        <dt>{{ label('failStreak') }}</dt><dd>{{ state.recovery.fail_streak ?? 0 }}</dd>
        <template v-if="validDate(state.recovery.next_at)"><dt>{{ label('nextAt') }}</dt><dd>{{ formatDateTime(state.recovery.next_at) }}</dd></template>
        <template v-if="validDate(state.recovery.recovered_at)"><dt>{{ label('recoveredAt') }}</dt><dd>{{ formatDateTime(state.recovery.recovered_at!) }}</dd></template>
        <template v-if="validDate(state.recovery.cooling_until)"><dt>{{ label('coolingUntil') }}</dt><dd>{{ formatDateTime(state.recovery.cooling_until!) }}</dd></template>
        <template v-if="validDate(state.recovery.rate_limit_until)"><dt>{{ label('rateLimitUntil') }}</dt><dd>{{ formatDateTime(state.recovery.rate_limit_until!) }}</dd></template>
      </dl>
      <p v-if="state.recovery?.auth_blocked_credential" class="text-amber-700 dark:text-amber-400">{{ label('authBlocked') }}</p>
      <p v-if="state.recovery?.last_error" class="text-amber-700 dark:text-amber-400">{{ label('runtimeError') }}</p>
    </section>
    <section v-for="entry in [{ key: 'hunter', title: 'runtimeTitle', attempts: state.hunter?.last }, { key: 'recovery', title: 'recoveryStatusTitle', attempts: state.recovery?.last }].filter((entry) => entry.attempts?.length)" :key="entry.key" class="space-y-2 text-xs" :data-testid="`codex-${entry.key}-attempts`">
      <h5 class="font-medium">{{ label(entry.title) }}</h5>
      <p class="text-gray-500 dark:text-gray-400">{{ label('attemptsHint') }}</p>
      <div class="overflow-x-auto">
        <table class="w-full text-left text-xs">
          <thead><tr class="border-b border-gray-200 dark:border-dark-600"><th v-for="column in ['attemptTime', 'attemptModel', 'attemptResponseModel', 'attemptProxy', 'attemptStatus', 'attemptLength', 'attemptResult', 'attemptLatency']" :key="column" scope="col" class="whitespace-nowrap px-2 py-1 font-medium">{{ label(column) }}</th></tr></thead>
          <tbody>
            <tr v-for="(attempt, index) in entry.attempts" :key="index" class="border-b border-gray-100 dark:border-dark-700">
              <td class="whitespace-nowrap px-2 py-1">{{ formatDateTime(attempt.at) }}</td>
              <td class="break-all px-2 py-1 font-mono">{{ attempt.model }}</td>
              <td class="break-all px-2 py-1 font-mono">{{ attempt.response_model || '—' }}</td>
              <td class="px-2 py-1">{{ attempt.proxy_id || '—' }}</td>
              <td class="px-2 py-1">{{ attempt.status || '—' }}</td>
              <td class="px-2 py-1">{{ attempt.chars }}</td>
              <td class="whitespace-nowrap px-2 py-1">{{ attempt.healthy ? label('accepted') : label('notAccepted') }}</td>
              <td class="px-2 py-1">{{ attempt.latency_ms }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>
