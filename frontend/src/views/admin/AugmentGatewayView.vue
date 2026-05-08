<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
          {{ t('admin.augmentGateway.title') }}
        </h1>
        <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">
          {{ t('admin.augmentGateway.description') }}
        </p>

        <div class="mt-6 grid gap-4 md:grid-cols-3">
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ t('admin.augmentGateway.cacheHitRatio') }}</p>
            <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">{{ summary.cache_hit_ratio ?? 0 }}</p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ t('admin.augmentGateway.estimatedCost') }}</p>
            <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">{{ summary.estimated_cost ?? 0 }}</p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ t('admin.augmentGateway.settledCost') }}</p>
            <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">{{ summary.settled_cost ?? 0 }}</p>
          </div>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.augmentGateway.providerGroups') }}</h2>
        <div class="mt-4 overflow-x-auto">
          <table class="min-w-full text-sm">
            <tbody>
              <tr v-for="row in providerGroups" :key="row.provider" class="border-b border-gray-100 dark:border-dark-800">
                <td class="px-3 py-2 font-medium text-gray-900 dark:text-white">{{ row.provider }}</td>
                <td class="px-3 py-2">{{ row.group_id }}</td>
                <td class="px-3 py-2">{{ row.active_accounts }}/{{ row.total_accounts }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.augmentGateway.models') }}</h2>
        <div class="mt-4 space-y-3">
          <div
            v-for="row in models"
            :key="row.model.id"
            class="flex items-center justify-between rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-950/40"
          >
            <div class="space-y-1">
              <p class="font-medium text-gray-900 dark:text-white">{{ row.model.id }}</p>
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ row.smoke_status }} / {{ row.model.provider }}</p>
            </div>
            <button
              :data-test="`model-toggle-${row.model.id}`"
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="row.smoke_status !== 'passed'"
              @click="toggleModel(row)"
            >
              {{ row.visible ? t('admin.augmentGateway.visible') : t('admin.augmentGateway.hidden') }}
            </button>
          </div>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.augmentGateway.officialSessions') }}</h2>
        <div class="mt-4 space-y-3">
          <div
            v-for="row in sessions"
            :key="row.user_id"
            class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40"
          >
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p class="font-medium text-gray-900 dark:text-white">{{ tenantHost(row.tenant_origin) }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ row.source }} / {{ row.status }}</p>
              </div>
              <div class="flex flex-wrap gap-2">
                <button
                  :data-test="`revoke-session-${row.user_id}`"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="revokeSession(row.user_id)"
                >
                  {{ t('admin.augmentGateway.revoke') }}
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="loadDiagnostics(row.user_id)"
                >
                  {{ t('admin.augmentGateway.diagnostics') }}
                </button>
              </div>
            </div>
          </div>
        </div>

        <pre
          v-if="diagnosticsText"
          class="mt-4 overflow-x-auto rounded-lg border border-gray-200 bg-gray-50 p-4 text-xs text-gray-700 dark:border-dark-700 dark:bg-dark-950/40 dark:text-gray-200"
        >{{ diagnosticsText }}</pre>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.augmentGateway.usage') }}</h2>
        <div class="mt-4 overflow-x-auto">
          <table class="min-w-full text-sm">
            <tbody>
              <tr v-for="row in usageRows" :key="row.request_id" class="border-b border-gray-100 dark:border-dark-800">
                <td class="px-3 py-2">{{ row.model }}</td>
                <td class="px-3 py-2">{{ row.estimated_cost }}</td>
                <td class="px-3 py-2">{{ row.settled_cost }}</td>
                <td class="px-3 py-2">{{ row.cache_read_tokens }}</td>
                <td class="px-3 py-2">{{ row.cache_creation_tokens }}</td>
                <td class="px-3 py-2 font-mono text-xs">{{ row.request_id }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import {
  getAugmentGatewaySummary,
  getAugmentProviderGroups,
  getAugmentGatewayModels,
  listAugmentOfficialSessions,
  revokeAugmentOfficialSessionAdmin,
  getAugmentOfficialSessionDiagnosticsAdmin,
  getAugmentGatewayAdminUsage,
  updateAugmentGatewayModel,
  type AugmentGatewayAdminUsageRow,
  type AugmentGatewayModelRow,
  type AugmentGatewaySummary,
  type AugmentOfficialSessionAdminRow,
  type AugmentProviderGroupRow,
} from '@/api/admin/augmentGateway'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const summary = ref<AugmentGatewaySummary>({})
const providerGroups = ref<AugmentProviderGroupRow[]>([])
const models = ref<AugmentGatewayModelRow[]>([])
const sessions = ref<AugmentOfficialSessionAdminRow[]>([])
const usageRows = ref<AugmentGatewayAdminUsageRow[]>([])
const diagnosticsText = ref('')

onMounted(async () => {
  await refreshAll()
})

async function refreshAll(): Promise<void> {
  try {
    const [summaryData, providerData, modelData, sessionData, usageData] = await Promise.all([
      getAugmentGatewaySummary(),
      getAugmentProviderGroups(),
      getAugmentGatewayModels(),
      listAugmentOfficialSessions(),
      getAugmentGatewayAdminUsage({ page: 1, page_size: 20 }),
    ])
    summary.value = summaryData
    providerGroups.value = providerData.rows ?? []
    models.value = modelData.rows ?? []
    sessions.value = sessionData.rows ?? []
    usageRows.value = usageData.rows ?? []
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.loadFailed')))
  }
}

async function toggleModel(row: AugmentGatewayModelRow): Promise<void> {
  if (row.smoke_status !== 'passed') {
    return
  }
  try {
    await updateAugmentGatewayModel(row.model.id, {
      enabled: !row.enabled,
      smoke_status: row.smoke_status,
      expected_version: row.settings_version,
    })
    await refreshAll()
    appStore.showSuccess(t('admin.augmentGateway.saved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.saveFailed')))
  }
}

async function revokeSession(userId: number): Promise<void> {
  try {
    await revokeAugmentOfficialSessionAdmin(userId)
    await refreshSessions()
    appStore.showSuccess(t('admin.augmentGateway.saved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.saveFailed')))
  }
}

async function refreshSessions(): Promise<void> {
  const sessionData = await listAugmentOfficialSessions()
  sessions.value = sessionData.rows ?? []
}

async function loadDiagnostics(userId: number): Promise<void> {
  try {
    const data = await getAugmentOfficialSessionDiagnosticsAdmin(userId)
    diagnosticsText.value = JSON.stringify(data, null, 2)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.loadFailed')))
  }
}

function tenantHost(origin: string): string {
  try {
    return new URL(origin).host
  } catch {
    return origin
  }
}
</script>
