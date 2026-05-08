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
              {{ t('plugin.augment.quickLogin.launch') }}
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
            <input
              class="input w-full font-mono text-xs"
              :value="deeplinkUrl"
              readonly
            />
          </div>
        </div>
      </section>

      <OfficialSessionStatusCard
        :session="officialSession"
        :revoking="isRevoking"
        @revoke="handleRevokeSession"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import OfficialSessionStatusCard from '@/components/plugin/augment/OfficialSessionStatusCard.vue'
import QuickLoginModeSelector from '@/components/plugin/augment/QuickLoginModeSelector.vue'
import { useClipboard } from '@/composables/useClipboard'
import {
  bindAugmentOfficialSession,
  createAugmentOfficialSessionBindIntent,
  getAugmentOfficialSession,
  requestAugmentQuickLoginGrant,
  revokeAugmentOfficialSession,
  type AugmentOfficialSessionView,
} from '@/api/augment'
import { useAppStore, useAuthStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  buildAugmentQuickLoginGrantPayload,
  buildAugmentOfficialBindPayload,
  extractAugmentOfficialTenantAllowlist,
  isAugmentLocalCompatGateEnabled,
  resolveAugmentQuickLoginDeeplink,
  summarizeAugmentQuickLoginDiagnostics,
} from '@/utils/augmentQuickLogin'

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const { copyToClipboard } = useClipboard()
const { t } = useI18n()

const isGranting = ref(false)
const isRevoking = ref(false)
const grantError = ref('')
const deeplinkUrl = ref('')
const officialSession = ref<AugmentOfficialSessionView | null>(null)
const consentChecked = ref(false)
const selectedMode = ref<'official_passthrough' | 'local_compat'>('official_passthrough')
const selectedSource = ref<'official_quick_login' | 'wukong_quick_login'>('official_quick_login')

const showLocalCompat = computed(() =>
  isAugmentLocalCompatGateEnabled({
    isAdmin: Boolean(authStore.isAdmin),
    query: route.query,
  })
)

const grantPayload = computed(() => buildAugmentQuickLoginGrantPayload(route.query))
const grantPayloadDiagnostics = computed(() =>
  summarizeAugmentQuickLoginDiagnostics({
    ...grantPayload.value,
    tenant_origin: officialSession.value?.tenant_origin ?? '',
    source: selectedSource.value,
    status: officialSession.value?.status ?? '',
  })
)

const consentBody = computed(() =>
  selectedSource.value === 'wukong_quick_login'
    ? t('plugin.augment.quickLogin.consent.wukong')
    : t('plugin.augment.quickLogin.consent.official')
)

onMounted(async () => {
  await refreshOfficialSession()
  selectedMode.value = 'official_passthrough'
})

async function refreshOfficialSession(): Promise<void> {
  try {
    officialSession.value = await getAugmentOfficialSession()
    if (officialSession.value?.source === 'wukong_quick_login') {
      selectedSource.value = 'wukong_quick_login'
    }
  } catch {
    officialSession.value = null
  }
}

async function handleRequestGrant(): Promise<void> {
  isGranting.value = true
  grantError.value = ''

  try {
    if (selectedMode.value === 'official_passthrough') {
      const tenantAllowlist = officialSession.value?.tenant_origin
        ? [officialSession.value.tenant_origin]
        : extractAugmentOfficialTenantAllowlist(route.query)
      const bindIntent = await createAugmentOfficialSessionBindIntent({
        mode: selectedMode.value,
        source: selectedSource.value,
        tenant_allowlist: tenantAllowlist,
      })
      const bindPayload = buildAugmentOfficialBindPayload(route.query)
      if (bindPayload) {
        await bindAugmentOfficialSession({
          bind_token: bindIntent.bind_token,
          bind_intent_id: bindIntent.bind_intent_id,
          state: bindIntent.state,
          mode: selectedMode.value,
          source: selectedSource.value,
          payload: bindPayload,
        })
        await refreshOfficialSession()
      }
    }

    const response = await requestAugmentQuickLoginGrant({
      mode: selectedMode.value,
    })
    const deeplink = resolveAugmentQuickLoginDeeplink(response)

    if (!deeplink) {
      throw new Error(t('plugin.augment.quickLogin.missingDeeplink'))
    }

    deeplinkUrl.value = deeplink
    await copyToClipboard(deeplink, t('plugin.augment.quickLogin.copySuccess'))
    launchDeeplink()
  } catch (error: unknown) {
    const message = extractApiErrorMessage(error, t('plugin.augment.quickLogin.requestFailed'))
    grantError.value = message
    appStore.showError(message)
  } finally {
    isGranting.value = false
  }
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

async function handleRevokeSession(): Promise<void> {
  isRevoking.value = true
  try {
    officialSession.value = await revokeAugmentOfficialSession()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('plugin.augment.quickLogin.requestFailed')))
  } finally {
    isRevoking.value = false
  }
}
</script>
