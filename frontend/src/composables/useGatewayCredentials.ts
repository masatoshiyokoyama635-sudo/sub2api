import { computed, onMounted, ref, watch } from 'vue'
import { keysAPI } from '@/api/keys'
import { userGroupsAPI } from '@/api/groups'
import type { ApiKey, Group } from '@/types'

interface UseGatewayCredentialsOptions {
  groupFilter?: (group: Group) => boolean
}

export function useGatewayCredentials(options: UseGatewayCredentialsOptions = {}) {
  const loading = ref(false)
  const error = ref<unknown | null>(null)
  const groups = ref<Group[]>([])
  const apiKeys = ref<ApiKey[]>([])
  const selectedGroupId = ref<number | null>(null)
  const selectedKeyId = ref<number | null>(null)
  let keyRequestId = 0
  let skipNextGroupWatch = false

  const visibleGroups = computed(() => {
    const filter = options.groupFilter
    return filter ? groups.value.filter(filter) : groups.value
  })

  const activeKeysForSelectedGroup = computed(() => {
    if (selectedGroupId.value === null) return []
    return apiKeys.value.filter((key) => key.group_id === selectedGroupId.value && key.status === 'active')
  })

  const selectedGroup = computed(() => visibleGroups.value.find((group) => group.id === selectedGroupId.value) || null)
  const selectedKey = computed(() => activeKeysForSelectedGroup.value.find((key) => key.id === selectedKeyId.value) || null)

  async function loadApiKeysForGroup(groupId: number | null) {
    const requestId = ++keyRequestId
    selectedKeyId.value = null

    if (groupId === null) {
      apiKeys.value = []
      return
    }

    const firstPage = await keysAPI.list(1, 100, { group_id: groupId, sort_by: 'created_at', sort_order: 'desc' })
    const pages = firstPage.pages || 1
    const remainingPages = Array.from({ length: Math.max(0, pages - 1) }, (_, index) => index + 2)
    const remaining = await Promise.all(
      remainingPages.map((page) => keysAPI.list(page, 100, { group_id: groupId, sort_by: 'created_at', sort_order: 'desc' }))
    )

    if (requestId === keyRequestId) {
      apiKeys.value = [firstPage, ...remaining].flatMap((page) => page.items)
    }
  }

  async function loadCredentials() {
    loading.value = true
    error.value = null
    try {
      groups.value = await userGroupsAPI.getAvailable()
      const nextGroupId = visibleGroups.value.some((group) => group.id === selectedGroupId.value)
        ? selectedGroupId.value
        : visibleGroups.value[0]?.id ?? null
      if (selectedGroupId.value !== nextGroupId) {
        skipNextGroupWatch = true
        selectedGroupId.value = nextGroupId
      }
      await loadApiKeysForGroup(nextGroupId)
    } catch (loadError) {
      error.value = loadError
      apiKeys.value = []
      selectedKeyId.value = null
    } finally {
      loading.value = false
    }
  }

  watch(selectedGroupId, async (groupId, previousGroupId) => {
    if (skipNextGroupWatch) {
      skipNextGroupWatch = false
      return
    }
    if (groupId === previousGroupId) return
    loading.value = true
    error.value = null
    try {
      await loadApiKeysForGroup(groupId)
    } catch (loadError) {
      error.value = loadError
      apiKeys.value = []
      selectedKeyId.value = null
    } finally {
      loading.value = false
    }
  })

  watch(activeKeysForSelectedGroup, (keys) => {
    if (!keys.some((key) => key.id === selectedKeyId.value)) {
      selectedKeyId.value = keys[0]?.id ?? null
    }
  }, { immediate: true })

  watch(visibleGroups, (nextGroups) => {
    if (!nextGroups.some((group) => group.id === selectedGroupId.value)) {
      selectedGroupId.value = nextGroups[0]?.id ?? null
    }
  })

  onMounted(loadCredentials)

  return {
    loading,
    error,
    groups: visibleGroups,
    apiKeys,
    activeKeysForSelectedGroup,
    selectedGroupId,
    selectedKeyId,
    selectedGroup,
    selectedKey,
    loadCredentials
  }
}
