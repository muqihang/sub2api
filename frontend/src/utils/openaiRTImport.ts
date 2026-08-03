export interface OpenAIImportAccountLike {
  id?: number
  credentials?: {
    refresh_token?: string
    access_token?: string
    [key: string]: unknown
  } | null
}

export interface OpenAIImportTokenLike {
  email?: string
  chatgpt_account_id?: string
  chatgpt_user_id?: string
  organization_id?: string
  [key: string]: unknown
}

export function normalizeOpenAIRTInput(raw: string): string[] {
  const seen = new Set<string>()
  const tokens: string[] = []
  raw.split(/\r?\n/).forEach((line) => {
    const token = line.trim()
    if (!token || seen.has(token)) return
    seen.add(token)
    tokens.push(token)
  })
  return tokens
}

export function findExistingOpenAIAccountByRefreshToken<T extends OpenAIImportAccountLike>(
  accounts: T[],
  refreshToken: string
): T | undefined {
  const target = refreshToken.trim()
  return accounts.find((account) => String(account.credentials?.refresh_token || '').trim() === target)
}

export function findExistingOpenAIAccountByAccessToken<T extends OpenAIImportAccountLike>(
  accounts: T[],
  accessToken: string
): T | undefined {
  const target = accessToken.trim()
  return accounts.find((account) => String(account.credentials?.access_token || '').trim() === target)
}

export interface ParsedOpenAIAccessTokenEntry {
  accessToken: string
  refreshToken: string
}

export function parseOpenAIAccessTokenInput(raw: string): ParsedOpenAIAccessTokenEntry[] {
  const seen = new Set<string>()
  const entries: ParsedOpenAIAccessTokenEntry[] = []
  raw.split(/\r?\n/).forEach((line) => {
    const trimmed = line.trim()
    if (!trimmed) return
    const parts = trimmed.includes('----') ? trimmed.split(/----(.+)/, 2) : [trimmed, '']
    const accessToken = String(parts[0] || '').trim()
    const refreshToken = String(parts[1] || '').trim()
    if (!accessToken) return
    const key = `${accessToken}\u0000${refreshToken}`
    if (seen.has(key)) return
    seen.add(key)
    entries.push({ accessToken, refreshToken })
  })
  return entries
}

export function buildImportedAccountName(
  tokenInfo: OpenAIImportTokenLike,
  index: number,
  now: Date = new Date(),
  fallbackPrefix: string = 'openai-oauth'
): string {
  const preferred = [
    tokenInfo.email,
    tokenInfo.chatgpt_account_id,
    tokenInfo.chatgpt_user_id,
    tokenInfo.organization_id
  ].map(value => String(value || '').trim()).find(Boolean)

  if (preferred) {
    return preferred
  }

  const pad = (value: number) => String(value).padStart(2, '0')
  const stamp = [
    now.getUTCFullYear(),
    pad(now.getUTCMonth() + 1),
    pad(now.getUTCDate())
  ].join('') + '-' + [
    pad(now.getUTCHours()),
    pad(now.getUTCMinutes()),
    pad(now.getUTCSeconds())
  ].join('')

  return `${fallbackPrefix}-${stamp}-${String(index).padStart(2, '0')}`
}
