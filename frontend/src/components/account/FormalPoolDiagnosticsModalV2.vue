<template>
  <div
    v-if="show && activeAccount"
    class="fixed inset-0 z-[100] flex bg-slate-950/80 p-3 text-slate-950 backdrop-blur-sm dark:text-slate-100 sm:p-5"
    role="dialog"
    aria-modal="true"
    aria-labelledby="diagnostics-v2-title"
    data-testid="formal-pool-diagnostics-v2"
  >
    <section class="flex min-h-0 w-full flex-col overflow-hidden rounded-2xl bg-slate-50 shadow-2xl ring-1 ring-slate-200 dark:bg-dark-950 dark:ring-dark-700">
      <header
        class="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 bg-white px-4 py-3 dark:border-dark-700 dark:bg-dark-900 sm:px-6"
        data-testid="diagnostics-v2-command-bar"
      >
        <div class="min-w-0">
          <p class="text-xs uppercase tracking-wide text-slate-400">Diagnostics command center</p>
          <h2 id="diagnostics-v2-title" class="mt-0.5 truncate text-lg font-semibold text-slate-950 dark:text-white">
            {{ safeAccountName }}
          </h2>
          <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
            <span data-testid="diagnostics-v2-environment" class="rounded-full bg-indigo-50 px-2 py-0.5 font-medium text-indigo-700 dark:bg-indigo-500/10 dark:text-indigo-200">
              正式号池 · {{ safeValue(activeAccount.platform) }} / {{ safeValue(activeAccount.type) }}
            </span>
            <span data-testid="diagnostics-v2-generated-at">生成时间：{{ generatedAtText }}</span>
            <span data-testid="diagnostics-v2-refresh-state" class="inline-flex items-center gap-1.5">
              <span class="h-1.5 w-1.5 rounded-full" :class="loading ? 'bg-amber-400' : 'bg-emerald-500'"></span>
              {{ loading ? '刷新中…' : '已刷新' }}
            </span>
          </div>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <button
            v-if="hero.primaryAction"
            type="button"
            class="rounded-lg bg-slate-900 px-3 py-1.5 text-sm font-medium text-white shadow-sm transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-slate-100 dark:text-slate-900"
            data-testid="diagnostics-v2-primary-action"
            :disabled="isBusy || hero.primaryAction.behavior === 'none' || isActionDisabled(hero.primaryAction.key)"
            @click="handleAction(hero.primaryAction.key)"
          >
            {{ hero.primaryAction.label }}
          </button>
          <button
            type="button"
            class="rounded-lg border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 transition hover:bg-slate-100 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-600 dark:text-slate-200 dark:hover:bg-dark-800"
            :disabled="isBusy"
            data-testid="action-refreshDiagnostics"
            @click="refreshDiagnostics()"
          >
            刷新诊断
          </button>
          <button
            type="button"
            class="rounded-lg border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 transition hover:bg-slate-100 dark:border-dark-600 dark:text-slate-200 dark:hover:bg-dark-800"
            @click="handleClose"
          >
            关闭
          </button>
        </div>
      </header>

      <main class="min-h-0 flex-1 overflow-y-auto px-4 py-5 sm:px-6">
        <div v-if="errorMessage" class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-800/60 dark:bg-rose-950/30 dark:text-rose-300">
          {{ errorMessage }}
        </div>

        <section :class="['rounded-2xl border p-5 shadow-sm', heroToneClass]" data-testid="diagnostics-v2-root-cause">
          <div class="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
            <div class="max-w-3xl">
              <div class="flex flex-wrap items-center gap-2 text-xs font-medium uppercase tracking-wide">
                <span :class="lanePillClass">{{ laneLabel }}</span>
                <span>{{ scenarioLabel }}（{{ hero.scenario }}）</span>
              </div>
              <h3 class="mt-2 text-2xl font-semibold">{{ hero.title }}</h3>
              <p class="mt-2 text-sm leading-6">{{ hero.summary }}</p>
              <ul class="mt-4 grid gap-2 text-sm sm:grid-cols-2">
                <li v-for="bullet in hero.rootCauseBullets" :key="bullet" class="rounded-lg bg-white/65 px-3 py-2 dark:bg-slate-950/20">
                  {{ bullet }}
                </li>
              </ul>
            </div>
            <div class="w-full rounded-xl bg-white/70 p-4 text-sm shadow-sm dark:bg-slate-950/20 lg:w-72">
              <div class="font-semibold">安全边界</div>
              <p class="mt-2 text-xs leading-5">
                不显示 raw token、raw body、raw prompt、raw telemetry、raw CCH、邮箱、UUID 或代理凭据。所有后端文本进入 DOM 前均脱敏。
              </p>
            </div>
          </div>
        </section>

        <section class="mt-5 grid grid-cols-1 gap-4 lg:grid-cols-[5fr_4fr]">
          <article class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-900" data-testid="diagnostics-v2-allowed-actions">
            <div class="flex items-center justify-between gap-3">
              <div>
                <h3 class="text-sm font-semibold text-slate-900 dark:text-white">允许的修复动作</h3>
                <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">只展示真实已有 API 或纯前端引导；禁止动作不渲染为按钮。</p>
              </div>
              <span v-if="busyAction" class="text-xs text-slate-400">执行中：{{ busyAction }}</span>
            </div>

            <div v-if="showSetupTokenForm" class="mt-4 rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-500/40 dark:bg-amber-500/10">
              <label class="block text-sm font-medium text-amber-950 dark:text-amber-100">
                Setup Token session key
                <input
                  v-model="sessionKey"
                  data-testid="session-key-input"
                  type="password"
                  autocomplete="off"
                  class="input mt-2 w-full"
                  placeholder="只在本次请求中使用，不会显示原文"
                />
              </label>
            </div>

            <div v-if="showSwapProxyForm" class="mt-4 rounded-xl border border-sky-200 bg-sky-50 p-4 dark:border-sky-500/40 dark:bg-sky-500/10">
              <label class="block text-sm font-medium text-sky-950 dark:text-sky-100">
                新出口代理 ID
                <input
                  v-model="proxyId"
                  data-testid="swap-proxy-id-input"
                  type="number"
                  min="1"
                  class="input mt-2 w-full"
                  placeholder="输入要替换成的新代理 ID"
                />
              </label>
              <p class="mt-2 text-xs text-sky-800 dark:text-sky-100">
                当前代理 ID：{{ activeAccount?.proxy_id ?? '—' }}。必须填写不同的新代理；提交后会按顺序代理测试、运行时注册、定向健康检查。
              </p>
              <p v-if="proxyIdError" class="mt-2 text-xs text-rose-600 dark:text-rose-300">
                {{ proxyIdError }}
              </p>
            </div>

            <div class="mt-4 flex flex-wrap gap-2">
              <button
                v-for="item in renderedActions"
                :key="item.key"
                type="button"
                class="rounded-lg border px-3 py-2 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-60"
                :class="item.destructive ? 'border-rose-300 text-rose-700 hover:bg-rose-50 dark:border-rose-500/50 dark:text-rose-300 dark:hover:bg-rose-950/30' : 'border-slate-300 text-slate-700 hover:bg-slate-100 dark:border-dark-600 dark:text-slate-200 dark:hover:bg-dark-800'"
                :data-testid="`action-${item.key}`"
                :disabled="isBusy || isActionDisabled(item.key)"
                @click="handleAction(item.key)"
              >
                {{ item.label }}
              </button>
            </div>

            <div v-if="hero.scenario === 'oauth_invalid_grant'" id="oauth-reauth-guide" class="mt-4 rounded-xl border border-indigo-200 bg-indigo-50 p-4 text-sm text-indigo-900 dark:border-indigo-500/30 dark:bg-indigo-500/10 dark:text-indigo-100">
              <div class="font-semibold">OAuth 重新授权引导</div>
              <p class="mt-2">当前后端无一键 OAuth 重新授权 API。请从上号引导开新 session，使用相同代理/分组完成 OAuth，再回到此处刷新诊断。</p>
            </div>

            <div v-if="hero.scenario === 'manual_risk'" class="mt-4 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-900 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-100">
              <div class="font-semibold">人工介入区</div>
              <p class="mt-2">先登录上游网页确认 hold / KYC / 风控状态。系统不会自动修复，也不会默认触发健康检查。</p>
            </div>

            <p v-if="hero.scenario === 'proxy_mismatch'" class="mt-4 rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100">
              更换代理后再执行 runtime-register / healthcheck；代理修复前不显示直接 healthcheck 按钮。
            </p>
          </article>

          <article class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-900">
            <h3 class="text-sm font-semibold text-slate-900 dark:text-white">四个语义状态 lanes</h3>
            <div class="mt-4 grid grid-cols-2 gap-2 text-xs">
              <div v-for="lane in lanes" :key="lane.key" :class="['rounded-xl border p-3', lane.key === hero.lane ? lane.activeClass : 'border-slate-200 text-slate-500 dark:border-dark-700 dark:text-slate-400']">
                <div class="font-semibold">{{ lane.label }}</div>
                <div class="mt-1">{{ lane.copy }}</div>
              </div>
            </div>
          </article>
        </section>

        <section class="mt-5 rounded-xl border border-slate-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
          <button
            type="button"
            class="flex w-full items-center justify-between gap-3 px-5 py-4 text-left"
            data-testid="evidence-toggle"
            @click="evidenceOpen = !evidenceOpen"
          >
            <span>
              <span class="block text-sm font-semibold text-slate-900 dark:text-white">诊断证据</span>
              <span class="text-xs text-slate-500 dark:text-slate-400">默认折叠 · 查看脱敏证据 · lifecycle / gateway / proxy / upstream 分组</span>
            </span>
            <span class="text-sm text-slate-400">{{ evidenceOpen ? '收起' : '展开' }}</span>
          </button>

          <div v-if="evidenceOpen" class="border-t border-slate-200 px-5 py-4 dark:border-dark-700">
            <input
              v-model="evidenceQuery"
              data-testid="evidence-search"
              type="search"
              class="input w-full"
              placeholder="搜索脱敏证据，例如 proxy mismatch / status_429"
            />
            <div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
              <section v-for="group in filteredEvidenceGroups" :key="group.key" :data-testid="`evidence-group-${group.key}`" class="rounded-xl border border-slate-200 p-4 dark:border-dark-700">
                <h4 class="text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">{{ group.label }}</h4>
                <div class="mt-3 space-y-2">
                  <div v-for="item in group.items" :key="item.key" :data-testid="`evidence-item-${item.key}`" class="rounded-lg bg-slate-50 px-3 py-2 text-xs dark:bg-dark-800">
                    <span class="font-mono text-slate-500 dark:text-slate-400">{{ item.key }}</span>
                    <span class="mx-1 text-slate-300">·</span>
                    <span class="text-slate-600 dark:text-slate-300">{{ item.label }}</span>
                    <div class="mt-1 break-words font-medium text-slate-900 dark:text-slate-100">{{ item.value }}</div>
                  </div>
                  <p v-if="!group.items.length" class="text-xs text-slate-400">无匹配证据</p>
                </div>
              </section>
            </div>
          </div>
        </section>
      </main>
    </section>

    <ConfirmDialog
      :show="pendingHealthcheckConfirm"
      :z-index="160"
      title="执行定向健康检查"
      message="将通过当前代理与 CC Gateway 发起一次真实上游请求，可能消耗少量配额。确认继续？"
      confirm-text="确认执行"
      cancel-text="取消"
      :danger="true"
      data-testid="healthcheck-confirm-dialog"
      @confirm="confirmHealthcheck"
      @cancel="cancelHealthcheckConfirm"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import type { Account, FormalPoolOperationsDiagnostics } from '@/types'
import {
  FormalPoolOperationError,
  getDiagnostics,
  healthcheck,
  promoteProduction,
  quarantine,
  replaceSetupToken,
  runtimeRegister,
  startWarming,
  swapProxy,
  type FormalPoolOperationResult,
} from '@/api/admin/formalPoolOperations'
import {
  deriveFormalPoolDiagnosticsHero,
  formatFormalPoolDiagnosticCodeWithRaw,
  type FormalPoolDiagnosticsActionKey,
  type FormalPoolDiagnosticsHeroAction,
} from '@/utils/formalPoolDiagnosticsHero'
import { safeFormalPoolOperatorLabel, scrubFormalPoolDisplayText } from '@/utils/formalPoolStatusDashboard'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const props = defineProps<{
  show: boolean
  account: Account | null
}>()

const emit = defineEmits<{
  close: []
  updated: [account: Account]
}>()

const router = useRouter()
const appStore = useAppStore()

const diagnostics = ref<FormalPoolOperationsDiagnostics | null>(null)
const latestAccount = ref<Account | null>(null)
const loading = ref(false)
const busyAction = ref<string | null>(null)
const errorMessage = ref('')
const sessionKey = ref('')
const proxyId = ref('')
const evidenceOpen = ref(false)
const evidenceQuery = ref('')
const pendingHealthcheckConfirm = ref(false)

const activeAccount = computed(() => latestAccount.value ?? props.account)
const hero = computed(() => deriveFormalPoolDiagnosticsHero({ account: activeAccount.value, diagnostics: diagnostics.value }))
const isBusy = computed(() => loading.value || Boolean(busyAction.value))

function safeValue(value: unknown, fallback = '—'): string {
  return scrubFormalPoolDisplayText(String(value ?? ''), fallback)
}

const safeAccountName = computed(() => {
  const account = activeAccount.value
  return safeFormalPoolOperatorLabel(account?.name, '账号（未命名）')
})

const generatedAtText = computed(() => {
  const generatedAt = (diagnostics.value as (FormalPoolOperationsDiagnostics & { generated_at?: string }) | null)?.generated_at
  return safeValue(generatedAt, diagnostics.value ? '刚刚' : '等待刷新')
})

const scenarioLabel = computed(() => {
  switch (hero.value.scenario) {
    case 'oauth_invalid_grant': return 'OAuth 授权失效'
    case 'setup_token_expired': return 'Setup Token 过期'
    case 'rate_limited_5h': return '5 小时限流冷却'
    case 'manual_risk': return '上游风控需人工处理'
    case 'proxy_mismatch': return '代理出口不一致'
    case 'evidence_missing': return '运行证据不完整'
    case 'monitor': return '继续观测'
    default: return '未知诊断场景'
  }
})

const laneLabel = computed(() => {
  switch (hero.value.lane) {
    case 'active': return '可用 / 可调度'
    case 'paused': return '暂停 / 冷却中'
    case 'needs_intervention': return '需要介入'
    case 'inactive': return '未启用'
    default: return '需要介入'
  }
})

const heroToneClass = computed(() => {
  switch (hero.value.tone) {
    case 'emerald': return 'border-emerald-200 bg-gradient-to-br from-emerald-50 to-white text-emerald-950 dark:border-emerald-500/30 dark:from-emerald-950/40 dark:to-dark-900 dark:text-emerald-100'
    case 'amber': return 'border-amber-200 bg-gradient-to-br from-amber-50 to-white text-amber-950 dark:border-amber-500/30 dark:from-amber-950/40 dark:to-dark-900 dark:text-amber-100'
    case 'rose': return 'border-rose-200 bg-gradient-to-br from-rose-50 to-white text-rose-950 dark:border-rose-500/30 dark:from-rose-950/40 dark:to-dark-900 dark:text-rose-100'
    case 'sky': return 'border-sky-200 bg-gradient-to-br from-sky-50 to-white text-sky-950 dark:border-sky-500/30 dark:from-sky-950/40 dark:to-dark-900 dark:text-sky-100'
    default: return 'border-slate-200 bg-white text-slate-900 dark:border-dark-700 dark:bg-dark-900 dark:text-slate-100'
  }
})

const lanePillClass = computed(() => {
  switch (hero.value.lane) {
    case 'active': return 'rounded-full bg-emerald-100 px-2 py-0.5 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-200'
    case 'paused': return 'rounded-full bg-amber-100 px-2 py-0.5 text-amber-700 dark:bg-amber-500/10 dark:text-amber-200'
    case 'needs_intervention': return 'rounded-full bg-rose-100 px-2 py-0.5 text-rose-700 dark:bg-rose-500/10 dark:text-rose-200'
    default: return 'rounded-full bg-slate-100 px-2 py-0.5 text-slate-600 dark:bg-dark-800 dark:text-slate-300'
  }
})

const lanes = [
  { key: 'active', label: '可用 / 可调度', copy: '证据完整，可调度', activeClass: 'border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-500/40 dark:bg-emerald-500/10 dark:text-emerald-200' },
  { key: 'paused', label: '暂停 / 冷却中', copy: '限流或冷却，默认等待', activeClass: 'border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-200' },
  { key: 'needs_intervention', label: '需要介入', copy: '需要安全修复或人工处理', activeClass: 'border-rose-300 bg-rose-50 text-rose-800 dark:border-rose-500/40 dark:bg-rose-500/10 dark:text-rose-200' },
  { key: 'inactive', label: '未启用', copy: '不参与调度', activeClass: 'border-slate-300 bg-slate-100 text-slate-700 dark:border-dark-600 dark:bg-dark-800 dark:text-slate-200' },
]

const showSetupTokenForm = computed(() => hero.value.primaryAction?.key === 'replaceSetupToken' || hero.value.secondaryActions.some((item) => item.key === 'replaceSetupToken'))
const showSwapProxyForm = computed(() => hero.value.primaryAction?.key === 'swapProxy' || hero.value.secondaryActions.some((item) => item.key === 'swapProxy'))

const renderedActions = computed<FormalPoolDiagnosticsHeroAction[]>(() => {
  const out: FormalPoolDiagnosticsHeroAction[] = []
  if (hero.value.primaryAction && hero.value.primaryAction.behavior !== 'none') out.push(hero.value.primaryAction)
  for (const item of hero.value.secondaryActions) {
    if (item.behavior !== 'none') out.push(item)
  }
  const seen = new Set<string>()
  return out.filter((item) => {
    if (seen.has(item.key)) return false
    seen.add(item.key)
    return true
  })
})

const forbiddenKeys = computed(() => new Set(hero.value.forbiddenActions.map((item) => item.key)))

function isActionDisabled(key: FormalPoolDiagnosticsActionKey): boolean {
  if (forbiddenKeys.value.has(key)) return true
  if (key === 'replaceSetupToken') return !sessionKey.value.trim()
  if (key === 'swapProxy') return Boolean(proxyIdError.value)
  return false
}

const proxyIdError = computed(() => {
  if (!showSwapProxyForm.value) return ''
  const id = Number(proxyId.value)
  if (!Number.isInteger(id) || id <= 0) return '请填写新的出口代理 ID。'
  if (activeAccount.value?.proxy_id && id === activeAccount.value.proxy_id) return '新代理 ID 不能与当前代理相同。'
  return ''
})

function boolText(value: unknown): string {
  if (value === true) return '是'
  if (value === false) return '否'
  return '—'
}

function diagnosticValue(value: unknown, kind: 'origin' | 'classification' | 'status' | 'check' | 'action' = 'classification'): string {
  return formatFormalPoolDiagnosticCodeWithRaw(value, kind, '—')
}

function diagnosticProse(value: unknown): string {
  return scrubFormalPoolDisplayText(String(value ?? ''), '—')
}

const evidenceGroups = computed(() => {
  const d = diagnostics.value
  return [
    {
      key: 'lifecycle',
      label: '生命周期',
      items: [
        { key: 'onboarding_stage', label: '上号阶段', value: safeValue(d?.onboarding_stage) },
        { key: 'schedulable', label: '可调度', value: boolText(d?.schedulable) },
        { key: 'effective_schedulable', label: '实际可调度', value: boolText(d?.effective_schedulable) },
        { key: 'quarantine_reason', label: '隔离原因', value: diagnosticValue(d?.quarantine_reason) },
      ],
    },
    {
      key: 'gateway',
      label: '网关证据',
      items: [
        { key: 'cc_gateway_seen', label: '看到 CC Gateway 证据', value: boolText(d?.cc_gateway_seen) },
        { key: 'cc_gateway_runtime_registered', label: '运行时已注册', value: boolText(d?.cc_gateway_runtime_registered) },
        { key: 'runtime_evidence_complete', label: '运行证据完整', value: boolText(d?.runtime_evidence_complete) },
        { key: 'raw_capture_present', label: 'Raw capture 证据存在', value: boolText(d?.raw_capture_present) },
        { key: 'raw_capture_ref', label: 'Raw capture 引用', value: safeValue(d?.raw_capture_ref) },
      ],
    },
    {
      key: 'proxy',
      label: '代理出口',
      items: [
        { key: 'proxy_mismatch', label: '代理出口不一致', value: boolText(d?.proxy_mismatch) },
        { key: 'fallback_detected', label: '发现 fallback', value: boolText(d?.fallback_detected) },
      ],
    },
    {
      key: 'upstream',
      label: '上游失败',
      items: [
        { key: 'failure_origin', label: '失败来源', value: diagnosticValue(d?.failure_origin, 'origin') },
        { key: 'failure_code', label: '失败分类', value: diagnosticValue(d?.failure_code) },
        { key: 'failure_source', label: '失败触发环节', value: diagnosticValue(d?.failure_source) },
        { key: 'status_code_bucket', label: '状态分组', value: diagnosticValue(d?.status_code_bucket, 'status') },
        { key: 'risk_event_ref', label: '风控事件引用', value: safeValue(d?.risk_event_ref) },
      ],
    },
    {
      key: 'checks',
      label: '检查项',
      items: (d?.checks ?? []).map((check, index) => ({
        key: `check_${index}_${safeValue(check.name, 'unknown')}`,
        label: `${diagnosticValue(check.name, 'check')} / ${check.status === 'pass' ? '通过' : check.status === 'warn' ? '警告' : '失败'}`,
        value: check.message ? diagnosticProse(check.message) : '—',
      })),
    },
    {
      key: 'actions',
      label: '建议动作',
      items: (d?.recommended_actions ?? []).map((item, index) => ({
        key: `action_${index}_${safeValue(item.key, 'unknown')}`,
        label: diagnosticValue(item.key, 'action'),
        value: item.label ? diagnosticProse(item.label) : '—',
      })),
    },
  ]
})

const filteredEvidenceGroups = computed(() => {
  const query = evidenceQuery.value.trim().toLowerCase()
  if (!query) return evidenceGroups.value
  return evidenceGroups.value.map((group) => ({
    ...group,
    items: group.items.filter((item) => `${item.key} ${item.key.replace(/_/g, ' ')} ${item.label} ${item.value}`.toLowerCase().includes(query)),
  }))
})

function setError(error: unknown): void {
  const message = error instanceof Error ? error.message : (error as { message?: string })?.message
  errorMessage.value = safeValue(message, '操作失败')
}

async function refreshDiagnostics(options: { keepError?: boolean } = {}): Promise<void> {
  const account = activeAccount.value
  if (!account) return
  loading.value = true
  if (!options.keepError) errorMessage.value = ''
  try {
    diagnostics.value = await getDiagnostics(account.id)
  } catch (error) {
    setError(error)
  } finally {
    loading.value = false
  }
}

async function applyOperationResult(result: FormalPoolOperationResult): Promise<void> {
  const mergedAccount = activeAccount.value ? { ...activeAccount.value, ...result.account } : result.account as Account
  latestAccount.value = mergedAccount
  emit('updated', mergedAccount)
  diagnostics.value = result.diagnostics ?? await getDiagnostics(mergedAccount.id)
  sessionKey.value = ''
}

async function handleOperationError(error: unknown): Promise<void> {
  if (error instanceof FormalPoolOperationError) {
    if (error.account && activeAccount.value) latestAccount.value = { ...activeAccount.value, ...error.account }
    if (error.diagnostics) diagnostics.value = error.diagnostics
  }
  setError(error)
  if (!(error instanceof FormalPoolOperationError) || !error.diagnostics) await refreshDiagnostics({ keepError: true })
}

async function runWithBusy(name: string, operation: () => Promise<FormalPoolOperationResult>): Promise<void> {
  if (isBusy.value) return
  busyAction.value = name
  errorMessage.value = ''
  try {
    const result = await operation()
    await applyOperationResult(result)
    appStore.showSuccess?.('Formal Pool 操作已完成')
  } catch (error) {
    await handleOperationError(error)
  } finally {
    busyAction.value = null
  }
}

function parsedProxyId(): number {
  const id = Number(proxyId.value)
  return Number.isInteger(id) && id > 0 ? id : 0
}

async function handleAction(key: FormalPoolDiagnosticsActionKey): Promise<void> {
  const account = activeAccount.value
  if (!account || forbiddenKeys.value.has(key)) return
  if (key === 'guideOAuthReauth' || key === 'replaceAccountAndProxy') {
    await router.push({ path: '/admin/claude-onboarding', query: { source: 'diagnostics-v2' } }).catch(() => undefined)
    return
  }
  if (key === 'manualReview') {
    document.querySelector('[data-testid=\"diagnostics-v2-allowed-actions\"]')?.scrollIntoView({ block: 'start' })
    return
  }
  if (key === 'refreshDiagnostics') return refreshDiagnostics()
  if (key === 'replaceSetupToken') {
    const raw = sessionKey.value.trim()
    if (!raw) return
    return runWithBusy(key, () => replaceSetupToken(account.id, { session_key: raw, run_runtime_register: true, run_healthcheck: true }))
  }
  if (key === 'runtimeRegister') return runWithBusy(key, () => runtimeRegister(account.id))
  if (key === 'healthcheck') {
    // Native browser confirm inside a custom modal is jarring and inaccessible;
    // route through the project's ConfirmDialog instead. The actual healthcheck
    // API call is gated behind the dialog's @confirm handler below.
    pendingHealthcheckConfirm.value = true
    return
  }
  if (key === 'startWarming') return runWithBusy(key, () => startWarming(account.id))
  if (key === 'promoteProduction') return runWithBusy(key, () => promoteProduction(account.id))
  if (key === 'swapProxy') {
    if (proxyIdError.value) return
    const id = parsedProxyId()
    if (!id) return
    return runWithBusy(key, () => swapProxy(account.id, { proxy_id: id, run_proxy_test: true, run_runtime_register: true, run_healthcheck: true }))
  }
  if (key === 'quarantine') return runWithBusy(key, () => quarantine(account.id, `manual-risk:${hero.value.scenario}`))
}

async function confirmHealthcheck(): Promise<void> {
  const account = activeAccount.value
  if (!account) {
    pendingHealthcheckConfirm.value = false
    return
  }
  pendingHealthcheckConfirm.value = false
  await runWithBusy('healthcheck', () => healthcheck(account.id))
}

function cancelHealthcheckConfirm(): void {
  pendingHealthcheckConfirm.value = false
}

function handleClose(): void {
  pendingHealthcheckConfirm.value = false
  emit('close')
}

watch(
  () => [props.show, props.account?.id] as const,
  ([visible]) => {
    if (visible && props.account) {
      latestAccount.value = props.account
      diagnostics.value = null
      errorMessage.value = ''
      sessionKey.value = ''
      proxyId.value = ''
      evidenceOpen.value = false
      evidenceQuery.value = ''
      pendingHealthcheckConfirm.value = false
      refreshDiagnostics()
      return
    }
    diagnostics.value = null
    latestAccount.value = null
    errorMessage.value = ''
    sessionKey.value = ''
    proxyId.value = ''
    pendingHealthcheckConfirm.value = false
  },
  { immediate: true },
)
</script>
