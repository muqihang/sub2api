<template>
  <div class="mx-auto max-w-5xl space-y-6 p-4 md:p-6">
    <div class="rounded-xl border border-blue-200 bg-blue-50 p-4 text-blue-900 dark:border-blue-800 dark:bg-blue-950/30 dark:text-blue-100">
      <h2 class="text-xl font-semibold">Claude 订阅号池上号向导</h2>
      <p class="mt-2 text-sm">独立正式号池流程：代理 -> 同出口确认 -> OAuth -> 创建不可调度账号 -> acceptance -> 手动激活。</p>
      <p class="mt-2 text-xs">不会展示或提交 Setup Token、cookie/sessionKey、TLS 指纹、CCH、自定义 base URL、cache TTL、session masking 或硬预算限制。</p>
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
      <h3 class="font-semibold">3. OAuth 与创建账号</h3>
      <button class="btn btn-secondary mt-3" :disabled="busy || !session.browser_egress_verified" @click="generateOAuth">生成 OAuth URL</button>
      <div v-if="session.auth_url" class="mt-3 space-y-2">
        <a class="break-all text-blue-600 underline" :href="session.auth_url" target="_blank" rel="noreferrer">{{ session.auth_url }}</a>
        <textarea v-model="oauthCode" class="input h-24 w-full" placeholder="粘贴授权 code；不会保存或回显 token"></textarea>
        <button class="btn btn-primary" :disabled="busy || !oauthCode" @click="exchangeCreate">Exchange code 并创建不可调度账号</button>
      </div>
    </section>

    <section v-if="session?.account_id" class="rounded-xl border bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
      <h3 class="font-semibold">4. Acceptance 与手动激活</h3>
      <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">Acceptance 前账号保持不可调度；全部 pass 后仍需手动激活。</p>
      <button class="btn btn-secondary mt-3" :disabled="busy" @click="acceptanceStep">运行 acceptance（不发真实 messages）</button>
      <ul v-if="acceptance?.checks?.length" class="mt-3 space-y-1 text-sm">
        <li v-for="check in acceptance.checks" :key="check.name" :class="check.status === 'pass' ? 'text-green-700' : check.status === 'warn' ? 'text-amber-700' : 'text-red-700'">
          {{ check.status.toUpperCase() }} · {{ check.name }} <span v-if="check.message">- {{ check.message }}</span>
        </li>
      </ul>
      <button class="btn btn-primary mt-4" :disabled="busy || acceptance?.status !== 'pending_activation'" @click="activateStep">手动激活 schedulable</button>
      <p v-if="session.status === 'ready_for_small_flow'" class="mt-3 rounded bg-green-50 p-3 text-green-800">ready_for_small_flow：可进入小流量准备；真实 smoke 仍需单独批准。</p>
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
const oauthCode = ref('')
const attestationCode = ref('')
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
  account_ref: session.value?.account_ref,
  oauth_summary: session.value?.oauth_summary
}, null, 2))

async function run<T>(fn: () => Promise<T>): Promise<T | null> {
  busy.value = true
  error.value = ''
  try { return await fn() } catch (e: any) { error.value = e?.response?.data?.message || e?.message || '操作失败'; return null } finally { busy.value = false }
}
async function start() {
  const payload: any = { ...form }
  if (form.proxy_mode === 'create') payload.proxy = { ...proxy }
  const res = await run(() => claudeOnboarding.createSession(payload))
  if (res) session.value = res
}
async function testProxyStep() { if (!session.value) return; const res = await run(() => claudeOnboarding.testProxy(session.value!.id)); if (res) session.value = res }
async function attest() { if (!session.value) return; const res = await run(() => claudeOnboarding.attestBrowserEgress(session.value!.id, attestationCode.value)); if (res) session.value = res }
async function generateOAuth() { if (!session.value) return; const res = await run(() => claudeOnboarding.generateAuthUrl(session.value!.id)); if (res) session.value = res }
async function exchangeCreate() { if (!session.value) return; const res = await run(() => claudeOnboarding.exchangeCodeAndCreate(session.value!.id, oauthCode.value)); if (res) session.value = res }
async function acceptanceStep() { if (!session.value) return; const res = await run(() => claudeOnboarding.runAcceptance(session.value!.id)); if (res) acceptance.value = res }
async function activateStep() { if (!session.value) return; const res = await run(() => claudeOnboarding.activate(session.value!.id)); if (res) session.value = res }
</script>
