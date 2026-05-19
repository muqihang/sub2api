export const POLICY_DOCUMENT_KEYS = ['terms', 'privacy', 'notice'] as const
export type PolicyDocumentKey = (typeof POLICY_DOCUMENT_KEYS)[number]

export const POLICY_LOCALES = ['zh-CN', 'en-US'] as const
export type PolicyLocale = (typeof POLICY_LOCALES)[number]

export const DEFAULT_POLICY_LOCALE: PolicyLocale = 'zh-CN'

export interface PolicyDocument {
  locale: PolicyLocale
  key: PolicyDocumentKey
  content: string
  isEmpty: boolean
  hidden: boolean
}

type RawPolicyMarkdownModules = Record<string, string>

const POLICY_LOCALE_ALIASES: Record<string, PolicyLocale> = {
  zh: 'zh-CN',
  'zh-cn': 'zh-CN',
  'zh-hans': 'zh-CN',
  'zh-hant': 'zh-CN',
  en: 'en-US',
  'en-us': 'en-US',
  'en-gb': 'en-US'
}

const RAW_POLICY_MARKDOWN_MODULES = import.meta.glob('./**/*.md', {
  eager: true,
  query: '?raw',
  import: 'default'
}) as RawPolicyMarkdownModules

function normalizeLocaleToken(value: string): string {
  return String(value || '')
    .trim()
    .replace(/_/g, '-')
    .toLowerCase()
}

function isPolicyLocale(value: string): value is PolicyLocale {
  return (POLICY_LOCALES as readonly string[]).includes(value)
}

function isPolicyDocumentKey(value: string): value is PolicyDocumentKey {
  return (POLICY_DOCUMENT_KEYS as readonly string[]).includes(value)
}

function parsePolicyDocumentPath(path: string): { locale: PolicyLocale; key: PolicyDocumentKey } | null {
  const match = path.match(/^\.\/([^/]+)\/([^/]+)\.md$/)
  if (!match) {
    return null
  }

  const locale = match[1]
  const key = match[2]

  if (!isPolicyLocale(locale) || !isPolicyDocumentKey(key)) {
    return null
  }

  return { locale, key }
}

function createPolicyDocument(locale: PolicyLocale, key: PolicyDocumentKey, content: string): PolicyDocument {
  const normalizedContent = String(content ?? '')
  const isEmpty = normalizedContent.trim().length === 0

  return {
    locale,
    key,
    content: normalizedContent,
    isEmpty,
    hidden: isEmpty
  }
}

function buildPolicyDocumentRegistry(): Record<PolicyLocale, Record<PolicyDocumentKey, PolicyDocument>> {
  const registry: Record<PolicyLocale, Record<PolicyDocumentKey, PolicyDocument>> = {
    'zh-CN': {} as Record<PolicyDocumentKey, PolicyDocument>,
    'en-US': {} as Record<PolicyDocumentKey, PolicyDocument>
  }

  for (const [path, content] of Object.entries(RAW_POLICY_MARKDOWN_MODULES)) {
    const meta = parsePolicyDocumentPath(path)
    if (!meta) {
      continue
    }

    registry[meta.locale][meta.key] = createPolicyDocument(meta.locale, meta.key, content)
  }

  return registry
}

export const policyDocumentRegistry = buildPolicyDocumentRegistry()

export function normalizePolicyLocale(locale?: string | null): PolicyLocale {
  const normalized = normalizeLocaleToken(locale ?? '')
  if (!normalized) {
    return DEFAULT_POLICY_LOCALE
  }

  if (normalized in POLICY_LOCALE_ALIASES) {
    return POLICY_LOCALE_ALIASES[normalized]
  }

  const language = normalized.split('-')[0]
  if (language === 'zh') {
    return 'zh-CN'
  }
  if (language === 'en') {
    return 'en-US'
  }

  return DEFAULT_POLICY_LOCALE
}

export function getPolicyDocument(locale: string | null | undefined, key: PolicyDocumentKey): PolicyDocument {
  const resolvedLocale = normalizePolicyLocale(locale)
  const localizedDocument = policyDocumentRegistry[resolvedLocale][key]
  if (localizedDocument) {
    return localizedDocument
  }

  const defaultDocument = policyDocumentRegistry[DEFAULT_POLICY_LOCALE][key]
  if (defaultDocument) {
    return defaultDocument
  }

  return createPolicyDocument(resolvedLocale, key, '')
}

export function listPolicyDocuments(locale: string | null | undefined): PolicyDocument[] {
  return POLICY_DOCUMENT_KEYS.map((key) => getPolicyDocument(locale, key))
}
