<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.formalPoolDiagnostics.title')"
    width="extra-wide"
    @close="handleClose"
  >
    <div v-if="account" class="space-y-5" data-test="formal-pool-diagnostics-modal">
      <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-700">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.formalPoolDiagnostics.account') }} #{{ account.id }}
            </p>
            <h4 class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
              {{ safeAccountName }}
            </h4>
          </div>
          <div class="flex flex-wrap gap-2">
            <span :class="['inline-flex rounded px-2 py-1 text-xs font-medium', stageBadgeClass]">
              {{ stageLabel }}
            </span>
            <span :class="['inline-flex rounded px-2 py-1 text-xs font-medium', failureBadgeClass]">
              {{ failureOriginLabel }}
            </span>
          </div>
        </div>
      </div>

      <div class="rounded-lg border border-blue-200 bg-blue-50 p-3 text-sm text-blue-800 dark:border-blue-500/30 dark:bg-blue-500/10 dark:text-blue-200">
        {{ t(canRepairSetupToken ? 'admin.accounts.formalPoolDiagnostics.noRawTokenWarningSetupToken' : 'admin.accounts.formalPoolDiagnostics.noRawTokenWarning') }}
      </div>

      <section v-if="!loading" class="space-y-2" data-test="formal-pool-stepper">
        <h5 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.accounts.formalPoolDiagnostics.lifecycle') }}
        </h5>
        <ol class="grid grid-cols-1 gap-2 text-xs sm:grid-cols-2 lg:grid-cols-3">
          <li v-for="step in lifecycleSteps" :key="step" :class="['rounded border px-3 py-2', lifecycleStepClass(step)]">
            {{ t(`admin.accounts.formalPool.stage.${step}`, step) }}
          </li>
        </ol>
      </section>

      <div v-if="failureOriginDescription" class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200" data-test="failure-origin-guidance">
        {{ failureOriginDescription }}
      </div>

      <div v-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300" data-test="operation-error">
        {{ errorMessage }}
      </div>

      <div v-if="loading" class="flex items-center justify-center py-8 text-sm text-gray-500 dark:text-gray-400">
        {{ t('common.loading') }}
      </div>

      <template v-else>
        <section class="space-y-3">
          <div class="flex items-center justify-between gap-3">
            <h5 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.accounts.formalPoolDiagnostics.evidence') }}
            </h5>
            <button type="button" class="btn btn-secondary px-3 py-1.5 text-xs" :disabled="isBusy" @click="() => refreshDiagnostics()">
              {{ t('admin.accounts.formalPoolDiagnostics.actions.refresh') }}
            </button>
          </div>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <div v-for="item in evidenceItems" :key="item.key" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ item.label }}</p>
              <p class="mt-1 break-words text-sm font-medium text-gray-900 dark:text-gray-100">
                {{ item.value }}
              </p>
            </div>
          </div>
        </section>

        <section class="space-y-3" data-test="healthcheck-safety-classification">
          <h5 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.accounts.formalPoolDiagnostics.healthcheckSafety.title') }}
          </h5>
          <div class="grid grid-cols-1 gap-2 lg:grid-cols-2">
            <div
              v-for="notice in healthcheckSafetyNotices"
              :key="notice.key"
              :class="['rounded-lg border p-3 text-sm', healthcheckSafetyClass(notice.severity)]"
            >
              <p class="font-semibold">{{ t(`admin.accounts.formalPoolDiagnostics.healthcheckSafety.${notice.key}.title`) }}</p>
              <p class="mt-1">{{ t(`admin.accounts.formalPoolDiagnostics.healthcheckSafety.${notice.key}.advice`) }}</p>
            </div>
          </div>
        </section>


        <section v-if="manualRiskGuidanceItems.length" class="space-y-3 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200" data-test="manual-risk-guidance">
          <h5 class="font-semibold">
            {{ t('admin.accounts.formalPoolDiagnostics.manualRisk.title') }}
          </h5>
          <p>{{ t('admin.accounts.formalPoolDiagnostics.manualRisk.summary') }}</p>
          <ul class="list-disc space-y-1 pl-5">
            <li v-for="item in manualRiskGuidanceItems" :key="item">
              {{ t(`admin.accounts.formalPoolDiagnostics.manualRisk.items.${item}`) }}
            </li>
          </ul>
          <p class="font-medium">{{ t('admin.accounts.formalPoolDiagnostics.manualRisk.nextSteps') }}</p>
        </section>

        <section class="space-y-3">
          <h5 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.accounts.formalPoolDiagnostics.checks') }}
          </h5>
          <div v-if="currentDiagnostics?.checks?.length" class="space-y-2">
            <div v-for="check in currentDiagnostics.checks" :key="`${check.name}-${check.message || ''}`" class="flex gap-2 rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-600">
              <span :class="['mt-0.5 h-2.5 w-2.5 shrink-0 rounded-full', checkStatusClass(check.status)]"></span>
              <div>
                <p class="font-medium text-gray-900 dark:text-gray-100">{{ checkNameLabel(check.name) }}</p>
                <p v-if="check.message" class="mt-1 text-gray-600 dark:text-gray-300">{{ checkMessageLabel(check.message) }}</p>
              </div>
            </div>
          </div>
          <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.formalPoolDiagnostics.noChecks') }}</p>
        </section>

        <section class="space-y-3">
          <h5 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.accounts.formalPoolDiagnostics.recommendedActions') }}
          </h5>
          <div v-if="recommendedActions.length" class="flex flex-wrap gap-2">
            <span v-for="action in recommendedActions" :key="action.key" :class="['rounded px-2 py-1 text-xs font-medium', actionClass(action.severity)]">
              {{ actionLabel(action) }}
            </span>
          </div>
          <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.formalPoolDiagnostics.noActions') }}</p>
          <p v-if="shouldShowReplacementGuidance" class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
            {{ t('admin.accounts.formalPoolDiagnostics.replacementGuidance') }}
          </p>
        </section>

        <section class="space-y-3">
          <h5 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.accounts.formalPoolDiagnostics.manualActions') }}
          </h5>

          <div v-if="canRepairSetupToken" class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
            <div class="grid grid-cols-1 gap-3 lg:grid-cols-[1fr_auto] lg:items-end">
              <label class="block">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.accounts.formalPoolDiagnostics.sessionKeyLabel') }}
                </span>
                <input
                  v-model="sessionKey"
                  data-test="session-key-input"
                  type="password"
                  autocomplete="off"
                  class="input mt-1 w-full"
                  :placeholder="t('admin.accounts.formalPoolDiagnostics.sessionKeyPlaceholder')"
                />
              </label>
              <button
                type="button"
                data-test="repair-token-button"
                class="btn btn-primary"
                :disabled="isBusy || !sessionKey.trim()"
                @click="() => handleReplaceSetupToken()"
              >
                {{ busyAction === 'replace-token' ? t('common.loading') : t('admin.accounts.formalPoolDiagnostics.actions.repairToken') }}
              </button>
            </div>
            <div class="mt-3 flex flex-wrap gap-4 text-sm text-gray-600 dark:text-gray-300">
              <label class="inline-flex items-center gap-2">
                <input v-model="runRuntimeRegisterAfterTokenRepair" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
                {{ t('admin.accounts.formalPoolDiagnostics.runRuntimeAfterRepair') }}
              </label>
              <label class="inline-flex items-center gap-2">
                <input v-model="runHealthcheckAfterTokenRepair" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
                {{ t('admin.accounts.formalPoolDiagnostics.runHealthcheckAfterRepair') }}
              </label>
            </div>
            <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.formalPoolDiagnostics.setupTokenSafetyCopy') }}
            </p>
          </div>

          <div v-else-if="canShowOAuthRecoveryGuidance" class="rounded-lg border border-indigo-200 bg-indigo-50 p-4 text-sm text-indigo-900 dark:border-indigo-500/30 dark:bg-indigo-500/10 dark:text-indigo-100" data-test="oauth-reauthorize-guidance">
            <p class="font-semibold">{{ t('admin.accounts.formalPoolDiagnostics.oauthRecovery.title') }}</p>
            <p class="mt-2">{{ t('admin.accounts.formalPoolDiagnostics.oauthRecovery.body') }}</p>
            <ol class="mt-3 list-decimal space-y-1 pl-5">
              <li>{{ t('admin.accounts.formalPoolDiagnostics.oauthRecovery.stepRefresh') }}</li>
              <li>{{ t('admin.accounts.formalPoolDiagnostics.oauthRecovery.stepRuntime') }}</li>
              <li>{{ t('admin.accounts.formalPoolDiagnostics.oauthRecovery.stepHealthcheck') }}</li>
              <li>{{ t('admin.accounts.formalPoolDiagnostics.oauthRecovery.stepWarming') }}</li>
            </ol>
          </div>

          <p
            v-if="canDirectedHealthcheck"
            :class="[
              'rounded-lg border p-3 text-sm',
              hasHealthcheckHighRisk
                ? 'border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-100'
                : 'border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-500/30 dark:bg-blue-500/10 dark:text-blue-200',
            ]"
            data-test="directed-healthcheck-warning"
          >
            {{ t(hasHealthcheckHighRisk ? 'admin.accounts.formalPoolDiagnostics.healthcheckHighRiskWarning' : 'admin.accounts.formalPoolDiagnostics.directedHealthcheckWarning') }}
          </p>

          <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
            <button v-if="canRuntimeRegister" type="button" class="btn btn-secondary" :disabled="isBusy" @click="runAccountAction('runtime-register')">
              {{ t('admin.accounts.formalPoolDiagnostics.actions.runtimeRegister') }}
            </button>
            <button v-if="canDirectedHealthcheck" type="button" data-test="directed-healthcheck-button" class="btn btn-secondary" :disabled="isBusy" @click="runAccountAction('healthcheck')">
              {{ t('admin.accounts.formalPoolDiagnostics.actions.healthcheck') }}
            </button>
            <button
              v-if="shouldShowStartWarming"
              type="button"
              data-test="start-warming-button"
              class="btn btn-secondary"
              :disabled="isBusy || !canStartWarming"
              :title="startWarmingTitle"
              @click="runAccountAction('start-warming')"
            >
              {{ t('admin.accounts.formalPoolDiagnostics.actions.startWarming') }}
            </button>
            <button
              v-if="canPromoteProduction"
              type="button"
              data-test="promote-production-button"
              class="btn btn-secondary"
              :disabled="isBusy"
              @click="runAccountAction('promote-production')"
            >
              {{ t('admin.accounts.formalPoolDiagnostics.actions.promoteProduction') }}
            </button>
            <button type="button" class="btn btn-secondary" :disabled="isBusy" @click="() => refreshDiagnostics()">
              {{ t('admin.accounts.formalPoolDiagnostics.actions.refresh') }}
            </button>
          </div>

          <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
            <div class="grid grid-cols-1 gap-3 lg:grid-cols-[1fr_auto] lg:items-end">
              <label class="block">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.accounts.formalPoolDiagnostics.proxyIdLabel') }}
                </span>
                <input
                  v-model="swapProxyId"
                  data-test="proxy-id-input"
                  type="number"
                  min="1"
                  class="input mt-1 w-full"
                  :placeholder="t('admin.accounts.formalPoolDiagnostics.proxyIdPlaceholder')"
                />
              </label>
              <button type="button" class="btn btn-secondary" :disabled="isBusy || !parsedSwapProxyId" @click="() => handleSwapProxy()">
                {{ t('admin.accounts.formalPoolDiagnostics.actions.proxySwap') }}
              </button>
            </div>
            <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.formalPoolDiagnostics.proxySwapSafetyCopy') }}
            </p>
          </div>
        </section>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { Account, FormalPoolOperationsDiagnostics, FormalPoolRecommendedAction } from '@/types'
import {
  FormalPoolOperationError,
  getDiagnostics,
  healthcheck,
  promoteProduction,
  replaceSetupToken,
  runtimeRegister,
  startWarming,
  swapProxy,
  type FormalPoolOperationResult,
} from '@/api/admin/formalPoolOperations'

const props = defineProps<{
  show: boolean
  account: Account | null
}>()

const emit = defineEmits<{
  close: []
  updated: [account: Account]
}>()

const { t } = useI18n()
const appStore = useAppStore()

const diagnostics = ref<FormalPoolOperationsDiagnostics | null>(null)
const latestAccount = ref<Account | null>(null)
const loading = ref(false)
const busyAction = ref<string | null>(null)
const errorMessage = ref('')
const sessionKey = ref('')
const runRuntimeRegisterAfterTokenRepair = ref(true)
const runHealthcheckAfterTokenRepair = ref(true)
const swapProxyId = ref('')

function scrubFormalPoolSecretText(input: unknown): string {
  const emailPattern = /\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b/gi
  const uuidPattern = /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi
  const secretKeyPattern = String.raw`(?:session[-_\s]?key|access[-_\s]?token|refresh[-_\s]?token|authorization|password|passwd|proxy[-_\s]?password|proxy[-_\s]?credentials?|api[-_\s]?key|apikey|cookie|raw[-_\s]?body|raw[-_\s]?prompt|raw[-_\s]?telemetry|raw[-_\s]?cch|raw(?:[-_\s]?cookie)?)`
  const proxyUrlSecretKeyPattern = String.raw`(?:proxy[-_\s]?url|proxy)`
  const quotedProxyUrlCredentialsPattern = new RegExp(String.raw`(["']?)\b(${proxyUrlSecretKeyPattern})\b\1(\s*[:=]\s*)(["'])[A-Za-z][A-Za-z0-9+.-]*:\/\/[^/?#\s"'\`,;)}@]+@(?:(?!\4).)*\4`, 'gi')
  const proxyUrlCredentialsPattern = new RegExp(String.raw`(["']?)\b(${proxyUrlSecretKeyPattern})\b\1(\s*[:=]\s*)[A-Za-z][A-Za-z0-9+.-]*:\/\/[^/?#\s"'\`,;)}@]+@[^\s"'\`,;)}]+`, 'gi')
  const quotedValuePattern = new RegExp(String.raw`(["']?)\b(${secretKeyPattern})\b\1(\s*[:=]\s*)(["'])(?:(?!\4).)*\4`, 'gi')
  const tokenValuePattern = new RegExp(String.raw`(["']?)\b(${secretKeyPattern})\b\1(\s*[:=]\s*)(?:Bearer\s+)?[^\s"'\`,;)}]+`, 'gi')

  return String(input ?? '')
    .replace(emailPattern, '[redacted]')
    .replace(uuidPattern, '[redacted]')
    .replace(/sk-ant-sid[^\s"'`,;)]*/gi, '[redacted]')
    .replace(/\bBearer\s+[A-Za-z0-9._~+/=-]+/gi, 'Bearer [redacted]')
    .replace(quotedProxyUrlCredentialsPattern, '$1$2$1$3$4[redacted]$4')
    .replace(proxyUrlCredentialsPattern, '$1$2$1$3[redacted]')
    .replace(quotedValuePattern, '$1$2$1$3$4[redacted]$4')
    .replace(tokenValuePattern, '$1$2$1$3[redacted]')
}

const safeDisplayText = (input: unknown) => scrubFormalPoolSecretText(input)
const translatedOrEmpty = (key: string) => {
  const translated = t(key, '')
  return translated && translated !== key ? translated : ''
}


const accountNameUnsafePattern = /(?:sk-|token|access_token|refresh|session_key|setup|bearer|https?:\/\/|:\/\/|raw|prompt|body|cch|telemetry|proxy|password|credential|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[a-z0-9_-]{32,})/i
const accountNameUserinfoPattern = /:\S+@/

function safeOperationalAccountName(account: Account | null | undefined): string {
  const fallback = account?.id ? `账号 #${account.id}` : '账号'
  const name = String(account?.name ?? '').trim()
  if (!name) return fallback
  if (accountNameUnsafePattern.test(name) || accountNameUserinfoPattern.test(name)) return fallback
  return name
}

const activeAccount = computed(() => latestAccount.value ?? props.account)
const currentDiagnostics = computed(() => diagnostics.value)
const safeAccountName = computed(() => safeOperationalAccountName(activeAccount.value))
const isBusy = computed(() => Boolean(busyAction.value) || loading.value)

const legacyActionAliases: Record<string, string> = {
  repair_token: 'replace_setup_token',
  repair_oauth: 'reauthorize_oauth',
}

const normalizeActionKey = (key: string) => legacyActionAliases[key] ?? key

const recommendedActions = computed<FormalPoolRecommendedAction[]>(() =>
  (currentDiagnostics.value?.recommended_actions ?? []).map(action => {
    const account = activeAccount.value
    const key = account?.type === 'oauth' && (action.key === 'repair_token' || action.key === 'replace_setup_token') ? 'reauthorize_oauth' : normalizeActionKey(action.key)
    return { ...action, key }
  })
)
const recommendedKeys = computed(() => new Set(recommendedActions.value.map(action => action.key)))

const canRepairSetupToken = computed(() => {
  const account = activeAccount.value
  return account?.platform === 'anthropic' && account.type === 'setup-token' && account.is_formal_pool === true && recommendedKeys.value.has('replace_setup_token')
})

const canShowOAuthRecoveryGuidance = computed(() => {
  const account = activeAccount.value
  return account?.platform === 'anthropic' && account.type === 'oauth' && account.is_formal_pool === true && recommendedKeys.value.has('reauthorize_oauth')
})


const normalizeI18nKey = (value: unknown) => String(value ?? '')
  .trim()
  .toLowerCase()
  .replace(/[^a-z0-9]+/g, '_')
  .replace(/^_+|_+$/g, '')

const checkNameLabel = (name: unknown) => {
  const raw = String(name ?? '').trim()
  if (!raw) return '-'
  const key = `admin.accounts.formalPoolDiagnostics.checkNames.${raw}`
  const translated = translatedOrEmpty(key)
  return translated || safeDisplayText(raw)
}

const checkMessageLabel = (message: unknown) => {
  const raw = String(message ?? '').trim()
  if (!raw) return ''
  const key = normalizeI18nKey(raw)
  const translated = key ? translatedOrEmpty(`admin.accounts.formalPoolDiagnostics.checkMessages.${key}`) : ''
  return translated || safeDisplayText(raw)
}

const failureOriginDescription = computed(() => {
  if (manualRiskGuidanceItems.value.length > 0) return ''
  const origin = currentDiagnostics.value?.failure_origin || 'unknown'
  return translatedOrEmpty(`admin.accounts.formalPoolDiagnostics.failureOriginDescriptions.${origin}`)
})

const evidenceComplete = computed(() => {
  const d = currentDiagnostics.value
  return Boolean(
    d?.onboarding_stage === 'healthcheck_passed' &&
    d.healthcheck_evidence_persisted &&
    d.status_code_bucket === 'status_2xx' &&
    d.cc_gateway_runtime_registered === true &&
    d.runtime_evidence_complete !== false &&
    Boolean(d.cc_gateway_runtime_registered_at) &&
    d.cc_gateway_seen &&
    d.raw_capture_present &&
    !d.fallback_detected &&
    !d.proxy_mismatch &&
    !d.risk_text_detected
  )
})
const isMonitorOnly = computed(() => recommendedKeys.value.has('monitor'))
const canStartWarming = computed(() => recommendedKeys.value.has('start_warming') && evidenceComplete.value)
const shouldShowStartWarming = computed(() => recommendedKeys.value.has('start_warming') || currentDiagnostics.value?.onboarding_stage === 'healthcheck_passed')
const canPromoteProduction = computed(() => recommendedKeys.value.has('promote_production'))
const canRuntimeRegister = computed(() => !isMonitorOnly.value)
const canDirectedHealthcheck = computed(() => !isMonitorOnly.value)
const lifecycleSteps = ['imported', 'refreshed', 'runtime_registered', 'healthcheck_passed', 'warming', 'production'] as const
const startWarmingTitle = computed(() => canStartWarming.value
  ? t('admin.accounts.formalPoolDiagnostics.startWarmingAllowed')
  : currentDiagnostics.value?.cc_gateway_runtime_registered !== true || currentDiagnostics.value?.runtime_evidence_complete === false || !currentDiagnostics.value?.cc_gateway_runtime_registered_at
    ? t('admin.accounts.formalPoolDiagnostics.startWarmingBlockedRuntime')
    : t('admin.accounts.formalPoolDiagnostics.startWarmingBlocked'))

const shouldShowReplacementGuidance = computed(() =>
  recommendedKeys.value.has('replace_account_and_proxy') || (currentDiagnostics.value?.failure_origin === 'token_exchange' && !recommendedKeys.value.has('monitor'))
)

const stageLabel = computed(() => {
  const stage = currentDiagnostics.value?.onboarding_stage || activeAccount.value?.onboarding_stage || 'legacy_unknown'
  return t(`admin.accounts.formalPool.stage.${stage}`, String(stage))
})
const failureOriginLabel = computed(() => {
  const origin = currentDiagnostics.value?.failure_origin || 'unknown'
  return t(`admin.accounts.formalPoolDiagnostics.failureOrigins.${origin}`, origin)
})

const stageBadgeClass = computed(() => {
  switch (currentDiagnostics.value?.onboarding_stage || activeAccount.value?.onboarding_stage) {
    case 'production': return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'warming': return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'
    case 'quarantined': return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
    case 'healthcheck_passed': return 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/40 dark:text-cyan-300'
    case 'runtime_registered': return 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300'
    case 'refreshed': return 'bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300'
    default: return 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'
  }
})
const lifecycleStepClass = (step: string) => {
  const stage = currentDiagnostics.value?.onboarding_stage || activeAccount.value?.onboarding_stage
  if (stage === 'quarantined') return 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200'
  if (stage === step) return 'border-primary-300 bg-primary-50 font-semibold text-primary-700 dark:border-primary-500/40 dark:bg-primary-500/10 dark:text-primary-200'
  return 'border-gray-200 text-gray-600 dark:border-dark-600 dark:text-gray-300'
}

const failureBadgeClass = computed(() => {
  switch (currentDiagnostics.value?.failure_origin) {
    case 'upstream':
    case 'token_exchange':
      return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
    case 'proxy': return 'bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300'
    case 'cc_gateway_control_plane': return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    case 'local_gate': return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'
    default: return 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'
  }
})

const formatBoolean = (value: unknown) => {
  if (value === true || value === 'true') return t('common.yes')
  if (value === false || value === 'false') return t('common.no')
  return '-'
}
const valueOrDash = (value: unknown) => {
  const text = String(value ?? '').trim()
  return text ? safeDisplayText(text) : '-'
}

const localizedBucketOrCode = (namespace: 'statusBuckets' | 'failureCodes' | 'failureSources' | 'healthcheckStatus', value: unknown) => {
  const raw = String(value ?? '').trim()
  if (!raw) return '-'
  const normalized = normalizeI18nKey(raw)
  const translated = normalized ? translatedOrEmpty(`admin.accounts.formalPoolDiagnostics.${namespace}.${normalized}`) : ''
  return translated || safeDisplayText(raw)
}

const diagnosticText = (value: unknown) => normalizeI18nKey(value)

const evidenceItems = computed(() => {
  const d = currentDiagnostics.value
  return [
    { key: 'healthcheck_status', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.healthcheckStatus'), value: localizedBucketOrCode('healthcheckStatus', d?.healthcheck_status) },
    { key: 'failure_code', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.failureCode'), value: localizedBucketOrCode('failureCodes', d?.failure_code) },
    { key: 'failure_source', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.failureSource'), value: localizedBucketOrCode('failureSources', d?.failure_source) },
    { key: 'last_cc_gateway_error_code', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.lastCCGatewayErrorCode'), value: localizedBucketOrCode('failureCodes', d?.last_cc_gateway_error_code) },
    { key: 'onboarding_last_error_code', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.onboardingLastErrorCode'), value: localizedBucketOrCode('failureCodes', d?.onboarding_last_error_code) },
    { key: 'onboarding_last_error_bucket', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.onboardingLastErrorBucket'), value: localizedBucketOrCode('statusBuckets', d?.onboarding_last_error_bucket) },
    { key: 'quarantine_reason', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.quarantineReason'), value: localizedBucketOrCode('failureCodes', d?.quarantine_reason) },
    { key: 'healthcheck_safe_error_code', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.safeErrorCode'), value: localizedBucketOrCode('failureCodes', d?.healthcheck_safe_error_code) },
    { key: 'healthcheck_safe_error_bucket', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.safeErrorBucket'), value: localizedBucketOrCode('failureCodes', d?.healthcheck_safe_error_bucket) },
    { key: 'rate_limit_error_class', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.rateLimitErrorClass'), value: localizedBucketOrCode('failureCodes', d?.formal_pool_rate_limit_error_class) },
    { key: 'rate_limit_window', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.rateLimitWindow'), value: localizedBucketOrCode('failureCodes', d?.formal_pool_rate_limit_window) },
    { key: 'rate_limit_action', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.rateLimitAction'), value: localizedBucketOrCode('failureCodes', d?.formal_pool_rate_limit_action) },
    { key: 'rate_limit_reset_bucket', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.rateLimitResetBucket'), value: localizedBucketOrCode('failureCodes', d?.formal_pool_rate_limit_reset_bucket) },
    { key: 'cc_gateway_seen', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.ccGatewaySeen'), value: formatBoolean(d?.cc_gateway_seen) },
    { key: 'cc_gateway_runtime_registered', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.runtimeRegistered'), value: formatBoolean(d?.cc_gateway_runtime_registered) },
    { key: 'cc_gateway_runtime_registered_at', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.runtimeRegisteredAt'), value: valueOrDash(d?.cc_gateway_runtime_registered_at) },
    { key: 'runtime_evidence_complete', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.runtimeEvidenceComplete'), value: formatBoolean(d?.runtime_evidence_complete) },
    { key: 'raw_capture_present', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.rawCapturePresent'), value: formatBoolean(d?.raw_capture_present) },
    { key: 'raw_capture_ref', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.rawCaptureRef'), value: valueOrDash(d?.raw_capture_ref) },
    { key: 'fallback_detected', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.fallbackDetected'), value: formatBoolean(d?.fallback_detected) },
    { key: 'proxy_mismatch', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.proxyMismatch'), value: formatBoolean(d?.proxy_mismatch) },
    { key: 'risk_text_detected', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.riskTextDetected'), value: formatBoolean(d?.risk_text_detected) },
    { key: 'status_code_bucket', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.statusBucket'), value: localizedBucketOrCode('statusBuckets', d?.status_code_bucket) },
    { key: 'risk_event_ref', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.riskEventRef'), value: valueOrDash(d?.risk_event_ref) },
    { key: 'healthcheck_evidence_persisted', label: t('admin.accounts.formalPoolDiagnostics.evidenceLabels.evidencePersisted'), value: formatBoolean(d?.healthcheck_evidence_persisted) },
  ]
})

type HealthcheckSafetyNotice = {
  key: 'status429' | 'rateLimitWindow' | 'auth' | 'hardRisk' | 'proxy' | 'gateway' | 'none'
  severity: 'info' | 'warning' | 'danger'
}

type ManualRiskGuidanceKey = 'accountRestricted' | 'accountVerification' | 'riskSignal' | 'forbidden'


const healthcheckSafetyNotices = computed<HealthcheckSafetyNotice[]>(() => {
  const d = currentDiagnostics.value
  const signals = new Set<string>()
  const addText = (value: unknown) => {
    const text = diagnosticText(value)
    if (text) signals.add(text)
  }
  addText(d?.status_code_bucket)
  addText(d?.failure_code)
  addText(d?.failure_source)
  addText(d?.healthcheck_status)
  addText(d?.healthcheck_safe_error_code)
  addText(d?.healthcheck_safe_error_bucket)
  addText(d?.formal_pool_rate_limit_error_class)
  addText(d?.formal_pool_rate_limit_window)
  addText(d?.formal_pool_rate_limit_action)
  addText(d?.quarantine_reason)
  if (d?.status_code_bucket === 'status_429') signals.add('status_429')
  if (d?.status_code_bucket === 'status_401') signals.add('status_401')
  if (d?.status_code_bucket === 'status_403') signals.add('status_403')
  if (d?.cc_gateway_seen === false) signals.add('cc_gateway_not_seen')
  if (d?.raw_capture_present === false) signals.add('raw_capture_missing')
  if (d?.fallback_detected) signals.add('fallback')
  if (d?.proxy_mismatch) signals.add('proxy')
  if (d?.risk_text_detected) signals.add('risk')

  const has = (...needles: string[]) => {
    const joined = Array.from(signals).join(' ')
    return needles.some(needle => joined.includes(needle))
  }

  const notices: HealthcheckSafetyNotice[] = []
  const add = (key: HealthcheckSafetyNotice['key'], severity: HealthcheckSafetyNotice['severity']) => {
    if (!notices.some(notice => notice.key === key)) notices.push({ key, severity })
  }

  if (has('status_429', 'too_many_requests', 'rate_limit')) add('status429', 'warning')
  if (has('5h', '7d', 'both', 'long_context_usage_credits', 'usage_credits')) add('rateLimitWindow', 'warning')
  if (has('status_401', 'invalid_grant', 'refresh_token_invalid', 'refresh_required', 'invalid_auth')) add('auth', 'danger')
  if (has('status_403', 'forbidden', 'hold', 'risk', 'kyc', 'unusual_activity', 'account_hold')) add('hardRisk', 'danger')
  if (has('proxy', 'egress_proxy_failure', 'proxy_mismatch')) add('proxy', 'warning')
  if (has('raw_capture_missing', 'cc_gateway_not_seen', 'fallback', 'missing_account_identity', 'missing_egress_bucket', 'verifier', 'sign_strip')) add('gateway', 'warning')
  if (!notices.length) add('none', 'info')
  return notices
})

const hasHealthcheckHighRisk = computed(() => healthcheckSafetyNotices.value.some(notice => notice.severity !== 'info'))
const manualRiskGuidanceItems = computed<ManualRiskGuidanceKey[]>(() => {
  const d = currentDiagnostics.value
  const values = [
    d?.status_code_bucket,
    d?.failure_code,
    d?.healthcheck_safe_error_code,
    d?.healthcheck_safe_error_bucket,
    d?.quarantine_reason,
  ].map(diagnosticText).join(' ')
  const out: ManualRiskGuidanceKey[] = []
  const add = (item: ManualRiskGuidanceKey) => {
    if (!out.includes(item)) out.push(item)
  }
  if (values.includes('account_on_hold') || values.includes('account_hold') || values.includes('hold')) add('accountRestricted')
  if (values.includes('kyc') || values.includes('verification')) add('accountVerification')
  if (values.includes('risk') || values.includes('unusual_activity') || d?.risk_text_detected) add('riskSignal')
  if (values.includes('status_403') || values.includes('forbidden') || values.includes('403')) add('forbidden')
  return out
})


const parsedSwapProxyId = computed(() => {
  const id = Number(swapProxyId.value)
  return Number.isInteger(id) && id > 0 ? id : 0
})

const checkStatusClass = (status: string) => {
  switch (status) {
    case 'pass': return 'bg-emerald-500'
    case 'warn': return 'bg-amber-500'
    case 'fail': return 'bg-red-500'
    default: return 'bg-gray-400'
  }
}
const actionClass = (severity?: string) => {
  switch (severity) {
    case 'danger': return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
    case 'warning': return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    default: return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'
  }
}
const healthcheckSafetyClass = (severity: HealthcheckSafetyNotice['severity']) => {
  switch (severity) {
    case 'danger': return 'border-red-200 bg-red-50 text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200'
    case 'warning': return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200'
    default: return 'border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-500/30 dark:bg-blue-500/10 dark:text-blue-200'
  }
}
const actionLabel = (action: FormalPoolRecommendedAction) => {
  const translated = translatedOrEmpty(`admin.accounts.formalPoolDiagnostics.recommendedActionKeys.${action.key}`)
  return translated || safeDisplayText(action.label || action.key)
}

const setError = (error: unknown) => {
  const message = error instanceof Error ? error.message : (error as { message?: string })?.message
  errorMessage.value = scrubFormalPoolSecretText(message || t('common.error'))
}

const applyOperationResult = async (result: FormalPoolOperationResult) => {
  latestAccount.value = result.account
  emit('updated', result.account)
  diagnostics.value = result.diagnostics ?? await getDiagnostics(result.account.id)
  sessionKey.value = ''
}

const handleOperationError = async (error: unknown) => {
  let hasOperationDiagnostics = false
  if (error instanceof FormalPoolOperationError) {
    if (error.account && activeAccount.value) latestAccount.value = { ...activeAccount.value, ...error.account }
    if (error.diagnostics) {
      diagnostics.value = error.diagnostics
      hasOperationDiagnostics = true
    }
  }
  setError(error)
  if (!hasOperationDiagnostics) await refreshDiagnostics({ keepError: true })
}

const refreshDiagnostics = async (options: { keepError?: boolean } = {}) => {
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

const runWithBusy = async (name: string, operation: () => Promise<FormalPoolOperationResult>) => {
  if (isBusy.value) return
  busyAction.value = name
  errorMessage.value = ''
  try {
    const result = await operation()
    await applyOperationResult(result)
    appStore.showSuccess(t('admin.accounts.formalPoolDiagnostics.operationSucceeded'))
  } catch (error) {
    await handleOperationError(error)
  } finally {
    busyAction.value = null
  }
}

const handleReplaceSetupToken = async () => {
  const account = activeAccount.value
  const rawSessionKey = sessionKey.value.trim()
  if (!account || !rawSessionKey || !canRepairSetupToken.value) return
  try {
    await runWithBusy('replace-token', () => replaceSetupToken(account.id, {
      session_key: rawSessionKey,
      run_runtime_register: runRuntimeRegisterAfterTokenRepair.value,
      run_healthcheck: runHealthcheckAfterTokenRepair.value,
    }))
  } finally {
    sessionKey.value = ''
  }
}

const runAccountAction = async (action: 'runtime-register' | 'healthcheck' | 'start-warming' | 'promote-production') => {
  const account = activeAccount.value
  if (!account) return
  if (action === 'runtime-register') {
    await runWithBusy(action, () => runtimeRegister(account.id))
  } else if (action === 'healthcheck') {
    const confirmKey = hasHealthcheckHighRisk.value
      ? 'admin.accounts.formalPoolDiagnostics.directedHealthcheckConfirmHighRisk'
      : 'admin.accounts.formalPoolDiagnostics.directedHealthcheckConfirm'
    if (!window.confirm(t(confirmKey))) return
    await runWithBusy(action, () => healthcheck(account.id))
  } else if (action === 'promote-production') {
    await runWithBusy(action, () => promoteProduction(account.id))
  } else if (canStartWarming.value) {
    await runWithBusy(action, () => startWarming(account.id))
  }
}

const handleSwapProxy = async () => {
  const account = activeAccount.value
  const proxyId = parsedSwapProxyId.value
  if (!account || !proxyId) return
  await runWithBusy('swap-proxy', () => swapProxy(account.id, {
    proxy_id: proxyId,
    run_proxy_test: true,
    run_runtime_register: true,
    run_healthcheck: true,
  }))
}

const handleClose = () => {
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
      swapProxyId.value = ''
      refreshDiagnostics()
      return
    }
    diagnostics.value = null
    latestAccount.value = null
    errorMessage.value = ''
    sessionKey.value = ''
    swapProxyId.value = ''
  },
  { immediate: true }
)
</script>
