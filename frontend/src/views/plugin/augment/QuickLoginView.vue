<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6">
      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <div class="space-y-2">
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('plugin.augment.quickLogin.title') }}
          </h1>
          <p class="text-sm text-gray-600 dark:text-gray-300">
            {{ t('plugin.augment.quickLogin.subtitle') }}
          </p>
        </div>

        <div class="mt-6 space-y-6">
          <div class="flex flex-wrap gap-3">
            <button
              data-test="quick-login-continue"
              type="button"
              class="btn btn-primary"
              :disabled="isGranting || (selectedMode === 'official_passthrough' && !consentChecked)"
              @click="handleRequestGrant"
            >
              {{ isGranting ? t('plugin.augment.quickLogin.requesting') : t('plugin.augment.quickLogin.continue') }}
            </button>
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="!deeplinkUrl"
              @click="launchDeeplink"
            >
              {{ needsManualOpen ? t('plugin.augment.quickLogin.manualOpen') : t('plugin.augment.quickLogin.launch') }}
            </button>
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="!deeplinkUrl"
              @click="copyDeeplink"
            >
              {{ t('plugin.augment.quickLogin.copy') }}
            </button>
          </div>

          <QuickLoginModeSelector
            v-model="selectedMode"
            v-model:source="selectedSource"
            :show-local-compat="showLocalCompat"
          />

          <QuickLoginEditorSelector
            v-if="selectedMode === 'official_passthrough'"
            :model-value="selectedEditorTarget"
            @update:model-value="handleEditorTargetChange"
          />

          <section
            v-if="showLocalCompat"
            class="rounded-xl border border-amber-200 bg-amber-50 p-5 dark:border-amber-900/40 dark:bg-amber-950/30"
          >
            <h2 class="text-sm font-semibold text-amber-900 dark:text-amber-100">
              {{ t('plugin.augment.quickLogin.internalCapture.title') }}
            </h2>
            <p class="mt-2 text-sm text-amber-800 dark:text-amber-200">
              {{ t('plugin.augment.quickLogin.internalCapture.description') }}
            </p>
          </section>

          <section class="rounded-xl border border-gray-200 bg-gray-50 p-5 dark:border-dark-700 dark:bg-dark-950/50">
            <div class="space-y-2">
              <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t('plugin.augment.quickLogin.consent.title') }}
              </h2>
              <p class="text-sm text-gray-600 dark:text-gray-300">
                {{ consentBody }}
              </p>
            </div>

            <label
              v-if="selectedMode === 'official_passthrough'"
              class="mt-4 flex items-start gap-3 rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900"
            >
              <input
                v-model="consentChecked"
                type="checkbox"
                class="mt-0.5"
              />
              <span class="text-sm text-gray-700 dark:text-gray-200">
                {{ t('plugin.augment.quickLogin.consent.confirm') }}
              </span>
            </label>
          </section>

          <div
            v-if="grantPayloadDiagnostics.length"
            class="grid gap-3 rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/50 sm:grid-cols-2"
          >
            <div
              v-for="[key, value] in grantPayloadDiagnostics"
              :key="key"
              class="space-y-1"
            >
              <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ key }}
              </p>
              <p class="break-all font-mono text-sm text-gray-900 dark:text-white">
                {{ value }}
              </p>
            </div>
          </div>

          <div
            v-if="grantError"
            class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-300"
          >
            {{ grantError }}
          </div>

          <div
            v-if="deeplinkUrl"
            class="space-y-3 rounded-xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-800/50 dark:bg-emerald-900/20"
          >
            <p class="text-sm font-medium text-emerald-800 dark:text-emerald-200">
              {{ t('plugin.augment.quickLogin.ready') }}
            </p>
            <p
              v-if="targetWarning"
              class="text-sm text-amber-700 dark:text-amber-200"
            >
              {{ targetWarning }}
            </p>
            <input
              class="input w-full font-mono text-xs"
              :value="deeplinkUrl"
              readonly
            />
            <p
              v-if="needsManualOpen"
              class="text-sm text-emerald-800 dark:text-emerald-200"
            >
              {{ t('plugin.augment.quickLogin.manualOpen') }}
            </p>
            <p
              v-if="needsManualOpen"
              class="text-xs text-emerald-700/80 dark:text-emerald-200/80"
            >
              {{ t('plugin.augment.quickLogin.copyHint') }}
            </p>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import QuickLoginEditorSelector from '@/components/plugin/augment/QuickLoginEditorSelector.vue'
import QuickLoginModeSelector from '@/components/plugin/augment/QuickLoginModeSelector.vue'
import { useClipboard } from '@/composables/useClipboard'
import { requestAugmentQuickLoginGrant } from '@/api/augment'
import { bindAugmentPoolSession, createAugmentPoolSessionBindIntent } from '@/api/admin/augmentGateway'
import { useAppStore, useAuthStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  buildAugmentOfficialBindPayload,
  buildAugmentQuickLoginGrantPayload,
  extractAugmentQuickLoginTargetWarning,
  extractAugmentOfficialTenantAllowlist,
  persistAugmentQuickLoginEditorTarget,
  isAugmentLocalCompatGateEnabled,
  resolveAugmentQuickLoginDeeplink,
  resolveAugmentQuickLoginEditorTarget,
  shouldAutoLaunchAugmentQuickLogin,
  summarizeAugmentQuickLoginDiagnostics,
} from '@/utils/augmentQuickLogin'
import type { AugmentQuickLoginEditorTarget } from '@/utils/augmentIdeTargets'

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const { copyToClipboard } = useClipboard()
const { t } = useI18n()

const isGranting = ref(false)
const grantError = ref('')
const deeplinkUrl = ref('')
const targetWarning = ref('')
const targetVerified = ref(true)
const consentChecked = ref(false)
const selectedMode = ref<'official_passthrough' | 'local_compat'>('official_passthrough')
const selectedSource = ref<'official_quick_login' | 'wukong_quick_login'>(
  route.query.source === 'wukong_quick_login' ? 'wukong_quick_login' : 'official_quick_login',
)
const selectedEditorTarget = ref<AugmentQuickLoginEditorTarget>(
  resolveAugmentQuickLoginEditorTarget({
    mode: selectedMode.value,
    query: route.query,
  }),
)

const showLocalCompat = computed(() =>
  isAugmentLocalCompatGateEnabled({
    isAdmin: Boolean(authStore.isAdmin),
    query: route.query,
  })
)

const isPoolCaptureMode = computed(() =>
  showLocalCompat.value && String(route.query.capture_target ?? '') === 'pool_session'
)

const grantPayload = computed(() => buildAugmentQuickLoginGrantPayload(route.query))
const grantPayloadDiagnostics = computed(() =>
  summarizeAugmentQuickLoginDiagnostics({
    ...grantPayload.value,
    source: selectedSource.value,
    editor_target: selectedMode.value === 'official_passthrough' ? selectedEditorTarget.value : '',
  })
)
const needsManualOpen = computed(() => targetWarning.value.length > 0 || !targetVerified.value)

const consentBody = computed(() =>
  selectedSource.value === 'wukong_quick_login'
    ? t('plugin.augment.quickLogin.consent.wukong')
    : t('plugin.augment.quickLogin.consent.official')
)

watch(
  () => route.query.editor_target,
  () => {
    if (selectedMode.value !== 'official_passthrough') {
      return
    }
    selectedEditorTarget.value = resolveAugmentQuickLoginEditorTarget({
      mode: selectedMode.value,
      query: route.query,
    })
    resetGrantState()
  }
)

watch(
  () => selectedMode.value,
  (mode) => {
    selectedEditorTarget.value = resolveAugmentQuickLoginEditorTarget({
      mode,
      query: route.query,
    })
    resetGrantState()
  }
)

watch(
  () => selectedSource.value,
  () => {
    resetGrantState()
  }
)

async function handleRequestGrant(): Promise<void> {
  isGranting.value = true
  grantError.value = ''
  resetGrantState()

  try {
    if (isPoolCaptureMode.value && selectedMode.value === 'official_passthrough') {
      const bindPayload = buildAugmentOfficialBindPayload(route.query)
      if (bindPayload) {
        const bindIntent = await createAugmentPoolSessionBindIntent({
          mode: selectedMode.value,
          source: selectedSource.value,
          tenant_allowlist: extractAugmentOfficialTenantAllowlist(route.query),
        })

        await bindAugmentPoolSession({
          bind_token: String(bindIntent.bind_token ?? ''),
          bind_intent_id: String(bindIntent.bind_intent_id ?? ''),
          state: String(bindIntent.state ?? ''),
          mode: selectedMode.value,
          source: selectedSource.value,
          payload: bindPayload,
        })
      }
    }

    const response = await requestAugmentQuickLoginGrant(
      selectedMode.value === 'official_passthrough'
        ? {
            mode: selectedMode.value,
            source: selectedSource.value,
            editor_target: selectedEditorTarget.value,
          }
        : {
            mode: selectedMode.value,
            source: selectedSource.value,
          }
    )
    const deeplink = resolveAugmentQuickLoginDeeplink(response)

    if (!deeplink) {
      throw new Error(t('plugin.augment.quickLogin.missingDeeplink'))
    }

    deeplinkUrl.value = deeplink
    targetWarning.value = extractAugmentQuickLoginTargetWarning(response)
    targetVerified.value = typeof response.target_verified === 'boolean' ? response.target_verified : true
    await copyToClipboard(deeplink, t('plugin.augment.quickLogin.copySuccess'))
    if (shouldAutoLaunchAugmentQuickLogin(response)) {
      launchDeeplink()
    }
  } catch (error: unknown) {
    const message = extractApiErrorMessage(error, t('plugin.augment.quickLogin.requestFailed'))
    grantError.value = message
    appStore.showError(message)
  } finally {
    isGranting.value = false
  }
}

function handleEditorTargetChange(value: AugmentQuickLoginEditorTarget): void {
  if (selectedEditorTarget.value === value) {
    return
  }
  selectedEditorTarget.value = value
  persistAugmentQuickLoginEditorTarget(value)
  resetGrantState()
}

function resetGrantState(): void {
  deeplinkUrl.value = ''
  targetWarning.value = ''
  targetVerified.value = true
}

function launchDeeplink(): void {
  if (!deeplinkUrl.value) {
    return
  }
  if (typeof navigator !== 'undefined' && /jsdom/i.test(navigator.userAgent)) {
    return
  }
  window.location.href = deeplinkUrl.value
}

async function copyDeeplink(): Promise<void> {
  if (!deeplinkUrl.value) {
    return
  }
  await copyToClipboard(deeplinkUrl.value, t('plugin.augment.quickLogin.copySuccess'))
}
</script>
