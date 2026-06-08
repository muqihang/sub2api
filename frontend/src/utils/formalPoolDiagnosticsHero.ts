import type { Account, FormalPoolOperationsDiagnostics } from '@/types'
import { scrubFormalPoolDisplayText } from './formalPoolStatusDashboard'

export type FormalPoolDiagnosticsScenario =
  | 'oauth_invalid_grant'
  | 'setup_token_expired'
  | 'rate_limited_5h'
  | 'manual_risk'
  | 'proxy_mismatch'
  | 'evidence_missing'
  | 'monitor'
  | 'unknown'

export type FormalPoolDiagnosticsActionKey =
  | 'guideOAuthReauth'
  | 'oneClickOAuthReauth'
  | 'replaceSetupToken'
  | 'genericTokenReplace'
  | 'refreshLoginState'
  | 'replaceAccountAndProxy'
  | 'wait'
  | 'manualReview'
  | 'swapProxy'
  | 'runtimeRegister'
  | 'runtimeRegisterThenHealthcheck'
  | 'healthcheck'
  | 'startWarming'
  | 'directHealthcheckBeforeProxyRepair'
  | 'refreshDiagnostics'
  | 'quarantine'
  | 'autoRepair'
  | 'promoteProduction'
  | 'none'

export type FormalPoolDiagnosticsActionBehavior = 'api' | 'guide' | 'navigate' | 'none'

export interface FormalPoolDiagnosticsHeroAction {
  key: FormalPoolDiagnosticsActionKey
  label: string
  description: string
  behavior: FormalPoolDiagnosticsActionBehavior
  destructive?: boolean
}

export interface FormalPoolDiagnosticsHero {
  scenario: FormalPoolDiagnosticsScenario
  lane: 'active' | 'paused' | 'needs_intervention' | 'inactive'
  tone: 'emerald' | 'amber' | 'rose' | 'sky' | 'slate'
  title: string
  summary: string
  rootCauseBullets: string[]
  primaryAction: FormalPoolDiagnosticsHeroAction | null
  secondaryActions: FormalPoolDiagnosticsHeroAction[]
  forbiddenActions: FormalPoolDiagnosticsHeroAction[]
}

const actionDefinitions: Record<FormalPoolDiagnosticsActionKey, FormalPoolDiagnosticsHeroAction> = {
  guideOAuthReauth: {
    key: 'guideOAuthReauth',
    label: '查看 OAuth 重新授权步骤',
    description: '前端引导到新的上号流程；当前后端无一键 OAuth 重新授权 API。',
    behavior: 'guide',
  },
  oneClickOAuthReauth: {
    key: 'oneClickOAuthReauth',
    label: '一键 OAuth 重新授权',
    description: '当前版本没有这个后端能力，不能作为可点击按钮展示。',
    behavior: 'none',
  },
  replaceSetupToken: {
    key: 'replaceSetupToken',
    label: '替换 Setup Token 登录态',
    description: '仅对 setup-token 账号调用现有替换登录态 API。',
    behavior: 'api',
  },
  genericTokenReplace: {
    key: 'genericTokenReplace',
    label: 'Generic token replace',
    description: '禁止泛化 token 替换；只能按账号类型走安全路径。',
    behavior: 'none',
  },
  refreshLoginState: {
    key: 'refreshLoginState',
    label: '刷新登录态并重测',
    description: '先尝试用已有 Refresh Token 刷新登录态；成功后重新拉取诊断。若刷新失败，需更换 Setup Token。',
    behavior: 'api',
  },
  replaceAccountAndProxy: {
    key: 'replaceAccountAndProxy',
    label: '重新上号并更换代理',
    description: '当前没有一键替换账号 API；跳转上号引导，用新账号和新出口代理完成闭环。',
    behavior: 'navigate',
  },
  wait: {
    key: 'wait',
    label: '等待 5 小时用量窗口恢复',
    description: '限流窗口恢复前不要触发真实上游健康检查。',
    behavior: 'none',
  },
  manualReview: {
    key: 'manualReview',
    label: '人工处理上游账号状态',
    description: '先登录上游网页确认 hold / KYC / 风控状态，系统不做自动修复。',
    behavior: 'guide',
  },
  swapProxy: {
    key: 'swapProxy',
    label: '更换出口代理',
    description: '调用现有代理替换 API，并重新跑代理测试、运行时注册和健康检查。',
    behavior: 'api',
  },
  runtimeRegister: {
    key: 'runtimeRegister',
    label: '注册运行映射',
    description: '为网关补齐账号与固定出口的运行映射。',
    behavior: 'api',
  },
  runtimeRegisterThenHealthcheck: {
    key: 'runtimeRegisterThenHealthcheck',
    label: '更换代理后再重新检查运行映射和健康状态',
    description: '代理修复前不要单独做健康检查；修复后系统会按顺序复查。',
    behavior: 'guide',
  },
  healthcheck: {
    key: 'healthcheck',
    label: '定向健康检查',
    description: '会发起一次真实上游请求，需确认后执行。',
    behavior: 'api',
  },
  startWarming: {
    key: 'startWarming',
    label: '进入预热期',
    description: '健康检查证据完整后，恢复低权重调度并刷新调度器。',
    behavior: 'api',
  },
  directHealthcheckBeforeProxyRepair: {
    key: 'directHealthcheckBeforeProxyRepair',
    label: '代理修复前直接健康检查',
    description: '代理出口异常修复前禁止直接健康检查。',
    behavior: 'none',
  },
  refreshDiagnostics: {
    key: 'refreshDiagnostics',
    label: '刷新诊断',
    description: '只刷新本地诊断视图，不发起上游请求。',
    behavior: 'api',
  },
  quarantine: {
    key: 'quarantine',
    label: '隔离账号',
    description: '将账号移出调度，等待人工确认后再恢复。',
    behavior: 'api',
    destructive: true,
  },
  autoRepair: {
    key: 'autoRepair',
    label: '自动修复',
    description: '上游 hold / KYC / 风控不能自动修复。',
    behavior: 'none',
  },
  promoteProduction: {
    key: 'promoteProduction',
    label: '进入生产',
    description: '证据缺失时禁止进入生产。',
    behavior: 'api',
  },
  none: {
    key: 'none',
    label: '无需修复',
    description: '继续观察即可。',
    behavior: 'none',
  },
}

function action(key: FormalPoolDiagnosticsActionKey): FormalPoolDiagnosticsHeroAction {
  return actionDefinitions[key]
}

function normalizedText(...values: unknown[]): string {
  return values.map((value) => String(value ?? '').toLowerCase()).join(' ')
}

function recommendedSet(diagnostics: FormalPoolOperationsDiagnostics | null | undefined): Set<string> {
  return new Set((diagnostics?.recommended_actions ?? []).map((item) => String(item.key)))
}

function recommends(recommended: Set<string>, ...keys: string[]): boolean {
  return keys.some((key) => recommended.has(key))
}

function hasAny(text: string, ...needles: string[]): boolean {
  return needles.some((needle) => text.includes(needle))
}

function hasAuthSignal(signals: string): boolean {
  return hasAny(
    signals,
    'status_401',
    '401',
    'auth',
    'unauthorized',
    'invalid_auth',
    'authentication_error',
    'identity_boundary_fail',
    'invalid_grant',
    'refresh_token_invalid',
  )
}

function isUpstreamAuthFailure(diagnostics: FormalPoolOperationsDiagnostics | null | undefined, signals: string): boolean {
  return diagnostics?.failure_origin === 'upstream' && hasAuthSignal(signals)
}

function primaryFailureCodeForDisplay(diagnostics: FormalPoolOperationsDiagnostics | null | undefined, signals: string, authSignal = hasAuthSignal(signals)): unknown {
  if (authSignal || isUpstreamAuthFailure(diagnostics, signals)) {
    return diagnostics?.onboarding_last_error_bucket || diagnostics?.status_code_bucket || diagnostics?.healthcheck_safe_error_bucket || diagnostics?.healthcheck_safe_error_code || diagnostics?.failure_code
  }
  return diagnostics?.failure_code || diagnostics?.status_code_bucket || diagnostics?.healthcheck_safe_error_bucket || diagnostics?.healthcheck_safe_error_code
}

function safe(value: unknown, fallback = '数据不足'): string {
  return scrubFormalPoolDisplayText(String(value ?? ''), fallback)
}

type FormalPoolDiagnosticDisplayKind = 'origin' | 'classification' | 'status' | 'check' | 'action' | 'generic'

const originDisplayNames: Record<string, string> = {
  local_gate: '本地准入门禁',
  cc_gateway_control_plane: 'CC Gateway 控制面',
  control_plane: '控制面证据',
  upstream: '上游返回异常',
  proxy: '代理出口异常',
  proxy_mismatch: '代理出口不一致',
  token_exchange: '授权换取失败',
  unknown: '未知来源',
}

const classificationDisplayNames: Record<string, string> = {
  '5h': '5 小时窗口',
  proxy_mismatch: '浏览器与代理出口不一致',
  bucket_mismatch: '出口分组不一致',
  token_exchange: '授权换取失败',
  invalid_grant: '授权已失效或授权码无效',
  refresh_token_invalid: '授权已失效或授权码无效',
  auth: '认证失败',
  formal_pool_healthcheck_failed: '健康检查失败',
  formal_pool_healthcheck: '正式号池健康检查',
  identity_boundary_fail: '上游身份边界校验失败',
  setup_token_expired: 'Setup Token 登录态已过期',
  invalid_auth: '认证失败',
  authentication_error: '认证失败',
  status_401: '401 / 认证失败',
  status_403: '403 / 禁止访问或风控',
  status_429: '5 小时窗口冷却/限流',
  '401': '401 / 认证失败',
  '403': '403 / 禁止访问或风控',
  '429': '5 小时窗口冷却/限流',
  rate_limit_5h: '5 小时窗口冷却/限流',
  reset_bucket: '5 小时窗口冷却/限流',
  rate_limit_reset_bucket: '5 小时窗口冷却/限流',
  long_context_usage_credits: '长上下文额度触发 5 小时窗口冷却/限流',
  evidence_missing: '运行证据不完整',
  runtime_evidence_incomplete: '运行证据不完整',
  healthcheck_evidence_missing: '健康检查证据不完整',
  raw_capture_missing: '运行证据不完整：缺少上游请求证据',
  cc_gateway_not_seen: '运行证据不完整：未看到 CC Gateway 证据',
  fallback_detected: '发现备用线路',
  fallback: '发现备用线路',
  account_on_hold: '上游账号被暂停或限制',
  account_hold: '上游账号被暂停或限制',
  hold: '上游账号被暂停或限制',
  kyc: '需要完成账号验证',
  risk: '上游账号风控提示',
  unusual_activity: '上游提示异常活动',
  imported: '已导入，待验证',
  refreshed: '登录态已刷新',
  runtime_registered: '运行时已注册映射',
  healthcheck_passed: '健康检查通过',
  warming: '预热中',
  production: '生产中',
  quarantined: '已隔离，需要修复',
  legacy_unknown: '历史账号，状态未知',
}

const checkDisplayNames: Record<string, string> = {
  cc_gateway_runtime_registered: '运行时注册映射',
  runtime_evidence_complete: '运行证据完整性',
  runtime_evidence_incomplete: '运行证据不完整',
  healthcheck_evidence_persisted: '健康检查证据持久化',
  raw_capture_present: '上游请求证据',
  cc_gateway_seen: 'CC Gateway 证据',
  proxy_mismatch: '代理出口不一致',
  fallback_detected: '发现备用线路',
  stage_gate: '阶段准入检查',
}

const actionDisplayNames: Record<string, string> = {
  refresh_only: '刷新诊断/凭证状态',
  runtime_register: '运行时注册/映射',
  healthcheck: '定向健康检查',
  start_warming: '进入预热',
  promote_production: '进入生产',
  replace_setup_token: '替换 Setup Token 登录态',
  reauthorize_oauth: '重新 OAuth 授权',
  monitor: '无需操作，继续观测',
  quarantine: '隔离账号',
  swap_proxy: '更换出口代理',
  wait_rate_limit: '等待 5 小时窗口冷却/限流恢复',
  repair_token: '替换 Setup Token 登录态',
  repair_oauth: '重新 OAuth 授权',
  replace_account_and_proxy: '更换账号和出口代理',
  manual_review: '人工查看具体失败分类',
}

function normalizedCode(value: unknown): string {
  return String(value ?? '').trim().toLowerCase()
}

function codeDisplayMap(kind: FormalPoolDiagnosticDisplayKind): Record<string, string> {
  if (kind === 'origin') return originDisplayNames
  if (kind === 'check') return { ...classificationDisplayNames, ...checkDisplayNames }
  if (kind === 'action') return { ...classificationDisplayNames, ...actionDisplayNames }
  if (kind === 'status') return classificationDisplayNames
  return classificationDisplayNames
}

function unknownPrefix(kind: FormalPoolDiagnosticDisplayKind): string {
  if (kind === 'origin') return '来源未返回可识别分类'
  if (kind === 'status') return '状态未返回可识别分类'
  if (kind === 'check') return '检查项未识别'
  if (kind === 'action') return '建议动作未识别'
  return '失败分类未识别'
}

export function formatFormalPoolDiagnosticCode(
  value: unknown,
  kind: FormalPoolDiagnosticDisplayKind = 'classification',
  fallback = '数据不足',
): string {
  const raw = safe(value, '').trim()
  if (!raw) return fallback
  const normalized = normalizedCode(raw)
  const label = codeDisplayMap(kind)[normalized]
  if (label) return label
  return unknownPrefix(kind)
}

export function formatFormalPoolDiagnosticCodeWithRaw(
  value: unknown,
  kind: FormalPoolDiagnosticDisplayKind = 'classification',
  fallback = '数据不足',
): string {
  const raw = safe(value, '').trim()
  if (!raw) return fallback
  const label = formatFormalPoolDiagnosticCode(raw, kind, fallback)
  if (label === unknownPrefix(kind) || label.includes(`（${raw}）`)) return label
  return `${label}（${raw}）`
}

function unique(actions: FormalPoolDiagnosticsHeroAction[]): FormalPoolDiagnosticsHeroAction[] {
  const seen = new Set<string>()
  return actions.filter((item) => {
    if (seen.has(item.key)) return false
    seen.add(item.key)
    return true
  })
}

function forbiddenActions(...keys: FormalPoolDiagnosticsActionKey[]): FormalPoolDiagnosticsHeroAction[] {
  const allKeys: FormalPoolDiagnosticsActionKey[] = [...keys, 'promoteProduction']
  return unique(allKeys.map(action))
}

function forbiddenActionsAllowPromote(...keys: FormalPoolDiagnosticsActionKey[]): FormalPoolDiagnosticsHeroAction[] {
  return unique(keys.map(action))
}

function isBlank(value: unknown): boolean {
  return String(value ?? '').trim().length === 0
}

function hasCheckStatus(
  diagnostics: FormalPoolOperationsDiagnostics | null | undefined,
  name: string,
  statuses: Array<'warn' | 'fail'> = ['warn', 'fail'],
): boolean {
  return (diagnostics?.checks ?? []).some((check) => check.name === name && check.status !== 'pass' && statuses.includes(check.status))
}

export function deriveFormalPoolDiagnosticsHero(input: {
  account: Account | null | undefined
  diagnostics: FormalPoolOperationsDiagnostics | null | undefined
}): FormalPoolDiagnosticsHero {
  const { account, diagnostics } = input
  const rec = recommendedSet(diagnostics)
  const signals = normalizedText(
    diagnostics?.failure_origin,
    diagnostics?.failure_code,
    diagnostics?.failure_source,
    diagnostics?.status_code_bucket,
    diagnostics?.healthcheck_status,
    diagnostics?.healthcheck_safe_error_code,
    diagnostics?.healthcheck_safe_error_bucket,
    diagnostics?.formal_pool_rate_limit_error_class,
    diagnostics?.formal_pool_rate_limit_window,
    diagnostics?.formal_pool_rate_limit_action,
    diagnostics?.quarantine_reason,
    diagnostics?.onboarding_last_error_code,
    diagnostics?.onboarding_last_error_bucket,
    diagnostics?.last_cc_gateway_error_code,
  )

  const runtimeRegistrationEvidenceComplete =
    diagnostics?.cc_gateway_runtime_registered === true &&
    !isBlank(diagnostics?.cc_gateway_runtime_registered_at) &&
    diagnostics?.runtime_evidence_complete === true
  const gatewayRuntimeMappingEvidenceMissing =
    diagnostics?.cc_gateway_runtime_registered === false ||
    hasCheckStatus(diagnostics, 'cc_gateway_runtime_registered') ||
    (diagnostics?.cc_gateway_runtime_registered === true && isBlank(diagnostics?.cc_gateway_runtime_registered_at)) ||
    diagnostics?.runtime_evidence_complete === false ||
    hasAny(signals, 'runtime_evidence_incomplete', 'runtime_registration_failed', 'missing_account_identity')
  const healthcheckOrCaptureEvidenceMissing =
    diagnostics?.cc_gateway_seen === false ||
    diagnostics?.raw_capture_present === false ||
    diagnostics?.healthcheck_evidence_persisted === false ||
    hasCheckStatus(diagnostics, 'healthcheck_evidence_persisted') ||
    hasAny(signals, 'raw_capture_missing', 'cc_gateway_not_seen')
  const evidenceMissing = gatewayRuntimeMappingEvidenceMissing || healthcheckOrCaptureEvidenceMissing
  const upstreamAuthFailure = isUpstreamAuthFailure(diagnostics, signals)
  const authSignal = hasAuthSignal(signals)
  const hasTokenRepairSignal =
    (account?.type === 'oauth' && (upstreamAuthFailure || authSignal || recommends(rec, 'reauthorize_oauth', 'repair_token') || hasAny(signals, 'invalid_grant', 'refresh_token_invalid', 'reauthorize'))) ||
    (account?.type === 'setup-token' && (upstreamAuthFailure || authSignal || recommends(rec, 'replace_setup_token', 'repair_token') || hasAny(signals, 'setup_token_expired', 'session_expired', 'invalid_grant')))
  const hasManualRiskSignal = recommends(rec, 'manual_review') || diagnostics?.risk_text_detected === true || hasAny(signals, 'status_403', '403', 'hold', 'kyc', 'risk', 'unusual_activity', 'account_on_hold')
  const hasExplicitProxyFailure = diagnostics?.proxy_mismatch === true || diagnostics?.fallback_detected === true || diagnostics?.failure_origin === 'proxy' || hasAny(signals, 'proxy_mismatch', 'fallback')
  const hasRateLimitSignal = !hasExplicitProxyFailure && (recommends(rec, 'wait_rate_limit') || hasAny(signals, 'status_429', 'rate_limit', '5h', 'long_context_usage_credits'))
  const hasProxyRepairSignal = hasExplicitProxyFailure || recommends(rec, 'swap_proxy')
  const hasHigherPriorityRepair =
    hasTokenRepairSignal ||
    hasRateLimitSignal ||
    hasManualRiskSignal ||
    hasProxyRepairSignal ||
    diagnostics?.proxy_mismatch === true ||
    diagnostics?.fallback_detected === true ||
    diagnostics?.risk_text_detected === true ||
    upstreamAuthFailure ||
    hasAny(signals, 'invalid_grant', 'refresh_token_invalid', 'reauthorize', 'setup_token_expired', 'session_expired', 'status_401', '401', 'identity_boundary_fail', 'status_429', 'rate_limit', '5h', 'long_context_usage_credits', 'proxy_mismatch', 'fallback', 'status_403', '403', 'hold', 'kyc', 'risk', 'unusual_activity', 'account_on_hold')
  const highRiskPromotionBlock =
    diagnostics?.proxy_mismatch === true ||
    diagnostics?.fallback_detected === true ||
    diagnostics?.risk_text_detected === true ||
    diagnostics?.failure_origin === 'proxy' ||
    hasAny(signals, 'proxy_mismatch', 'fallback', 'status_429', 'rate_limit', '5h', 'long_context_usage_credits', 'status_403', '403', 'hold', 'kyc', 'risk', 'unusual_activity', 'account_on_hold')

  const baseBullets = [
    `失败来源：${formatFormalPoolDiagnosticCode(diagnostics?.failure_origin, 'origin', '数据不足')}`,
    `失败分类：${formatFormalPoolDiagnosticCode(primaryFailureCodeForDisplay(diagnostics, signals, authSignal), 'classification', '数据不足')}`,
  ]

  if (evidenceMissing && !hasHigherPriorityRepair) {
    const primary = runtimeRegistrationEvidenceComplete ? action('healthcheck') : action('runtimeRegister')
    return {
      scenario: 'evidence_missing',
      lane: 'needs_intervention',
      tone: 'sky',
      title: '运行证据缺失',
      summary: '先补齐运行映射、网关记录和健康检查证据；证据缺失时不能升级到生产状态。',
      rootCauseBullets: [
        ...baseBullets,
        runtimeRegistrationEvidenceComplete
          ? '运行映射和网关记录已完整，下一步只需补健康检查证据。'
          : '缺少运行映射或网关记录，先注册运行映射。',
      ],
      primaryAction: primary,
      secondaryActions: [],
      forbiddenActions: forbiddenActions(),
    }
  }

  if (
    diagnostics?.onboarding_stage === 'healthcheck_passed' &&
    recommends(rec, 'start_warming') &&
    runtimeRegistrationEvidenceComplete &&
    diagnostics?.healthcheck_evidence_persisted === true &&
    diagnostics?.raw_capture_present === true &&
    diagnostics?.cc_gateway_seen === true &&
    !highRiskPromotionBlock
  ) {
    return {
      scenario: 'monitor',
      lane: 'paused',
      tone: 'sky',
      title: '健康检查已通过，可进入预热',
      summary: '运行映射、网关记录和健康检查证据完整，可以进入预热期；进入后账号会以低权重参与调度。',
      rootCauseBullets: [
        ...baseBullets,
        '当前账号已通过健康检查，系统建议进入预热期。',
      ],
      primaryAction: action('startWarming'),
      secondaryActions: [action('refreshDiagnostics')],
      forbiddenActions: forbiddenActionsAllowPromote('replaceSetupToken', 'swapProxy', 'runtimeRegister', 'healthcheck', 'quarantine', 'promoteProduction'),
    }
  }

  if (
    diagnostics?.onboarding_stage === 'warming' &&
    recommends(rec, 'promote_production') &&
    runtimeRegistrationEvidenceComplete &&
    diagnostics?.healthcheck_evidence_persisted === true &&
    diagnostics?.raw_capture_present === true &&
    diagnostics?.cc_gateway_seen === true &&
    !highRiskPromotionBlock
  ) {
    return {
      scenario: 'monitor',
      lane: 'active',
      tone: 'emerald',
      title: '预热完成，可进入生产',
      summary: '运行映射、网关记录和健康检查证据完整，可以切换到生产期。',
      rootCauseBullets: [
        ...baseBullets,
        '当前账号处于预热期，系统建议进入生产调度。',
      ],
      primaryAction: action('promoteProduction'),
      secondaryActions: [action('refreshDiagnostics')],
      forbiddenActions: forbiddenActionsAllowPromote('replaceSetupToken', 'swapProxy', 'runtimeRegister', 'healthcheck', 'quarantine'),
    }
  }

  if (
    diagnostics?.onboarding_stage === 'production' &&
    recommends(rec, 'promote_production') &&
    runtimeRegistrationEvidenceComplete &&
    diagnostics?.healthcheck_evidence_persisted === true &&
    diagnostics?.raw_capture_present === true &&
    diagnostics?.cc_gateway_seen === true &&
    !highRiskPromotionBlock
  ) {
    return {
      scenario: 'monitor',
      lane: 'paused',
      tone: 'sky',
      title: '生产账号已通过证据检查，可恢复调度',
      summary: '账号处于 production 且证据完整，但本地调度开关或状态未开启；点击后恢复生产调度。',
      rootCauseBullets: [
        ...baseBullets,
        '当前账号已处于生产阶段，系统建议恢复生产调度。',
      ],
      primaryAction: action('promoteProduction'),
      secondaryActions: [action('refreshDiagnostics')],
      forbiddenActions: forbiddenActionsAllowPromote('replaceSetupToken', 'swapProxy', 'runtimeRegister', 'healthcheck', 'quarantine'),
    }
  }

  const explicitProxyOrigin = diagnostics?.failure_origin === 'proxy'

  if (account?.type === 'oauth' && hasTokenRepairSignal && !explicitProxyOrigin) {
    const secondaries: FormalPoolDiagnosticsHeroAction[] = []
    if (recommends(rec, 'swap_proxy')) secondaries.push(action('swapProxy'))
    if (recommends(rec, 'replace_account_and_proxy')) secondaries.push(action('replaceAccountAndProxy'))
    if (recommends(rec, 'runtime_register')) secondaries.push(action('runtimeRegister'))
    return {
      scenario: 'oauth_invalid_grant',
      lane: 'needs_intervention',
      tone: 'rose',
      title: 'OAuth 授权已失效',
      summary: '上游拒绝刷新登录态；当前只能引导重新 OAuth 授权，不能显示一键授权假按钮。',
      rootCauseBullets: [...baseBullets, 'OAuth 账号不会显示 Setup Token 替换输入。'],
      primaryAction: action('guideOAuthReauth'),
      secondaryActions: unique(secondaries),
      forbiddenActions: forbiddenActions('oneClickOAuthReauth'),
    }
  }

  if (account?.type === 'setup-token' && hasTokenRepairSignal && !explicitProxyOrigin) {
    const secondaries: FormalPoolDiagnosticsHeroAction[] = []
    if (upstreamAuthFailure || authSignal) secondaries.push(action('refreshLoginState'))
    if (recommends(rec, 'swap_proxy')) secondaries.push(action('swapProxy'))
    if (recommends(rec, 'replace_account_and_proxy')) secondaries.push(action('replaceAccountAndProxy'))
    const title = upstreamAuthFailure || authSignal ? '上游认证失败，需要更换 Setup Token' : 'Setup Token 登录态已过期'
    const summary = upstreamAuthFailure || authSignal
      ? '健康检查已打到上游，但上游返回 401 / 认证失败；刷新登录态失败时说明 Refresh Token 已失效，需要替换新的 Setup Token。'
      : '使用 setup-token 账号专用替换登录态流程；不显示泛化 token 替换。'
    const extraBullet = upstreamAuthFailure || authSignal
      ? '已确认不是运行映射或网关记录缺失；请更换 Setup Token 后重新检查运行映射和健康状态。'
      : '替换后可继续检查运行映射和健康状态。'
    return {
      scenario: 'setup_token_expired',
      lane: 'needs_intervention',
      tone: 'rose',
      title,
      summary,
      rootCauseBullets: [...baseBullets, extraBullet],
      primaryAction: action('replaceSetupToken'),
      secondaryActions: unique(secondaries),
      forbiddenActions: forbiddenActions('genericTokenReplace'),
    }
  }

  if (recommends(rec, 'monitor') && !hasHigherPriorityRepair) {
    return {
      scenario: 'monitor',
      lane: 'active',
      tone: 'emerald',
      title: '账号处于可用观测状态',
      summary: '当前没有需要修复的信号；必要时刷新诊断即可。',
      rootCauseBullets: ['调度和证据未显示需要介入的信号。'],
      primaryAction: action('none'),
      secondaryActions: [action('refreshDiagnostics')],
      forbiddenActions: forbiddenActions('replaceSetupToken', 'swapProxy', 'runtimeRegister', 'healthcheck', 'quarantine'),
    }
  }

  if (hasProxyRepairSignal && (hasExplicitProxyFailure || (!hasTokenRepairSignal && !hasRateLimitSignal && !hasManualRiskSignal))) {
    return {
      scenario: 'proxy_mismatch',
      lane: 'needs_intervention',
      tone: 'amber',
      title: '代理出口证据不一致',
      summary: '先修复代理链路；代理修复前禁止直接健康检查。',
      rootCauseBullets: [
        ...baseBullets,
        `代理出口不一致：${diagnostics?.proxy_mismatch === true ? '是' : '否'}`,
        `发现备用线路：${diagnostics?.fallback_detected === true ? '是' : '否'}`,
      ],
      primaryAction: action('swapProxy'),
      secondaryActions: [action('runtimeRegisterThenHealthcheck')],
      forbiddenActions: forbiddenActions('directHealthcheckBeforeProxyRepair'),
    }
  }

  if (hasRateLimitSignal) {
    return {
      scenario: 'rate_limited_5h',
      lane: 'paused',
      tone: 'amber',
      title: '5 小时用量窗口冷却中',
      summary: '这是暂停/冷却状态，默认等待恢复；不要用健康检查制造更多真实请求。',
      rootCauseBullets: [...baseBullets, `窗口：${formatFormalPoolDiagnosticCode(diagnostics?.formal_pool_rate_limit_window || '5h', 'status')}`],
      primaryAction: action('wait'),
      secondaryActions: [action('refreshDiagnostics')],
      forbiddenActions: forbiddenActions('healthcheck'),
    }
  }


  if (hasManualRiskSignal) {
    const secondaries = recommends(rec, 'quarantine') ? [action('quarantine')] : []
    return {
      scenario: 'manual_risk',
      lane: 'needs_intervention',
      tone: 'rose',
      title: '上游账号状态需要人工介入',
      summary: '上游返回 403、账号暂停、身份验证或风控时，系统不能自动修复；请人工确认后再处理。',
      rootCauseBullets: [...baseBullets, '不要重复健康检查或自动刷新凭证。'],
      primaryAction: action('manualReview'),
      secondaryActions: secondaries,
      forbiddenActions: forbiddenActions('autoRepair', 'healthcheck'),
    }
  }

  return {
    scenario: 'unknown',
    lane: 'needs_intervention',
    tone: 'slate',
    title: '需要刷新诊断后再处理',
    summary: '当前信号不足以安全选择修复路径。',
    rootCauseBullets: baseBullets,
    primaryAction: action('refreshDiagnostics'),
    secondaryActions: [],
    forbiddenActions: forbiddenActions(),
  }
}
