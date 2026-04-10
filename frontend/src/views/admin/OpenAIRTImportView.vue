<template>
  <div class="space-y-6">
    <section class="rounded-2xl border border-gray-200 bg-white p-6 shadow-sm dark:border-dark-700 dark:bg-dark-800">
      <div class="flex flex-col gap-3">
        <span class="inline-flex w-fit items-center rounded-full border border-emerald-500/20 bg-emerald-500/10 px-3 py-1 text-xs font-semibold text-emerald-600 dark:text-emerald-300">
          目标导入实例：当前 18081 后台 API
        </span>
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">OpenAI Token 导入</h1>
          <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">
            支持两种模式：<span class="font-semibold">RT 导入</span> 与 <span class="font-semibold">AT 导入</span>。已存在账号会自动更新，不会重复创建。
          </p>
        </div>
      </div>
    </section>

    <section class="grid gap-6 xl:grid-cols-[minmax(0,1.1fr)_420px]">
      <div class="rounded-2xl border border-gray-200 bg-white p-6 shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <div class="flex items-center justify-between">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">输入 Token</h2>
          <span class="text-xs text-gray-500 dark:text-gray-400">自动去重，支持批量导入</span>
        </div>
        <div class="mt-4 inline-flex rounded-2xl border border-gray-200 bg-gray-50 p-1 dark:border-dark-600 dark:bg-dark-900">
          <button
            type="button"
            class="rounded-xl px-4 py-2 text-sm font-semibold transition"
            :class="currentMode === 'rt' ? 'bg-primary-600 text-white shadow-sm' : 'text-gray-600 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white'"
            @click="switchMode('rt')"
          >
            RT 导入
          </button>
          <button
            type="button"
            class="rounded-xl px-4 py-2 text-sm font-semibold transition"
            :class="currentMode === 'at' ? 'bg-primary-600 text-white shadow-sm' : 'text-gray-600 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white'"
            @click="switchMode('at')"
          >
            AT 导入
          </button>
        </div>
        <p class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ modeHint }}</p>
        <textarea
          v-model="rawTokens"
          class="mt-4 min-h-[360px] w-full rounded-2xl border border-gray-200 bg-gray-50 p-4 font-mono text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-900 dark:text-white"
          :placeholder="textareaPlaceholder"
        />
        <div class="mt-4 flex flex-wrap items-center gap-3">
          <label class="inline-flex items-center gap-2 rounded-xl border border-gray-200 px-3 py-2 text-sm text-gray-700 dark:border-dark-600 dark:text-gray-200">
            <input v-model="validateOnly" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span>仅验证，不导入</span>
          </label>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="running"
            @click="pasteFromClipboard"
          >
            从剪贴板粘贴
          </button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="running || normalizedTokens.length === 0"
            @click="runImport"
          >
            {{ running ? '执行中…' : '开始执行' }}
          </button>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="running"
            @click="clearAll"
          >
            清空
          </button>
        </div>
      </div>

      <div class="space-y-4">
        <div class="rounded-2xl border border-gray-200 bg-white p-6 shadow-sm dark:border-dark-700 dark:bg-dark-800">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">执行概览</h2>
          <div
            class="mt-4 rounded-2xl border px-4 py-3 text-sm"
            :class="statusClass"
          >
            {{ statusText }}
          </div>
          <div class="mt-4 grid grid-cols-3 gap-3">
            <div class="rounded-2xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900">
              <div class="text-xs text-gray-500 dark:text-gray-400">总数</div>
              <div class="mt-2 text-2xl font-bold text-gray-900 dark:text-white">{{ summary.total }}</div>
            </div>
            <div class="rounded-2xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900">
              <div class="text-xs text-gray-500 dark:text-gray-400">成功</div>
              <div class="mt-2 text-2xl font-bold text-emerald-600 dark:text-emerald-300">{{ summary.success }}</div>
            </div>
            <div class="rounded-2xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900">
              <div class="text-xs text-gray-500 dark:text-gray-400">失败</div>
              <div class="mt-2 text-2xl font-bold text-rose-600 dark:text-rose-300">{{ summary.failed }}</div>
            </div>
          </div>
          <div class="mt-4 flex flex-wrap gap-3">
            <button type="button" class="btn btn-secondary" :disabled="!reportJson" @click="copyReport">
              复制 JSON 报告
            </button>
          </div>
        </div>

        <div class="rounded-2xl border border-gray-200 bg-white p-6 shadow-sm dark:border-dark-700 dark:bg-dark-800">
          <div class="flex items-center justify-between">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">执行日志</h2>
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ logs.length }} 条</span>
          </div>
          <pre class="mt-4 max-h-[280px] overflow-auto rounded-2xl border border-gray-200 bg-gray-50 p-4 text-xs leading-6 text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-200">{{ renderedLogs }}</pre>
        </div>
      </div>
    </section>

    <section class="rounded-2xl border border-gray-200 bg-white p-6 shadow-sm dark:border-dark-700 dark:bg-dark-800">
      <div class="flex items-center justify-between gap-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">明细结果</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">成功项会显示创建 / 更新动作和账号 ID；AT-only 会提示无法自动续期。</p>
        </div>
        <div class="text-xs text-gray-500 dark:text-gray-400">默认 client_id：{{ openAIClientId }}</div>
      </div>
      <div class="mt-4 overflow-x-auto">
        <table class="min-w-full text-left text-sm">
          <thead>
            <tr class="border-b border-gray-200 text-xs uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:text-gray-400">
              <th class="px-3 py-3 font-medium">状态</th>
              <th class="px-3 py-3 font-medium">Token</th>
              <th class="px-3 py-3 font-medium">动作</th>
              <th class="px-3 py-3 font-medium">账号</th>
              <th class="px-3 py-3 font-medium">说明</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="results.length === 0">
              <td colspan="5" class="px-3 py-8 text-center text-sm text-gray-500 dark:text-gray-400">
                暂无结果，开始执行后会显示在这里。
              </td>
            </tr>
            <tr
              v-for="item in results"
              :key="`${item.refreshToken}-${item.action}`"
              class="border-b border-gray-100 last:border-none dark:border-dark-700/60"
            >
              <td class="px-3 py-3 align-top">
                <span
                  class="inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold"
                  :class="item.success ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-300' : 'bg-rose-500/10 text-rose-600 dark:text-rose-300'"
                >
                  {{ item.success ? '成功' : '失败' }}
                </span>
              </td>
              <td class="px-3 py-3 font-mono text-xs text-gray-700 dark:text-gray-200">{{ item.preview }}</td>
              <td class="px-3 py-3 text-gray-700 dark:text-gray-200">{{ item.action }}</td>
              <td class="px-3 py-3 text-gray-700 dark:text-gray-200">
                <template v-if="item.accountId">#{{ item.accountId }}</template>
                <span v-if="item.accountName">{{ item.accountName }}</span>
                <span v-if="!item.accountId && !item.accountName">-</span>
              </td>
              <td class="px-3 py-3 text-gray-500 dark:text-gray-400">{{ item.message }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { adminAPI } from '@/api/admin'
import type { Account } from '@/types'
import { useAppStore } from '@/stores/app'
import {
  buildImportedAccountName,
  findExistingOpenAIAccountByAccessToken,
  findExistingOpenAIAccountByRefreshToken,
  normalizeOpenAIRTInput,
  parseOpenAIAccessTokenInput
} from '@/utils/openaiRTImport'

const appStore = useAppStore()
const openAIClientId = 'app_LlGpXReQgckcGGUo2JrYvtJK'
const currentMode = ref<'rt' | 'at'>('rt')

interface ImportResultRow {
  refreshToken: string
  preview: string
  success: boolean
  action: string
  accountId?: number
  accountName?: string
  message: string
}

interface TokenInfoRecord extends Record<string, unknown> {
  access_token?: string
  refresh_token?: string
  expires_at?: number | string
  expires_in?: number
  client_id?: string
  id_token?: string
  scope?: string
  email?: string
  chatgpt_account_id?: string
  chatgpt_user_id?: string
  organization_id?: string
  plan_type?: string
}

const rawTokens = ref('')
const validateOnly = ref(false)
const running = ref(false)
const logs = ref<string[]>(['等待开始。'])
const results = ref<ImportResultRow[]>([])
const statusText = ref('等待开始。')
const statusKind = ref<'info' | 'success' | 'error'>('info')
const reportJson = ref('')
const normalizedRTTokens = computed(() => normalizeOpenAIRTInput(rawTokens.value))
const normalizedATEntries = computed(() => parseOpenAIAccessTokenInput(rawTokens.value))
const normalizedTokens = computed(() => currentMode.value === 'rt' ? normalizedRTTokens.value : normalizedATEntries.value)
const normalizedTokenCount = computed(() => currentMode.value === 'rt' ? normalizedRTTokens.value.length : normalizedATEntries.value.length)
const summary = computed(() => ({
  total: results.value.length,
  success: results.value.filter(item => item.success).length,
  failed: results.value.filter(item => !item.success).length
}))
const renderedLogs = computed(() => logs.value.join('\n'))
const statusClass = computed(() => {
  if (statusKind.value === 'success') {
    return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
  }
  if (statusKind.value === 'error') {
    return 'border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300'
  }
  return 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300'
})
const modeHint = computed(() => currentMode.value === 'rt'
  ? 'RT 模式：每行一个 refresh token，系统会先验证，再导入或更新账号。'
  : 'AT 模式：每行一个 access token；如果同时带 RT，请使用 `AT----RT`。AT-only 导入后无法自动续期。')
const textareaPlaceholder = computed(() => currentMode.value === 'rt'
  ? 'rt_xxx\nrt_yyy\nrt_zzz'
  : 'eyJhb...\neyJhb_access----rt_xxx\neyJhb_access_2')

const appendLog = (message: string) => {
  logs.value = [...logs.value, `[${new Date().toLocaleTimeString()}] ${message}`]
}

const previewToken = (refreshToken: string): string => {
  if (refreshToken.length <= 18) return refreshToken
  return `${refreshToken.slice(0, 8)}...${refreshToken.slice(-6)}`
}

const buildCredentials = (tokenInfo: TokenInfoRecord): Record<string, unknown> => {
  const credentials: Record<string, unknown> = {
    access_token: tokenInfo.access_token
  }
  if (tokenInfo.refresh_token) credentials.refresh_token = tokenInfo.refresh_token
  if (tokenInfo.refresh_token) credentials.client_id = openAIClientId
  if (tokenInfo.expires_at !== undefined) credentials.expires_at = tokenInfo.expires_at
  if (tokenInfo.expires_in !== undefined) credentials.expires_in = tokenInfo.expires_in
  if (tokenInfo.id_token) credentials.id_token = tokenInfo.id_token
  if (tokenInfo.scope) credentials.scope = tokenInfo.scope
  if (tokenInfo.email) credentials.email = tokenInfo.email
  if (tokenInfo.chatgpt_account_id) credentials.chatgpt_account_id = tokenInfo.chatgpt_account_id
  if (tokenInfo.chatgpt_user_id) credentials.chatgpt_user_id = tokenInfo.chatgpt_user_id
  if (tokenInfo.organization_id) credentials.organization_id = tokenInfo.organization_id
  if (tokenInfo.plan_type) credentials.plan_type = tokenInfo.plan_type
  return credentials
}

const decodeJwtPayloadUnverified = (token: string): Record<string, unknown> => {
  const parts = token.split('.')
  if (parts.length !== 3 || !parts[1]) return {}
  try {
    const normalized = parts[1].replace(/-/g, '+').replace(/_/g, '/')
    const padded = normalized + '='.repeat((4 - normalized.length % 4) % 4)
    return JSON.parse(atob(padded))
  } catch {
    return {}
  }
}

const buildTokenInfoFromAccessToken = (accessToken: string, refreshToken: string): TokenInfoRecord => {
  const claims = decodeJwtPayloadUnverified(accessToken)
  const exp = typeof claims.exp === 'number' ? claims.exp : null
  const expiresAt = exp ? new Date(exp * 1000).toISOString() : new Date(Date.now() + 55 * 60 * 1000).toISOString()
  return {
    access_token: accessToken,
    refresh_token: refreshToken,
    expires_at: expiresAt,
    email: typeof claims.email === 'string' ? claims.email : undefined,
    chatgpt_account_id: typeof claims.chatgpt_account_id === 'string' ? claims.chatgpt_account_id : undefined,
    chatgpt_user_id: typeof claims.chatgpt_user_id === 'string' ? claims.chatgpt_user_id : undefined,
    organization_id: typeof claims.organization_id === 'string' ? claims.organization_id : undefined
  }
}

const loadAllOpenAIOAuthAccounts = async (): Promise<Account[]> => {
  const accounts: Account[] = []
  let page = 1
  let totalPages = 1
  do {
    const response = await adminAPI.accounts.list(page, 200, {
      platform: 'openai',
      type: 'oauth',
      lite: 'false'
    })
    accounts.push(...response.items)
    totalPages = response.pages || 1
    page += 1
  } while (page <= totalPages)
  return accounts
}

const mergeCredentials = (existing: Account, nextCredentials: Record<string, unknown>) => ({
  ...(existing.credentials || {}),
  ...nextCredentials
})

const setStatus = (message: string, kind: 'info' | 'success' | 'error') => {
  statusText.value = message
  statusKind.value = kind
}

const switchMode = (mode: 'rt' | 'at') => {
  currentMode.value = mode
  setStatus(mode === 'rt' ? '当前模式：RT 导入。' : '当前模式：AT 导入。', 'info')
}

const pasteFromClipboard = async () => {
  try {
    rawTokens.value = await navigator.clipboard.readText()
    setStatus('已从剪贴板读取内容。', 'info')
  } catch (error: any) {
    const message = error?.message || String(error)
    appStore.showError(message)
    setStatus(`读取剪贴板失败：${message}`, 'error')
  }
}

const clearAll = () => {
  rawTokens.value = ''
  logs.value = ['等待开始。']
  results.value = []
  reportJson.value = ''
  setStatus('输入已清空。', 'info')
}

const copyReport = async () => {
  if (!reportJson.value) return
  await navigator.clipboard.writeText(reportJson.value)
  appStore.showSuccess('JSON 报告已复制到剪贴板')
}

const runImport = async () => {
  if (normalizedTokenCount.value === 0) {
    setStatus('请先输入至少一条 token。', 'error')
    return
  }

  running.value = true
  logs.value = ['开始执行导入流程。']
  results.value = []
  reportJson.value = ''
  setStatus('正在执行，请稍候…', 'info')

  try {
    const existingAccounts = validateOnly.value ? [] : await loadAllOpenAIOAuthAccounts()
    appendLog(`已加载 ${existingAccounts.length} 个现有 OpenAI OAuth 账号`)

    if (currentMode.value === 'rt') {
      for (const [index, refreshToken] of normalizedRTTokens.value.entries()) {
        const preview = previewToken(refreshToken)
        try {
          appendLog(`正在验证 ${preview}`)
          const tokenInfo = await adminAPI.accounts.refreshOpenAIToken(
            refreshToken,
            null,
            '/admin/openai/refresh-token',
            openAIClientId
          ) as TokenInfoRecord

          const accountName = buildImportedAccountName(tokenInfo, index + 1)

          if (validateOnly.value) {
            results.value.push({
              refreshToken,
              preview,
              success: true,
              action: 'validated',
              accountName,
              message: '验证成功，未导入'
            })
            appendLog(`${preview} 验证成功`)
            continue
          }

          const credentials = buildCredentials(tokenInfo)
          const existing = findExistingOpenAIAccountByRefreshToken(existingAccounts, refreshToken)
          if (existing?.id) {
            const updated = await adminAPI.accounts.update(existing.id, {
              name: accountName,
              credentials: mergeCredentials(existing, credentials),
              confirm_mixed_channel_risk: true
            })
            results.value.push({
              refreshToken,
              preview,
              success: true,
              action: 'updated',
              accountId: updated.id,
              accountName: updated.name,
              message: '已更新现有账号'
            })
            appendLog(`${preview} 已更新账号 #${updated.id}`)
          } else {
            const created = await adminAPI.accounts.create({
              name: accountName,
              platform: 'openai',
              type: 'oauth',
              credentials,
              concurrency: 1,
              priority: 0,
              confirm_mixed_channel_risk: true
            })
            existingAccounts.push(created)
            results.value.push({
              refreshToken,
              preview,
              success: true,
              action: 'created',
              accountId: created.id,
              accountName: created.name,
              message: '已创建新账号'
            })
            appendLog(`${preview} 已创建账号 #${created.id}`)
          }
        } catch (error: any) {
          const message = error?.response?.data?.detail || error?.message || String(error)
          results.value.push({
            refreshToken,
            preview,
            success: false,
            action: 'failed',
            message
          })
          appendLog(`${preview} 失败：${message}`)
        }
      }
    } else {
      for (const [index, entry] of normalizedATEntries.value.entries()) {
        const preview = previewToken(entry.accessToken)
        try {
          appendLog(`正在解析 ${preview}`)
          const tokenInfo = buildTokenInfoFromAccessToken(entry.accessToken, entry.refreshToken)
          const accountName = buildImportedAccountName(tokenInfo, index + 1)

          if (validateOnly.value) {
            results.value.push({
              refreshToken: entry.accessToken,
              preview,
              success: true,
              action: 'validated',
              accountName,
              message: entry.refreshToken ? 'AT+RT 解析成功，未导入' : 'AT-only 解析成功，未导入'
            })
            appendLog(`${preview} 解析成功`)
            continue
          }

          const credentials = buildCredentials(tokenInfo)
          const existing = entry.refreshToken
            ? findExistingOpenAIAccountByRefreshToken(existingAccounts, entry.refreshToken)
            : findExistingOpenAIAccountByAccessToken(existingAccounts, entry.accessToken)

          if (existing?.id) {
            const updated = await adminAPI.accounts.update(existing.id, {
              name: accountName,
              credentials: mergeCredentials(existing, credentials),
              confirm_mixed_channel_risk: true
            })
            results.value.push({
              refreshToken: entry.accessToken,
              preview,
              success: true,
              action: 'updated',
              accountId: updated.id,
              accountName: updated.name,
              message: entry.refreshToken ? '已用 AT+RT 更新现有账号' : '已用 AT-only 更新现有账号（无自动续期）'
            })
            appendLog(`${preview} 已更新账号 #${updated.id}`)
          } else {
            const created = await adminAPI.accounts.create({
              name: accountName,
              platform: 'openai',
              type: 'oauth',
              credentials,
              concurrency: 1,
              priority: 0,
              confirm_mixed_channel_risk: true
            })
            existingAccounts.push(created)
            results.value.push({
              refreshToken: entry.accessToken,
              preview,
              success: true,
              action: 'created',
              accountId: created.id,
              accountName: created.name,
              message: entry.refreshToken ? '已用 AT+RT 创建新账号' : '已用 AT-only 创建新账号（无自动续期）'
            })
            appendLog(`${preview} 已创建账号 #${created.id}`)
          }
        } catch (error: any) {
          const message = error?.response?.data?.detail || error?.message || String(error)
          results.value.push({
            refreshToken: entry.accessToken,
            preview,
            success: false,
            action: 'failed',
            message
          })
          appendLog(`${preview} 失败：${message}`)
        }
      }
    }

    reportJson.value = JSON.stringify({
      generated_at: new Date().toISOString(),
      validate_only: validateOnly.value,
      summary: summary.value,
      results: results.value
    }, null, 2)

    if (summary.value.failed > 0) {
      setStatus(`执行完成：成功 ${summary.value.success}，失败 ${summary.value.failed}。`, 'info')
      appStore.showWarning(`执行完成：有 ${summary.value.failed} 条失败`)
    } else {
      setStatus(`执行完成：全部 ${summary.value.success} 条成功。`, 'success')
      appStore.showSuccess(`执行完成：共处理 ${summary.value.success} 条`)
    }
  } catch (error: any) {
    const message = error?.response?.data?.detail || error?.message || String(error)
    appendLog(`流程中断：${message}`)
    setStatus(`执行失败：${message}`, 'error')
    appStore.showError(message)
  } finally {
    running.value = false
  }
}
</script>
