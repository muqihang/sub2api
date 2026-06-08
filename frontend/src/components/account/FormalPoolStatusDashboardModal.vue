<template>
  <div
      v-if="show"
      class="fixed inset-0 z-[100] flex bg-gray-950/70 p-2 text-gray-900 backdrop-blur-sm dark:text-gray-100 sm:p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="formal-pool-dashboard-title"
    >
      <section class="flex min-h-0 w-full flex-col overflow-hidden rounded-2xl bg-white shadow-2xl dark:bg-dark-900">
        <header class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-700 sm:px-6">
          <div>
            <h2 id="formal-pool-dashboard-title" class="text-lg font-semibold text-gray-950 dark:text-white">
              号池实时看板
            </h2>
            <div class="mt-1 flex flex-wrap items-center gap-3 text-xs text-gray-500 dark:text-gray-400">
              <span>最后更新：{{ lastUpdatedText }}</span>
              <span class="inline-flex items-center gap-1">
                <span class="h-2 w-2 rounded-full" :class="autoRefreshDotClass"></span>
                自动刷新：打开时每 5 秒
              </span>
              <span v-if="loading">刷新中...</span>
            </div>
          </div>
          <div class="flex items-center gap-2">
            <button
              type="button"
              class="rounded-lg border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 transition hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-600 dark:text-gray-200 dark:hover:bg-dark-800"
              :disabled="loading"
              @click="manualRefresh"
            >
              手动刷新
            </button>
            <button
              type="button"
              class="rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white transition hover:bg-gray-800 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-white"
              @click="emit('close')"
            >
              关闭
            </button>
          </div>
        </header>

        <main class="min-h-0 flex-1 overflow-y-auto bg-gray-50 p-4 dark:bg-dark-950 sm:p-6">
          <div v-if="error" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-800/60 dark:bg-red-950/30 dark:text-red-300">
            {{ error }}
          </div>

          <section class="grid grid-cols-2 gap-3 lg:grid-cols-4 xl:grid-cols-6">
            <article
              v-for="card in summaryCards"
              :key="card.label"
              class="rounded-xl border border-gray-200 bg-white p-3 shadow-sm dark:border-dark-700 dark:bg-dark-900"
            >
              <div class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ card.label }}</div>
              <div class="mt-1 text-2xl font-semibold" :class="card.className">{{ card.value }}</div>
            </article>
          </section>

          <section class="mt-4 flex flex-wrap gap-2 rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900">
            <button
              v-for="filter in filters"
              :key="filter.key"
              type="button"
              class="rounded-full px-3 py-1.5 text-sm font-medium transition"
              :class="activeFilter === filter.key ? 'bg-primary-600 text-white shadow-sm' : 'bg-gray-100 text-gray-700 hover:bg-gray-200 dark:bg-dark-800 dark:text-gray-200 dark:hover:bg-dark-700'"
              :data-filter="filter.key"
              @click="activeFilter = filter.key"
            >
              {{ filter.label }}
            </button>
          </section>

          <section class="mt-4 overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
            <div class="overflow-x-auto">
              <table class="min-w-[1280px] w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
                <thead class="bg-gray-100 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-gray-400">
                  <tr>
                    <th class="px-4 py-3 text-left">账号</th>
                    <th class="px-4 py-3 text-left">状态</th>
                    <th class="px-4 py-3 text-left">阶段</th>
                    <th class="px-4 py-3 text-left">5 小时限额</th>
                    <th class="px-4 py-3 text-left">RPM 实况</th>
                    <th class="px-4 py-3 text-left">并发</th>
                    <th class="px-4 py-3 text-left">会话</th>
                    <th class="px-4 py-3 text-left">最近请求</th>
                    <th class="px-4 py-3 text-left">建议动作</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
                  <tr v-if="!loading && filteredAccounts.length === 0">
                    <td colspan="9" class="px-4 py-12 text-center text-gray-500 dark:text-gray-400">暂无匹配账号</td>
                  </tr>
                  <tr
                    v-for="row in filteredAccounts"
                    :key="row.account_id"
                    :data-account-row="row.account_id"
                    class="align-top hover:bg-gray-50 dark:hover:bg-dark-800/70"
                  >
                    <td class="px-4 py-3">
                      <div class="font-medium text-gray-950 dark:text-white">{{ row.account_label }}</div>
                      <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ row.platform }} / {{ row.type }}</div>
                    </td>
                    <td class="px-4 py-3">
                      <span :class="['inline-flex rounded-full px-2.5 py-1 text-xs font-semibold', stateBadgeClass(row)]">
                        {{ row.state_label || fallbackStateLabel(row.state) }}
                      </span>
                    </td>
                    <td class="px-4 py-3 text-gray-700 dark:text-gray-200">{{ stageLabel(row.stage) }}</td>
                    <td class="px-4 py-3">
                      <MetricCell :text="formatFiveHourWindow(row.five_hour_window)" :available="row.five_hour_window.available" :percent="row.five_hour_window.utilization ?? undefined" />
                    </td>
                    <td class="px-4 py-3">
                      <MetricCell :text="formatRpmText(row.rpm)" :available="row.rpm.available" :percent="row.rpm.utilization ?? undefined" />
                    </td>
                    <td class="px-4 py-3">
                      <MetricCell :text="formatConcurrencyText(row.concurrency)" :available="row.concurrency.available" :percent="row.concurrency.utilization ?? undefined" />
                    </td>
                    <td class="px-4 py-3">
                      <MetricCell :text="formatSessionsText(row.sessions)" :available="row.sessions.available" :percent="row.sessions.utilization ?? undefined" />
                    </td>
                    <td class="px-4 py-3 text-gray-700 dark:text-gray-200">
                      <div>{{ formatRecent(row) }}</div>
                      <div v-if="row.last_failure_code || row.last_failure_bucket" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                        {{ row.last_failure_code || row.last_failure_bucket }}
                      </div>
                    </td>
                    <td class="px-4 py-3 text-gray-700 dark:text-gray-200">
                      <div class="font-medium">{{ recommendationText(row) }}</div>
                      <div v-if="row.recommendation?.detail" class="mt-1 max-w-xs whitespace-normal text-xs text-gray-500 dark:text-gray-400">
                        {{ row.recommendation.detail }}
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </main>
      </section>
    </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, ref, watch } from 'vue'
import { adminAPI } from '@/api/admin'
import type { FormalPoolDashboardState, FormalPoolStatusDashboard, FormalPoolStatusDashboardAccount, FormalPoolStatusRuntime } from '@/types'
import {
  formatConcurrencyText,
  dashboardRatioToPercent,
  formatDashboardPercent,
  formatFiveHourWindow,
  formatRpmText,
  getDashboardRecommendationText,
} from '@/utils/formalPoolStatusDashboard'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
}>()

const REFRESH_MS = 5000
const dashboard = ref<FormalPoolStatusDashboard | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)
const activeFilter = ref('all')
const lastUpdatedAt = ref<string | null>(null)
let refreshTimer: number | null = null
let inFlight = false
let activeAbortController: AbortController | null = null

const MetricCell = defineComponent({
  name: 'MetricCell',
  props: {
    text: { type: String, required: true },
    available: { type: Boolean, required: true },
    percent: { type: Number, default: null },
  },
  setup(cellProps) {
    return () => h('div', { class: 'min-w-[160px]' }, [
      h('div', { class: cellProps.available ? 'text-gray-800 dark:text-gray-100' : 'font-medium text-amber-700 dark:text-amber-300' }, cellProps.text),
      cellProps.available
        ? h('div', { class: 'mt-2 h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700' }, [
            h('div', {
              class: 'h-full rounded-full bg-primary-500',
              style: { width: `${dashboardRatioToPercent(cellProps.percent) ?? 0}%` },
            }),
          ])
        : null,
    ])
  },
})

const dashboardStatePriority: Record<FormalPoolDashboardState, number> = {
  inactive: 0,
  manual_risk: 1,
  rate_limited: 2,
  quarantined: 3,
  error: 4,
  not_schedulable: 5,
  evidence_missing: 6,
  data_missing: 7,
  warming: 8,
  production: 9,
  normal: 10,
}

const filters = [
  { key: 'all', label: '全部' },
  { key: 'inactive', label: '已停用' },
  { key: 'manual_risk', label: '需人工介入' },
  { key: 'rate_limited', label: '限流' },
  { key: 'quarantined', label: '隔离' },
  { key: 'error', label: '错误' },
  { key: 'not_schedulable', label: '不可调度' },
  { key: 'evidence_missing', label: '证据不足' },
  { key: 'data_missing', label: '数据不足' },
  { key: 'warming', label: '预热' },
  { key: 'production', label: '生产' },
  { key: 'normal', label: '正常' },
  { key: 'schedulable_only', label: '只看可调度' },
]

const summaryCards = computed(() => {
  const s = dashboard.value?.summary
  return [
    { label: '可正常调度', value: s ? String(s.schedulable) : '-', className: 'text-emerald-600 dark:text-emerald-400' },
    { label: '预热中', value: s ? String(s.warming) : '-', className: 'text-sky-600 dark:text-sky-400' },
    { label: '生产中', value: s ? String(s.production) : '-', className: 'text-emerald-600 dark:text-emerald-400' },
    { label: '限流冷却', value: s ? String(s.rate_limited) : '-', className: 'text-orange-600 dark:text-orange-400' },
    { label: '需人工介入', value: s ? String(s.manual_risk) : '-', className: 'text-red-600 dark:text-red-400' },
    { label: '错误/隔离', value: s ? String(s.error + s.quarantined) : '-', className: 'text-red-600 dark:text-red-400' },
    { label: '已停用', value: s ? String(s.inactive) : '-', className: 'text-gray-500 dark:text-gray-400' },
    { label: '不可调度', value: s ? String(s.not_schedulable) : '-', className: 'text-amber-600 dark:text-amber-400' },
    { label: '证据不足', value: s ? String(s.evidence_missing) : '-', className: 'text-amber-600 dark:text-amber-400' },
    { label: '数据不足', value: s ? String(s.data_missing) : '-', className: 'text-amber-600 dark:text-amber-400' },
    { label: '当前总 RPM', value: s ? `${s.total_current_rpm} / ${s.total_rpm_limit}` : '-', className: 'text-primary-600 dark:text-primary-400' },
    { label: '5 小时总体余量', value: s ? formatDashboardPercent(s.five_hour_remaining_ratio) : '-', className: 'text-primary-600 dark:text-primary-400' },
  ]
})

const sortedDashboardRows = (rows: FormalPoolStatusDashboardAccount[]) => [...rows].sort((a, b) => {
  const pa = dashboardStatePriority[a.state] ?? Number.MAX_SAFE_INTEGER
  const pb = dashboardStatePriority[b.state] ?? Number.MAX_SAFE_INTEGER
  if (pa !== pb) return pa - pb
  return a.account_id - b.account_id
})

const filteredAccounts = computed(() => {
  const rows = sortedDashboardRows(dashboard.value?.accounts ?? [])
  if (activeFilter.value === 'all') return rows
  if (activeFilter.value === 'schedulable_only') return rows.filter(row => row.effective_schedulable)
  return rows.filter(row => row.state === activeFilter.value)
})

const lastUpdatedText = computed(() => {
  const value = lastUpdatedAt.value || dashboard.value?.summary.generated_at
  if (!value) return '尚未更新'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
})

const autoRefreshDotClass = computed(() => (props.show && !error.value ? 'bg-emerald-500' : 'bg-gray-400'))

async function refreshDashboard() {
  if (!props.show || inFlight) return
  const controller = new AbortController()
  activeAbortController = controller
  inFlight = true
  loading.value = true
  try {
    const data = await adminAPI.accounts.getFormalPoolStatusDashboard({ signal: controller.signal })
    if (!props.show || controller.signal.aborted || activeAbortController !== controller) return
    dashboard.value = data
    lastUpdatedAt.value = data.summary.generated_at || new Date().toISOString()
    error.value = null
  } catch (err: any) {
    if (controller.signal.aborted || err?.name === 'CanceledError' || err?.name === 'AbortError' || err?.code === 'ERR_CANCELED') return
    if (props.show && activeAbortController === controller) {
      error.value = err?.response?.data?.message || err?.message || '刷新号池实时看板失败'
    }
  } finally {
    if (activeAbortController === controller) {
      activeAbortController = null
      loading.value = false
      inFlight = false
    }
  }
}

function startAutoRefresh() {
  stopAutoRefresh()
  refreshDashboard()
  refreshTimer = window.setInterval(() => {
    refreshDashboard()
  }, REFRESH_MS)
}

function stopAutoRefresh() {
  if (refreshTimer !== null) {
    window.clearInterval(refreshTimer)
    refreshTimer = null
  }
  if (activeAbortController) {
    activeAbortController.abort()
    activeAbortController = null
  }
  loading.value = false
  inFlight = false
}

function manualRefresh() {
  refreshDashboard()
}

function fallbackStateLabel(state: FormalPoolDashboardState) {
  const labels: Record<string, string> = {
    normal: '正常',
    warming: '预热中',
    production: '生产中',
    rate_limited: '限流冷却中',
    manual_risk: '需人工介入',
    error: '错误',
    quarantined: '已隔离',
    inactive: '已停用',
    not_schedulable: '不可调度',
    evidence_missing: '证据不足',
    data_missing: '数据不足',
  }
  return labels[state] ?? '数据不足'
}

function stateBadgeClass(row: FormalPoolStatusDashboardAccount) {
  switch (row.state) {
    case 'normal':
    case 'production':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'warming':
      return 'bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-300'
    case 'rate_limited':
      return 'bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300'
    case 'manual_risk':
    case 'error':
    case 'quarantined':
      return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
    case 'inactive':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
    default:
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
  }
}

function stageLabel(stage: string) {
  const labels: Record<string, string> = {
    imported: '已导入',
    refreshed: '已刷新',
    runtime_registered: '运行时已注册',
    healthcheck_passed: '健康检查通过',
    warming: '预热中',
    production: '生产中',
    quarantined: '已隔离',
    legacy_unknown: '历史未知',
  }
  return labels[stage] ?? (stage || '-')
}

function formatSessionsText(runtime: FormalPoolStatusRuntime | null | undefined) {
  if (!runtime?.available) return '数据不足'
  if (runtime.limit <= 0) return '未配置会话'
  return `${runtime.current} / ${runtime.limit} 会话 (${formatDashboardPercent(runtime.utilization)})`
}

function formatRecent(row: FormalPoolStatusDashboardAccount) {
  if (row.last_success_hint) return row.last_success_hint
  if (!row.last_used_at) return '最近请求未知'
  const date = new Date(row.last_used_at)
  if (Number.isNaN(date.getTime())) return row.last_used_at
  return date.toLocaleString('zh-CN', { hour12: false })
}

function recommendationText(row: FormalPoolStatusDashboardAccount) {
  return row.recommendation?.label || getDashboardRecommendationText(row)
}

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      activeFilter.value = 'all'
      startAutoRefresh()
    } else {
      stopAutoRefresh()
    }
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  stopAutoRefresh()
})
</script>
