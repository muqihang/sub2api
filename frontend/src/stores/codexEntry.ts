import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { keysAPI } from '@/api/keys'
import { userChannelsAPI } from '@/api/channels'
import type { UserSupportedModel } from '@/api/channels'
import { useAuthStore } from '@/stores/auth'
import type {
  ApiKey,
  CodexEntrySummary,
  CodexEntryModelSummary,
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

export interface LoadCodexSummaryOptions {
  silent?: boolean
}

export type CodexModelPreview = CodexEntryModelSummary | CodexAvailableModelSummary

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
  const pendingSetupSessionLaunch = ref<Pick<CodexSetupSessionDTO, 'id' | 'launch_url' | 'cli_command' | 'expires_at'> | null>(null)
  const forceCredentialStep = ref(false)

  const supportingDataLoading = ref(false)
  const supportingDataLoaded = ref(false)
  const availableReuseKeys = ref<ApiKey[]>([])
  const availableModels = ref<CodexAvailableModelSummary[]>([])

  // Local UI state (not from server).
  const selectedAttachmentMode = ref<CodexAttachmentMode>('independent_credential')
  const selectedReuseKeyId = ref<number | null>(null)
  const credentialLabel = ref('我的 MacBook')

  // ─── Derived state (three-layer model) ───
  const pageState = computed<CodexPageState>(() => {
    if (forceCredentialStep.value && summary.value?.page_state !== 'console') {
      return 'onboarding_credential'
    }
    return summary.value?.page_state ?? 'onboarding_credential'
  })
  const wizardStep = computed<CodexWizardStep>(() => {
    if (forceCredentialStep.value && summary.value?.page_state !== 'console') {
      return 1
    }
    return summary.value?.wizard_step ?? null
  })
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

  const modelPreview = computed<CodexModelPreview[]>(() => {
    const catalog = summary.value?.model_catalog ?? []
    if (catalog.length > 0) {
      return catalog
    }
    return availableModels.value
  })

  async function loadSummary(options: LoadCodexSummaryOptions = {}) {
    const silent = options.silent === true
    if (!silent) {
      loading.value = true
      error.value = null
    }
    try {
      summary.value = mergePendingSetupSessionLaunch(await getCodexSummary())
      if (summary.value.page_state === 'console') {
        forceCredentialStep.value = false
      }
      if (summary.value?.attachment_mode) {
        selectedAttachmentMode.value = summary.value.attachment_mode
      }
      if (summary.value?.setup_session?.credential_label && credentialLabel.value === '我的 MacBook') {
        credentialLabel.value = summary.value.setup_session.credential_label
      }
    } catch (e: any) {
      if (!silent) {
        error.value = e?.message ?? 'Failed to load summary'
      }
    } finally {
      if (!silent) {
        loading.value = false
      }
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
      const created = await createCodexSetupSession({
        attachment_mode: selectedAttachmentMode.value,
        credential_label: credentialLabel.value || 'Codex',
        reuse_api_key_id: selectedAttachmentMode.value === 'reused_key' ? selectedReuseKeyId.value! : undefined,
      })
      rememberSetupSessionLaunch(created.setup_session)
      forceCredentialStep.value = false
      await loadSummary()
    } catch (e: any) {
      error.value = e?.message ?? 'Failed to create setup session'
    } finally {
      loading.value = false
    }
  }

  async function openLocal() {
    if (setupSession.value?.launch_url) {
      window.open(setupSession.value.launch_url, '_blank')
      return
    }
    const pendingWindow = window.open('', '_blank')
    const regeneratedLaunchURL = await regenerateSetupSession()
    if (regeneratedLaunchURL) {
      if (pendingWindow) {
        pendingWindow.location.href = regeneratedLaunchURL
      } else {
        window.location.href = regeneratedLaunchURL
      }
    }
  }

  async function copyCli() {
    if (setupSession.value?.cli_command) {
      await navigator.clipboard.writeText(setupSession.value.cli_command)
    }
  }

  async function regenerateSetupSession(): Promise<string | null> {
    if (!setupSession.value) return null
    loading.value = true
    error.value = null
    try {
      const regenerated = await regenerateCodexSetupSession(setupSession.value.id)
      rememberSetupSessionLaunch(regenerated.setup_session)
      await loadSummary()
      return regenerated.setup_session.launch_url ?? null
    } catch (e: any) {
      error.value = e?.message ?? 'Failed to regenerate session'
      return null
    } finally {
      loading.value = false
    }
  }

  async function diagnoseSetupSession() {
    if (!setupSession.value) return
    diagnosing.value = true
    error.value = null
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
    error.value = null
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

  function rememberSetupSessionLaunch(session: Pick<CodexSetupSessionDTO, 'id' | 'launch_url' | 'cli_command' | 'expires_at'>) {
    if (!session.launch_url && !session.cli_command) return
    pendingSetupSessionLaunch.value = {
      id: session.id,
      launch_url: session.launch_url,
      cli_command: session.cli_command,
      expires_at: session.expires_at,
    }
  }

  function returnToCredentialStep() {
    forceCredentialStep.value = true
    lastDiagnose.value = null
    error.value = null
  }

  function clearDiagnose() {
    lastDiagnose.value = null
  }

  function mergePendingSetupSessionLaunch(next: CodexEntrySummary): CodexEntrySummary {
    const pending = pendingSetupSessionLaunch.value
    const session = next.setup_session
    if (!pending || !session) return next

    const expiresAt = Date.parse(pending.expires_at)
    if (Number.isFinite(expiresAt) && expiresAt <= Date.now()) {
      pendingSetupSessionLaunch.value = null
      return next
    }

    const sameSession = session.id === pending.id || session.expires_at === pending.expires_at
    if (!sameSession) return next

    return {
      ...next,
      setup_session: {
        ...session,
        launch_url: session.launch_url ?? pending.launch_url,
        cli_command: session.cli_command ?? pending.cli_command,
      },
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
    returnToCredentialStep,
    clearDiagnose,
    resyncDevice,
    repairDevice,
    reAttachDevice,
    revokeAttachment,
    removeDevice,
  }
})
