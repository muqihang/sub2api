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
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.augmentGateway.sourcePriority') }}</h2>
        <div class="mt-4 space-y-3">
          <div
            v-for="source in sourcePriority"
            :key="source"
            class="flex items-center justify-between rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-950/40"
          >
            <span class="font-medium text-gray-900 dark:text-white">{{ source }}</span>
            <div class="flex gap-2">
              <button
                :data-test="`source-priority-up-${source}`"
                type="button"
                class="btn btn-secondary btn-sm"
                @click="moveSource(source, -1)"
              >
                ↑
              </button>
              <button
                :data-test="`source-priority-down-${source}`"
                type="button"
                class="btn btn-secondary btn-sm"
                @click="moveSource(source, 1)"
              >
                ↓
              </button>
            </div>
          </div>
          <div class="flex justify-end">
            <button
              data-test="save-source-priority"
              type="button"
              class="btn btn-primary btn-sm"
              @click="saveSourcePriority"
            >
              {{ t('admin.augmentGateway.saved') }}
            </button>
          </div>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.augmentGateway.capture') }}</h2>
        <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">
          {{ t('admin.augmentGateway.captureDescription') }}
        </p>
        <div class="mt-4 flex gap-3">
          <button
            data-test="capture-pool-session"
            type="button"
            class="btn btn-primary btn-sm"
            @click="capturePoolSession"
          >
            {{ t('admin.augmentGateway.captureNow') }}
          </button>
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
            :key="row.id"
            class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40"
          >
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p class="font-medium text-gray-900 dark:text-white">{{ tenantHost(row.tenant_origin) }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ row.source }} / {{ row.status }}</p>
              </div>
              <div class="flex flex-wrap gap-2">
                <button
                  :data-test="`revoke-session-${row.id}`"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="revokeSession(row.id)"
                >
                  {{ t('admin.augmentGateway.revoke') }}
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="loadDiagnostics(row.id)"
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
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import {
  bindAugmentPoolSession,
  createAugmentPoolSessionBindIntent,
  getAugmentGatewayAdminUsage,
  getAugmentGatewayModels,
  getAugmentGatewaySourcePriority,
  getAugmentGatewaySummary,
  getAugmentPoolSessionDiagnosticsAdmin,
  getAugmentProviderGroups,
  listAugmentPoolSessions,
  revokeAugmentPoolSessionAdmin,
  updateAugmentGatewayModel,
  updateAugmentGatewaySourcePriority,
  type AugmentGatewayAdminUsageRow,
  type AugmentGatewayModelRow,
  type AugmentGatewaySummary,
  type AugmentPoolSessionAdminRow,
  type AugmentProviderGroupRow,
} from '@/api/admin/augmentGateway'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'
import { buildAugmentOfficialBindPayload, extractAugmentOfficialTenantAllowlist } from '@/utils/augmentQuickLogin'

const { t } = useI18n()
const appStore = useAppStore()
const route = useRoute()

const summary = ref<AugmentGatewaySummary>({})
const providerGroups = ref<AugmentProviderGroupRow[]>([])
const models = ref<AugmentGatewayModelRow[]>([])
const sessions = ref<AugmentPoolSessionAdminRow[]>([])
const usageRows = ref<AugmentGatewayAdminUsageRow[]>([])
const diagnosticsText = ref('')
const sourcePriority = ref<string[]>([])

onMounted(async () => {
  await refreshAll()
})

async function refreshAll(): Promise<void> {
  try {
    const [summaryData, providerData, priorityData, modelData, sessionData, usageData] = await Promise.all([
      getAugmentGatewaySummary(),
      getAugmentProviderGroups(),
      getAugmentGatewaySourcePriority(),
      getAugmentGatewayModels(),
      listAugmentPoolSessions(),
      getAugmentGatewayAdminUsage({ page: 1, page_size: 20 }),
    ])
    summary.value = summaryData
    providerGroups.value = providerData.rows ?? []
    sourcePriority.value = priorityData.sources ?? []
    models.value = modelData.rows ?? []
    sessions.value = sessionData.rows ?? []
    usageRows.value = usageData.rows ?? []
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.loadFailed')))
  }
}

function moveSource(source: string, direction: number): void {
  const index = sourcePriority.value.indexOf(source)
  const next = index + direction
  if (index < 0 || next < 0 || next >= sourcePriority.value.length) {
    return
  }
  const clone = [...sourcePriority.value]
  ;[clone[index], clone[next]] = [clone[next], clone[index]]
  sourcePriority.value = clone
}

async function saveSourcePriority(): Promise<void> {
  try {
    await updateAugmentGatewaySourcePriority({
      sources: sourcePriority.value,
    })
    appStore.showSuccess(t('admin.augmentGateway.saved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.saveFailed')))
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

async function revokeSession(sessionId: number): Promise<void> {
  try {
    await revokeAugmentPoolSessionAdmin(sessionId)
    await refreshSessions()
    appStore.showSuccess(t('admin.augmentGateway.saved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.saveFailed')))
  }
}

async function refreshSessions(): Promise<void> {
  const sessionData = await listAugmentPoolSessions()
  sessions.value = sessionData.rows ?? []
}

async function loadDiagnostics(sessionId: number): Promise<void> {
  try {
    const data = await getAugmentPoolSessionDiagnosticsAdmin(sessionId)
    diagnosticsText.value = JSON.stringify(sanitizeDiagnostics(data), null, 2)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.loadFailed')))
  }
}

async function capturePoolSession(): Promise<void> {
  try {
    const payload = buildAugmentOfficialBindPayload(route.query)
    const tenantAllowlist = extractAugmentOfficialTenantAllowlist(route.query)
    const source =
      typeof route.query.source === 'string' && route.query.source.length > 0
        ? route.query.source
        : (sourcePriority.value[0] || 'official_quick_login')
    if (!payload || tenantAllowlist.length === 0) {
      if (typeof window !== 'undefined') {
        const target = new URL('/plugin/augment/quick-login', window.location.origin)
        target.searchParams.set('emergency_local_compat', '1')
        target.searchParams.set('capture_target', 'pool_session')
        target.searchParams.set('source', source)
        window.location.href = target.toString()
      } else {
        appStore.showError(t('admin.augmentGateway.captureMissing'))
      }
      return
    }
    const bindIntent = await createAugmentPoolSessionBindIntent({
      mode: 'official_passthrough',
      source,
      tenant_allowlist: tenantAllowlist,
    })
    await bindAugmentPoolSession({
      bind_token: String(bindIntent.bind_token ?? ''),
      bind_intent_id: String(bindIntent.bind_intent_id ?? ''),
      state: String(bindIntent.state ?? ''),
      mode: 'official_passthrough',
      source,
      payload,
    })
    await refreshSessions()
    appStore.showSuccess(t('admin.augmentGateway.saved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.saveFailed')))
  }
}

function tenantHost(origin: string): string {
  try {
    return new URL(origin).host
  } catch {
    return origin
  }
}

function sanitizeDiagnostics(input: Record<string, unknown>): Record<string, unknown> {
  const forbidden = ['token', 'cookie', 'payload', 'authorization', 'session_bundle']
  return Object.fromEntries(
    Object.entries(input).filter(([key]) => {
      const lower = key.toLowerCase()
      return !forbidden.some((word) => lower.includes(word))
    }),
  )
}
</script>
