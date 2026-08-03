<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6">
      <section class="rounded-xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
        <div class="space-y-2">
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('plugin.augment.billingTitle') }}
          </h1>
          <p class="text-sm text-gray-600 dark:text-gray-300">
            {{ t('plugin.augment.billing.summaryTitle') }}
          </p>
        </div>

        <div v-if="loading" class="mt-8 flex items-center justify-center py-12">
          <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
        </div>

        <div v-else class="mt-6 space-y-6">
          <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-6">
            <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
              <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('plugin.augment.billing.estimatedCost') }}
              </p>
              <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">
                {{ summary?.estimated_cost ?? 0 }}
              </p>
            </div>
            <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
              <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('plugin.augment.billing.settledCost') }}
              </p>
              <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">
                {{ summary?.settled_cost ?? 0 }}
              </p>
            </div>
            <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
              <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('plugin.augment.billing.sharedWalletFreeQuota') }}
              </p>
              <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">
                {{ summary?.free_quota ?? 0 }}
              </p>
            </div>
            <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
              <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('plugin.augment.billing.sharedWalletPaidBalance') }}
              </p>
              <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">
                {{ summary?.paid_balance ?? 0 }}
              </p>
            </div>
            <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
              <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('plugin.augment.billing.cacheReadTokens') }}
              </p>
              <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">
                {{ summary?.total_cache_read_tokens ?? 0 }}
              </p>
            </div>
            <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-950/40">
              <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('plugin.augment.billing.cacheCreationTokens') }}
              </p>
              <p class="mt-2 text-xl font-semibold text-gray-900 dark:text-white">
                {{ summary?.total_cache_creation_tokens ?? 0 }}
              </p>
            </div>
          </div>

          <OfficialSessionStatusCard :session="officialSession" />

          <section class="rounded-xl border border-blue-200 bg-blue-50 p-5 dark:border-blue-900/40 dark:bg-blue-950/30">
            <h2 class="text-sm font-semibold text-blue-900 dark:text-blue-100">
              {{ t('plugin.augment.billing.sharedWalletTitle') }}
            </h2>
            <p class="mt-2 text-sm text-blue-800 dark:text-blue-200">
              {{ t('plugin.augment.billing.sharedWalletDescription') }}
            </p>
            <p class="mt-2 text-sm text-blue-800 dark:text-blue-200">
              {{ t('plugin.augment.billing.singleActiveKey') }}
            </p>
          </section>

          <section class="rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
            <div class="flex items-center justify-between">
              <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t('plugin.augment.billing.requestsTitle') }}
              </h2>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('plugin.augment.billing.cacheHitRatio') }}: {{ summary?.cache_hit_ratio ?? 0 }}
              </p>
            </div>

            <div class="mt-4 overflow-x-auto">
              <table class="min-w-full text-sm">
                <thead>
                  <tr class="border-b border-gray-200 text-left text-xs uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:text-gray-400">
                    <th class="px-3 py-2">{{ t('plugin.augment.billing.model') }}</th>
                    <th class="px-3 py-2">{{ t('plugin.augment.billing.endpoint') }}</th>
                    <th class="px-3 py-2">{{ t('plugin.augment.billing.attribution') }}</th>
                    <th class="px-3 py-2">{{ tokensLabel }}</th>
                    <th class="px-3 py-2">{{ t('plugin.augment.billing.cacheReadTokens') }}</th>
                    <th class="px-3 py-2">{{ t('plugin.augment.billing.cacheCreationTokens') }}</th>
                    <th class="px-3 py-2">{{ t('plugin.augment.billing.estimatedCost') }}</th>
                    <th class="px-3 py-2">{{ t('plugin.augment.billing.settledCost') }}</th>
                    <th class="px-3 py-2">{{ t('plugin.augment.billing.requestId') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="row in usageRows"
                    :key="row.request_id"
                    class="border-b border-gray-100 text-gray-900 dark:border-dark-800 dark:text-gray-100"
                  >
                    <td class="px-3 py-2">
                      <div>{{ row.model }}</div>
                      <div
                        v-if="row.upstream_model"
                        class="text-xs text-gray-500 dark:text-gray-400"
                      >
                        {{ row.upstream_model }}
                      </div>
                    </td>
                    <td class="px-3 py-2">
                      <div>{{ row.endpoint }}</div>
                      <div
                        v-if="row.request_scope || row.feature_scope"
                        class="text-xs text-gray-500 dark:text-gray-400"
                      >
                        {{ row.request_scope || '—' }} / {{ row.feature_scope || '—' }}
                      </div>
                    </td>
                    <td class="px-3 py-2">
                      <div class="text-xs text-gray-600 dark:text-gray-300">
                        {{ t('plugin.augment.billing.groupId') }}: {{ row.group_id ?? '—' }}
                      </div>
                      <div class="font-mono text-xs text-gray-500 dark:text-gray-400">
                        {{ t('plugin.augment.billing.sessionId') }}: {{ row.augment_session_id || '—' }}
                      </div>
                    </td>
                    <td class="px-3 py-2">{{ row.tokens }}</td>
                    <td class="px-3 py-2">{{ row.cache_read_tokens }}</td>
                    <td class="px-3 py-2">{{ row.cache_creation_tokens }}</td>
                    <td class="px-3 py-2">{{ row.estimated_cost }}</td>
                    <td class="px-3 py-2">{{ row.settled_cost }}</td>
                    <td class="px-3 py-2">
                      <div class="font-mono text-xs">{{ row.request_id }}</div>
                      <div class="text-xs text-gray-500 dark:text-gray-400">
                        {{ t('plugin.augment.billing.routePolicyVersion') }}: {{ row.route_policy_version || '—' }}
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section class="rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('plugin.augment.billing.errorsTitle') }}
            </h2>
            <ul class="mt-4 space-y-3">
              <li
                v-for="row in recentErrors"
                :key="row.request_id"
                class="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-sm dark:border-dark-700 dark:bg-dark-950/40"
              >
                <div class="flex flex-wrap items-center justify-between gap-3">
                  <span class="font-medium text-gray-900 dark:text-white">{{ row.model }}</span>
                  <span class="font-mono text-xs text-gray-500 dark:text-gray-400">{{ row.request_id }}</span>
                </div>
                <p class="mt-1 text-gray-600 dark:text-gray-300">
                  {{ row.error_class }}
                </p>
              </li>
            </ul>
          </section>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import OfficialSessionStatusCard from '@/components/plugin/augment/OfficialSessionStatusCard.vue'
import { getAugmentOfficialSession, type AugmentOfficialSessionView } from '@/api/augment'
import {
  getAugmentBillingSummary,
  listAugmentBillingUsage,
  listAugmentRecentErrors,
  type AugmentBillingRecentErrorRow,
  type AugmentBillingSummary,
  type AugmentBillingUsageRow,
} from '@/api/augmentBilling'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const summary = ref<AugmentBillingSummary | null>(null)
const usageRows = ref<AugmentBillingUsageRow[]>([])
const recentErrors = ref<AugmentBillingRecentErrorRow[]>([])
const officialSession = ref<AugmentOfficialSessionView | null>(null)

const tokensLabel = computed(() => {
  const value = t('plugin.augment.billing.tokens')
  return value === 'plugin.augment.billing.tokens' ? 'Usage' : value
})

onMounted(async () => {
  try {
    const [session, summaryData, usageData, errorData] = await Promise.all([
      getAugmentOfficialSession(),
      getAugmentBillingSummary(),
      listAugmentBillingUsage({ page: 1, page_size: 20 }),
      listAugmentRecentErrors({ limit: 10 }),
    ])
    officialSession.value = session
    summary.value = summaryData
    usageRows.value = usageData.rows ?? []
    recentErrors.value = errorData.rows ?? []
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('plugin.augment.billing.loadFailed')))
  } finally {
    loading.value = false
  }
})
</script>
