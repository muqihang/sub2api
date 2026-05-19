import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { keysAPI } from '@/api/keys'
import { userChannelsAPI } from '@/api/channels'
import type { UserSupportedModel } from '@/api/channels'
import { useAuthStore } from '@/stores/auth'
import type {
  ApiKey,
  CodexEntrySummary,
  CodexPageState,
  CodexWizardStep,
  CodexAttachmentMode,
  CodexSetupSessionPresentation,
  CodexSetupSessionDTO,
  CodexDeviceDTO,
  CodexDiagnoseReport,
} from '@/types'
import {
  getCodexSummary,
  createCodexSetupSession,
  regenerateCodexSetupSession,
  diagnoseCodex,
  resyncCodexDevice,
  repairCodexDevice,
  reattachCodexDevice,
  revokeCodexAttachment,
  removeCodexDevice,
} from '@/api/zhumengAgent'

export interface CodexAvailableModelSummary {
  name: string
  platform: string
  pricing: UserSupportedModel['pricing']
}

function uniqueModels(models: CodexAvailableModelSummary[]): CodexAvailableModelSummary[] {
  const seen = new Set<string>()
  return models.filter((model) => {
    const key = `${model.platform}::${model.name}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

export const useCodexEntryStore = defineStore('codexEntry', () => {
  const authStore = useAuthStore()

  // ─── State ───
  const summary = ref<CodexEntrySummary | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const lastDiagnose = ref<CodexDiagnoseReport | null>(null)
  const diagnosing = ref(false)

  const supportingDataLoading = ref(false)
  const supportingDataLoaded = ref(false)
  const availableReuseKeys = ref<ApiKey[]>([])
  const availableModels = ref<CodexAvailableModelSummary[]>([])

  // Local UI state (not from server).
  const selectedAttachmentMode = ref<CodexAttachmentMode>('independent_credential')
  const selectedReuseKeyId = ref<number | null>(null)
  const credentialLabel = ref('我的 MacBook')

  // ─── Derived state (three-layer model) ───
  const pageState = computed<CodexPageState>(() => summary.value?.page_state ?? 'onboarding_credential')
  const wizardStep = computed<CodexWizardStep>(() => summary.value?.wizard_step ?? null)
  const setupSession = computed<CodexSetupSessionDTO | null>(() => summary.value?.setup_session ?? null)
  const setupSessionPresentation = computed<CodexSetupSessionPresentation | null>(() => summary.value?.setup_session_presentation ?? null)
  const devices = computed<CodexDeviceDTO[]>(() => summary.value?.devices ?? [])
  const focusDeviceId = computed<number | null>(() => summary.value?.focus_device_id ?? null)
  const attachmentMode = computed<CodexAttachmentMode | null>(() => summary.value?.attachment_mode ?? null)
  const currentBalance = computed<number>(() => authStore.user?.balance ?? 0)
  const hasPendingSession = computed<boolean>(() => !!setupSession.value)

  const focusDevice = computed<CodexDeviceDTO | null>(() => {
    if (!focusDeviceId.value) return null
    return devices.value.find(d => d.device_id === focusDeviceId.value) ?? null
  })

  const connectedDeviceCount = computed(() => devices.value.length)

  const modelPreview = computed(() => availableModels.value.slice(0, 6))

  async function loadSummary() {
    loading.value = true
    error.value = null
    try {
      summary.value = await getCodexSummary()
      if (summary.value?.attachment_mode) {
        selectedAttachmentMode.value = summary.value.attachment_mode
      }
      if (summary.value?.setup_session?.credential_label && credentialLabel.value === '我的 MacBook') {
        credentialLabel.value = summary.value.setup_session.credential_label
      }
    } catch (e: any) {
      error.value = e?.message ?? 'Failed to load summary'
    } finally {
      loading.value = false
    }
  }

  async function loadSupportingData(force = false) {
    if (supportingDataLoading.value) return
    if (supportingDataLoaded.value && !force) return

    supportingDataLoading.value = true
    try {
      const [keysResponse, channels] = await Promise.all([
        keysAPI.list(1, 100, { status: 'active' }),
        userChannelsAPI.getAvailable(),
      ])

      availableReuseKeys.value = keysResponse.items.filter((key) => !key.augment_only)

      const flattenedModels = uniqueModels(
        channels.flatMap((channel) =>
          channel.platforms.flatMap((section) =>
            section.supported_models.map((model) => ({
              name: model.name,
              platform: model.platform,
              pricing: model.pricing,
            })),
          ),
        ),
      )

      availableModels.value = flattenedModels
      supportingDataLoaded.value = true
    } catch (e) {
      console.error('Failed to load Codex supporting data:', e)
    } finally {
      supportingDataLoading.value = false
    }
  }

  function setAttachmentMode(mode: CodexAttachmentMode) {
    selectedAttachmentMode.value = mode
    error.value = null
  }

  function selectReuseKey(apiKeyId: number) {
    selectedReuseKeyId.value = Number.isFinite(apiKeyId) ? apiKeyId : null
    error.value = null
  }

  async function startSetup() {
    if (selectedAttachmentMode.value === 'reused_key' && !selectedReuseKeyId.value) {
      error.value = 'Please select an API key to reuse'
      return
    }

    loading.value = true
    error.value = null
    try {
      await createCodexSetupSession({
        attachment_mode: selectedAttachmentMode.value,
        credential_label: credentialLabel.value || 'Codex',
        reuse_api_key_id: selectedAttachmentMode.value === 'reused_key' ? selectedReuseKeyId.value! : undefined,
      })
      await loadSummary()
    } catch (e: any) {
      error.value = e?.message ?? 'Failed to create setup session'
    } finally {
      loading.value = false
    }
  }

  function openLocal() {
    if (setupSession.value?.launch_url) {
      window.open(setupSession.value.launch_url, '_blank')
    }
  }

  async function copyCli() {
    if (setupSession.value?.cli_command) {
      await navigator.clipboard.writeText(setupSession.value.cli_command)
    }
  }

  async function regenerateSetupSession() {
    if (!setupSession.value) return
    loading.value = true
    error.value = null
    try {
      await regenerateCodexSetupSession(setupSession.value.id)
      await loadSummary()
    } catch (e: any) {
      error.value = e?.message ?? 'Failed to regenerate session'
    } finally {
      loading.value = false
    }
  }

  async function diagnoseSetupSession() {
    if (!setupSession.value) return
    diagnosing.value = true
    try {
      lastDiagnose.value = await diagnoseCodex({ setup_session_id: setupSession.value.id })
    } catch (e: any) {
      error.value = e?.message ?? 'Diagnose failed'
    } finally {
      diagnosing.value = false
    }
  }

  async function diagnoseDevice(deviceId: number) {
    diagnosing.value = true
    try {
      lastDiagnose.value = await diagnoseCodex({ device_id: deviceId })
    } catch (e: any) {
      error.value = e?.message ?? 'Diagnose failed'
    } finally {
      diagnosing.value = false
    }
  }

  async function resyncDevice(deviceId: number) {
    loading.value = true
    try {
      await resyncCodexDevice(deviceId)
      await loadSummary()
    } catch (e: any) {
      error.value = e?.message ?? 'Resync failed'
    } finally {
      loading.value = false
    }
  }

  async function repairDevice(deviceId: number) {
    loading.value = true
    try {
      await repairCodexDevice(deviceId)
      await loadSummary()
    } catch (e: any) {
      error.value = e?.message ?? 'Repair failed'
    } finally {
      loading.value = false
    }
  }

  async function reAttachDevice(deviceId: number) {
    loading.value = true
    try {
      await reattachCodexDevice(deviceId)
      await loadSummary()
    } catch (e: any) {
      error.value = e?.message ?? 'Reattach failed'
    } finally {
      loading.value = false
    }
  }

  async function revokeAttachment(deviceId: number) {
    loading.value = true
    try {
      await revokeCodexAttachment(deviceId)
      await loadSummary()
    } catch (e: any) {
      error.value = e?.message ?? 'Revoke failed'
    } finally {
      loading.value = false
    }
  }

  async function removeDevice(deviceId: number) {
    loading.value = true
    try {
      await removeCodexDevice(deviceId)
      await loadSummary()
    } catch (e: any) {
      error.value = e?.message ?? 'Remove failed'
    } finally {
      loading.value = false
    }
  }

  return {
    summary,
    loading,
    error,
    lastDiagnose,
    diagnosing,
    supportingDataLoading,
    supportingDataLoaded,
    availableReuseKeys,
    availableModels,
    selectedAttachmentMode,
    selectedReuseKeyId,
    credentialLabel,

    pageState,
    wizardStep,
    setupSession,
    setupSessionPresentation,
    devices,
    focusDeviceId,
    focusDevice,
    attachmentMode,
    currentBalance,
    hasPendingSession,
    connectedDeviceCount,
    modelPreview,

    loadSummary,
    loadSupportingData,
    setAttachmentMode,
    selectReuseKey,
    startSetup,
    openLocal,
    copyCli,
    regenerateSetupSession,
    diagnoseSetupSession,
    diagnoseDevice,
    resyncDevice,
    repairDevice,
    reAttachDevice,
    revokeAttachment,
    removeDevice,
  }
})
