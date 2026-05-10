<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div class="max-w-3xl space-y-2">
            <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
              {{ t('admin.augmentGateway.title') }}
            </h1>
            <p class="text-sm text-gray-600 dark:text-gray-300">
              {{ t('admin.augmentGateway.description') }}
            </p>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.augmentGateway.operationalGuidance') }}
            </p>
          </div>

          <div class="flex flex-wrap gap-2">
            <RouterLink
              data-test="manage-entitlement-groups-link"
              to="/admin/groups"
              class="btn btn-secondary btn-sm"
            >
              {{ t('admin.augmentGateway.manageGroups') }}
            </RouterLink>
            <RouterLink
              data-test="manage-provider-accounts-link"
              to="/admin/accounts"
              class="btn btn-secondary btn-sm"
            >
              {{ t('admin.augmentGateway.manageAccounts') }}
            </RouterLink>
            <RouterLink
              data-test="open-quick-login-link"
              to="/plugin/augment/quick-login"
              class="btn btn-primary btn-sm"
            >
              {{ t('admin.augmentGateway.openQuickLogin') }}
            </RouterLink>
          </div>
        </div>

        <div class="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('admin.augmentGateway.entitlementGroups') }}
            </p>
            <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
              {{ formatInteger(summary.entitlement_groups?.total_count) }}
            </p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('admin.augmentGateway.providerRoutingGroups') }}
            </p>
            <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
              {{ formatInteger(summary.provider_routing_groups?.total_count) }}
            </p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('admin.augmentGateway.activePoolSessions') }}
            </p>
            <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
              {{ formatInteger(summary.official_session_pool?.active_count) }}
            </p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('admin.augmentGateway.estimatedCost') }}
            </p>
            <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
              {{ formatCurrency(summary.usage?.estimated_cost) }}
            </p>
          </div>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-col gap-2 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.augmentGateway.entitlementGroups') }}
            </h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">
              {{ t('admin.augmentGateway.entitlementGroupsDescription') }}
            </p>
          </div>
          <RouterLink to="/admin/groups" class="btn btn-secondary btn-sm">
            {{ t('admin.augmentGateway.manageGroups') }}
          </RouterLink>
        </div>

        <div class="mt-4 overflow-x-auto">
          <table class="min-w-full text-sm">
            <thead>
              <tr class="border-b border-gray-100 text-left text-xs uppercase tracking-wide text-gray-500 dark:border-dark-800 dark:text-gray-400">
                <th class="px-3 py-2">{{ t('admin.augmentGateway.group') }}</th>
                <th class="px-3 py-2">ID</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.status') }}</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.accountCoverage') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="row in entitlementGroups"
                :key="row.id"
                class="border-b border-gray-100 dark:border-dark-800"
              >
                <td class="px-3 py-2 font-medium text-gray-900 dark:text-white">{{ row.name }}</td>
                <td class="px-3 py-2 font-mono text-xs text-gray-600 dark:text-gray-300">{{ row.id }}</td>
                <td class="px-3 py-2">{{ row.status }}</td>
                <td class="px-3 py-2">{{ row.active_accounts }}/{{ row.total_accounts }}</td>
              </tr>
              <tr v-if="entitlementGroups.length === 0">
                <td colspan="4" class="px-3 py-6 text-center text-sm text-gray-500 dark:text-gray-400">
                  {{ t('admin.augmentGateway.emptyEntitlementGroups') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-col gap-2 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.augmentGateway.providerRoutingGroups') }}
            </h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">
              {{ t('admin.augmentGateway.providerRoutingDescription') }}
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <span class="inline-flex items-center rounded-md border border-gray-200 px-2.5 py-1 text-xs font-medium text-gray-600 dark:border-dark-700 dark:text-gray-300">
              {{ t('admin.augmentGateway.configuredRoutePolicyVersion') }}: {{ configuredRoutePolicyVersion }}
            </span>
            <RouterLink to="/admin/accounts" class="btn btn-secondary btn-sm">
              {{ t('admin.augmentGateway.manageAccounts') }}
            </RouterLink>
          </div>
        </div>

        <div class="mt-4 overflow-x-auto">
          <table class="min-w-full text-sm">
            <thead>
              <tr class="border-b border-gray-100 text-left text-xs uppercase tracking-wide text-gray-500 dark:border-dark-800 dark:text-gray-400">
                <th class="px-3 py-2">{{ t('admin.augmentGateway.provider') }}</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.groupId') }}</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.accountCoverage') }}</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.health') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="row in providerGroups"
                :key="row.provider"
                class="border-b border-gray-100 dark:border-dark-800"
              >
                <td class="px-3 py-2 font-medium text-gray-900 dark:text-white">{{ row.provider }}</td>
                <td class="px-3 py-2 font-mono text-xs text-gray-600 dark:text-gray-300">{{ row.group_id || '—' }}</td>
                <td class="px-3 py-2">{{ row.active_accounts }}/{{ row.total_accounts }}</td>
                <td class="px-3 py-2">
                  <span
                    class="inline-flex items-center rounded-md px-2 py-1 text-xs font-medium"
                    :class="row.healthy ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300' : 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-300'"
                  >
                    {{ row.healthy ? t('admin.augmentGateway.healthy') : t('admin.augmentGateway.needsAttention') }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="mt-6">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.augmentGateway.sourcePriority') }}
          </h3>
          <div class="mt-3 space-y-3">
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
                {{ t('admin.augmentGateway.savePriority') }}
              </button>
            </div>
          </div>
        </div>

        <div class="mt-6">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.augmentGateway.models') }}
          </h3>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">
            {{ t('admin.augmentGateway.modelsDescription') }}
          </p>
          <div class="mt-4 space-y-3">
            <div
              v-for="row in models"
              :key="row.model.id"
              class="flex items-center justify-between rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-950/40"
            >
              <div class="space-y-1">
                <p class="font-medium text-gray-900 dark:text-white">{{ row.model.id }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400">
                  {{ row.model.provider }} / {{ row.smoke_status }}
                </p>
                <div class="flex flex-wrap gap-2">
                  <span
                    :data-test="`model-enabled-state-${row.model.id}`"
                    class="inline-flex items-center rounded-md border border-gray-200 px-2 py-1 text-xs font-medium text-gray-600 dark:border-dark-700 dark:text-gray-300"
                  >
                    {{ row.enabled ? t('admin.augmentGateway.enabledState') : t('admin.augmentGateway.disabledState') }}
                  </span>
                  <span
                    :data-test="`model-visible-state-${row.model.id}`"
                    class="inline-flex items-center rounded-md border border-gray-200 px-2 py-1 text-xs font-medium text-gray-600 dark:border-dark-700 dark:text-gray-300"
                  >
                    {{ row.visible ? t('admin.augmentGateway.visibleState') : t('admin.augmentGateway.notVisibleState') }}
                  </span>
                </div>
              </div>
              <button
                :data-test="`model-toggle-${row.model.id}`"
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="row.smoke_status !== 'passed'"
                @click="toggleModel(row)"
              >
                {{ row.enabled ? t('admin.augmentGateway.disableModel') : t('admin.augmentGateway.enableModel') }}
              </button>
            </div>
          </div>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-col gap-2 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.augmentGateway.officialSessions') }}
            </h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">
              {{ t('admin.augmentGateway.sessionPoolDescription') }}
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <RouterLink to="/plugin/augment/quick-login" class="btn btn-secondary btn-sm">
              {{ t('admin.augmentGateway.openQuickLogin') }}
            </RouterLink>
            <button
              data-test="capture-pool-session"
              type="button"
              class="btn btn-primary btn-sm"
              @click="capturePoolSession"
            >
              {{ t('admin.augmentGateway.captureNow') }}
            </button>
          </div>
        </div>

        <div class="mt-4 grid gap-4 md:grid-cols-3">
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('admin.augmentGateway.totalPoolSessions') }}
            </p>
            <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">
              {{ formatInteger(summary.official_session_pool?.total_count) }}
            </p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('admin.augmentGateway.activePoolSessions') }}
            </p>
            <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">
              {{ formatInteger(summary.official_session_pool?.active_count) }}
            </p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('admin.augmentGateway.healthyPoolSessions') }}
            </p>
            <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">
              {{ formatInteger(summary.official_session_pool?.healthy_count) }}
            </p>
          </div>
        </div>

        <div v-if="sessionSourceCounts.length > 0" class="mt-4 flex flex-wrap gap-2">
          <span
            v-for="[source, count] in sessionSourceCounts"
            :key="source"
            class="inline-flex items-center rounded-md border border-gray-200 px-2.5 py-1 text-xs font-medium text-gray-600 dark:border-dark-700 dark:text-gray-300"
          >
            {{ source }}: {{ count }}
          </span>
        </div>

        <div class="mt-4 space-y-3">
          <div
            v-for="row in sessions"
            :key="row.id"
            class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40"
          >
            <div class="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
              <div class="space-y-1">
                <p class="font-medium text-gray-900 dark:text-white">{{ tenantHost(row.tenant_origin) }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400">
                  {{ row.source }} / {{ row.status }}
                </p>
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
                  :data-test="`disable-session-${row.id}`"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="disableSession(row.id)"
                >
                  {{ t('admin.augmentGateway.disable') }}
                </button>
                <button
                  :data-test="`require-relogin-session-${row.id}`"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="requireRelogin(row.id)"
                >
                  {{ t('admin.augmentGateway.requireRelogin') }}
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
        <div class="flex flex-col gap-2 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.augmentGateway.usage') }}
            </h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">
              {{ t('admin.augmentGateway.usageDescription') }}
            </p>
          </div>
          <span class="inline-flex items-center rounded-md border border-gray-200 px-2.5 py-1 text-xs font-medium text-gray-600 dark:border-dark-700 dark:text-gray-300">
            {{ t('admin.augmentGateway.configuredRoutePolicyVersion') }}: {{ configuredRoutePolicyVersion }}
          </span>
        </div>

        <div class="mt-4 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ t('admin.augmentGateway.cacheHitRatio') }}</p>
            <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">{{ formatPercent(summary.usage?.cache_hit_ratio) }}</p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ t('admin.augmentGateway.estimatedCost') }}</p>
            <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">{{ formatCurrency(summary.usage?.estimated_cost) }}</p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ t('admin.augmentGateway.settledCost') }}</p>
            <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">{{ formatCurrency(summary.usage?.settled_cost) }}</p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ t('admin.augmentGateway.cacheReadTokens') }}</p>
            <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">{{ formatInteger(summary.usage?.total_cache_read_tokens) }}</p>
          </div>
        </div>

        <div class="mt-4 overflow-x-auto">
          <table class="min-w-full text-sm">
            <thead>
              <tr class="border-b border-gray-100 text-left text-xs uppercase tracking-wide text-gray-500 dark:border-dark-800 dark:text-gray-400">
                <th class="px-3 py-2">{{ t('admin.augmentGateway.createdAt') }}</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.model') }}</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.scope') }}</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.groupId') }}</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.sessionId') }}</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.costs') }}</th>
                <th class="px-3 py-2">{{ t('admin.augmentGateway.requestId') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="row in usageRows"
                :key="row.request_id"
                class="border-b border-gray-100 dark:border-dark-800"
              >
                <td class="px-3 py-2 text-xs text-gray-500 dark:text-gray-400">{{ formatTimestamp(row.created_at) }}</td>
                <td class="px-3 py-2">
                  <div class="font-medium text-gray-900 dark:text-white">{{ row.model }}</div>
                  <div v-if="row.upstream_model" class="text-xs text-gray-500 dark:text-gray-400">{{ row.upstream_model }}</div>
                </td>
                <td class="px-3 py-2">
                  <div>{{ row.request_scope || '—' }}</div>
                  <div class="text-xs text-gray-500 dark:text-gray-400">{{ row.feature_scope || '—' }}</div>
                </td>
                <td class="px-3 py-2 font-mono text-xs text-gray-600 dark:text-gray-300">{{ row.group_id ?? '—' }}</td>
                <td class="px-3 py-2 font-mono text-xs text-gray-600 dark:text-gray-300">{{ row.augment_session_id || '—' }}</td>
                <td class="px-3 py-2">
                  <div>{{ formatCurrency(row.estimated_cost) }}</div>
                  <div class="text-xs text-gray-500 dark:text-gray-400">{{ formatCurrency(row.settled_cost) }}</div>
                </td>
                <td class="px-3 py-2">
                  <div class="font-mono text-xs text-gray-600 dark:text-gray-300">{{ row.request_id }}</div>
                  <div class="text-xs text-gray-500 dark:text-gray-400">
                    {{ t('admin.augmentGateway.executedRoutePolicyVersion') }}: {{ row.route_policy_version || '—' }}
                  </div>
                </td>
              </tr>
              <tr v-if="usageRows.length === 0">
                <td colspan="7" class="px-3 py-6 text-center text-sm text-gray-500 dark:text-gray-400">
                  {{ t('admin.augmentGateway.emptyUsage') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
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
  disableAugmentPoolSessionAdmin,
  requireAugmentPoolSessionReloginAdmin,
  updateAugmentGatewayModel,
  updateAugmentGatewaySourcePriority,
  type AugmentGatewayAdminUsageRow,
  type AugmentGatewayModelRow,
  type AugmentGatewaySummary,
  type AugmentPoolSessionAdminRow,
  type AugmentProviderGroupRow,
  type AugmentGatewayEntitlementGroupRow,
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

const entitlementGroups = computed<AugmentGatewayEntitlementGroupRow[]>(() => summary.value.entitlement_groups?.rows ?? [])
const configuredRoutePolicyVersion = computed(() =>
  summary.value.provider_routing_groups?.configured_route_policy_version
  ?? summary.value.provider_routing_groups?.route_policy_version
  ?? summary.value.configured_route_policy_version
  ?? summary.value.route_policy_version
  ?? 'v1',
)
const sessionSourceCounts = computed<[string, number][]>(() => Object.entries(summary.value.official_session_pool?.source_counts ?? {}))

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
    sourcePriority.value = [...(priorityData.sources ?? summaryData.provider_routing_groups?.source_priority ?? summaryData.source_priority ?? [])]
    models.value = modelData.rows ?? summaryData.models ?? []
    sessions.value = sessionData.rows ?? []
    usageRows.value = usageData.rows ?? []
    syncSessionPoolSummary(sessions.value)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.loadFailed')))
  }
}

async function refreshSourcePriority(): Promise<void> {
  const priorityData = await getAugmentGatewaySourcePriority()
  sourcePriority.value = [...(priorityData.sources ?? [])]
  if (summary.value.provider_routing_groups) {
    summary.value.provider_routing_groups = {
      ...summary.value.provider_routing_groups,
      source_priority: [...sourcePriority.value],
    }
  }
  summary.value = {
    ...summary.value,
    source_priority: [...sourcePriority.value],
  }
}

async function refreshModels(): Promise<void> {
  const modelData = await getAugmentGatewayModels()
  models.value = modelData.rows ?? []
}

async function refreshSessions(): Promise<void> {
  const sessionData = await listAugmentPoolSessions()
  sessions.value = sessionData.rows ?? []
  syncSessionPoolSummary(sessions.value)
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
    await refreshSourcePriority()
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
    await refreshModels()
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

async function disableSession(sessionId: number): Promise<void> {
  try {
    await disableAugmentPoolSessionAdmin(sessionId)
    await refreshSessions()
    appStore.showSuccess(t('admin.augmentGateway.saved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.saveFailed')))
  }
}

async function requireRelogin(sessionId: number): Promise<void> {
  try {
    await requireAugmentPoolSessionReloginAdmin(sessionId)
    await refreshSessions()
    appStore.showSuccess(t('admin.augmentGateway.saved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.augmentGateway.saveFailed')))
  }
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

function syncSessionPoolSummary(rows: AugmentPoolSessionAdminRow[]): void {
  const sourceCounts: Record<string, number> = {}
  let activeCount = 0
  let healthyCount = 0
  for (const row of rows) {
    sourceCounts[row.source] = (sourceCounts[row.source] ?? 0) + 1
    if (row.status === 'active') {
      activeCount += 1
      if (row.has_credential_payload) {
        healthyCount += 1
      }
    }
  }
  summary.value = {
    ...summary.value,
    official_session_pool: {
      total_count: rows.length,
      active_count: activeCount,
      healthy_count: healthyCount,
      source_counts: sourceCounts,
    },
    official_session_count: rows.length,
    active_session_count: activeCount,
    healthy_session_count: healthyCount,
  }
}

function tenantHost(origin: string): string {
  try {
    return new URL(origin).host
  } catch {
    return origin
  }
}

function formatCurrency(value?: number): string {
  const amount = Number.isFinite(value ?? Number.NaN) ? Number(value) : 0
  const currency = summary.value.usage?.currency || 'USD'
  return `${currency} ${amount.toFixed(2)}`
}

function formatPercent(value?: number): string {
  const ratio = Number.isFinite(value ?? Number.NaN) ? Number(value) : 0
  return `${(ratio * 100).toFixed(1)}%`
}

function formatInteger(value?: number): string {
  return new Intl.NumberFormat().format(value ?? 0)
}

function formatTimestamp(value?: string): string {
  if (!value) {
    return '—'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

function sanitizeDiagnostics(input: unknown): unknown {
  const forbidden = ['token', 'cookie', 'payload', 'authorization', 'session_bundle']
  if (Array.isArray(input)) {
    return input.map((item) => sanitizeDiagnostics(item))
  }
  if (input && typeof input === 'object') {
    return Object.fromEntries(
      Object.entries(input as Record<string, unknown>)
        .filter(([key]) => {
          const lower = key.toLowerCase()
          return !forbidden.some((word) => lower.includes(word))
        })
        .map(([key, value]) => [key, sanitizeDiagnostics(value)]),
    )
  }
  return input
}
</script>
