<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Proxy } from '@/types'
import {
  HUNTER_REASONING_EFFORTS, parseHunterModels, validateTurnStateHunter,
  type TurnStateHunterConfig, type TurnStateRecoveryConfig
} from '@/utils/turnStateHunter'
import { parseCodexTurnStateLengths } from '@/utils/codexTurnState'

const props = defineProps<{
  hunter: TurnStateHunterConfig
  recovery: TurnStateRecoveryConfig
  proxies: Proxy[]
  lengths: string
  eligible: boolean
  recoveryEligible: boolean
  idPrefix: string
}>()
const emit = defineEmits<{
  'update:hunter': [value: TurnStateHunterConfig]
  'update:recovery': [value: TurnStateRecoveryConfig]
}>()
const { t } = useI18n()
const label = (key: string) => t(`admin.accounts.turnStateHunter.${key}`)
function updateHunter<K extends keyof TurnStateHunterConfig>(key: K, value: TurnStateHunterConfig[K]) {
  emit('update:hunter', { ...props.hunter, [key]: value })
}
function updateRecovery<K extends keyof TurnStateRecoveryConfig>(key: K, value: TurnStateRecoveryConfig[K]) {
  emit('update:recovery', { ...props.recovery, [key]: value })
}
const checked = (event: Event) => (event.target as HTMLInputElement).checked
const textValue = (event: Event) => (event.target as HTMLInputElement).value
const numberValue = (event: Event): number | null => textValue(event) === '' ? null : Number(textValue(event))
const proxyOptions = computed(() => {
  const known = props.proxies.map((proxy) => ({
    id: proxy.id, label: `${proxy.name} (${proxy.protocol}://${proxy.host}:${proxy.port})`,
    inactive: proxy.status !== 'active',
    rotates: proxy.host.toLowerCase().endsWith('webshare.io') && proxy.username?.toLowerCase().endsWith('-rotate') === true
  }))
  for (const id of props.hunter.proxy_ids) {
    if (!known.some((proxy) => proxy.id === id)) known.push({ id, label: `${label('unavailableProxy')} #${id}`, inactive: true, rotates: false })
  }
  return known
})
const selectedProxies = computed(() => proxyOptions.value.filter((proxy) => props.hunter.proxy_ids.includes(proxy.id)))
const selectedProxyIDs = computed({
  get: () => props.hunter.proxy_ids,
  set: (ids: number[]) => emit('update:hunter', { ...props.hunter, proxy_ids: ids, rotating_proxy_ids: props.hunter.rotating_proxy_ids.filter((id) => ids.includes(id)) })
})
function setRotating(id: number, enabled: boolean) {
  updateHunter('rotating_proxy_ids', enabled ? [...new Set([...props.hunter.rotating_proxy_ids, id])] : props.hunter.rotating_proxy_ids.filter((value) => value !== id))
}
const errorKey = computed(() => props.eligible || props.recoveryEligible ? validateTurnStateHunter(
  { ...props.hunter, enabled: props.eligible && props.hunter.enabled },
  { ...props.recovery, enabled: props.recoveryEligible && props.recovery.enabled },
  parseCodexTurnStateLengths(props.lengths)
) : null)
const hunterNumbers = [
  { key: 'max_per_hour', label: 'hourlyLimit', placeholder: '30', min: 0, max: 600 },
  { key: 'gap_seconds', label: 'gap', placeholder: '20', min: 0, max: 600 },
  { key: 'lead_minutes', label: 'lead', placeholder: '10', min: 0, max: 55 },
  { key: 'idle_minutes', label: 'idleMinutes', placeholder: '60', min: -1, max: 1440 },
  { key: 'retry_minutes', label: 'retry', placeholder: '10', min: 0, max: 1440 },
  { key: 'usage_api_key_id', label: 'usageKey', placeholder: '', min: 1, max: 2147483647 }
] as const
const recoveryNumbers = [
  { key: 'streak_target', label: 'streakTarget', placeholder: '5', min: 1, max: 50 },
  { key: 'cooldown_hours', label: 'cooldown', placeholder: '16', min: 1, max: 168 },
  { key: 'min_minutes', label: 'minInterval', placeholder: '30', min: 1, max: 1440 },
  { key: 'max_minutes', label: 'maxInterval', placeholder: '90', min: 1, max: 1440 },
  { key: 'usage_api_key_id', label: 'recoveryUsageKey', placeholder: '', min: 1, max: 2147483647 }
] as const
</script>

<template>
  <section tabindex="-1" class="my-4 space-y-3 rounded-lg border border-gray-200 p-3 focus:outline-none focus:ring-2 focus:ring-primary-500/40 dark:border-dark-600" :data-testid="`${idPrefix}-hunter-section`">
    <div class="flex items-center justify-between gap-4">
      <div>
        <label :for="`${idPrefix}-hunter-enabled`" class="input-label mb-0">{{ label('title') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ label('description') }}</p>
      </div>
      <input :id="`${idPrefix}-hunter-enabled`" :data-testid="`${idPrefix}-hunter-enabled`" type="checkbox" class="h-4 w-4 flex-shrink-0 rounded text-primary-600" :checked="hunter.enabled" :disabled="!eligible" @change="updateHunter('enabled', checked($event))" />
    </div>
    <p v-if="!eligible" class="text-xs text-amber-700 dark:text-amber-400" :data-testid="`${idPrefix}-hunter-needs-reuse`">{{ label('needsReuse') }}</p>
    <fieldset v-if="hunter.enabled" :disabled="!eligible" class="grid gap-3 sm:grid-cols-2">
      <div>
        <label :for="`${idPrefix}-hunter-models`" class="input-label text-xs">{{ label('models') }}</label>
        <textarea :id="`${idPrefix}-hunter-models`" :data-testid="`${idPrefix}-hunter-models`" :value="hunter.models.join('\n')" :disabled="!eligible || hunter.auto_models" rows="3" class="input text-xs" :placeholder="label('modelsPlaceholder')" @change="updateHunter('models', parseHunterModels(textValue($event)))" />
        <label class="mt-2 flex items-start gap-2 text-xs text-gray-600 dark:text-gray-300">
          <input type="checkbox" class="mt-0.5 rounded text-primary-600" :data-testid="`${idPrefix}-hunter-auto-models`" :checked="hunter.auto_models" @change="updateHunter('auto_models', checked($event))" />
          <span>{{ label('autoModels') }}</span>
        </label>
      </div>
      <div>
        <label :for="`${idPrefix}-hunter-proxies`" class="input-label text-xs">{{ label('proxies') }}</label>
        <select :id="`${idPrefix}-hunter-proxies`" :data-testid="`${idPrefix}-hunter-proxies`" v-model="selectedProxyIDs" multiple size="4" class="input min-h-24 text-xs">
          <option v-for="proxy in proxyOptions" :key="proxy.id" :value="proxy.id" :disabled="proxy.inactive && !hunter.proxy_ids.includes(proxy.id)">{{ proxy.label }}</option>
        </select>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ label('proxiesHint') }}</p>
        <p v-if="!proxyOptions.length" class="mt-1 text-xs text-amber-700 dark:text-amber-400">{{ label('noProxies') }}</p>
        <p v-if="selectedProxies.length" class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ label('rotationHint') }}</p>
        <label v-for="proxy in selectedProxies" :key="proxy.id" class="mt-1 flex items-start gap-2 text-xs">
          <input type="checkbox" class="mt-0.5 rounded text-primary-600" :data-testid="`${idPrefix}-hunter-rotating-${proxy.id}`" :checked="proxy.rotates || hunter.rotating_proxy_ids.includes(proxy.id)" :disabled="!eligible || proxy.rotates" @change="setRotating(proxy.id, checked($event))" />
          <span class="break-all">{{ proxy.label }}<span v-if="proxy.rotates"> · {{ label('autoRotation') }}</span></span>
        </label>
      </div>
      <div v-for="field in hunterNumbers" :key="field.key">
        <label :for="`${idPrefix}-hunter-${field.key}`" class="input-label text-xs">{{ label(field.label) }}</label>
        <input :id="`${idPrefix}-hunter-${field.key}`" :data-testid="`${idPrefix}-hunter-${field.key}`" :value="hunter[field.key]" type="number" step="1" :min="field.min" :max="field.max" :placeholder="field.placeholder" class="input text-xs" @input="updateHunter(field.key, numberValue($event))" />
      </div>
      <div>
        <label :for="`${idPrefix}-hunter-effort`" class="input-label text-xs">{{ label('effort') }}</label>
        <select :id="`${idPrefix}-hunter-effort`" :value="hunter.reasoning_effort" class="input text-xs" @change="updateHunter('reasoning_effort', textValue($event))">
          <option value="">{{ label('defaultEffort') }}</option>
          <option v-for="effort in HUNTER_REASONING_EFFORTS" :key="effort" :value="effort">{{ effort }}</option>
        </select>
      </div>
      <label class="flex items-start gap-2 text-xs sm:col-span-2">
        <input type="checkbox" class="mt-0.5 rounded text-primary-600" :data-testid="`${idPrefix}-hunter-hold`" :checked="hunter.hold_when_degraded" @change="updateHunter('hold_when_degraded', checked($event))" />
        <span><span class="font-medium">{{ label('hold') }}</span><span class="mt-1 block text-gray-500 dark:text-gray-400">{{ label('holdHint') }}</span></span>
      </label>
      <p class="text-xs text-gray-500 dark:text-gray-400 sm:col-span-2">{{ label('lengthsHint') }} {{ lengths || '—' }}</p>
    </fieldset>
    <div class="space-y-3 rounded border border-gray-200 p-3 dark:border-dark-600">
      <div class="flex items-center justify-between gap-4">
        <div>
          <label :for="`${idPrefix}-recovery-enabled`" class="input-label mb-0 text-xs">{{ label('recoveryTitle') }}</label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ label('recoveryHint') }}</p>
        </div>
        <input :id="`${idPrefix}-recovery-enabled`" :data-testid="`${idPrefix}-recovery-enabled`" type="checkbox" class="h-4 w-4 flex-shrink-0 rounded text-primary-600" :checked="recovery.enabled" :disabled="!recoveryEligible" @change="updateRecovery('enabled', checked($event))" />
      </div>
      <fieldset v-if="recovery.enabled" :disabled="!recoveryEligible" class="grid gap-3 sm:grid-cols-2">
        <div class="sm:col-span-2">
          <label :for="`${idPrefix}-recovery-model`" class="input-label text-xs">{{ label('recoveryModel') }}</label>
          <input :id="`${idPrefix}-recovery-model`" :value="recovery.model" class="input text-xs" :placeholder="label('recoveryModelAuto')" @input="updateRecovery('model', textValue($event))" />
        </div>
        <div v-for="field in recoveryNumbers" :key="field.key">
          <label :for="`${idPrefix}-recovery-${field.key}`" class="input-label text-xs">{{ label(field.label) }}</label>
          <input :id="`${idPrefix}-recovery-${field.key}`" :data-testid="`${idPrefix}-recovery-${field.key}`" :value="recovery[field.key]" type="number" step="1" :min="field.min" :max="field.max" :placeholder="field.placeholder" class="input text-xs" @input="updateRecovery(field.key, numberValue($event))" />
        </div>
        <div>
          <label :for="`${idPrefix}-recovery-effort`" class="input-label text-xs">{{ label('effort') }}</label>
          <select :id="`${idPrefix}-recovery-effort`" :value="recovery.reasoning_effort" class="input text-xs" @change="updateRecovery('reasoning_effort', textValue($event))">
            <option value="">{{ label('defaultEffort') }}</option>
            <option v-for="effort in HUNTER_REASONING_EFFORTS" :key="effort" :value="effort">{{ effort }}</option>
          </select>
        </div>
      </fieldset>
    </div>
    <p v-if="errorKey" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ label(errorKey) }}</p>
  </section>
</template>
