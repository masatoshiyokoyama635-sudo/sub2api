<template>
  <fieldset :disabled="disabled" class="my-4 space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <div class="flex items-center justify-between gap-4">
      <div class="min-w-0">
        <label :for="`${idPrefix}-mode`" class="input-label mb-0">{{ t('admin.accounts.openai.codexTurnStateMode') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.codexTurnStateModeDesc') }}</p>
      </div>
      <div class="w-52 flex-shrink-0">
        <Select
          :id="`${idPrefix}-mode`"
          :model-value="mode"
          :disabled="disabled"
          :data-testid="`${idPrefix}-mode`"
          :options="modeOptions"
          @update:model-value="$emit('update:mode', $event as CodexTurnStateMode)"
        />
      </div>
    </div>
    <div>
      <label :for="`${idPrefix}-lengths`" class="input-label">{{ t('admin.accounts.openai.codexTurnStateLengths') }}</label>
      <input
        :id="`${idPrefix}-lengths`"
        :value="lengths"
        type="text"
        class="input"
        :data-testid="`${idPrefix}-lengths`"
        :disabled="disabled || mode === 'off'"
        :aria-describedby="`${idPrefix}-lengths-hint`"
        :aria-invalid="invalidLengths"
        placeholder="292, 332"
        @input="$emit('update:lengths', ($event.target as HTMLInputElement).value)"
      />
      <p :id="`${idPrefix}-lengths-hint`" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.codexTurnStateLengthsDesc') }}</p>
      <p v-if="invalidLengths" role="alert" class="mt-1 text-xs text-red-600">{{ t('admin.accounts.openai.codexTurnStateLengthsInvalid') }}</p>
    </div>
  </fieldset>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { parseCodexTurnStateLengths, type CodexTurnStateMode } from '@/utils/codexTurnState'

const props = withDefaults(defineProps<{
  mode: CodexTurnStateMode
  lengths: string
  idPrefix: string
  disabled?: boolean
}>(), { disabled: false })

defineEmits<{
  'update:mode': [value: CodexTurnStateMode]
  'update:lengths': [value: string]
}>()

const { t } = useI18n()
const modeOptions = computed(() => [
  { value: 'off', label: t('admin.accounts.openai.codexTurnStateOff') },
  { value: 'observe', label: t('admin.accounts.openai.codexTurnStateObserve') },
  { value: 'reuse', label: t('admin.accounts.openai.codexTurnStateReuse') },
])
const invalidLengths = computed(() => !props.disabled && props.mode !== 'off' && parseCodexTurnStateLengths(props.lengths) === null)
</script>
