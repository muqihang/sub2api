<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6">
      <section class="card overflow-hidden">
        <div class="border-b border-gray-100 bg-gradient-to-r from-sky-50 via-white to-emerald-50 p-6 dark:border-dark-700 dark:from-sky-950/30 dark:via-dark-800 dark:to-emerald-950/20">
          <p class="text-xs font-semibold uppercase tracking-[0.3em] text-sky-700 dark:text-sky-300">
            Augment
          </p>
          <h1 class="mt-3 text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('plugin.augment.quickLogin.title') }}
          </h1>
          <p class="mt-2 max-w-2xl text-sm text-gray-600 dark:text-gray-300">
            {{ t('plugin.augment.quickLogin.subtitle') }}
          </p>
        </div>

        <div class="space-y-6 p-6">
          <div
            v-if="grantPayloadDiagnostics.length"
            class="grid gap-4 rounded-2xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/60 sm:grid-cols-2"
          >
            <div v-for="[key, value] in grantPayloadDiagnostics" :key="key" class="space-y-1">
              <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">
                {{ key }}
              </p>
              <p class="truncate font-mono text-sm text-gray-900 dark:text-white">
                {{ value }}
              </p>
            </div>
          </div>

          <div class="flex flex-wrap gap-3">
            <button
              type="button"
              class="btn btn-primary"
              :disabled="isGranting"
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

          <div
            v-if="grantError"
            class="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-300"
          >
            {{ grantError }}
          </div>

          <div v-if="deeplinkUrl" class="space-y-3 rounded-2xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-800/50 dark:bg-emerald-900/20">
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
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useClipboard } from '@/composables/useClipboard'
import { requestAugmentQuickLoginGrant } from '@/api/augment'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  buildAugmentQuickLoginGrantPayload,
  resolveAugmentQuickLoginDeeplink,
} from '@/utils/augmentQuickLogin'

const route = useRoute()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()
const { t } = useI18n()

const isGranting = ref(false)
const grantError = ref('')
const deeplinkUrl = ref('')

const grantPayload = computed(() => buildAugmentQuickLoginGrantPayload(route.query))
const grantPayloadDiagnostics = computed(() => summarizeGrantPayload(grantPayload.value))

const safeDiagnosticKeys = new Set([
  'mode',
  'tenant_url',
  'official_tenant_url',
  'scopes',
  'official_scopes',
  'expires_at',
  'official_expires_at',
])

const redactedDiagnosticKeys = new Set([
  'access_token',
  'refresh_token',
  'session_bundle',
  'official_access_token',
  'official_refresh_token',
  'official_session_bundle',
  'code',
  'state',
])

async function handleRequestGrant(): Promise<void> {
  isGranting.value = true
  grantError.value = ''

  try {
    const response = await requestAugmentQuickLoginGrant(grantPayload.value)
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

  window.location.href = deeplinkUrl.value
}

async function copyDeeplink(): Promise<void> {
  if (!deeplinkUrl.value) {
    return
  }

  await copyToClipboard(deeplinkUrl.value, t('plugin.augment.quickLogin.copySuccess'))
}

function summarizeGrantPayload(payload: Record<string, string>): Array<[string, string]> {
  const diagnostics: Array<[string, string]> = []

  Object.entries(payload).forEach(([key, value]) => {
    if (safeDiagnosticKeys.has(key)) {
      diagnostics.push([key, value])
      return
    }
    if (redactedDiagnosticKeys.has(key)) {
      diagnostics.push([key, '[redacted]'])
    }
  })

  return diagnostics.slice(0, 6)
}
</script>
