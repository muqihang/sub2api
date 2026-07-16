<template>
  <div class="mx-auto max-w-5xl space-y-6 p-4 md:p-6">
    <div class="rounded-xl border border-blue-200 bg-blue-50 p-4 text-blue-900 dark:border-blue-800 dark:bg-blue-950/30 dark:text-blue-100">
      <h2 class="text-xl font-semibold">Claude 订阅号池上号向导</h2>
      <p class="mt-2 text-sm">独立正式号池流程：代理 -> 同出口确认 -> OAuth/Setup Token -> refresh-only -> runtime 注册 -> 定向健康检查 -> warming -> production。</p>
      <p class="mt-2 text-xs">Setup Token 登录态只提交给后端换取 inference token，不在页面回显、不进入 Safe summary；健康检查会由管理员显式触发一次极小真实 messages，并强制经过 CC Gateway 与 raw capture。</p>
    </div>

    <section class="rounded-xl border bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
      <h3 class="font-semibold">1. 代理/IP 与账号策略</h3>
      <div class="mt-4 grid gap-3 md:grid-cols-2">
        <label class="text-sm">代理模式
          <select v-model="form.proxy_mode" class="input mt-1 w-full">
            <option value="existing">选择已有代理</option>
            <option value="create">创建新代理</option>
          </select>
        </label>
        <label v-if="form.proxy_mode === 'existing'" class="text-sm">已有代理 ID
          <input v-model.number="form.proxy_id" type="number" min="1" class="input mt-1 w-full" />
        </label>
        <template v-else>
          <label class="text-sm">代理名称（仅后台备注）
            <input v-model="proxy.name" class="input mt-1 w-full" placeholder="例如：claude-01-38.75.201.125" />
          </label>
          <label class="text-sm">协议
            <select v-model="proxy.protocol" class="input mt-1 w-full">
              <option value="socks5">socks5（后端按远程 DNS 语义规范化）</option>
              <option value="socks5h">socks5h</option>
              <option value="http">http</option>
              <option value="https">https</option>
            </select>
          </label>
          <label class="text-sm">代理服务器地址 / IP（Host）
            <input v-model="proxy.host" class="input mt-1 w-full" autocomplete="off" placeholder="例如：38.75.201.125" />
          </label>
          <label class="text-sm">代理端口（Port）
            <input v-model.number="proxy.port" type="number" class="input mt-1 w-full" placeholder="例如：443" />
          </label>
          <label class="text-sm">代理用户名（Username）
            <input v-model="proxy.username" class="input mt-1 w-full" autocomplete="off" placeholder="代理商提供的用户名" />
          </label>
          <label class="text-sm">代理密码（Password，只提交，不回显）
            <input v-model="proxy.password" type="password" class="input mt-1 w-full" autocomplete="new-password" placeholder="代理商提供的密码" />
          </label>
        </template>
        <label class="text-sm">Claude Code 专用分组 ID
          <input v-model.number="form.group_id" type="number" min="1" class="input mt-1 w-full" placeholder="只让 Claude Code/API Key 用户从这个分组调度" />
        </label>
        <label class="text-sm">账号名称
          <input v-model="form.account_name" class="input mt-1 w-full" placeholder="例如：claude-oauth-01" />
        </label>
        <label class="text-sm">用量策略
          <select v-model="form.pool_profile" class="input mt-1 w-full">
            <option value="normal">正常策略：7 天平滑消耗，尽量用完但优先稳定</option>
            <option value="aggressive">激进策略（速刷）：约 3 天用到 95%-100%，不降低安全线</option>
          </select>
        </label>
        <label class="text-sm">账号并发上限
          <input v-model.number="form.concurrency" type="number" min="1" max="10" class="input mt-1 w-full" />
        </label>
      </div>
      <button class="btn btn-primary mt-4" :disabled="busy || !canStart" @click="start">创建 onboarding session</button>
    </section>

    <section v-if="session" class="rounded-xl border bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
      <h3 class="font-semibold">2. 代理测试与同出口确认</h3>
      <dl class="mt-3 grid gap-2 text-sm md:grid-cols-2">
        <div><dt class="text-gray-500">Session</dt><dd>{{ session.id }}</dd></div>
        <div><dt class="text-gray-500">状态</dt><dd>{{ session.status }}</dd></div>
        <div><dt class="text-gray-500">Proxy ref</dt><dd class="break-all">{{ session.proxy_ref || '-' }}</dd></div>
        <div><dt class="text-gray-500">Egress bucket</dt><dd>{{ session.egress_bucket || '-' }}</dd></div>
      </dl>
      <div class="mt-4 flex flex-wrap gap-2">
        <button class="btn btn-secondary" :disabled="busy" @click="testProxyStep">测试代理</button>
        <a v-if="session.browser_egress_check_url" class="btn btn-secondary" :href="session.browser_egress_check_url" target="_blank" rel="noreferrer">打开 browser-egress-check</a>
      </div>
      <div class="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-100">
        必须在即将登录 Claude 的同出口浏览器打开校验 URL。若自动匹配不可用，需要完成不可绕过人工 attestation；未确认前无法生成 OAuth URL。
      </div>
      <div class="mt-3 flex gap-2">
        <input v-model="attestationCode" class="input flex-1" placeholder="填写脱敏校验码/人工 attestation 记录" />
        <button class="btn btn-secondary" :disabled="busy || !attestationCode" @click="attest">确认同出口</button>
      </div>
    </section>

    <section v-if="session" class="rounded-xl border bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
      <h3 class="font-semibold">3. 授权与创建账号</h3>
      <div class="mt-3 flex flex-wrap gap-3 text-sm">
        <label class="inline-flex items-center gap-2">
          <input v-model="authMode" type="radio" value="oauth" />
          <span>OAuth URL（完整 Claude Code OAuth）</span>
        </label>
        <label class="inline-flex items-center gap-2">
          <input v-model="authMode" type="radio" value="setup-token-cookie" />
          <span>Setup Token 登录态（sk-ant-sid，仅换 inference token）</span>
        </label>
      </div>
      <div v-if="authMode === 'oauth'" class="mt-3 space-y-2">
        <button class="btn btn-secondary" :disabled="busy || !session.browser_egress_verified" @click="generateOAuth">生成 OAuth URL</button>
        <div v-if="session.auth_url" class="space-y-2">
          <a class="break-all text-blue-600 underline" :href="session.auth_url" target="_blank" rel="noreferrer">{{ session.auth_url }}</a>
          <textarea v-model="oauthCode" class="input h-24 w-full" placeholder="粘贴授权 code；不会保存或回显 token"></textarea>
          <button class="btn btn-primary" :disabled="busy || !oauthCode" @click="exchangeCreate">Exchange code 并创建不可调度账号</button>
        </div>
      </div>
      <div v-else class="mt-3 space-y-2">
        <input v-model="setupSessionKey" type="password" class="input w-full" autocomplete="new-password" placeholder="粘贴 sk-ant-sid 登录态；只提交给后端换取 setup-token，不回显、不进入 Safe summary" />
        <button class="btn btn-primary" :disabled="busy || !session.browser_egress_verified || !setupSessionKey" @click="setupTokenCreate">导入 setup-token 并创建不可调度账号</button>
      </div>
    </section>

    <section v-if="session?.account_id" class="rounded-xl border bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
      <h3 class="font-semibold">4. Refresh / Runtime / Healthcheck / Warming / Production</h3>
      <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">账号创建后仍不可调度。必须 refresh-only 成功、runtime 注册成功、定向健康检查 200 且确认 CC Gateway/raw capture 后，才可进入 warming；production 需要单独 promote。</p>
      <div class="mt-3 flex flex-wrap gap-2">
        <button class="btn btn-secondary" :disabled="busy" @click="refreshOnlyStep">Refresh-only</button>
        <button class="btn btn-secondary" :disabled="busy" @click="runtimeRegisterStep">Runtime 注册</button>
        <button class="btn btn-secondary" :disabled="busy" @click="healthcheckStep">定向健康检查（一次极小真实 messages）</button>
        <button class="btn btn-primary" :disabled="busy || acceptance?.status !== 'healthcheck_passed'" @click="startWarmingStep">进入 warming（low weight）</button>
        <button class="btn btn-primary" :disabled="busy || session.status !== 'warming'" @click="promoteProductionStep">Promote production</button>
      </div>
      <div v-if="acceptance" class="mt-3 grid gap-2 rounded bg-gray-50 p-3 text-xs dark:bg-gray-950 md:grid-cols-2">
        <div>Status: {{ acceptance.status }}</div>
        <div>Status bucket: {{ acceptance.status_code_bucket || '-' }}</div>
        <div>CC Gateway seen: {{ acceptance.cc_gateway_seen ? 'yes' : 'no' }}</div>
        <div>Raw capture: {{ acceptance.raw_capture_present ? 'yes' : 'no' }}</div>
        <div>Fallback: {{ acceptance.fallback_detected ? 'yes' : 'no' }}</div>
        <div>Proxy mismatch: {{ acceptance.proxy_mismatch ? 'yes' : 'no' }}</div>
      </div>
      <ul v-if="acceptance?.checks?.length" class="mt-3 space-y-1 text-sm">
        <li v-for="check in acceptance.checks" :key="check.name" :class="check.status === 'pass' ? 'text-green-700' : check.status === 'warn' ? 'text-amber-700' : 'text-red-700'">
          {{ check.status.toUpperCase() }} · {{ check.name }} <span v-if="check.message">- {{ check.message }}</span>
        </li>
      </ul>
      <p v-if="session.status === 'production'" class="mt-3 rounded bg-green-50 p-3 text-green-800">production：账号已通过硬门禁；requested aggressive 只在此阶段后才允许生效。</p>
    </section>

    <section v-if="session" class="rounded-xl border bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
      <h3 class="font-semibold">Safe summary</h3>
      <pre class="mt-2 max-h-64 overflow-auto rounded bg-gray-100 p-3 text-xs dark:bg-gray-950">{{ safeSession }}</pre>
    </section>

    <p v-if="error" class="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import claudeOnboarding, { type FormalPoolAcceptanceResult, type FormalPoolSession, type FormalPoolProfile, type FormalPoolProxyMode } from '@/api/admin/claudeOnboarding'

const busy = ref(false)
const error = ref('')
const session = ref<FormalPoolSession | null>(null)
const acceptance = ref<FormalPoolAcceptanceResult | null>(null)
const authMode = ref<'oauth' | 'setup-token-cookie'>('oauth')
const oauthCode = ref('')
const setupSessionKey = ref('')
const attestationCode = ref('')
const createIdempotencyKey = ref('')
const exchangeIdempotencyKey = ref('')
const promoteIdempotencyKey = ref('')
const form = reactive<{ proxy_mode: FormalPoolProxyMode; proxy_id?: number; group_id?: number; account_name: string; pool_profile: FormalPoolProfile; concurrency: number }>({ proxy_mode: 'existing', proxy_id: undefined, group_id: undefined, account_name: '', pool_profile: 'normal', concurrency: 10 })
const proxy = reactive({ name: '', protocol: 'socks5' as 'http' | 'https' | 'socks5' | 'socks5h', host: '', port: 1080, username: '', password: '' })
const canStart = computed(() => !!form.group_id && !!form.account_name && (form.proxy_mode === 'existing' ? !!form.proxy_id : !!proxy.host && !!proxy.port))
const safeSession = computed(() => JSON.stringify({
  safe_summary: session.value?.safe_summary || {},
  status: session.value?.status,
  proxy_ref: session.value?.proxy_ref,
  egress_bucket: session.value?.egress_bucket,
  pool_profile: session.value?.pool_profile,
  browser_egress_verified: session.value?.browser_egress_verified,
  cc_gateway_runtime_registered: session.value?.cc_gateway_runtime_registered,
  healthcheck_passed: session.value?.healthcheck_passed,
  production_ready: session.value?.production_ready,
  account_ref: session.value?.account_ref,
  oauth_summary: session.value?.oauth_summary
}, null, 2))

async function run<T>(fn: () => Promise<T>): Promise<T | null> {
  busy.value = true
  error.value = ''
  try { return await fn() } catch (e: any) {
    const status = Number(e?.response?.status || 0)
    const id = session.value?.id
    if (id && (status === 409 || status >= 500 || status === 0)) {
      try { session.value = await claudeOnboarding.getSession(id) } catch { /* retain the last known safe snapshot */ }
    }
    error.value = e?.response?.data?.message || e?.message || '操作失败'
    return null
  } finally { busy.value = false }
}
async function start() {
  const payload: any = { ...form }
  if (form.proxy_mode === 'create') payload.proxy = { ...proxy }
  if (!createIdempotencyKey.value) createIdempotencyKey.value = crypto.randomUUID()
  const res = await run(() => claudeOnboarding.createSession(payload, createIdempotencyKey.value))
  if (res) { session.value = res; createIdempotencyKey.value = '' }
}
async function testProxyStep() { if (!session.value) return; const res = await run(() => claudeOnboarding.testProxy(session.value!)); if (res) session.value = res }
async function attest() { if (!session.value) return; const res = await run(() => claudeOnboarding.attestBrowserEgress(session.value!, attestationCode.value)); if (res) session.value = res }
async function generateOAuth() { if (!session.value) return; const res = await run(() => claudeOnboarding.generateAuthUrl(session.value!)); if (res) session.value = res }
async function exchangeCreate() { if (!session.value) return; if (!exchangeIdempotencyKey.value) exchangeIdempotencyKey.value = crypto.randomUUID(); const res = await run(() => claudeOnboarding.exchangeCodeAndCreate(session.value!, oauthCode.value, exchangeIdempotencyKey.value)); if (res) { session.value = res; exchangeIdempotencyKey.value = '' } }
async function setupTokenCreate() {
  if (!session.value) return
  const res = await run(() => claudeOnboarding.setupTokenCookieAuthAndCreate(session.value!, setupSessionKey.value))
  if (res) {
    session.value = res
    setupSessionKey.value = ''
  }
}
async function refreshOnlyStep() { if (!session.value) return; const res = await run(() => claudeOnboarding.refreshOnly(session.value!)); if (res) session.value = res }
async function runtimeRegisterStep() { if (!session.value) return; const res = await run(() => claudeOnboarding.runtimeRegister(session.value!)); if (res) session.value = res }
async function healthcheckStep() { if (!session.value) return; const res = await run(() => claudeOnboarding.healthcheck(session.value!)); if (res) { acceptance.value = res; session.value = { ...session.value!, version: res.version, status: res.status, healthcheck_passed: res.status === 'healthcheck_passed' } } }
async function startWarmingStep() { if (!session.value) return; const res = await run(() => claudeOnboarding.startWarming(session.value!)); if (res) session.value = res }
async function promoteProductionStep() { if (!session.value) return; if (!promoteIdempotencyKey.value) promoteIdempotencyKey.value = crypto.randomUUID(); const res = await run(() => claudeOnboarding.promoteProduction(session.value!, promoteIdempotencyKey.value)); if (res) { session.value = res; promoteIdempotencyKey.value = '' } }
</script>
