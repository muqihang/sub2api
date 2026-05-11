<template>
  <section class="rounded-xl border border-gray-200 bg-gray-50/80 p-4 dark:border-dark-700 dark:bg-dark-800/60">
    <div class="flex items-start justify-between gap-4">
      <div class="space-y-1">
        <p class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('keys.zhumengAgent.title') }}
        </p>
        <p class="text-sm text-gray-600 dark:text-gray-400">
          {{ t('keys.zhumengAgent.description') }}
        </p>
      </div>
      <span class="rounded-full bg-gray-200 px-2.5 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-300">
        Codex
      </span>
    </div>

    <div class="mt-4 flex flex-wrap items-center gap-3">
      <button
        type="button"
        class="btn btn-primary"
        :disabled="isSetupDisabled || isCreating"
        @click="handleSetup"
      >
        {{ isCreating ? t('keys.zhumengAgent.creating') : t('keys.zhumengAgent.setup') }}
      </button>
      <button
        type="button"
        class="btn btn-secondary"
        @click="showFallbackHelp = true"
      >
        {{ t('keys.zhumengAgent.install') }}
      </button>
      <span v-if="isSetupDisabled" class="text-xs text-amber-600 dark:text-amber-400">
        {{ t('keys.zhumengAgent.keyIdRequired') }}
      </span>
    </div>

    <div
      v-if="showFallbackHelp"
      class="mt-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300"
    >
      {{ t('keys.zhumengAgent.fallbackHelp') }}
    </div>

    <div class="mt-4 space-y-3">
      <div class="flex items-center justify-between">
        <p class="text-sm font-medium text-gray-800 dark:text-gray-200">
          {{ t('keys.zhumengAgent.devicesTitle') }}
        </p>
        <button
          type="button"
          class="text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
          @click="loadDevices"
        >
          {{ t('common.refresh') }}
        </button>
      </div>

      <div v-if="isLoadingDevices" class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('common.loading') }}
      </div>

      <div v-else-if="devices.length === 0" class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('keys.zhumengAgent.emptyDevices') }}
      </div>

      <ul v-else class="space-y-2">
        <li
          v-for="device in devices"
          :key="device.id"
          class="flex items-center justify-between gap-4 rounded-lg border border-gray-200 bg-white px-3 py-3 dark:border-dark-700 dark:bg-dark-900/60"
        >
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <p class="truncate text-sm font-medium text-gray-900 dark:text-white">
                {{ device.name }}
              </p>
              <span
                class="rounded-full px-2 py-0.5 text-xs font-medium"
                :class="statusClass(device.status)"
              >
                {{ statusLabel(device.status) }}
              </span>
            </div>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ device.platform }} / {{ device.arch }}
            </p>
          </div>

          <div class="flex items-center gap-2">
            <template v-if="pendingRevokeId === device.id">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                @click="pendingRevokeId = null"
              >
                {{ t('common.cancel') }}
              </button>
              <button
                type="button"
                class="btn btn-primary btn-sm"
                :disabled="revokingId === device.id"
                @click="confirmRevoke(device.id)"
              >
                {{ revokingId === device.id ? t('keys.zhumengAgent.revoking') : t('keys.zhumengAgent.confirmRevoke') }}
              </button>
            </template>
            <button
              v-else
              type="button"
              class="btn btn-secondary btn-sm"
              @click="pendingRevokeId = device.id"
            >
              {{ t('keys.zhumengAgent.revoke') }}
            </button>
          </div>
        </li>
      </ul>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CodexManagedDevice } from '@/types'
import { createCodexSetupGrant, listCodexManagedDevices, revokeCodexManagedDevice } from '@/api/zhumengAgent'

interface Props {
  apiKeyId: number | null
}

const props = defineProps<Props>()
const { t } = useI18n()

const isCreating = ref(false)
const isLoadingDevices = ref(false)
const showFallbackHelp = ref(false)
const devices = ref<CodexManagedDevice[]>([])
const pendingRevokeId = ref<number | null>(null)
const revokingId = ref<number | null>(null)
let loadVersion = 0

const isSetupDisabled = computed(() => props.apiKeyId == null)

const loadDevices = async () => {
  const currentVersion = ++loadVersion
  if (props.apiKeyId == null) {
    devices.value = []
    return
  }

  isLoadingDevices.value = true
  try {
    const result = await listCodexManagedDevices(props.apiKeyId)
    if (currentVersion === loadVersion && props.apiKeyId != null) {
      devices.value = result
    }
  } finally {
    if (currentVersion === loadVersion) {
      isLoadingDevices.value = false
    }
  }
}

const handleSetup = async () => {
  if (props.apiKeyId == null || isCreating.value) return

  isCreating.value = true
  showFallbackHelp.value = false
  try {
    const result = await createCodexSetupGrant(props.apiKeyId)
    window.open(result.deeplink, '_self')
    window.setTimeout(() => {
      showFallbackHelp.value = true
    }, 1500)
  } finally {
    isCreating.value = false
  }
}

const confirmRevoke = async (deviceId: number) => {
  revokingId.value = deviceId
  try {
    await revokeCodexManagedDevice(deviceId)
    pendingRevokeId.value = null
    await loadDevices()
  } finally {
    revokingId.value = null
  }
}

const statusClass = (status: CodexManagedDevice['status']) => {
  switch (status) {
    case 'active':
      return 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
    case 'revoked':
      return 'bg-gray-200 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
    case 'reauthorization_required':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    default:
      return 'bg-gray-200 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
  }
}

const statusLabel = (status: CodexManagedDevice['status']) => {
  switch (status) {
    case 'active':
      return t('keys.zhumengAgent.status.active')
    case 'revoked':
      return t('keys.zhumengAgent.status.revoked')
    case 'reauthorization_required':
      return t('keys.zhumengAgent.status.reauthorizationRequired')
    default:
      return status
  }
}

watch(() => props.apiKeyId, () => {
  pendingRevokeId.value = null
  devices.value = []
  void loadDevices()
}, { immediate: true })
</script>
