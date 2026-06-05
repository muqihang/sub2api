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
  | 'wait'
  | 'manualReview'
  | 'swapProxy'
  | 'runtimeRegister'
  | 'runtimeRegisterThenHealthcheck'
  | 'healthcheck'
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
    label: 'OAuth one-click reauth',
    description: 'No backend API exists in this phase.',
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
  wait: {
    key: 'wait',
    label: '等待 5h 用量窗口恢复',
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
    label: '运行时注册 / 映射',
    description: '调用现有 runtime-register API 补齐网关身份映射。',
    behavior: 'api',
  },
  runtimeRegisterThenHealthcheck: {
    key: 'runtimeRegisterThenHealthcheck',
    label: '更换代理后再执行 runtime-register / healthcheck',
    description: '代理修复前禁止单独健康检查；修复后按顺序复查。',
    behavior: 'guide',
  },
  healthcheck: {
    key: 'healthcheck',
    label: '定向健康检查',
    description: '调用现有 healthcheck API；会发起一次真实上游请求，需确认。',
    behavior: 'api',
  },
  directHealthcheckBeforeProxyRepair: {
    key: 'directHealthcheckBeforeProxyRepair',
    label: 'Direct healthcheck before proxy repair',
    description: '代理 mismatch/fallback 修复前禁止直接健康检查。',
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
    description: '调用现有账号隔离 API；V2 会归一化返回结果。',
    behavior: 'api',
    destructive: true,
  },
  autoRepair: {
    key: 'autoRepair',
    label: 'Auto repair',
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
  setup_token_expired: 'Setup Token 登录态已过期',
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
  raw_capture_missing: '运行证据不完整：缺少 raw capture 证据',
  cc_gateway_not_seen: '运行证据不完整：未看到 CC Gateway 证据',
  fallback_detected: '发现 fallback',
  fallback: '发现 fallback',
  account_on_hold: '上游账号被暂停或限制',
  account_hold: '上游账号被暂停或限制',
  hold: '上游账号被暂停或限制',
  kyc: '需要完成账号验证',
  risk: '上游账号风控提示',
  unusual_activity: '上游提示异常活动',
}

const checkDisplayNames: Record<string, string> = {
  cc_gateway_runtime_registered: '运行时注册映射',
  runtime_evidence_complete: '运行证据完整性',
  runtime_evidence_incomplete: '运行证据不完整',
  healthcheck_evidence_persisted: '健康检查证据持久化',
  raw_capture_present: 'Raw capture 证据',
  cc_gateway_seen: 'CC Gateway 证据',
  proxy_mismatch: '代理出口不一致',
  fallback_detected: '发现 fallback',
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
  if (kind === 'origin') return '未知来源'
  if (kind === 'status') return '未知状态'
  if (kind === 'check') return '未知检查项'
  if (kind === 'action') return '未知动作'
  return '未知分类'
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
  return `${unknownPrefix(kind)}（${raw}）`
}

export function formatFormalPoolDiagnosticCodeWithRaw(
  value: unknown,
  kind: FormalPoolDiagnosticDisplayKind = 'classification',
  fallback = '数据不足',
): string {
  const raw = safe(value, '').trim()
  if (!raw) return fallback
  const label = formatFormalPoolDiagnosticCode(raw, kind, fallback)
  if (label.includes(`（${raw}）`)) return label
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
  const highRiskPromotionBlock =
    diagnostics?.proxy_mismatch === true ||
    diagnostics?.fallback_detected === true ||
    diagnostics?.risk_text_detected === true ||
    diagnostics?.failure_origin === 'proxy' ||
    hasAny(signals, 'proxy_mismatch', 'fallback', 'status_429', 'rate_limit', '5h', 'long_context_usage_credits', 'status_403', '403', 'hold', 'kyc', 'risk', 'unusual_activity', 'account_on_hold')

  const baseBullets = [
    `失败来源：${formatFormalPoolDiagnosticCodeWithRaw(diagnostics?.failure_origin, 'origin', '数据不足')}`,
    `失败分类：${formatFormalPoolDiagnosticCodeWithRaw(diagnostics?.failure_code || diagnostics?.status_code_bucket, 'classification', '数据不足')}`,
  ]

  if (evidenceMissing) {
    const primary = runtimeRegistrationEvidenceComplete ? action('healthcheck') : action('runtimeRegister')
    return {
      scenario: 'evidence_missing',
      lane: 'needs_intervention',
      tone: 'sky',
      title: '运行证据缺失',
      summary: '先补齐 runtime / gateway / healthcheck 证据；证据缺失时禁止升级生产状态。',
      rootCauseBullets: [
        ...baseBullets,
        runtimeRegistrationEvidenceComplete
          ? 'runtime / gateway 注册证据已完整，下一步只补 healthcheck / raw capture 证据。'
          : '缺少 runtime / gateway 注册映射证据，先执行 runtime-register。',
      ],
      primaryAction: primary,
      secondaryActions: [],
      forbiddenActions: forbiddenActions(),
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
      summary: 'runtime / gateway / healthcheck 证据完整，可以手动切换到生产期。',
      rootCauseBullets: [
        ...baseBullets,
        '当前账号处于 warming，后端诊断推荐 promote_production。',
      ],
      primaryAction: action('promoteProduction'),
      secondaryActions: [action('refreshDiagnostics')],
      forbiddenActions: forbiddenActionsAllowPromote('replaceSetupToken', 'swapProxy', 'runtimeRegister', 'healthcheck', 'quarantine'),
    }
  }

  if (recommends(rec, 'monitor')) {
    return {
      scenario: 'monitor',
      lane: 'active',
      tone: 'emerald',
      title: '账号处于可用观测状态',
      summary: '诊断建议为 monitor：无需修复按钮，必要时只刷新诊断。',
      rootCauseBullets: ['调度和证据未显示需要介入的信号。'],
      primaryAction: action('none'),
      secondaryActions: [action('refreshDiagnostics')],
      forbiddenActions: forbiddenActions('replaceSetupToken', 'swapProxy', 'runtimeRegister', 'healthcheck', 'quarantine'),
    }
  }

  if (diagnostics?.proxy_mismatch || diagnostics?.fallback_detected || diagnostics?.failure_origin === 'proxy' || hasAny(signals, 'proxy_mismatch', 'fallback')) {
    return {
      scenario: 'proxy_mismatch',
      lane: 'needs_intervention',
      tone: 'amber',
      title: '代理出口证据不一致',
      summary: '先修复代理链路；代理修复前禁止直接 healthcheck。',
      rootCauseBullets: [
        ...baseBullets,
        `代理出口不一致：${diagnostics?.proxy_mismatch === true ? '是' : '否'}`,
        `发现 fallback：${diagnostics?.fallback_detected === true ? '是' : '否'}`,
      ],
      primaryAction: action('swapProxy'),
      secondaryActions: [action('runtimeRegisterThenHealthcheck')],
      forbiddenActions: forbiddenActions('directHealthcheckBeforeProxyRepair'),
    }
  }

  if (account?.type === 'oauth' && hasAny(signals, 'invalid_grant', 'refresh_token_invalid', 'reauthorize')) {
    const secondaries: FormalPoolDiagnosticsHeroAction[] = []
    if (recommends(rec, 'swap_proxy')) secondaries.push(action('swapProxy'))
    if (recommends(rec, 'runtime_register')) secondaries.push(action('runtimeRegister'))
    return {
      scenario: 'oauth_invalid_grant',
      lane: 'needs_intervention',
      tone: 'rose',
      title: 'OAuth refresh token 已失效',
      summary: '上游拒绝 refresh；当前阶段只能引导重新 OAuth，不能显示一键授权假按钮。',
      rootCauseBullets: [...baseBullets, 'OAuth 账号不会显示 Setup Token 替换输入。'],
      primaryAction: action('guideOAuthReauth'),
      secondaryActions: unique(secondaries),
      forbiddenActions: forbiddenActions('oneClickOAuthReauth'),
    }
  }

  if (account?.type === 'setup-token' && (recommends(rec, 'replace_setup_token', 'repair_token') || hasAny(signals, 'setup_token_expired', 'session_expired', 'invalid_grant'))) {
    const secondaries: FormalPoolDiagnosticsHeroAction[] = []
    if (recommends(rec, 'swap_proxy')) secondaries.push(action('swapProxy'))
    return {
      scenario: 'setup_token_expired',
      lane: 'needs_intervention',
      tone: 'rose',
      title: 'Setup Token 登录态已过期',
      summary: '使用 setup-token 账号专用替换登录态流程；不显示泛化 token 替换。',
      rootCauseBullets: [...baseBullets, '替换后可选择继续 runtime-register / healthcheck。'],
      primaryAction: action('replaceSetupToken'),
      secondaryActions: unique(secondaries),
      forbiddenActions: forbiddenActions('genericTokenReplace'),
    }
  }

  if (hasAny(signals, 'status_429', 'rate_limit', '5h', 'long_context_usage_credits')) {
    return {
      scenario: 'rate_limited_5h',
      lane: 'paused',
      tone: 'amber',
      title: '5h 用量窗口冷却中',
      summary: '这是暂停/冷却状态，默认等待恢复；不要用健康检查制造更多真实请求。',
      rootCauseBullets: [...baseBullets, `窗口：${formatFormalPoolDiagnosticCodeWithRaw(diagnostics?.formal_pool_rate_limit_window || '5h', 'status')}`],
      primaryAction: action('wait'),
      secondaryActions: [action('refreshDiagnostics')],
      forbiddenActions: forbiddenActions('healthcheck'),
    }
  }


  if (hasAny(signals, 'status_403', '403', 'hold', 'kyc', 'risk', 'unusual_activity', 'account_on_hold') || diagnostics?.risk_text_detected) {
    const secondaries = recommends(rec, 'quarantine') ? [action('quarantine')] : []
    return {
      scenario: 'manual_risk',
      lane: 'needs_intervention',
      tone: 'rose',
      title: '上游账号状态需要人工介入',
      summary: '403 / hold / KYC / 风控不是普通自动修复场景；保持隔离并人工确认。',
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
