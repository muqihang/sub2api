import { computed, ref } from 'vue'
import { getActivePinia } from 'pinia'
import { keysAPI } from '@/api/keys'
import { useAuthStore } from '@/stores/auth'
import type { ApiKey } from '@/types'

const loaded = ref(false)
const loading = ref(false)
const hasAllowedBatchImageKey = ref(false)
let pendingLoad: Promise<boolean> | null = null
let loadedForUserId: number | null = null
let loadGeneration = 0
const pageSize = 100

function keyAllowsBatchImage(key: ApiKey): boolean {
  return (
    key.status === 'active' &&
    key.group?.platform === 'gemini' &&
    key.group?.allow_batch_image_generation === true
  )
}

function resetBatchImageAccess(userId: number | null = null): void {
  loaded.value = false
  loading.value = false
  hasAllowedBatchImageKey.value = false
  pendingLoad = null
  loadedForUserId = userId
  loadGeneration += 1
}

export async function loadBatchImageAccess(force = false): Promise<boolean> {
  if (!getActivePinia()) {
    loaded.value = true
    hasAllowedBatchImageKey.value = false
    return false
  }
  const authStore = useAuthStore()
  const userId = authStore.user?.id ?? null
  if (!authStore.isAuthenticated || userId === null) {
    resetBatchImageAccess()
    loaded.value = true
    return false
  }

  if (loadedForUserId !== userId) {
    resetBatchImageAccess(userId)
  }

  if (loaded.value && !force) {
    return hasAllowedBatchImageKey.value
  }

  if (pendingLoad && !force) {
    return pendingLoad
  }

  loading.value = true
  const generation = ++loadGeneration
  const load = (async () => {
    let page = 1
    while (true) {
      const response = await keysAPI.list(page, pageSize, {
        status: 'active',
        sort_by: 'created_at',
        sort_order: 'desc'
      })
      if (useAuthStore().user?.id !== userId || generation !== loadGeneration) {
        return false
      }

      if ((response.items || []).some(keyAllowsBatchImage)) {
        hasAllowedBatchImageKey.value = true
        loaded.value = true
        return true
      }

      if (page >= response.pages || (response.items || []).length === 0) {
        hasAllowedBatchImageKey.value = false
        loaded.value = true
        return false
      }

      page += 1
    }
  })()
    .catch(() => {
      if (useAuthStore().user?.id === userId && generation === loadGeneration) {
        hasAllowedBatchImageKey.value = false
        loaded.value = true
      }
      return false
    })
    .finally(() => {
      if (pendingLoad === load) {
        loading.value = false
        pendingLoad = null
      }
    })

  pendingLoad = load
  return pendingLoad
}

export function useBatchImageAccess() {
  const canUseBatchImage = computed(() => hasAllowedBatchImageKey.value)

  return {
    canUseBatchImage,
    batchImageAccessLoaded: computed(() => loaded.value),
    batchImageAccessLoading: computed(() => loading.value),
    refreshBatchImageAccess: loadBatchImageAccess,
  }
}
