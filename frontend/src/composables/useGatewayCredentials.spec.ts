import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useGatewayCredentials } from '@/composables/useGatewayCredentials'
import { keysAPI } from '@/api/keys'
import { userGroupsAPI } from '@/api/groups'
import type { ApiKey, Group } from '@/types'

vi.mock('@/api/keys', () => ({
  keysAPI: {
    list: vi.fn()
  }
}))

vi.mock('@/api/groups', () => ({
  userGroupsAPI: {
    getAvailable: vi.fn()
  }
}))

const groups = [
  { id: 1, name: 'OpenAI', platform: 'openai' },
  { id: 2, name: 'Claude', platform: 'anthropic' }
] as Group[]

const apiKeys = [
  { id: 10, name: 'OpenAI active', key: 'sk-openai', group_id: 1, status: 'active' },
  { id: 11, name: 'OpenAI inactive', key: 'sk-inactive', group_id: 1, status: 'inactive' },
  { id: 20, name: 'Claude active', key: 'sk-claude', group_id: 2, status: 'active' }
] as ApiKey[]

function mockCredentials() {
  vi.mocked(userGroupsAPI.getAvailable).mockResolvedValue(groups)
  vi.mocked(keysAPI.list).mockImplementation((_page, _pageSize, filters) => {
    const groupId = Number(filters?.group_id)
    const items = apiKeys.filter((key) => key.group_id === groupId)
    return Promise.resolve({
      items,
      total: items.length,
      page: 1,
      page_size: 100,
      pages: 1
    })
  })
}

function mountCredentials(options?: Parameters<typeof useGatewayCredentials>[0]) {
  let credentials: ReturnType<typeof useGatewayCredentials> | undefined

  const Harness = defineComponent({
    setup() {
      credentials = useGatewayCredentials(options)
      return () => null
    }
  })

  mount(Harness)
  return () => credentials!
}

describe('useGatewayCredentials', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockCredentials()
  })

  it('selects the first group and only active API keys in that group', async () => {
    const getCredentials = mountCredentials()

    await flushPromises()

    const credentials = getCredentials()
    expect(keysAPI.list).toHaveBeenCalledWith(1, 100, expect.objectContaining({ group_id: 1 }))
    expect(credentials.selectedGroupId.value).toBe(1)
    expect(credentials.selectedKeyId.value).toBe(10)
    expect(credentials.activeKeysForSelectedGroup.value.map((key) => key.id)).toEqual([10])
  })

  it('loads API keys for the newly selected group', async () => {
    const getCredentials = mountCredentials()

    await flushPromises()
    const credentials = getCredentials()
    credentials.selectedGroupId.value = 2
    await nextTick()
    await flushPromises()

    expect(keysAPI.list).toHaveBeenCalledWith(1, 100, expect.objectContaining({ group_id: 2 }))
    expect(credentials.selectedKeyId.value).toBe(20)
    expect(credentials.activeKeysForSelectedGroup.value.map((key) => key.id)).toEqual([20])
  })

  it('loads API keys after refreshing credentials and then changing groups', async () => {
    const getCredentials = mountCredentials()

    await flushPromises()
    const credentials = getCredentials()
    await credentials.loadCredentials()
    credentials.selectedGroupId.value = 2
    await nextTick()
    await flushPromises()

    expect(keysAPI.list).toHaveBeenCalledWith(1, 100, expect.objectContaining({ group_id: 2 }))
    expect(credentials.selectedKeyId.value).toBe(20)
  })

  it('filters visible groups before selecting credentials', async () => {
    const getCredentials = mountCredentials({ groupFilter: (group) => group.platform === 'openai' })

    await flushPromises()

    const credentials = getCredentials()
    expect(credentials.groups.value.map((group) => group.platform)).toEqual(['openai'])
    expect(credentials.selectedGroupId.value).toBe(1)
    expect(credentials.selectedKeyId.value).toBe(10)
  })

  it('exposes credential loading errors without throwing unhandled rejections', async () => {
    const loadError = new Error('load failed')
    vi.mocked(userGroupsAPI.getAvailable).mockRejectedValue(loadError)
    const getCredentials = mountCredentials()

    await flushPromises()

    const credentials = getCredentials()
    expect(credentials.error.value).toBe(loadError)
    expect(credentials.loading.value).toBe(false)
    expect(credentials.apiKeys.value).toEqual([])
    expect(credentials.selectedKeyId.value).toBeNull()
  })
})
