<template>
  <div
    v-if="show"
    class="fixed inset-0 z-[100] flex bg-slate-950/80 p-2 text-slate-900 backdrop-blur-sm dark:text-slate-100 sm:p-4"
    role="dialog"
    aria-modal="true"
    aria-labelledby="formal-pool-dashboard-v2-title"
    data-testid="formal-pool-dashboard-v2"
  >
    <section
      class="flex min-h-0 w-full flex-col overflow-hidden rounded-2xl bg-slate-50 shadow-2xl ring-1 ring-slate-200 dark:bg-dark-950 dark:ring-dark-700"
    >
      <!-- Header -->
      <header
        class="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 bg-white px-4 py-3 dark:border-dark-700 dark:bg-dark-900 sm:px-6"
      >
        <div>
          <p class="text-xs uppercase tracking-wide text-slate-400 dark:text-slate-500">
            Account command center
          </p>
          <h2
            id="formal-pool-dashboard-v2-title"
            class="text-lg font-semibold text-slate-950 dark:text-white"
          >
            号池实时看板
          </h2>
          <div class="mt-1 flex flex-wrap items-center gap-3 text-xs text-slate-500 dark:text-slate-400">
            <span>{{ totalText }}</span>
            <span>最后更新：{{ lastUpdatedText }}</span>
            <span class="inline-flex items-center gap-1.5">
              <span
                class="h-1.5 w-1.5 rounded-full"
                :class="autoRefreshDotClass"
              ></span>
              自动刷新 · 30s
            </span>
            <span v-if="loading" class="text-slate-400">刷新中…</span>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="rounded-lg border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 transition hover:bg-slate-100 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-600 dark:text-slate-200 dark:hover:bg-dark-800"
            :disabled="loading"
            data-testid="dashboard-v2-manual-refresh"
            @click="manualRefresh"
          >
            手动刷新
          </button>
          <button
            type="button"
            class="rounded-lg bg-slate-900 px-3 py-1.5 text-sm font-medium text-white transition hover:bg-slate-800 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
            data-testid="dashboard-v2-close"
            @click="emit('close')"
          >
            关闭
          </button>
        </div>
      </header>

      <main class="min-h-0 flex-1 overflow-y-auto px-4 py-5 sm:px-6">
        <div
          v-if="error"
          class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-800/60 dark:bg-rose-950/30 dark:text-rose-300"
        >
          {{ scrubFormalPoolDisplayText(error, '刷新号池实时看板失败') }}
        </div>

        <!-- Hero: health distribution bar + 3 command metrics -->
        <section
          class="grid grid-cols-1 gap-4 lg:grid-cols-[5fr_7fr]"
          data-testid="dashboard-v2-hero"
        >
          <!-- Health distribution bar -->
          <div
            class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-900"
            data-testid="dashboard-v2-health-distribution"
          >
            <div class="flex items-baseline justify-between">
              <div class="text-sm font-medium text-slate-700 dark:text-slate-200">
                号池健康分布
              </div>
              <div class="text-xs text-slate-400 dark:text-slate-500">
                共 {{ buckets.total }} 个
              </div>
            </div>
            <div
              class="mt-3 flex h-3 w-full overflow-hidden rounded-full bg-slate-100 dark:bg-dark-800"
              role="img"
              aria-label="号池健康分布"
            >
              <div
                class="bg-emerald-500"
                :style="{ width: `${distribution.active}%` }"
                data-testid="distribution-segment-active"
                :title="`能用 ${buckets.active}`"
              ></div>
              <div
                class="bg-amber-400"
                :style="{ width: `${distribution.paused}%` }"
                data-testid="distribution-segment-paused"
                :title="`暂停 ${buckets.paused}`"
              ></div>
              <div
                class="bg-rose-500"
                :style="{ width: `${distribution.needs_intervention}%` }"
                data-testid="distribution-segment-needs-intervention"
                :title="`待介入 ${buckets.needs_intervention}`"
              ></div>
              <div
                class="bg-slate-300 dark:bg-dark-600"
                :style="{ width: `${distribution.inactive}%` }"
                data-testid="distribution-segment-inactive"
                :title="`已停用 ${buckets.inactive}`"
              ></div>
            </div>
            <div class="mt-3 grid grid-cols-2 gap-2 text-xs sm:grid-cols-4">
              <div
                v-for="legend in legendEntries"
                :key="legend.bucket"
                class="flex items-center gap-1.5"
                :data-testid="`legend-${legend.bucket}`"
              >
                <span class="h-2 w-2 rounded-full" :class="legend.dotClass"></span>
                <span class="text-slate-700 dark:text-slate-200">{{ legend.label }}</span>
                <span
                  class="ml-auto tabular-nums"
                  :class="legend.bucket === 'needs_intervention' && legend.count > 0 ? 'font-medium text-rose-600 dark:text-rose-300' : 'text-slate-400 dark:text-slate-500'"
                >
                  {{ legend.count }}
                </span>
              </div>
            </div>
          </div>

          <!-- 3 command metrics -->
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-3" data-testid="dashboard-v2-command-metrics">
            <article
              class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-900"
              data-testid="command-metric-usable-capacity"
            >
              <div class="text-xs text-slate-500 dark:text-slate-400">可用容量</div>
              <div class="mt-1 text-2xl font-semibold tabular-nums text-emerald-600 dark:text-emerald-400">
                {{ buckets.active }}
              </div>
              <div class="mt-1 text-xs text-slate-500 dark:text-slate-400">
                能用账号 · 含 <span class="tabular-nums">{{ buckets.warming }}</span> 个预热中
              </div>
            </article>
            <article
              class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-900"
              data-testid="command-metric-cooling-window"
            >
              <div class="text-xs text-slate-500 dark:text-slate-400">冷却窗口</div>
              <div class="mt-1 text-2xl font-semibold tabular-nums text-amber-600 dark:text-amber-400">
                {{ buckets.paused }}
              </div>
              <div class="mt-1 text-xs text-slate-500 dark:text-slate-400">
                暂停 · 等待自动恢复
              </div>
            </article>
            <article
              class="rounded-xl border p-4 shadow-sm"
              :class="
                buckets.needs_intervention > 0
                  ? 'border-rose-200 bg-rose-50 dark:border-rose-800/60 dark:bg-rose-950/30'
                  : 'border-slate-200 bg-white dark:border-dark-700 dark:bg-dark-900'
              "
              data-testid="command-metric-intervention-queue"
            >
              <div
                class="text-xs font-medium"
                :class="buckets.needs_intervention > 0 ? 'text-rose-700 dark:text-rose-300' : 'text-slate-500 dark:text-slate-400'"
              >
                待介入队列
              </div>
              <div
                class="mt-1 text-2xl font-semibold tabular-nums"
                :class="buckets.needs_intervention > 0 ? 'text-rose-700 dark:text-rose-300' : 'text-slate-700 dark:text-slate-200'"
              >
                {{ buckets.needs_intervention }}
              </div>
              <button
                v-if="buckets.needs_intervention > 0"
                type="button"
                class="mt-2 text-xs font-medium text-rose-700 hover:underline dark:text-rose-300"
                data-testid="jump-needs-intervention"
                @click="activeBucketFilter = 'needs_intervention'"
              >
                筛选并优先处理 →
              </button>
            </article>
          </div>
        </section>

        <!-- Segmented status lanes -->
        <section class="mt-5 flex flex-wrap items-center justify-between gap-3">
          <div
            class="inline-flex rounded-lg bg-white p-1 text-sm shadow-sm ring-1 ring-slate-200 dark:bg-dark-900 dark:ring-dark-700"
            role="tablist"
            aria-label="账号桶筛选"
            data-testid="dashboard-v2-lanes"
          >
            <button
              type="button"
              class="rounded-md px-3 py-1.5 font-medium transition"
              :class="
                activeBucketFilter === 'all'
                  ? 'bg-slate-900 text-white shadow-sm dark:bg-slate-100 dark:text-slate-900'
                  : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-dark-800'
              "
              data-testid="lane-all"
              role="tab"
              :aria-selected="activeBucketFilter === 'all'"
              @click="activeBucketFilter = 'all'"
            >
              全部
              <span class="ml-1 tabular-nums" :class="activeBucketFilter === 'all' ? 'text-slate-300 dark:text-slate-500' : 'text-slate-400'">
                {{ buckets.total }}
              </span>
            </button>
            <button
              v-for="bucket in DASHBOARD_BUCKET_ORDER"
              :key="bucket"
              type="button"
              class="ml-1 inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 font-medium transition"
              :class="
                activeBucketFilter === bucket
                  ? getBucketLanePresentation(bucket).laneActiveClass
                  : getBucketLanePresentation(bucket).laneInactiveClass
              "
              :data-testid="`lane-${bucket}`"
              role="tab"
              :aria-selected="activeBucketFilter === bucket"
              @click="activeBucketFilter = bucket"
            >
              <span
                v-if="bucket === 'needs_intervention'"
                class="h-1.5 w-1.5 rounded-full"
                :class="getBucketLanePresentation(bucket).dotClass"
              ></span>
              {{ getBucketLanePresentation(bucket).label }}
              <span
                class="tabular-nums"
                :class="activeBucketFilter === bucket ? 'text-white/80' : 'text-slate-400 dark:text-slate-500'"
              >
                {{ buckets[bucket] }}
              </span>
            </button>
          </div>
        </section>

        <!-- Table with expandable row drawers -->
        <section
          class="mt-4 overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900"
          data-testid="dashboard-v2-table"
        >
          <div class="overflow-x-auto">
            <table class="w-full divide-y divide-slate-200 text-sm dark:divide-dark-700">
              <thead
                class="bg-slate-50 text-xs uppercase tracking-wide text-slate-500 dark:bg-dark-800 dark:text-slate-400"
              >
                <tr>
                  <th
                    v-for="column in primaryColumns"
                    :key="column.key"
                    class="px-4 py-3 text-left font-medium"
                    :class="column.alignRight ? 'text-right' : ''"
                    :data-testid="`column-${column.key}`"
                  >
                    {{ column.label }}
                  </th>
                </tr>
              </thead>
              <tbody class="divide-y divide-slate-100 dark:divide-dark-700">
                <tr v-if="!loading && filteredRows.length === 0">
                  <td
                    :colspan="primaryColumns.length"
                    class="px-4 py-10 text-center text-slate-500 dark:text-slate-400"
                  >
                    暂无匹配账号
                  </td>
                </tr>
                <template v-for="(row, rowIndex) in filteredRows" :key="row.account_id">
                  <tr
                    :class="rowTintClass(row)"
                    :data-bucket="getDashboardBucket(row.state)"
                    :data-warming="isWarmingState(row.state) ? 'true' : 'false'"
                    :data-account-row="rowDomRef(row, rowIndex)"
                    :data-testid="`row-${rowDomRef(row, rowIndex)}`"
                  >
                    <!-- 账号 -->
                    <td class="relative px-4 py-3 align-top">
                      <span
                        :class="['pointer-events-none absolute inset-y-0 left-0 w-1', railElementClass(row)]"
                        :data-testid="`row-rail-${rowDomRef(row, rowIndex)}`"
                        :data-rail-tone="railTone(row)"
                        :data-rail-warming="isWarmingState(row.state) ? 'true' : 'false'"
                        aria-hidden="true"
                      ></span>
                      <div class="font-medium text-slate-950 dark:text-white">
                        {{ displayAccountLabel(row) }}
                      </div>
                      <div class="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
                        {{ displayPlatformType(row) }}
                      </div>
                    </td>
                    <!-- 状态 -->
                    <td class="px-4 py-3 align-top">
                      <span
                        class="inline-flex rounded-full px-2.5 py-0.5 text-xs font-medium"
                        :class="stateBadgeClass(row)"
                      >
                        {{ displayStateText(row) }}
                      </span>
                      <div
                        v-if="isWarmingState(row.state)"
                        class="mt-1 text-xs text-sky-700 dark:text-sky-300"
                        data-testid="warming-presentation-label"
                      >
                        {{ WARMING_PRESENTATION_LABEL }}
                      </div>
                    </td>
                    <!-- 5h 余量 -->
                    <td class="px-4 py-3 align-top">
                      <div class="text-xs text-slate-700 dark:text-slate-200">
                        {{ row.five_hour_window.available ? formatDashboardPercent(row.five_hour_window.utilization) : '— 数据不足 —' }}
                      </div>
                      <div
                        v-if="row.five_hour_window.available"
                        class="mt-1 h-1.5 rounded-full bg-slate-100 dark:bg-dark-700"
                        aria-hidden="true"
                      >
                        <div
                          class="h-1.5 rounded-full"
                          :class="fiveHourBarClass(row)"
                          :style="{ width: `${dashboardRatioToPercent(row.five_hour_window.utilization) ?? 0}%` }"
                        ></div>
                      </div>
                    </td>
                    <!-- 最近请求 -->
                    <td class="px-4 py-3 align-top text-xs text-slate-700 dark:text-slate-200">
                      <div>{{ formatRecent(row) }}</div>
                      <div
                        v-if="displayFailureText(row)"
                        class="mt-0.5 text-rose-600 dark:text-rose-300"
                      >
                        {{ displayFailureText(row) }}
                      </div>
                    </td>
                    <!-- 操作 -->
                    <td class="px-4 py-3 align-top text-right">
                      <button
                        type="button"
                        class="rounded-md border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 transition hover:bg-slate-100 dark:border-dark-600 dark:text-slate-200 dark:hover:bg-dark-800"
                        :data-testid="`expand-${rowDomRef(row, rowIndex)}`"
                        :aria-expanded="expandedRowId === row.account_id"
                        @click="toggleRow(row.account_id)"
                      >
                        {{ expandedRowId === row.account_id ? '收起详情' : '详情' }}
                      </button>
                    </td>
                  </tr>
                  <tr
                    v-if="expandedRowId === row.account_id"
                    :class="rowTintClass(row)"
                    :data-testid="`drawer-${rowDomRef(row, rowIndex)}`"
                  >
                    <td
                      :colspan="primaryColumns.length"
                      class="relative px-4 py-4"
                    >
                      <span
                        :class="['pointer-events-none absolute inset-y-0 left-0 w-1', railElementClass(row)]"
                        :data-testid="`drawer-rail-${rowDomRef(row, rowIndex)}`"
                        :data-rail-tone="railTone(row)"
                        :data-rail-warming="isWarmingState(row.state) ? 'true' : 'false'"
                        aria-hidden="true"
                      ></span>
                      <div class="grid grid-cols-1 gap-3 sm:grid-cols-4">
                        <article class="rounded-lg bg-slate-50 p-3 dark:bg-dark-800/70">
                          <div class="text-[11px] uppercase text-slate-400 dark:text-slate-500">RPM</div>
                          <div class="mt-1 text-sm font-medium text-slate-800 dark:text-slate-200">
                            {{ formatRpmText(row.rpm) }}
                          </div>
                        </article>
                        <article class="rounded-lg bg-slate-50 p-3 dark:bg-dark-800/70">
                          <div class="text-[11px] uppercase text-slate-400 dark:text-slate-500">并发</div>
                          <div class="mt-1 text-sm font-medium text-slate-800 dark:text-slate-200">
                            {{ formatConcurrencyText(row.concurrency) }}
                          </div>
                        </article>
                        <article class="rounded-lg bg-slate-50 p-3 dark:bg-dark-800/70">
                          <div class="text-[11px] uppercase text-slate-400 dark:text-slate-500">会话</div>
                          <div class="mt-1 text-sm font-medium text-slate-800 dark:text-slate-200">
                            {{ formatSessionsText(row.sessions) }}
                          </div>
                        </article>
                        <article class="rounded-lg bg-slate-50 p-3 dark:bg-dark-800/70">
                          <div class="text-[11px] uppercase text-slate-400 dark:text-slate-500">5h 窗口</div>
                          <div class="mt-1 text-sm font-medium text-slate-800 dark:text-slate-200">
                            {{ formatFiveHourWindow(row.five_hour_window) }}
                          </div>
                        </article>
                      </div>
                      <div
                        v-if="hasRecommendationDisplay(row)"
                        class="mt-3 rounded-lg border border-slate-200 bg-white p-3 text-xs dark:border-dark-700 dark:bg-dark-900"
                      >
                        <div class="font-medium text-slate-800 dark:text-slate-100">
                          {{ recommendationText(row) }}
                        </div>
                        <div v-if="recommendationDetailText(row)" class="mt-1 text-slate-500 dark:text-slate-400">
                          {{ recommendationDetailText(row) }}
                        </div>
                      </div>
                    </td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>
        </section>
      </main>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'

import { adminAPI } from '@/api/admin'
import type {
  FormalPoolStatusDashboard,
  FormalPoolStatusDashboardAccount,
  FormalPoolStatusRuntime,
} from '@/types'
import {
  DASHBOARD_BUCKET_ORDER,
  WARMING_PRESENTATION_LABEL,
  dashboardRatioToPercent,
  formatConcurrencyText,
  formatDashboardPercent,
  formatFiveHourWindow,
  formatRpmText,
  getBucketLanePresentation,
  getDashboardBucket,
  getDashboardBucketSortKey,
  getDashboardRecommendationText,
  isWarmingState,
  scrubFormalPoolDisplayText,
  summarizeBuckets,
  type FormalPoolFourBucket,
} from '@/utils/formalPoolStatusDashboard'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
}>()

// V2 is deliberately calmer than V1: 30s instead of 5s.
const REFRESH_MS = 30_000

const dashboard = ref<FormalPoolStatusDashboard | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)
const lastUpdatedAt = ref<string | null>(null)
const activeBucketFilter = ref<'all' | FormalPoolFourBucket>('all')
const expandedRowId = ref<number | null>(null)

let refreshTimer: number | null = null
let inFlight = false
let activeAbortController: AbortController | null = null

// ─── Derived state ───────────────────────────────────────────────────────────

interface DashboardColumn {
  key: 'account' | 'state' | 'five-hour' | 'recent' | 'actions'
  label: string
  alignRight?: boolean
}

const primaryColumns: ReadonlyArray<DashboardColumn> = [
  { key: 'account', label: '账号' },
  { key: 'state', label: '状态' },
  { key: 'five-hour', label: '5h 余量' },
  { key: 'recent', label: '最近请求' },
  { key: 'actions', label: '操作', alignRight: true },
]

const allRows = computed<FormalPoolStatusDashboardAccount[]>(() => {
  const rows = dashboard.value?.accounts ?? []
  return [...rows].sort((a, b) => {
    const pa = getDashboardBucketSortKey(a.state)
    const pb = getDashboardBucketSortKey(b.state)
    if (pa !== pb) return pa - pb
    return a.account_id - b.account_id
  })
})

const filteredRows = computed<FormalPoolStatusDashboardAccount[]>(() => {
  const rows = allRows.value
  if (activeBucketFilter.value === 'all') return rows
  return rows.filter((row) => getDashboardBucket(row.state) === activeBucketFilter.value)
})

const buckets = computed(() => summarizeBuckets(allRows.value))

const distribution = computed(() => {
  const total = buckets.value.total || 1
  const pct = (n: number) => (n / total) * 100
  return {
    active: pct(buckets.value.active),
    paused: pct(buckets.value.paused),
    needs_intervention: pct(buckets.value.needs_intervention),
    inactive: pct(buckets.value.inactive),
  }
})

const legendEntries = computed(() =>
  DASHBOARD_BUCKET_ORDER.map((bucket) => {
    const preset = getBucketLanePresentation(bucket)
    return {
      bucket,
      label: preset.label,
      dotClass: preset.dotClass,
      count: buckets.value[bucket],
    }
  }),
)

const totalText = computed(() => `${buckets.value.total} 个账号`)

const lastUpdatedText = computed(() => {
  const value = lastUpdatedAt.value || dashboard.value?.summary.generated_at
  if (!value) return '尚未更新'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
})

const autoRefreshDotClass = computed(() =>
  props.show && !error.value ? 'bg-emerald-500' : 'bg-slate-400',
)

// ─── Row rendering helpers ───────────────────────────────────────────────────

const WARMING_ROW_TINT_CLASS = 'bg-sky-50/40 dark:bg-sky-900/10'

const ROW_TINT_CLASS: Record<FormalPoolFourBucket, string> = {
  needs_intervention: 'bg-rose-50/40 dark:bg-rose-900/10',
  paused: '',
  active: '',
  inactive: '',
}

function rowTintClass(row: FormalPoolStatusDashboardAccount): string {
  if (isWarmingState(row.state)) return WARMING_ROW_TINT_CLASS
  return ROW_TINT_CLASS[getDashboardBucket(row.state)]
}

// railTone / railElementClass drive a visible <span> inside the first <td>.
// The legacy approach put border-l on the <tr>, but Tailwind's default
// border-collapse: collapse makes <tr> borders unreliable across browsers,
// so the actual rail bar lives inside the cell.
type RailTone = 'rose' | 'sky' | 'amber' | 'emerald' | 'slate'

function railTone(row: FormalPoolStatusDashboardAccount): RailTone {
  if (isWarmingState(row.state)) return 'sky'
  switch (getDashboardBucket(row.state)) {
    case 'needs_intervention':
      return 'rose'
    case 'paused':
      return 'amber'
    case 'active':
      return 'emerald'
    case 'inactive':
      return 'slate'
    default:
      return 'rose'
  }
}

function railElementClass(row: FormalPoolStatusDashboardAccount): string {
  switch (railTone(row)) {
    case 'rose':
      return 'bg-rose-500 dark:bg-rose-400'
    case 'sky':
      return 'bg-sky-500 dark:bg-sky-400'
    case 'amber':
      return 'bg-amber-400 dark:bg-amber-500/80'
    case 'emerald':
      return 'bg-emerald-500/80 dark:bg-emerald-500/70'
    case 'slate':
      return 'bg-slate-300 dark:bg-dark-600'
  }
}

function stateBadgeClass(row: FormalPoolStatusDashboardAccount): string {
  const bucket = getDashboardBucket(row.state)
  if (isWarmingState(row.state)) {
    return 'bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-300'
  }
  switch (bucket) {
    case 'needs_intervention':
      return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
    case 'paused':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    case 'active':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'inactive':
      return 'bg-slate-100 text-slate-600 dark:bg-dark-700 dark:text-slate-300'
    default:
      return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
  }
}

function fiveHourBarClass(row: FormalPoolStatusDashboardAccount): string {
  const pct = dashboardRatioToPercent(row.five_hour_window.utilization)
  if (pct === null) return 'bg-slate-300 dark:bg-dark-600'
  if (pct >= 75) return 'bg-amber-500'
  return 'bg-emerald-500'
}

function formatSessionsText(runtime: FormalPoolStatusRuntime | null | undefined): string {
  if (!runtime?.available) return '— 数据不足 —'
  if (runtime.limit <= 0) return '未配置会话'
  return `${runtime.current} / ${runtime.limit} (${formatDashboardPercent(runtime.utilization)})`
}

function displayAccountLabel(row: FormalPoolStatusDashboardAccount): string {
  const label = scrubFormalPoolDisplayText(row.account_label, '账号（未命名）')
  return /^账号\s*#\d+$/i.test(label) ? '账号（未命名）' : label
}

function rowDomRef(row: FormalPoolStatusDashboardAccount, index: number): string {
  const bucket = getDashboardBucket(row.state).replace(/[^a-z0-9_-]/gi, '-')
  return `acct-${bucket}-${index}`
}

function displayPlatformType(row: FormalPoolStatusDashboardAccount): string {
  const platform = scrubFormalPoolDisplayText(row.platform, '未知平台')
  const type = scrubFormalPoolDisplayText(row.type, '未知类型')
  return `${platform} · ${type}`
}

function displayStateText(row: FormalPoolStatusDashboardAccount): string {
  return scrubFormalPoolDisplayText(row.state_label || row.state, '未知')
}

function displayFailureText(row: FormalPoolStatusDashboardAccount): string {
  return scrubFormalPoolDisplayText(row.last_failure_code || row.last_failure_bucket, '')
}

function formatRecent(row: FormalPoolStatusDashboardAccount): string {
  if (row.last_success_hint) return scrubFormalPoolDisplayText(row.last_success_hint, '最近请求未知')
  if (!row.last_used_at) return '从未调度'
  const date = new Date(row.last_used_at)
  if (Number.isNaN(date.getTime())) return scrubFormalPoolDisplayText(row.last_used_at, '最近请求未知')
  return date.toLocaleString('zh-CN', { hour12: false })
}

function recommendationText(row: FormalPoolStatusDashboardAccount): string {
  return scrubFormalPoolDisplayText(row.recommendation?.label || getDashboardRecommendationText(row), '数据不足')
}

function recommendationDetailText(row: FormalPoolStatusDashboardAccount): string {
  return scrubFormalPoolDisplayText(row.recommendation?.detail, '')
}

function hasRecommendationDisplay(row: FormalPoolStatusDashboardAccount): boolean {
  return Boolean(row.recommendation?.label || row.recommendation?.detail)
}

function toggleRow(id: number): void {
  expandedRowId.value = expandedRowId.value === id ? null : id
}

// ─── Lifecycle ───────────────────────────────────────────────────────────────

async function refreshDashboard(): Promise<void> {
  if (!props.show || inFlight) return
  const controller = new AbortController()
  activeAbortController = controller
  inFlight = true
  loading.value = true
  try {
    const data = await adminAPI.accounts.getFormalPoolStatusDashboard({
      signal: controller.signal,
    })
    if (!props.show || controller.signal.aborted || activeAbortController !== controller) return
    dashboard.value = data
    lastUpdatedAt.value = data.summary.generated_at || new Date().toISOString()
    error.value = null
  } catch (err: any) {
    if (
      controller.signal.aborted ||
      err?.name === 'CanceledError' ||
      err?.name === 'AbortError' ||
      err?.code === 'ERR_CANCELED'
    ) {
      return
    }
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

function startAutoRefresh(): void {
  stopAutoRefresh()
  refreshDashboard()
  refreshTimer = window.setInterval(() => {
    refreshDashboard()
  }, REFRESH_MS)
}

function stopAutoRefresh(): void {
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

function manualRefresh(): void {
  refreshDashboard()
}

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      activeBucketFilter.value = 'all'
      expandedRowId.value = null
      startAutoRefresh()
    } else {
      stopAutoRefresh()
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  stopAutoRefresh()
})
</script>
