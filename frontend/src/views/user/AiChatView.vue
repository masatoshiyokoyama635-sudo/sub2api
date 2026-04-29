<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="grid gap-4 lg:grid-cols-4">
        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
              <Icon name="chat" size="md" />
            </div>
            <div>
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('aiChat.selectedGroup') }}</p>
              <p class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ selectedGroup?.name || t('aiChat.notSelected') }}</p>
            </div>
          </div>
        </div>
        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-100 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300">
              <Icon name="key" size="md" />
            </div>
            <div>
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('aiChat.selectedKey') }}</p>
              <p class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ selectedKey?.name || t('aiChat.notSelected') }}</p>
            </div>
          </div>
        </div>
        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-purple-100 text-purple-600 dark:bg-purple-900/30 dark:text-purple-300">
              <Icon name="brain" size="md" />
            </div>
            <div>
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('aiChat.model') }}</p>
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
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('aiChat.lastLatency') }}</p>
              <p class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ lastLatency ? `${lastLatency}ms` : '-' }}</p>
            </div>
          </div>
        </div>
      </div>

      <div class="grid gap-6 xl:grid-cols-[320px_1fr]">
        <aside class="space-y-4">
          <div class="card p-5">
            <h2 class="mb-4 text-base font-semibold text-gray-900 dark:text-white">{{ t('aiChat.configuration') }}</h2>
            <div class="space-y-4">
              <div>
                <label class="input-label">{{ t('aiChat.group') }}</label>
                <Select
                  v-model="selectedGroupId"
                  :options="groupOptions"
                  :placeholder="t('aiChat.selectGroup')"
                  :disabled="loading || groupOptions.length === 0"
                  searchable
                />
              </div>
              <div>
                <label class="input-label">{{ t('aiChat.apiKey') }}</label>
                <Select
                  v-model="selectedKeyId"
                  :options="keyOptions"
                  :placeholder="t('aiChat.selectApiKey')"
                  :disabled="loading || keyOptions.length === 0"
                  searchable
                />
              </div>
              <div>
                <label for="chat-model" class="input-label">{{ t('aiChat.model') }}</label>
                <input id="chat-model" v-model.trim="model" class="input" placeholder="gpt-5.5" />
              </div>
              <button class="btn btn-secondary w-full" :disabled="loading" @click="loadCredentials">
                <Icon name="refresh" size="sm" class="mr-2" />
                {{ t('aiChat.refreshCredentials') }}
              </button>
            </div>
          </div>

          <div class="card p-5">
            <h2 class="mb-3 text-base font-semibold text-gray-900 dark:text-white">{{ t('aiChat.usageInfo') }}</h2>
            <div class="space-y-3 text-sm text-gray-600 dark:text-dark-300">
              <div class="flex justify-between">
                <span>{{ t('aiChat.promptTokens') }}</span>
                <span class="font-medium text-gray-900 dark:text-white">{{ usage.prompt_tokens ?? '-' }}</span>
              </div>
              <div class="flex justify-between">
                <span>{{ t('aiChat.completionTokens') }}</span>
                <span class="font-medium text-gray-900 dark:text-white">{{ usage.completion_tokens ?? '-' }}</span>
              </div>
              <div class="flex justify-between">
                <span>{{ t('aiChat.totalTokens') }}</span>
                <span class="font-medium text-gray-900 dark:text-white">{{ usage.total_tokens ?? '-' }}</span>
              </div>
            </div>
          </div>
        </aside>

        <section class="card flex min-h-[640px] flex-col overflow-hidden">
          <div class="flex items-center justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('aiChat.chatWindow') }}</h2>
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('aiChat.chatHint') }}</p>
            </div>
            <button class="btn btn-ghost" :disabled="submitting" @click="clearConversation">
              <Icon name="trash" size="sm" class="mr-2" />
              {{ t('aiChat.clear') }}
            </button>
          </div>

          <div class="flex-1 space-y-4 overflow-y-auto bg-gradient-to-br from-primary-50/40 to-cyan-50/30 p-6 dark:from-dark-900 dark:to-dark-800">
            <div v-if="messages.length === 0" class="empty-state py-20">
              <Icon name="chatBubble" size="xl" class="mx-auto mb-4 text-primary-500" />
              <h3 class="empty-state-title">{{ t('aiChat.emptyTitle') }}</h3>
              <p class="empty-state-description">{{ t('aiChat.emptyDescription') }}</p>
            </div>

            <div
              v-for="message in messages"
              :key="message.id"
              class="flex"
              :class="message.role === 'user' ? 'justify-end' : 'justify-start'"
            >
              <div
                class="max-w-[78%] rounded-2xl px-4 py-3 shadow-sm"
                :class="message.role === 'user'
                  ? 'bg-primary-600 text-white'
                  : 'border border-gray-100 bg-white text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-100'"
              >
                <div class="mb-1 text-xs font-medium opacity-80">
                  {{ message.role === 'user' ? t('aiChat.you') : t('aiChat.assistant') }}
                </div>
                <div class="whitespace-pre-wrap text-sm leading-6">{{ message.content }}</div>
              </div>
            </div>
          </div>

          <form class="border-t border-gray-100 bg-white p-4 dark:border-dark-700 dark:bg-dark-900" @submit.prevent="sendMessage">
            <div v-if="credentialWarning" class="mb-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-800/50 dark:bg-amber-900/20 dark:text-amber-200">
              {{ credentialWarning }}
            </div>
            <textarea
              v-model="prompt"
              class="input min-h-[96px] resize-none"
              :placeholder="t('aiChat.inputPlaceholder')"
              :disabled="submitting"
              @keydown.enter.exact.prevent="sendMessage"
            ></textarea>
            <div class="mt-3 flex flex-wrap items-center justify-between gap-3">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('aiChat.enterHint') }}</p>
              <div class="flex gap-2">
                <button v-if="submitting" type="button" class="btn btn-secondary" @click="abortRequest">
                  <Icon name="x" size="sm" class="mr-2" />
                  {{ t('aiChat.stop') }}
                </button>
                <button type="submit" class="btn btn-primary" :disabled="!canSend">
                  <Icon name="arrowRight" size="sm" class="mr-2" />
                  {{ submitting ? t('aiChat.sending') : t('aiChat.send') }}
                </button>
              </div>
            </div>
          </form>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import { gatewayAPI, type GatewayChatMessage } from '@/api/gateway'
import { useGatewayCredentials } from '@/composables/useGatewayCredentials'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'

interface ChatMessage extends GatewayChatMessage {
  id: string
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
  selectedKey,
  loadCredentials
} = useGatewayCredentials()

const model = ref('gpt-5.5')
const prompt = ref('')
const messages = ref<ChatMessage[]>([])
const submitting = ref(false)
const lastLatency = ref<number | null>(null)
const usage = reactive({
  prompt_tokens: undefined as number | undefined,
  completion_tokens: undefined as number | undefined,
  total_tokens: undefined as number | undefined
})
const abortController = ref<AbortController | null>(null)

const groupOptions = computed(() => groups.value.map((group) => ({ value: group.id, label: `${group.name} · ${group.platform}` })))
const keyOptions = computed(() => activeKeysForSelectedGroup.value.map((key) => ({ value: key.id, label: key.name })))

const credentialWarning = computed(() => {
  if (credentialError.value) return extractApiErrorMessage(credentialError.value, t('aiChat.credentialsFailed'))
  if (!loading.value && groups.value.length === 0) return t('aiChat.noGroups')
  if (selectedGroupId.value !== null && activeKeysForSelectedGroup.value.length === 0) return t('aiChat.noKeysInGroup')
  return ''
})

const canSend = computed(() => {
  return !submitting.value && !!selectedKey.value?.key && !!model.value.trim() && !!prompt.value.trim()
})

function buildGatewayMessages(): GatewayChatMessage[] {
  return messages.value
    .filter((message) => message.role === 'user' || message.role === 'assistant')
    .map(({ role, content }) => ({ role, content }))
}

async function sendMessage() {
  if (!canSend.value || !selectedKey.value) return

  const content = prompt.value.trim()
  const userMessage: ChatMessage = { id: crypto.randomUUID(), role: 'user', content }
  messages.value = [...messages.value, userMessage]
  prompt.value = ''
  submitting.value = true
  abortController.value = new AbortController()
  const startedAt = performance.now()

  try {
    const response = await gatewayAPI.createChatCompletion(
      selectedKey.value.key,
      {
        model: model.value.trim(),
        messages: buildGatewayMessages(),
        stream: false
      },
      abortController.value.signal
    )

    lastLatency.value = Math.round(performance.now() - startedAt)
    usage.prompt_tokens = response.usage?.prompt_tokens
    usage.completion_tokens = response.usage?.completion_tokens
    usage.total_tokens = response.usage?.total_tokens

    const answer = response.choices?.[0]?.message?.content?.trim() || t('aiChat.emptyResponse')
    messages.value = [...messages.value, { id: crypto.randomUUID(), role: 'assistant', content: answer }]
  } catch (error) {
    if ((error as Error).name !== 'AbortError') {
      appStore.showError(extractApiErrorMessage(error, t('aiChat.requestFailed')))
      messages.value = messages.value.filter((message) => message.id !== userMessage.id)
      prompt.value = content
    }
  } finally {
    submitting.value = false
    abortController.value = null
  }
}

function abortRequest() {
  abortController.value?.abort()
}

function clearConversation() {
  messages.value = []
  usage.prompt_tokens = undefined
  usage.completion_tokens = undefined
  usage.total_tokens = undefined
  lastLatency.value = null
}

onBeforeUnmount(abortRequest)
</script>
