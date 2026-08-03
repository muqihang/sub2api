<template>
  <div class="card mb-4 overflow-hidden">
    <div class="flex flex-wrap items-center gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
      <div>
        <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('admin.usage.tokenRanking.title') }}</h3>
        <p class="text-xs text-gray-400">{{ t('admin.usage.tokenRanking.subtitle') }}</p>
      </div>
      <div class="ml-auto flex items-center gap-2">
        <select v-model.number="limit" class="input h-8 text-sm" @change="load">
          <option :value="20">Top 20</option>
          <option :value="50">Top 50</option>
          <option :value="100">Top 100</option>
          <option :value="200">Top 200</option>
        </select>
        <button type="button" class="btn btn-secondary h-8 px-2" :title="t('common.refresh')" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
        </button>
      </div>
    </div>
    <div class="overflow-x-auto">
      <table class="w-full text-sm">
        <thead class="border-b border-gray-100 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
          <tr>
            <th class="px-4 py-3 text-left">#</th>
            <th class="px-4 py-3 text-left">{{ t('admin.usage.tokenRanking.user') }}</th>
            <th v-for="column in columns" :key="column.key" class="cursor-pointer whitespace-nowrap px-4 py-3 text-right hover:text-primary-500" @click="setSort(column.key)">
              {{ t(column.label) }}<span v-if="sortBy === column.key"> v</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading"><td :colspan="columns.length + 2" class="px-4 py-8 text-center text-gray-400">{{ t('common.loading') }}</td></tr>
          <tr v-else-if="items.length === 0"><td :colspan="columns.length + 2" class="px-4 py-8 text-center text-gray-400">{{ t('admin.dashboard.noDataAvailable') }}</td></tr>
          <tr v-for="(item, index) in items" v-else :key="item.user_id" class="cursor-pointer border-b border-gray-50 hover:bg-gray-50 dark:border-dark-800 dark:hover:bg-dark-700/40" @click="$emit('select-user', item.user_id, item.email)">
            <td class="px-4 py-3 text-gray-400">{{ index + 1 }}</td>
            <td class="max-w-[220px] truncate px-4 py-3 text-gray-700 dark:text-gray-200">{{ item.email || `User #${item.user_id}` }}</td>
            <td class="px-4 py-3 text-right tabular-nums">{{ item.requests.toLocaleString() }}</td>
            <td class="px-4 py-3 text-right tabular-nums">{{ compact(item.input_tokens) }}</td>
            <td class="px-4 py-3 text-right tabular-nums">{{ compact(item.output_tokens) }}</td>
            <td class="px-4 py-3 text-right tabular-nums">{{ compact(item.cache_tokens) }}</td>
            <td class="px-4 py-3 text-right font-medium tabular-nums">{{ compact(item.total_tokens) }}</td>
            <td class="px-4 py-3 text-right font-medium tabular-nums text-green-600 dark:text-green-400">${{ item.actual_cost.toFixed(4) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getUserBreakdown, type UserBreakdownParams } from '@/api/admin/dashboard'
import type { UserBreakdownItem } from '@/types'
import Icon from '@/components/icons/Icon.vue'

type SortKey = NonNullable<UserBreakdownParams['sort_by']>

const props = defineProps<{ startDate: string; endDate: string; filters: Record<string, unknown>; model?: string }>()
defineEmits<{ (event: 'select-user', userId: number, email: string): void }>()

const { t } = useI18n()
const columns: { key: SortKey; label: string }[] = [
  { key: 'requests', label: 'admin.usage.tokenRanking.requests' },
  { key: 'input_tokens', label: 'admin.usage.inputTokens' },
  { key: 'output_tokens', label: 'admin.usage.outputTokens' },
  { key: 'cache_tokens', label: 'admin.usage.tokenRanking.cacheTokens' },
  { key: 'total_tokens', label: 'admin.usage.tokenRanking.totalTokens' },
  { key: 'actual_cost', label: 'admin.usage.tokenRanking.actualCost' },
]
const items = ref<UserBreakdownItem[]>([])
const loading = ref(false)
const limit = ref(50)
const sortBy = ref<SortKey>('total_tokens')
let requestSequence = 0

const compact = (value: number) => new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(value)
const setSort = (key: SortKey) => { if (sortBy.value !== key) { sortBy.value = key; void load() } }
const load = async () => {
  const sequence = ++requestSequence
  loading.value = true
  try {
    const response = await getUserBreakdown({ ...props.filters, start_date: props.startDate, end_date: props.endDate, model: props.model, sort_by: sortBy.value, limit: limit.value } as UserBreakdownParams)
    if (sequence === requestSequence) items.value = response.users ?? []
  } catch {
    if (sequence === requestSequence) items.value = []
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}
watch(() => [props.startDate, props.endDate, props.model, JSON.stringify(props.filters)], () => void load(), { immediate: true })
defineExpose({ reload: load })
</script>
