<template>
  <div data-testid="onboarding-v2" class="mx-auto max-w-7xl p-4 md:p-6">
    <div class="overflow-hidden rounded-3xl border border-slate-200 bg-slate-950 text-white shadow-xl dark:border-slate-700">
      <div class="grid gap-0 lg:grid-cols-[18rem_1fr]">
        <aside class="border-b border-white/10 bg-gradient-to-b from-slate-900 via-indigo-950 to-slate-950 p-5 lg:border-b-0 lg:border-r">
          <p class="text-xs font-semibold uppercase tracking-[0.3em] text-cyan-300">上号向导（新版）</p>
          <h1 class="mt-3 text-2xl font-bold">Claude 正式号池上号向导</h1>
          <p class="mt-3 text-sm leading-6 text-slate-300">不限制 Claude Code 能力；OAuth 展示同出口、调度器接入、真实上游可用性检查与预热准入；Setup Token 只做代理健康和后续上号准入。</p>

          <nav class="mt-8 space-y-3" aria-label="Onboarding steps">
            <button
              v-for="step in steps"
              :key="step.key"
              type="button"
              :class="stepperButtonClass(step.key)"
              :data-testid="`stepper-${step.key}`"
              :data-step-status="getStepStatus(step.key)"
              :disabled="getStepStatus(step.key) === 'locked'"
              :aria-disabled="getStepStatus(step.key) === 'locked' ? 'true' : 'false'"
              :aria-current="getStepStatus(step.key) === 'active' ? 'step' : undefined"
              :title="getStepLockReason(step.key) || undefined"
              @click="onStepperClick(step.key)"
            >
              <span
                :class="stepperIconClass(step.key)"
                :data-testid="`stepper-icon-${step.key}`"
                :data-step-status="getStepStatus(step.key)"
                aria-hidden="true"
              >
                <svg
                  v-if="getStepStatus(step.key) === 'done'"
                  class="h-4 w-4"
                  viewBox="0 0 20 20"
                  fill="currentColor"
                  data-testid="stepper-icon-done"
                >
                  <path fill-rule="evenodd" d="M16.704 5.29a1 1 0 0 1 0 1.42l-7.5 7.5a1 1 0 0 1-1.42 0l-3.5-3.5a1 1 0 1 1 1.42-1.42l2.79 2.79 6.79-6.79a1 1 0 0 1 1.42 0Z" clip-rule="evenodd" />
                </svg>
                <svg
                  v-else-if="getStepStatus(step.key) === 'locked'"
                  class="h-4 w-4"
                  viewBox="0 0 20 20"
                  fill="currentColor"
                  data-testid="stepper-icon-locked"
                >
                  <path fill-rule="evenodd" d="M5 9V7a5 5 0 1 1 10 0v2h.5A1.5 1.5 0 0 1 17 10.5v6A1.5 1.5 0 0 1 15.5 18h-11A1.5 1.5 0 0 1 3 16.5v-6A1.5 1.5 0 0 1 4.5 9H5Zm2 0V7a3 3 0 0 1 6 0v2H7Z" clip-rule="evenodd" />
                </svg>
                <template v-else>{{ step.index }}</template>
              </span>
              <span class="min-w-0">
                <span class="block text-sm font-semibold">{{ step.title }}</span>
                <span class="mt-1 block text-xs text-slate-300">{{ step.caption }}</span>
                <span
                  v-if="getStepStatus(step.key) === 'locked'"
                  class="mt-1 block text-xs text-amber-200"
                  :data-testid="`stepper-lock-reason-${step.key}`"
                >
                  {{ getStepLockReason(step.key) }}
                </span>
              </span>
            </button>
          </nav>
        </aside>

        <main class="bg-slate-50 p-5 text-slate-900 dark:bg-slate-950 dark:text-slate-100 md:p-8">
          <section class="mb-5 rounded-3xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">状态机阶段</p>
                <h2 class="mt-1 text-xl font-bold">{{ currentStepTitle }}</h2>
              </div>
              <div class="rounded-full border border-slate-200 bg-slate-100 px-3 py-1 text-xs font-semibold dark:border-slate-700 dark:bg-slate-800">
                会话编号：<span data-testid="session-ref">{{ displaySessionRef }}</span>
              </div>
            </div>
            <div class="mt-4 grid gap-2 md:grid-cols-7">
              <div
                v-for="stage in stageList"
                :key="stage"
                :data-testid="`stage-${stage}`"
                class="rounded-2xl border px-3 py-2 text-xs font-semibold"
                :class="stageClass(stage)"
              >
                {{ stageLabel(stage) }}
              </div>
            </div>
            <p class="mt-3 text-sm text-slate-600 dark:text-slate-300">新号进入预热期时使用<strong>低权重</strong>；进入生产期后按正式权重生效；加速请求策略只表示请求节奏，不跳过上号检查。</p>
          </section>

          <section v-if="renderStep === 'proxy'" class="grid gap-4 xl:grid-cols-2">
            <div class="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
              <h3 class="text-lg font-bold">代理与出口设置</h3>
              <p class="mt-1 text-sm text-slate-600 dark:text-slate-300">先创建上号会话；OAuth 需要同出口浏览器校验，Setup Token 只要求代理健康通过。</p>
              <div class="mt-4 grid gap-3">
                <div class="rounded-2xl border border-slate-200 bg-slate-50 p-3 dark:border-slate-800 dark:bg-slate-950" data-testid="auth-mode-chooser">
                  <p class="text-sm font-semibold">授权方式</p>
                  <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">先选方式，向导会自动调整第 1 步门禁。</p>
                  <div class="mt-3 flex flex-wrap gap-4 text-sm">
                    <label class="inline-flex items-center gap-2"><input v-model="authMode" type="radio" value="oauth" /> OAuth 授权链接（需要同出口浏览器校验）</label>
                    <label class="inline-flex items-center gap-2"><input data-testid="auth-mode-setup-token" v-model="authMode" type="radio" value="setup-token-cookie" /> Setup Token（无需同出口浏览器校验）</label>
                  </div>
                </div>
                <label class="text-sm font-medium">代理模式
                  <select data-testid="proxy-mode-select" v-model="form.proxy_mode" class="input mt-1 w-full">
                    <option value="existing">选择已有代理</option>
                    <option value="create">创建新代理</option>
                  </select>
                </label>
                <div v-if="form.proxy_mode === 'existing'" class="space-y-3" data-testid="proxy-picker">
                  <div class="flex flex-wrap items-center justify-between gap-2">
                    <div>
                      <p class="text-sm font-semibold">选择已有代理</p>
                      <p class="text-xs text-slate-500 dark:text-slate-400">从代理池选择，系统内部写入 proxy_id。</p>
                    </div>
                    <div class="flex flex-wrap gap-2">
                      <button data-testid="refresh-proxies" type="button" class="btn btn-secondary btn-sm" :disabled="proxyListLoading" @click="loadProxies">刷新代理</button>
                      <RouterLink to="/admin/proxies" class="btn btn-secondary btn-sm">去代理管理添加 IP</RouterLink>
                    </div>
                  </div>
                  <div v-if="proxyListLoading" data-testid="proxy-list-loading" class="rounded-2xl border border-slate-200 p-3 text-sm text-slate-500 dark:border-slate-800 dark:text-slate-400">正在加载代理池...</div>
                  <div v-else-if="proxyListError" data-testid="proxy-list-error" class="rounded-2xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-100">
                    <p>{{ safeText(proxyListError) }}</p>
                    <button data-testid="reload-proxies" type="button" class="btn btn-secondary btn-sm mt-2" @click="loadProxies">重新加载代理</button>
                  </div>
                  <div v-else-if="proxyOptions.length === 0" data-testid="empty-proxy-list" class="rounded-2xl border border-dashed border-slate-300 p-3 text-sm text-slate-600 dark:border-slate-700 dark:text-slate-300">
                    <p class="font-semibold">暂无可选代理</p>
                    <p class="mt-1">请先去代理管理添加 IP，再回到这里刷新选择。</p>
                    <RouterLink to="/admin/proxies" class="btn btn-secondary btn-sm mt-3">去代理管理添加 IP</RouterLink>
                  </div>
                  <div v-else class="space-y-2">
                    <div class="flex flex-wrap items-center justify-between gap-2 rounded-2xl bg-slate-50 px-3 py-2 text-xs text-slate-500 dark:bg-slate-950 dark:text-slate-400">
                      <span data-testid="proxy-list-summary">共 {{ sortedProxyOptions.length }} 个代理，优先显示未绑定/低绑定量代理。</span>
                      <button
                        v-if="sortedProxyOptions.length > PROXY_COLLAPSED_LIMIT"
                        data-testid="proxy-list-toggle"
                        type="button"
                        class="btn btn-secondary btn-sm"
                        @click="proxyListExpanded = !proxyListExpanded"
                      >
                        {{ proxyListExpanded ? '收起代理列表' : `展开全部 ${sortedProxyOptions.length} 个` }}
                      </button>
                    </div>
                    <div class="grid max-h-[28rem] gap-2 overflow-y-auto pr-1 md:grid-cols-2" data-testid="proxy-card-grid">
                      <button
                        v-for="item in visibleProxyOptions"
                        :key="item.id"
                        type="button"
                        :data-testid="`proxy-card-${item.id}`"
                        :class="pickerCardClass(form.proxy_id === item.id)"
                        @click="selectProxy(item.id)"
                      >
                        <span class="flex items-start justify-between gap-2">
                          <span class="min-w-0">
                            <span class="block truncate text-sm font-semibold">{{ safeText(item.name, '未命名代理') }}</span>
                            <span class="mt-1 block text-xs text-slate-500 dark:text-slate-400">{{ proxySafeEndpoint(item) }}</span>
                          </span>
                          <span :class="statusPillClass(item.status)">{{ safeText(item.status) }}</span>
                        </span>
                        <span class="mt-2 flex flex-wrap gap-2 text-xs text-slate-500 dark:text-slate-400">
                          <span>协议 {{ safeText(item.protocol) }}</span>
                          <span>绑定 {{ item.account_count ?? 0 }}</span>
                          <span v-if="item.latency_ms != null">延迟 {{ item.latency_ms }}ms</span>
                          <span v-if="item.quality_grade || item.quality_status">质量 {{ safeText(item.quality_grade || item.quality_status) }}</span>
                        </span>
                      </button>
                    </div>
                  </div>
                </div>
                <template v-else>
                  <label class="text-sm font-medium">代理名称
                    <input v-model="proxy.name" class="input mt-1 w-full" placeholder="代理备注名称" />
                  </label>
                  <label class="text-sm font-medium">协议
                    <select v-model="proxy.protocol" class="input mt-1 w-full">
                      <option value="socks5">socks5</option>
                      <option value="socks5h">socks5h</option>
                      <option value="http">http</option>
                      <option value="https">https</option>
                    </select>
                  </label>
                  <label class="text-sm font-medium">代理地址
                    <input v-model="proxy.host" class="input mt-1 w-full" autocomplete="off" placeholder="示例：proxy.example.com" />
                    <span class="mt-1 block text-xs text-slate-500 dark:text-slate-400">示例：proxy.example.com；填写代理服务器域名或 IP，不要带账号密码。</span>
                  </label>
                  <label class="text-sm font-medium">代理端口
                    <input data-testid="create-proxy-port-input" v-model.number="proxy.port" type="number" class="input mt-1 w-full" placeholder="示例：1080" />
                    <span class="mt-1 block text-xs text-slate-500 dark:text-slate-400">示例：1080；常见端口如 1080、7890、8080，请按供应商提供填写。</span>
                  </label>
                  <label class="text-sm font-medium">代理用户名
                    <input v-model="proxy.username" class="input mt-1 w-full" autocomplete="off" placeholder="没有账号密码可留空" />
                    <span class="mt-1 block text-xs text-slate-500 dark:text-slate-400">没有账号密码可留空。</span>
                  </label>
                  <label class="text-sm font-medium">代理密码
                    <input v-model="proxy.password" type="password" class="input mt-1 w-full" autocomplete="new-password" placeholder="没有账号密码可留空，只提交不回显" />
                    <span class="mt-1 block text-xs text-slate-500 dark:text-slate-400">没有账号密码可留空；如填写，只提交不回显。</span>
                  </label>
                </template>
                <div class="space-y-3" data-testid="group-picker">
                  <div class="flex flex-wrap items-center justify-between gap-2">
                    <div>
                      <p class="text-sm font-semibold">Claude Code 分组</p>
                      <p class="text-xs text-slate-500 dark:text-slate-400">选择 Anthropic/Claude 分组，系统内部写入 group_id。</p>
                    </div>
                    <div class="flex flex-wrap gap-2">
                      <button data-testid="refresh-groups" type="button" class="btn btn-secondary btn-sm" :disabled="groupListLoading" @click="loadGroups">刷新分组</button>
                      <RouterLink to="/admin/groups" class="btn btn-secondary btn-sm">去分组管理创建 Claude Code 专用分组</RouterLink>
                    </div>
                  </div>
                  <div v-if="groupListLoading" data-testid="group-list-loading" class="rounded-2xl border border-slate-200 p-3 text-sm text-slate-500 dark:border-slate-800 dark:text-slate-400">正在加载 Claude 分组...</div>
                  <div v-else-if="groupListError" data-testid="group-list-error" class="rounded-2xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-100">
                    <p>{{ safeText(groupListError) }}</p>
                    <button data-testid="reload-groups" type="button" class="btn btn-secondary btn-sm mt-2" @click="loadGroups">重新加载分组</button>
                  </div>
                  <div v-else-if="groupOptions.length === 0" data-testid="empty-group-list" class="rounded-2xl border border-dashed border-slate-300 p-3 text-sm text-slate-600 dark:border-slate-700 dark:text-slate-300">
                    <p class="font-semibold">暂无 Anthropic/Claude 分组</p>
                    <p class="mt-1">请先去分组管理创建 Claude Code 专用分组，再回到这里刷新选择。</p>
                    <RouterLink to="/admin/groups" class="btn btn-secondary btn-sm mt-3">去分组管理创建 Claude Code 专用分组</RouterLink>
                  </div>
                  <div v-else class="grid gap-2 md:grid-cols-2">
                    <button
                      v-for="item in groupOptions"
                      :key="item.id"
                      type="button"
                      :data-testid="`group-card-${item.id}`"
                      :class="pickerCardClass(form.group_id === item.id)"
                      @click="selectGroup(item.id)"
                    >
                      <span class="flex items-start justify-between gap-2">
                        <span class="min-w-0">
                          <span class="block truncate text-sm font-semibold">{{ safeText(item.name, '未命名分组') }}</span>
                          <span class="mt-1 block text-xs text-slate-500 dark:text-slate-400">平台 {{ safeText(item.platform) }}</span>
                        </span>
                        <span :class="statusPillClass(item.status)">{{ safeText(item.status) }}</span>
                      </span>
                      <span class="mt-2 flex flex-wrap gap-2 text-xs text-slate-500 dark:text-slate-400">
                        <span v-if="item.claude_code_only">Claude Code only</span>
                        <span v-if="groupCapacityLabel(item.id)">{{ groupCapacityLabel(item.id) }}</span>
                        <span v-if="item.rpm_limit != null">RPM {{ item.rpm_limit }}</span>
                      </span>
                    </button>
                  </div>
                </div>
                <label class="text-sm font-medium">账号名称
                  <input data-testid="account-name-input" v-model="form.account_name" class="input mt-1 w-full" placeholder="claude-oauth-01" />
                </label>
                <label class="text-sm font-medium">用量策略
                  <select v-model="form.pool_profile" class="input mt-1 w-full">
                    <option value="normal">标准消耗：约 7 天平滑使用（推荐）</option>
                    <option value="aggressive">加速消耗：请求更积极，但仍需通过健康门禁</option>
                  </select>
                </label>
                <label class="text-sm font-medium">账号并发上限
                  <input v-model.number="form.concurrency" type="number" min="1" max="10" class="input mt-1 w-full" />
                </label>
              </div>
              <div
                v-if="!canStart"
                data-testid="start-session-missing-items"
                class="mt-2 rounded-2xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100"
              >
                <p class="font-semibold">创建上号会话前还差：</p>
                <ul class="mt-2 list-disc space-y-1 pl-5">
                  <li v-for="item in startSessionMissingItems" :key="item">{{ item }}</li>
                </ul>
              </div>
              <button data-testid="start-session" class="btn btn-primary mt-4" :disabled="busy || !canStart" @click="startSession">创建上号会话</button>
            </div>

            <div class="rounded-3xl border border-cyan-200 bg-cyan-50 p-5 shadow-sm dark:border-cyan-900 dark:bg-cyan-950/30">
              <h3 class="text-lg font-bold">{{ requiresBrowserEgress ? '浏览器同出口校验' : 'Setup Token 代理健康检查' }}</h3>
              <div class="mt-4 space-y-3 text-sm">
                <div class="rounded-2xl bg-white p-3 dark:bg-slate-900">
                  <span class="text-slate-500">状态：</span>
                  <strong data-testid="browser-egress-status">{{ browserStatus }}</strong>
                </div>
                <button data-testid="test-proxy" class="btn btn-secondary" :disabled="busy || !session" @click="testProxyStep">{{ requiresBrowserEgress ? '测试代理并生成同出口校验链接' : '测试代理健康并继续 Setup Token' }}</button>
                <div v-if="requiresBrowserEgress && session?.browser_egress_check_url" class="rounded-2xl border border-cyan-300 bg-white p-3 dark:border-cyan-800 dark:bg-slate-900">
                  <p class="font-semibold">只在即将登录 Claude 的同出口浏览器中复制打开：</p>
                  <p data-testid="browser-egress-check-url" class="mt-2 text-xs text-cyan-700 dark:text-cyan-300">已生成一次性校验链接</p>
                  <p data-testid="browser-egress-check-url-display" class="mt-1 font-mono text-xs text-cyan-700 dark:text-cyan-300">{{ redactedBrowserEgressCheckUrl }}</p>
                  <button data-testid="copy-browser-egress-check-url" class="btn btn-secondary mt-3" type="button" @click="copyBrowserEgressCheckUrl">复制校验链接</button>
                  <p v-if="copyStatus" data-testid="browser-egress-copy-status" class="mt-2 text-xs text-slate-500 dark:text-slate-400">{{ copyStatus }}</p>
                </div>
                <div v-else-if="requiresBrowserEgress" class="rounded-2xl border border-dashed border-slate-300 p-3 text-slate-500 dark:border-slate-700 dark:text-slate-400">代理测试成功前不展示同出口校验链接。</div>
                <div
                  v-else-if="setupTokenProxyReady"
                  class="rounded-2xl border border-emerald-200 bg-white p-3 text-emerald-800 dark:border-emerald-900 dark:bg-slate-900 dark:text-emerald-100"
                  data-testid="setup-token-egress-skip"
                >
                  Setup Token 不需要打开同出口浏览器校验链接；代理健康已通过，可以进入授权创建。
                </div>
                <div
                  v-else
                  class="rounded-2xl border border-amber-200 bg-amber-50 p-3 text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100"
                  data-testid="setup-token-proxy-not-ready"
                >
                  代理健康检查未通过。请先点击“测试代理健康”，通过后才能继续导入 Setup Token。
                </div>

                <div v-if="requiresBrowserEgress && rawBrowserStatus === 'expired'" class="rounded-2xl border border-amber-300 bg-amber-50 p-3 text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100">
                  <p>校验 nonce 已过期。为了避免复用旧链接，必须重新开一个上号会话。</p>
                  <button data-testid="expired-start-new-session" class="btn btn-primary mt-3" :disabled="busy" @click="startSession">重新开一个上号会话</button>
                </div>

                <div v-if="requiresBrowserEgress && rawBrowserStatus === 'mismatch'" data-testid="browser-egress-mismatch" class="rounded-2xl border border-rose-300 bg-rose-50 p-3 text-rose-900 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-100">
                  <p class="font-semibold">浏览器出口与代理出口不一致，不能继续授权。</p>
                  <p class="mt-1">浏览器出口分组：{{ safeText(session?.browser_egress_browser_ip_bucket, '未返回浏览器出口分组') }}</p>
                  <p>代理出口分组：{{ safeText(session?.browser_egress_proxy_ip_bucket, '未返回代理出口分组') }}</p>
                  <p class="mt-1 text-xs">如果分组不可用，请按出口不一致处理：更换浏览器出口或代理后重新开会话。</p>
                </div>
              </div>
            </div>
          </section>

          <section v-else-if="renderStep === 'auth'" class="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
            <h3 class="text-lg font-bold">授权与创建不可调度账号</h3>
            <p class="mt-1 text-sm text-slate-600 dark:text-slate-300">OAuth 需要完成同出口校验；Setup Token 已跳过同出口浏览器校验，只要求代理健康通过。</p>
            <div class="mt-4 flex flex-wrap gap-4 text-sm">
              <label class="inline-flex items-center gap-2"><input v-model="authMode" type="radio" value="oauth" /> OAuth 授权链接</label>
              <label class="inline-flex items-center gap-2"><input v-model="authMode" type="radio" value="setup-token-cookie" /> Setup Token 登录态</label>
            </div>
            <div v-if="authMode === 'oauth'" class="mt-4 space-y-3">
              <ol data-testid="oauth-human-flow" class="grid gap-2 text-sm md:grid-cols-3">
                <li class="rounded-2xl border border-slate-200 bg-slate-50 p-3 dark:border-slate-800 dark:bg-slate-950"><strong>1. 复制授权链接</strong><p class="mt-1 text-slate-500 dark:text-slate-400">先生成链接，再点击复制；页面不会直接展示真实链接。</p></li>
                <li class="rounded-2xl border border-slate-200 bg-slate-50 p-3 dark:border-slate-800 dark:bg-slate-950"><strong>2. 同出口浏览器授权</strong><p class="mt-1 text-slate-500 dark:text-slate-400">在刚才完成同出口校验的浏览器里打开链接，登录 Claude 并完成授权。</p></li>
                <li class="rounded-2xl border border-slate-200 bg-slate-50 p-3 dark:border-slate-800 dark:bg-slate-950"><strong>3. 粘贴授权码并创建账号</strong><p class="mt-1 text-slate-500 dark:text-slate-400">把 Claude 返回的授权码粘贴到下方，系统只提交不回显。</p></li>
              </ol>
              <button data-testid="generate-oauth-url" class="btn btn-secondary" :disabled="busy || !session?.browser_egress_verified" @click="generateOAuth">生成授权链接</button>
              <div v-if="session?.auth_url" class="rounded-2xl border border-cyan-200 bg-cyan-50 p-3 text-sm text-cyan-900 dark:border-cyan-900 dark:bg-cyan-950/30 dark:text-cyan-100">
                <p data-testid="oauth-url-ready" class="font-semibold">授权链接已生成</p>
                <p class="mt-1 text-xs">请复制后在同出口浏览器打开；真实链接已隐藏，避免误贴到聊天或工单。</p>
                <button data-testid="copy-oauth-url" class="btn btn-secondary btn-sm mt-3" type="button" @click="copyOAuthUrl">复制授权链接</button>
                <p v-if="oauthCopyStatus" data-testid="oauth-copy-status" class="mt-2 text-xs">{{ oauthCopyStatus }}</p>
              </div>
              <textarea v-model="oauthCode" class="input h-24 w-full" placeholder="粘贴授权码；只提交，不回显令牌"></textarea>
              <button class="btn btn-primary" :disabled="busy || !session?.browser_egress_verified || !oauthCode" @click="exchangeCreate">提交授权码并创建账号</button>
            </div>
            <div v-else class="mt-4 space-y-3">
              <input data-testid="setup-token-input" v-model="setupSessionKey" type="password" class="input w-full" autocomplete="new-password" placeholder="粘贴 Setup Token" />
              <button data-testid="setup-token-create" class="btn btn-primary" :disabled="busy || !setupTokenCanCreate || !setupSessionKey" @click="setupTokenCreate">导入 Setup Token 并创建账号</button>
            </div>
          </section>

          <section v-else-if="renderStep === 'gates'" class="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
            <h3 class="text-lg font-bold">完成上号检查，进入预热期</h3>
            <p class="mt-1 text-sm text-slate-600 dark:text-slate-300">按推荐动作推进账号：先确认登录态，再接入调度器，做一次上游可用性检查，最后进入低权重预热。只有点击上游可用性检查相关动作时，才会发起真实上游请求。</p>
            <div class="mt-4 rounded-2xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-900 dark:bg-emerald-950/30">
              <p class="text-sm font-semibold text-emerald-900 dark:text-emerald-100">推荐下一步</p>
              <p class="mt-1 text-sm text-emerald-800 dark:text-emerald-100">{{ recommendedGateAction.description }}</p>
              <button
                data-testid="recommended-gate-action"
                type="button"
                class="btn btn-primary mt-3"
                :disabled="busy || recommendedGateAction.disabled"
                @click="runRecommendedGateAction"
              >
                {{ recommendedGateAction.label }}
              </button>
            </div>
            <div class="mt-4">
              <button
                data-testid="advanced-manual-toggle"
                type="button"
                class="btn btn-secondary btn-sm"
                :aria-expanded="showAdvancedManualActions ? 'true' : 'false'"
                @click="showAdvancedManualActions = !showAdvancedManualActions"
              >
                {{ showAdvancedManualActions ? '收起高级操作 / 手动修复' : '高级操作 / 手动修复（排障时使用）' }}
              </button>
            </div>
            <div v-if="showAdvancedManualActions" data-testid="advanced-manual-actions" class="mt-4 rounded-2xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-900 dark:bg-amber-950/30">
              <p class="text-sm font-semibold text-amber-900 dark:text-amber-100">排障手动按钮</p>
              <p class="mt-1 text-xs text-amber-800 dark:text-amber-100">仅在诊断或修复流程中使用；日常上号请优先点击上方推荐按钮。</p>
              <div class="mt-3 flex flex-wrap gap-2">
                <button data-testid="refresh-only" class="btn btn-secondary" :disabled="busy || !session?.account_id" @click="refreshOnlyStep">排障时使用：仅刷新账号凭证</button>
                <button data-testid="runtime-register" class="btn btn-secondary" :disabled="busy || !session?.account_id" @click="runtimeRegisterStep">排障时使用：手动注册运行映射</button>
                <button data-testid="healthcheck" class="btn btn-secondary" :disabled="busy || !session?.account_id || !session?.cc_gateway_runtime_registered" @click="healthcheckStep">排障时使用：手动健康检查（会发起一次真实上游请求）</button>
                <button data-testid="start-warming" class="btn btn-secondary" :disabled="busy || !canStartWarming" @click="startWarmingStep">排障时使用：手动进入预热期（低权重）</button>
                <button data-testid="promote-production" class="btn btn-secondary" :disabled="busy || session?.status !== 'warming'" @click="promoteProductionStep">排障时使用：手动进入生产期</button>
              </div>
            </div>
            <div class="mt-4 grid gap-3 md:grid-cols-3">
              <div class="rounded-2xl border border-slate-200 p-3 dark:border-slate-800"><strong>登录态确认</strong><p class="text-sm text-slate-500">确认登录态仍可用，避免账号刚创建就进入后续调度。</p></div>
              <div class="rounded-2xl border border-slate-200 p-3 dark:border-slate-800"><strong>调度器接入</strong><p class="text-sm text-slate-500">让调度器识别这个账号，之后才能做上游可用性检查。</p></div>
              <div class="rounded-2xl border border-slate-200 p-3 dark:border-slate-800"><strong>上游可用性检查</strong><p class="text-sm text-slate-500">确认上游可用后，账号才能进入低权重预热。</p></div>
            </div>
            <div v-if="acceptance" class="mt-4 rounded-2xl bg-slate-100 p-3 text-sm dark:bg-slate-800">
              <p>可用性检查结果：{{ acceptanceStatusLabel(acceptance.status) }}</p>
              <p>网关证据：{{ acceptance.cc_gateway_seen ? '已看到' : '未看到' }} · 采集证据：{{ acceptance.raw_capture_present ? '已就绪' : '未就绪' }}</p>
            </div>
          </section>

          <section v-else class="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
            <h3 class="text-lg font-bold">脱敏证据与检查</h3>
            <p class="mt-1 text-sm text-slate-600 dark:text-slate-300">这里只展示已脱敏的安全摘要；令牌、代理凭据和原始账号标识不会显示。</p>
            <dl class="mt-4 grid gap-3 md:grid-cols-2 text-sm">
              <div><dt class="text-slate-500">状态</dt><dd>{{ stageLabel(String(session?.status || '')) }}</dd></div>
              <div><dt class="text-slate-500">账号</dt><dd>{{ safeText(session?.account_ref || session?.account_name) }}</dd></div>
              <div><dt class="text-slate-500">代理标识</dt><dd>{{ safeFieldText('proxy_ref', session?.proxy_ref) }}</dd></div>
              <div><dt class="text-slate-500">出口分组</dt><dd>{{ safeText(session?.egress_bucket) }}</dd></div>
            </dl>
            <ul v-if="safeChecks.length" class="mt-4 space-y-2 text-sm">
              <li v-for="(check, index) in safeChecks" :key="`${check.name}-${index}`" class="rounded-2xl border border-slate-200 p-3 dark:border-slate-800">
                <strong>{{ check.status }} · {{ check.name }}</strong>
                <p v-if="check.message">{{ check.message }}</p>
              </li>
            </ul>
            <div class="mt-4">
              <button
                data-testid="advanced-safe-session-toggle"
                type="button"
                class="btn btn-secondary btn-sm"
                :aria-expanded="showAdvancedSafeSessionData ? 'true' : 'false'"
                @click="showAdvancedSafeSessionData = !showAdvancedSafeSessionData"
              >
                {{ showAdvancedSafeSessionData ? '收起高级脱敏诊断数据' : '高级脱敏诊断数据（排障时展开）' }}
              </button>
              <pre
                v-if="showAdvancedSafeSessionData"
                data-testid="advanced-safe-session-json"
                class="mt-3 max-h-80 overflow-auto rounded-2xl bg-slate-100 p-3 text-xs dark:bg-slate-800"
              >{{ safeSession }}</pre>
            </div>
          </section>

          <p v-if="error" class="mt-4 rounded-2xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-100">{{ safeText(error) }}</p>
        </main>
      </div>
    </div>
    <ConfirmDialog
      :show="pendingHealthcheckConfirm"
      :z-index="160"
      title="执行上游可用性检查"
      message="将通过当前代理发起一次真实上游请求，可能消耗少量额度。确认继续？"
      confirm-text="确认执行"
      cancel-text="取消"
      :danger="true"
      data-testid="healthcheck-confirm-dialog"
      @confirm="confirmHealthcheckStep"
      @cancel="cancelHealthcheckConfirm"
    />
    <ConfirmDialog
      :show="pendingPromoteProductionConfirm"
      :z-index="160"
      title="切换到生产调度"
      message="确认将该账号从低权重预热切换到生产调度？切换后会按生产策略承接请求。"
      confirm-text="确认切换"
      cancel-text="取消"
      :danger="false"
      data-testid="promote-production-confirm-dialog"
      @confirm="confirmPromoteProductionStep"
      @cancel="cancelPromoteProductionConfirm"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

import claudeOnboarding, {
  type FormalPoolAcceptanceResult,
  type FormalPoolCheck,
  type FormalPoolProfile,
  type FormalPoolProxyMode,
  type FormalPoolSession,
} from '@/api/admin/claudeOnboarding'
import { adminAPI } from '@/api/admin'
import { useEgressCheckPolling } from '@/composables/useEgressCheckPolling'
import { classifyFormalPoolMutationError, mergeFormalPoolMutationResult, monotonicFormalPoolSession } from '@/utils/formalPoolMutation'
import { scrubFormalPoolDisplayText } from '@/utils/formalPoolStatusDashboard'
import type { AdminGroup, Proxy } from '@/types'

type StepKey = 'proxy' | 'auth' | 'gates' | 'evidence'

const steps: Array<{ key: StepKey; index: number; title: string; caption: string }> = [
  { key: 'proxy', index: 1, title: '代理与出口', caption: '代理设置 + 同出口校验' },
  { key: 'auth', index: 2, title: '授权创建', caption: 'OAuth / Setup Token' },
  { key: 'gates', index: 3, title: '完成上号检查，进入预热期', caption: '登录态 + 调度器接入 + 预热' },
  { key: 'evidence', index: 4, title: '脱敏证据', caption: '安全摘要' },
]
const stageList = ['imported', 'refreshed', 'runtime_registered', 'healthcheck_passed', 'warming', 'production', 'quarantined']

const activeStep = ref<StepKey>('proxy')
const busy = ref(false)
const error = ref('')
const session = ref<FormalPoolSession | null>(null)
const acceptance = ref<FormalPoolAcceptanceResult | null>(null)
const showAdvancedManualActions = ref(false)
const showAdvancedSafeSessionData = ref(false)
const pendingHealthcheckConfirm = ref(false)
const pendingPromoteProductionConfirm = ref(false)
const authMode = ref<'oauth' | 'setup-token-cookie'>('oauth')
const oauthCode = ref('')
const setupSessionKey = ref('')
const createIdempotencyKey = ref('')
const exchangeIdempotencyKey = ref('')
const promoteIdempotencyKey = ref('')
const copyStatus = ref('')
const oauthCopyStatus = ref('')
const proxyOptions = ref<Proxy[]>([])
const proxyListExpanded = ref(false)
const PROXY_COLLAPSED_LIMIT = 8
const proxyListLoading = ref(false)
const proxyListError = ref('')
const groupOptions = ref<AdminGroup[]>([])
const groupListLoading = ref(false)
const groupListError = ref('')
type GroupCapacitySummary = Awaited<ReturnType<typeof adminAPI.groups.getCapacitySummary>>[number]
const groupCapacityById = ref<Record<number, GroupCapacitySummary>>({})

const form = reactive<{
  proxy_mode: FormalPoolProxyMode
  proxy_id?: number
  group_id?: number
  account_name: string
  pool_profile: FormalPoolProfile
  concurrency: number
}>({
  proxy_mode: 'existing',
  proxy_id: undefined,
  group_id: undefined,
  account_name: '',
  pool_profile: 'normal',
  concurrency: 10,
})
const proxy = reactive({
  name: '',
  protocol: 'socks5' as 'http' | 'https' | 'socks5' | 'socks5h',
  host: '',
  port: 1080,
  username: '',
  password: '',
})

const egressPolling = useEgressCheckPolling()

onMounted(() => {
  void loadProxies()
  void loadGroups()
})

const renderStep = computed(() => safeEnterableStep(activeStep.value))
const currentStepTitle = computed(() => steps.find((step) => step.key === renderStep.value)?.title ?? 'Onboarding')
const canStart = computed(() => startSessionMissingItems.value.length === 0)
const startSessionMissingItems = computed(() => getStartSessionMissingItems())
const requiresBrowserEgress = computed(() => authMode.value === 'oauth')
const setupTokenProxyReady = computed(() => isSetupTokenProxyReady(session.value))
const setupTokenCanCreate = computed(() => !!session.value && setupTokenProxyReady.value)
const sortedProxyOptions = computed(() => [...proxyOptions.value].sort(compareProxyOptions))
const visibleProxyOptions = computed(() => proxyListExpanded.value ? sortedProxyOptions.value : sortedProxyOptions.value.slice(0, PROXY_COLLAPSED_LIMIT))
const rawBrowserStatus = computed(() => session.value?.browser_egress_check_status ?? egressPolling.status.value ?? 'idle')
const browserStatus = computed(() => browserStatusLabel(rawBrowserStatus.value))
const displaySessionRef = computed(() => safeSessionRef(session.value))
const canStartWarming = computed(() => session.value?.healthcheck_passed || acceptance.value?.status === 'healthcheck_passed')
const recommendedGateAction = computed(() => getRecommendedGateAction())
const safeChecks = computed(() => (session.value?.checks ?? []).map(sanitizeCheckForDisplay))
const safeSession = computed(() => JSON.stringify(sanitizeForDisplay({
  safe_summary: session.value?.safe_summary || {},
  status: session.value?.status,
  proxy_ref: session.value?.proxy_ref,
  egress_bucket: session.value?.egress_bucket,
  pool_profile: session.value?.pool_profile,
  browser_egress_verified: session.value?.browser_egress_verified,
  browser_egress_check_status: session.value?.browser_egress_check_status,
  browser_egress_browser_ip_bucket: session.value?.browser_egress_browser_ip_bucket,
  browser_egress_proxy_ip_bucket: session.value?.browser_egress_proxy_ip_bucket,
  cc_gateway_runtime_registered: session.value?.cc_gateway_runtime_registered,
  healthcheck_passed: session.value?.healthcheck_passed,
  production_ready: session.value?.production_ready,
  account_ref: session.value?.account_ref,
  oauth_summary: session.value?.oauth_summary,
}), null, 2))

watch(() => egressPolling.session.value, (nextSession) => {
	if (nextSession && nextSession.id === session.value?.id) {
		if (nextSession.version < session.value.version) return
		const mergedSession: FormalPoolSession = {
      ...(session.value as FormalPoolSession),
      ...(nextSession as FormalPoolSession),
      browser_egress_check_url: nextSession.browser_egress_check_url || session.value.browser_egress_check_url,
    }
		acceptSession(mergedSession)
  }
})

watch(activeStep, () => {
  egressPolling.stop()
})

watch(session, () => {
  const nextStep = safeEnterableStep(activeStep.value)
  if (nextStep !== activeStep.value) {
    activeStep.value = nextStep
  }
})

function setStep(step: StepKey) {
  activeStep.value = step
}

// ─── Stepper gating ──────────────────────────────────────────────────────────
//
// Each step has an explicit prerequisite. Locked steps refuse navigation
// (setStep is bypassed via onStepperClick) and surface a clear reason both as
// visible copy and an aria-disabled/title attribute.

type StepStatus = 'done' | 'active' | 'available' | 'locked'

async function loadProxies() {
  proxyListLoading.value = true
  proxyListError.value = ''
  try {
    proxyOptions.value = await fetchProxyOptions()
    proxyListExpanded.value = false
    if (form.proxy_id && !proxyOptions.value.some((item) => item.id === form.proxy_id)) {
      form.proxy_id = undefined
    }
  } catch (err: any) {
    proxyListError.value = err?.response?.data?.message || err?.message || '代理列表加载失败'
  } finally {
    proxyListLoading.value = false
  }
}

async function fetchProxyOptions(): Promise<Proxy[]> {
  const withCountLoader = adminAPI.proxies.getAllWithCount
  const fallbackLoader = adminAPI.proxies.getAll
  if (withCountLoader) {
    try {
      return await withCountLoader()
    } catch (err) {
      if (!fallbackLoader) throw err
    }
  }
  if (fallbackLoader) {
    return await fallbackLoader()
  }
  return []
}

function compareProxyOptions(a: Proxy, b: Proxy): number {
  return compareNumber(proxyStatusRank(a), proxyStatusRank(b)) ||
    compareNumber(proxyAccountCount(a), proxyAccountCount(b)) ||
    compareNumber(proxyQualityRank(a), proxyQualityRank(b)) ||
    compareNumber(proxyLatencyRank(a), proxyLatencyRank(b)) ||
    safeText(a.name, '').localeCompare(safeText(b.name, ''), 'zh-Hans-CN') ||
    compareNumber(a.id, b.id)
}

function proxyStatusRank(proxy: Proxy): number {
  return proxy.status === 'active' ? 0 : 1
}

function proxyAccountCount(proxy: Proxy): number {
  return Number.isFinite(proxy.account_count) ? Number(proxy.account_count) : Number.MAX_SAFE_INTEGER
}

function proxyQualityRank(proxy: Proxy): number {
  const status = String(proxy.quality_status || '').toLowerCase()
  if (status === 'healthy') return 0
  if (status === 'warn') return 1
  if (status === 'challenge') return 2
  if (status === 'failed') return 3
  return 4
}

function proxyLatencyRank(proxy: Proxy): number {
  return Number.isFinite(proxy.latency_ms) ? Number(proxy.latency_ms) : Number.MAX_SAFE_INTEGER
}

function compareNumber(a: number, b: number): number {
  return a === b ? 0 : a < b ? -1 : 1
}

async function loadGroups() {
  groupListLoading.value = true
  groupListError.value = ''
  try {
    const [groups, capacity] = await Promise.all([
      adminAPI.groups.getAll('anthropic'),
      adminAPI.groups.getCapacitySummary?.().catch(() => [] as GroupCapacitySummary[]) ?? Promise.resolve([] as GroupCapacitySummary[]),
    ])
    groupOptions.value = groups
    groupCapacityById.value = Object.fromEntries(capacity.map((item) => [item.group_id, item]))
    if (form.group_id && !groupOptions.value.some((item) => item.id === form.group_id)) {
      form.group_id = undefined
    }
  } catch (err: any) {
    groupListError.value = err?.response?.data?.message || err?.message || 'Claude 分组加载失败'
  } finally {
    groupListLoading.value = false
  }
}

function selectProxy(proxyId: number) {
  form.proxy_id = proxyId
}

function selectGroup(groupId: number) {
  form.group_id = groupId
}

function pickerCardClass(selected: boolean): string {
  const base = 'rounded-2xl border p-3 text-left transition'
  return selected
    ? `${base} border-cyan-500 bg-cyan-50 shadow-sm ring-2 ring-cyan-300 dark:border-cyan-400 dark:bg-cyan-950/40`
    : `${base} border-slate-200 bg-white hover:border-cyan-300 hover:bg-cyan-50/50 dark:border-slate-800 dark:bg-slate-900 dark:hover:border-cyan-700`
}

function statusPillClass(status: unknown): string {
  const safeStatus = String(status ?? '')
  const base = 'shrink-0 rounded-full px-2 py-0.5 text-xs font-semibold'
  return safeStatus === 'active'
    ? `${base} bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-200`
    : `${base} bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300`
}

function proxySafeEndpoint(item: Proxy): string {
  return `${safeText(item.protocol)}://${proxyDisplayHost(item.host)}:${Number(item.port) || '—'}`
}

function proxyDisplayHost(host: unknown): string {
  const value = typeof host === 'string' ? host.trim() : ''
  if (!value) return 'host 未配置'
  const hostname = extractProxyHostname(value)
  if (!hostname) return 'host 未配置'
  const unbracketed = hostname.startsWith('[') && hostname.endsWith(']')
    ? hostname.slice(1, -1)
    : hostname
  return unbracketed.includes(':') ? `[${unbracketed}]` : unbracketed
}

function extractProxyHostname(value: string): string {
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) {
    try {
      return new URL(value).hostname
    } catch {
      return stripProxyHostDecorations(value.replace(/^[a-z][a-z0-9+.-]*:\/\//i, ''))
    }
  }
  if (value.includes('@')) {
    try {
      return new URL(`proxy://${value}`).hostname
    } catch {
      return stripProxyHostDecorations(value)
    }
  }
  return stripProxyHostDecorations(value)
}

function stripProxyHostDecorations(value: string): string {
  const hostPort = value.slice(value.lastIndexOf('@') + 1).split(/[/?#]/, 1)[0] ?? ''
  if (hostPort.startsWith('[')) {
    const bracketEnd = hostPort.indexOf(']')
    return bracketEnd >= 0 ? hostPort.slice(0, bracketEnd + 1) : hostPort
  }
  const colonCount = (hostPort.match(/:/g) ?? []).length
  return colonCount === 1 ? hostPort.split(':', 1)[0] ?? '' : hostPort
}

function groupCapacityLabel(groupId: number): string {
  const capacity = groupCapacityById.value[groupId]
  if (!capacity) return ''
  const segments = [
    capacitySegment('并发', capacity.concurrency_used, capacity.concurrency_max),
    capacitySegment('会话', capacity.sessions_used, capacity.sessions_max),
    capacitySegment('RPM', capacity.rpm_used, capacity.rpm_max),
  ].filter(Boolean)
  return segments.join(' · ')
}

function capacitySegment(label: string, used: unknown, max: unknown): string {
  if (!isFiniteNumber(used) || !isFiniteNumber(max)) return ''
  return `${label} ${used}/${max}`
}

function getStartSessionMissingItems(): string[] {
  const missing: string[] = []
  if (!form.account_name.trim()) missing.push('账号名称')
  if (!form.group_id) missing.push('分组')
  if (form.proxy_mode === 'existing') {
    if (!form.proxy_id) missing.push('代理')
  } else {
    if (!proxy.host.trim()) missing.push('创建代理地址')
    if (!Number(proxy.port)) missing.push('创建代理端口')
  }
  return missing
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function canEnterStep(stepKey: StepKey): boolean {
  const s = session.value
  switch (stepKey) {
    case 'proxy':
      return true
    case 'auth':
      return s !== null && (requiresBrowserEgress.value ? s.browser_egress_verified === true : isSetupTokenProxyReady(s))
    case 'gates':
      return s !== null && typeof s.account_id === 'number'
    case 'evidence':
      return s !== null
  }
}

function isSetupTokenProxyReady(value: FormalPoolSession | null): boolean {
  if (!value) return false
  if (value.browser_egress_verified === true) return true
  const status = String(value.status || '')
  const browserCheck = String(value.browser_egress_check_status || '')
  return status === 'proxy_verified' || browserCheck === 'verified'
}

function safeEnterableStep(stepKey: StepKey): StepKey {
  if (canEnterStep(stepKey)) return stepKey
  return 'proxy'
}

function isStepDone(stepKey: StepKey): boolean {
  const s = session.value
  if (!s) return false
  switch (stepKey) {
    case 'proxy':
      return requiresBrowserEgress.value ? s.browser_egress_verified === true : isSetupTokenProxyReady(s)
    case 'auth':
      return typeof s.account_id === 'number'
    case 'gates': {
      if (s.healthcheck_passed === true) return true
      const status = String(s.status ?? '')
      return status === 'warming' || status === 'production' || status === 'quarantined'
    }
    case 'evidence':
      // Evidence is an inspector view; it has no terminal "done" state.
      return false
  }
}

function getStepStatus(stepKey: StepKey): StepStatus {
  if (!canEnterStep(stepKey)) return 'locked'
  if (safeEnterableStep(activeStep.value) === stepKey) return 'active'
  if (stepKey !== 'proxy' && isStepDone(stepKey)) return 'done'
  if (stepKey === 'proxy' && isStepDone(stepKey) && safeEnterableStep(activeStep.value) !== 'proxy') return 'done'
  return 'available'
}

function getStepLockReason(stepKey: StepKey): string {
  if (canEnterStep(stepKey)) return ''
  switch (stepKey) {
    case 'auth':
      return requiresBrowserEgress.value ? '需先在第 1 步完成代理与同出口校验' : '需先在第 1 步完成代理健康检查'
    case 'gates':
      return '需先在第 2 步完成授权并创建账号'
    case 'evidence':
      return '需先在第 1 步创建上号会话'
    default:
      return ''
  }
}

function onStepperClick(stepKey: StepKey) {
  if (!canEnterStep(stepKey)) return
  setStep(stepKey)
}

function stepperButtonClass(stepKey: StepKey): string {
  const base = 'flex w-full items-start gap-3 rounded-2xl border p-3 text-left transition'
  switch (getStepStatus(stepKey)) {
    case 'active':
      return `${base} border-cyan-300 bg-white/15 shadow-lg shadow-cyan-950/30`
    case 'done':
      return `${base} border-emerald-300/60 bg-emerald-500/10 hover:bg-emerald-500/15`
    case 'locked':
      return `${base} cursor-not-allowed border-white/5 bg-white/[0.02] opacity-60`
    default:
      return `${base} border-white/10 bg-white/5 hover:bg-white/10`
  }
}

function stepperIconClass(stepKey: StepKey): string {
  const base = 'flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-sm font-bold'
  switch (getStepStatus(stepKey)) {
    case 'done':
      return `${base} bg-emerald-400 text-emerald-950`
    case 'locked':
      return `${base} bg-white/10 text-slate-400`
    default:
      return `${base} bg-cyan-400 text-slate-950`
  }
}

function safeSessionRef(value: FormalPoolSession | null): string {
  const summary = value?.safe_summary as Record<string, unknown> | undefined
  const ref = summary?.session_ref
  if (typeof ref === 'string' && ref.trim()) return safeText(ref, '会话编号暂不可用')
  return value ? '会话编号暂不可用' : '未创建'
}

function safeText(value: unknown, fallback = '—'): string {
  if (typeof value !== 'string') {
    if (value === null || value === undefined) return fallback
    return scrubExtra(String(value), fallback)
  }
  return scrubExtra(value, fallback)
}

function safeFieldText(key: string, value: unknown, fallback = '—'): string {
  if (value === null || value === undefined) return fallback
  return hasSensitiveKeySemantic(key) ? REDACTED_TEXT : safeText(value, fallback)
}

function scrubExtra(value: string, fallback = '—'): string {
  return scrubFormalPoolDisplayText(value, fallback)
    .replace(RAW_IPV6_PATTERN, '$1[redacted]')
    .replace(RAW_IPV4_PATTERN, REDACTED_TEXT)
}

const redactedBrowserEgressCheckUrl = computed(() => redactOneTimeURL(session.value?.browser_egress_check_url, 'browser-egress-check/[一次性 nonce 已隐藏]'))

function redactOneTimeURL(raw: string | undefined, fallback: string): string {
  const value = String(raw || '').trim()
  if (!value) return fallback
  try {
    const parsed = new URL(value)
    const parts = parsed.pathname.split('/').filter(Boolean)
    const markerIndex = parts.lastIndexOf('browser-egress-check')
    if (markerIndex >= 0) {
      const safePath = `/${parts.slice(0, markerIndex + 1).join('/')}/[一次性 nonce 已隐藏]`
      return `${parsed.origin}${safePath}`
    }
    return `${parsed.origin}/[一次性链接已隐藏]`
  } catch {
    return fallback
  }
}

async function copyBrowserEgressCheckUrl() {
  const url = session.value?.browser_egress_check_url
  if (!url) return
  copyStatus.value = ''
  if (await copySensitiveText(url)) {
    copyStatus.value = '已复制校验链接'
  } else {
    copyStatus.value = '复制失败，请稍后重试'
  }
}

async function copyOAuthUrl() {
  const url = session.value?.auth_url
  if (!url) return
  oauthCopyStatus.value = ''
  if (await copySensitiveText(url)) {
    oauthCopyStatus.value = '已复制授权链接'
  } else {
    oauthCopyStatus.value = '复制失败，请重试'
  }
}

async function copySensitiveText(value: string): Promise<boolean> {
  try {
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value)
      return true
    }
  } catch {
    // Fall through to the legacy copy path for HTTP admin pages and restricted browsers.
  }
  return copySensitiveTextWithTemporaryTextarea(value)
}

function copySensitiveTextWithTemporaryTextarea(value: string): boolean {
  if (typeof document === 'undefined' || typeof document.execCommand !== 'function') return false
  const textarea = document.createElement('textarea')
  textarea.value = value
  textarea.setAttribute('readonly', 'true')
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  textarea.style.top = '0'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  try {
    textarea.focus()
    textarea.select()
    textarea.setSelectionRange(0, value.length)
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    textarea.remove()
  }
}

const REDACTED_TEXT = '[redacted]'
const RAW_IPV4_PATTERN = /\b(?:\d{1,3}\.){3}\d{1,3}\b/g
const RAW_IPV6_PATTERN = /(^|[^\w:])((?:(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}|(?:[0-9a-f]{1,4}:){1,7}:|(?:[0-9a-f]{1,4}:){1,6}:[0-9a-f]{1,4}|(?:[0-9a-f]{1,4}:){1,5}(?::[0-9a-f]{1,4}){1,2}|(?:[0-9a-f]{1,4}:){1,4}(?::[0-9a-f]{1,4}){1,3}|(?:[0-9a-f]{1,4}:){1,3}(?::[0-9a-f]{1,4}){1,4}|(?:[0-9a-f]{1,4}:){1,2}(?::[0-9a-f]{1,4}){1,5}|[0-9a-f]{1,4}:(?:(?::[0-9a-f]{1,4}){1,6})|:(?:(?::[0-9a-f]{1,4}){1,7}|:)|::ffff:(?:\d{1,3}\.){3}\d{1,3}))(?![\w:])/gi
const SENSITIVE_KEY_PARTS = [
  'prompt',
  'body',
  'telemetry',
  'cch',
  'token',
  'secret',
  'password',
  'passwd',
  'pwd',
  'proxy',
  'email',
  'uuid',
  'raw',
  'capture',
  'credential',
  'credentials',
  'session_key',
  'access_token',
  'refresh_token',
  'api_key',
] as const

function normalizeDisplayKey(key: string): string {
  return key.replace(/([a-z0-9])([A-Z])/g, '$1_$2').toLowerCase()
}

function isSafeDisplayKey(key: string): boolean {
  const normalized = normalizeDisplayKey(key)
  return normalized === 'status' ||
    normalized === 'stage' ||
    normalized.includes('bucket') ||
    normalized.startsWith('boolean_')
}

function hasSensitiveKeySemantic(key: string): boolean {
  const normalized = normalizeDisplayKey(key)
  if (isSafeDisplayKey(normalized)) return false
  return SENSITIVE_KEY_PARTS.some((part) => normalized.includes(part))
}

function sanitizeForDisplay(value: unknown, key = ''): unknown {
  if (key && hasSensitiveKeySemantic(key)) return REDACTED_TEXT
  if (typeof value === 'string') return safeText(value)
  if (Array.isArray(value)) return value.map((child) => sanitizeForDisplay(child))
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>).map(([childKey, child]) => [safeText(childKey), sanitizeForDisplay(child, childKey)]),
    )
  }
  return value
}

function sanitizeCheckForDisplay(check: FormalPoolCheck) {
  const rawName = String(check.name ?? '')
  const redactCheckText = hasSensitiveKeySemantic(rawName)
  return {
    name: redactCheckText ? REDACTED_TEXT : safeText(rawName),
    status: safeText(check.status),
    message: check.message
      ? redactCheckText || hasSensitiveKeySemantic(String(check.message))
        ? REDACTED_TEXT
        : safeText(check.message)
      : '',
  }
}

function stageLabel(stage: string): string {
  const labels: Record<string, string> = {
    idle: '未开始',
    proxy_tested: '代理已测试',
    proxy_verified: '代理健康已通过',
    imported: '已创建',
    refreshed: '登录态已确认',
    runtime_registered: '已接入调度器',
    healthcheck_passed: '上游可用性已通过',
    warming: '预热期 / 低权重',
    production: '生产调度中',
    quarantined: '隔离 / 待修复',
  }
  return labels[stage] ?? '未知阶段'
}

function browserStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    idle: '未开始',
    waiting: setupTokenProxyReady.value ? '代理健康已通过' : '等待校验',
    verified: '已通过',
    mismatch: '出口不一致',
    expired: '已过期',
    proxy_verified: '代理健康已通过',
    proxy_tested: '代理已测试',
    failed: '检查失败',
    error: '检查异常',
  }
  if (!requiresBrowserEgress.value && setupTokenProxyReady.value) return '代理健康已通过'
  return labels[status] ?? safeText(status, '未知状态')
}

function acceptanceStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    healthcheck_passed: '上游可用性已通过',
    runtime_registered: '已接入调度器',
    warming: '预热中',
    production: '生产中',
    quarantined: '已隔离',
    error: '异常',
    failed: '失败',
  }
  return labels[status] ?? safeText(status)
}

type RecommendedGateAction = {
  label: string
  description: string
  disabled: boolean
  action: 'refresh-only' | 'runtime-register' | 'healthcheck' | 'start-warming' | 'promote-production' | 'view-status'
}

function getRecommendedGateAction(): RecommendedGateAction {
  const s = session.value
  const status = String(s?.status ?? '')
  const hasAccount = typeof s?.account_id === 'number'
  const runtimeReady = s?.cc_gateway_runtime_registered === true || status === 'runtime_registered'
  const healthPassed = s?.healthcheck_passed === true || acceptance.value?.status === 'healthcheck_passed' || status === 'healthcheck_passed'

  if (status === 'quarantined' || status === 'error' || status === 'failed') {
    return {
      label: '查看修复建议',
      description: '账号处于隔离或异常状态，请打开诊断面板查看原因、证据和修复建议。',
      disabled: false,
      action: 'view-status',
    }
  }
  if (status === 'production') {
    return {
      label: '查看诊断状态',
      description: '账号已进入生产调度，可在诊断面板持续查看可用状态。',
      disabled: false,
      action: 'view-status',
    }
  }
  if (status === 'warming') {
    return {
      label: '切换到生产调度',
      description: '账号已在低权重预热期，可按策略切换到生产调度。',
      disabled: !hasAccount,
      action: 'promote-production',
    }
  }
  if (healthPassed) {
    return {
      label: '继续下一步：进入低权重预热',
      description: '上游可用性已通过，下一步将账号放入低权重预热。',
      disabled: !hasAccount,
      action: 'start-warming',
    }
  }
  if (runtimeReady) {
    return {
      label: '继续下一步：做一次上游可用性检查',
      description: '账号已接入调度器，继续做一次真实上游可用性检查。',
      disabled: !hasAccount,
      action: 'healthcheck',
    }
  }
  if (status === 'refreshed') {
    return {
      label: '继续下一步：接入调度器',
      description: '让调度器识别这个账号，之后才能做健康检查。',
      disabled: !hasAccount,
      action: 'runtime-register',
    }
  }
  return {
    label: '继续下一步：刷新登录状态',
    description: '账号已创建，先确认登录态仍可用。',
    disabled: !hasAccount,
    action: 'refresh-only',
  }
}

function stageClass(stage: string) {
  const current = session.value?.status
  const active = current === stage || (stage === 'healthcheck_passed' && canStartWarming.value)
  return active
    ? 'is-active border-emerald-300 bg-emerald-100 text-emerald-800 dark:border-emerald-700 dark:bg-emerald-950 dark:text-emerald-100'
    : 'border-slate-200 bg-slate-50 text-slate-500 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-400'
}

function acceptSession(next: FormalPoolSession): boolean {
	const current = session.value
	if (current && current.id === next.id && next.version < current.version) return false
	session.value = monotonicFormalPoolSession(current, next)
	return true
}

async function reconcileSession(id: string) {
	try {
		acceptSession(await claudeOnboarding.getSession(id))
	} catch {
		// Keep the last known safe snapshot when reconciliation also fails.
	}
}

async function run<T>(fn: () => Promise<T>, clearOperationKey?: () => void): Promise<T | null> {
  busy.value = true
  error.value = ''
  try {
		return await fn()
	} catch (err: any) {
		const classification = classifyFormalPoolMutationError(err)
		if (!classification.retainOperationKey) clearOperationKey?.()
		const id = session.value?.id
		if (id && classification.reconcile) await reconcileSession(id)
    error.value = err?.response?.data?.message || err?.message || '操作失败'
    return null
  } finally {
    busy.value = false
  }
}

function sessionPayload() {
  const payload: any = { ...form }
  if (form.proxy_mode === 'create') {
    delete payload.proxy_id
    payload.proxy = { ...proxy }
  }
  return payload
}

async function startSession() {
	egressPolling.stop()
	acceptance.value = null
	if (session.value) {
		createIdempotencyKey.value = ''
		exchangeIdempotencyKey.value = ''
		promoteIdempotencyKey.value = ''
	}
	if (!createIdempotencyKey.value) createIdempotencyKey.value = crypto.randomUUID()
	const res = await run(() => claudeOnboarding.createSession(sessionPayload(), createIdempotencyKey.value), () => { createIdempotencyKey.value = '' })
	if (res) {
		acceptSession(res)
    createIdempotencyKey.value = ''
    activeStep.value = 'proxy'
  }
}

async function testProxyStep() {
  if (!session.value) return
  egressPolling.stop()
	const res = await run(() => claudeOnboarding.testProxy(session.value!))
	if (res) {
		acceptSession(res)
    if (requiresBrowserEgress.value) {
      if (res.browser_egress_check_url) egressPolling.start(res.id)
    } else {
      activeStep.value = 'auth'
    }
  }
}

async function generateOAuth() {
  if (!session.value) return
	const res = await run(() => claudeOnboarding.generateAuthUrl(session.value!))
	if (res) acceptSession(res)
}

async function exchangeCreate() {
  if (!session.value) return
  if (!exchangeIdempotencyKey.value) exchangeIdempotencyKey.value = crypto.randomUUID()
	const res = await run(() => claudeOnboarding.exchangeCodeAndCreate(session.value!, oauthCode.value, exchangeIdempotencyKey.value), () => { exchangeIdempotencyKey.value = '' })
	if (res) {
		acceptSession(res)
    exchangeIdempotencyKey.value = ''
    activeStep.value = 'gates'
  }
}

async function setupTokenCreate() {
  if (!session.value) return
	const res = await run(() => claudeOnboarding.setupTokenCookieAuthAndCreate(session.value!, setupSessionKey.value))
	if (res) {
		acceptSession(res)
    setupSessionKey.value = ''
    activeStep.value = 'gates'
  }
}

async function refreshOnlyStep() {
  if (!session.value) return
	const res = await run(() => claudeOnboarding.refreshOnly(session.value!))
	if (res) acceptSession(res)
}

async function runtimeRegisterStep() {
  if (!session.value) return
	const res = await run(() => claudeOnboarding.runtimeRegister(session.value!))
	if (res) acceptSession(res)
}

async function healthcheckStep() {
  if (!session.value) return
  pendingHealthcheckConfirm.value = true
}

async function confirmHealthcheckStep() {
  pendingHealthcheckConfirm.value = false
  if (!session.value) return
  const res = await run(() => claudeOnboarding.healthcheck(session.value!))
	if (res && res.version >= session.value.version) {
		acceptance.value = res
		session.value = mergeFormalPoolMutationResult(session.value, res)
  }
}

function cancelHealthcheckConfirm() {
  pendingHealthcheckConfirm.value = false
}

async function startWarmingStep() {
  if (!session.value) return
	const res = await run(() => claudeOnboarding.startWarming(session.value!))
	if (res) acceptSession(res)
}

async function runRecommendedGateAction() {
  switch (recommendedGateAction.value.action) {
    case 'refresh-only':
      await refreshOnlyStep()
      break
    case 'runtime-register':
      await runtimeRegisterStep()
      break
    case 'healthcheck':
      await healthcheckStep()
      break
    case 'start-warming':
      await startWarmingStep()
      break
    case 'promote-production':
      await promoteProductionStep()
      break
    case 'view-status':
      activeStep.value = 'evidence'
      break
  }
}

async function promoteProductionStep() {
  if (!session.value) return
  pendingPromoteProductionConfirm.value = true
}

async function confirmPromoteProductionStep() {
  pendingPromoteProductionConfirm.value = false
  if (!session.value) return
  if (!promoteIdempotencyKey.value) promoteIdempotencyKey.value = crypto.randomUUID()
	const res = await run(() => claudeOnboarding.promoteProduction(session.value!, promoteIdempotencyKey.value), () => { promoteIdempotencyKey.value = '' })
	if (res) {
		acceptSession(res)
    promoteIdempotencyKey.value = ''
  }
}

function cancelPromoteProductionConfirm() {
  pendingPromoteProductionConfirm.value = false
}
</script>
