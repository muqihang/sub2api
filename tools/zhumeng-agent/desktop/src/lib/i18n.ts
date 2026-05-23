const zh = {
  app: {
    name: "逐梦注入工具",
    mark: "逐",
    subtitle: "Codex 接入控制台"
  },
  nav: {
    overview: "概览",
    apps: "已接入应用",
    codex: "Codex App",
    wizard: "接入向导",
    diagnostics: "诊断与日志",
    settings: "设置",
    about: "分发与安全",
    restart: "重启",
    sectionLabel: "导航"
  },
  websiteCta: {
    sidebarVisit: "访问逐梦官网",
    sidebarHint: "查看新版本与教程",
    overviewTitle: "在浏览器中继续",
    overviewBody: "前往逐梦控制台领取授权码、管理订阅与查看用量。",
    overviewAction: "前往逐梦控制台",
    wizardCta: "前往逐梦控制台获取授权",
    distributionAction: "下载页",
    distributionDocs: "查看安装指南",
    aboutTitle: "了解更多",
    aboutBody: "教程、订阅与版本说明都在逐梦官网。",
    learnMore: "了解更多"
  },
  global: {
    subtitle: "Codex App · 逐梦托管模型 · 本机代理",
    proxyPort: "代理端口",
    proxyStopped: "未启动",
    refresh: "刷新状态",
    toggleTheme: "切换主题"
  },
  actions: {
    repairCodex: "一键修复 Codex 接入",
    openCodex: "打开 Codex App",
    repair: "修复接入",
    authorize: "执行授权",
    enableEnhancements: "启用 Codex 增强项",
    enableAllEnhancements: "启用全部增强项",
    copyDiagnostics: "生成并复制报告",
    sync: "同步",
    syncShort: "同步"
  },
  overview: {
    title: "概览",
    subtitle: "检查授权、代理、Codex 增强项和模型目录是否处于可用状态。",
    globalStatus: "全局状态",
    modelCatalog: "模型目录",
    mainListModels: "主列表模型",
    missingPricing: "缺少定价",
    quickActions: "快速操作",
    repairHint: "修复会重新接入授权、代理与 Codex 增强项，不会改动其他配置。",
    runtime: "运行时",
    proxyEndpoint: "本机代理",
    deviceId: "授权设备",
    runtimeNotReady: "未启动",
    deviceUnknown: "未授权"
  },
  apps: {
    title: "已接入应用",
    subtitle: "目前支持 Codex App，未来还会接入更多桌面应用。",
    claude: "Claude Desktop",
    custom: "自定义目标应用",
    planned: "即将支持",
    v2Reserved: "敬请期待"
  },
  codex: {
    title: "Codex App",
    subtitle: "授权、代理、增强项和模型目录都在这里集中管理。",
    emptyTitle: "Codex 还未接入",
    emptyBody: "前往接入向导完成授权后，即可在这里启用增强项并查看可用模型。",
    goWizard: "去接入向导",
    learnAtSite: "去逐梦官网了解 Codex 接入"
  },
  wizard: {
    title: "接入向导",
    subtitle: "完成授权、启用增强项后，Codex App 就能用上逐梦托管模型。",
    receivedAuth: "收到网页授权",
    detectCodex: "检测 Codex App",
    injectAuth: "授权与配置注入",
    startProxy: "启动本机代理",
    enableCodexEnhancements: "启用 Codex 增强项",
    healthCheck: "健康检查",
    done: "完成",
    receivedAuthHint: "在浏览器中点击「在 Codex 中打开」后，授权码会自动到达。",
    detectCodexHint: "确认 /Applications/Codex.app 已安装并可启动。",
    injectAuthHint: "把授权信息和服务地址写入本机配置。",
    startProxyHint: "启动本机代理，自动避开常见占用端口。",
    enableEnhancementsHint: "启用模型选择器、插件门禁等增强项。",
    healthCheckHint: "检查代理、网关和模型目录是否正常。",
    doneHint: "全部就绪，可以在 Codex App 中开始使用。",
    statusDone: "已完成",
    statusPending: "待完成",
    needAuthCode: "还没有授权码？"
  },
  diagnostics: {
    title: "诊断与日志",
    subtitle: "查看本机状态报告，遇到问题可一键复制并发送给逐梦支持。",
    reportTitle: "脱敏诊断报告",
    calloutTitle: "报告会自动脱敏",
    calloutBody: "诊断报告只包含运行状态与错误代码，不含授权 token、设备指纹或代码内容。"
  },
  settings: {
    title: "设置",
    subtitle: "管理界面语言与代理、模型门禁等通用偏好。",
    languageTitle: "语言",
    languageDescription: "默认中文；切换后会保存到本机，下次打开继续使用。",
    chinese: "中文",
    english: "English",
    proxyPolicy: "代理端口策略",
    proxyPolicyValue: "自动避开常见代理端口",
    strictGate: "严格模型门禁",
    strictGateValue: "只展示兼容 Codex Agent 的模型",
    autoUpdate: "自动更新",
    autoUpdateValue: "即将支持"
  },
  distribution: {
    title: "分发与安全",
    subtitle: "查看安装来源、签名信息和工具的安全边界。",
    releasePath: "发布路径",
    releaseCopy: "通过逐梦官网下载安装，不通过 Mac App Store。正式版本经 Apple Developer ID 签名与公证，并附带 SHA256 校验。",
    safetyBoundary: "安全边界",
    safetyCopy: "本工具只在本机管理 Codex 接入、代理与配置，不会上传或读取你的代码内容。",
    websiteTitle: "在官网获取最新版本",
    websiteCopy: "下载页提供历史版本、SHA256 校验和升级说明。",
    websiteAction: "前往下载页"
  },
  health: {
    title: "健康检查",
    authorization: "授权",
    proxy: "本机代理",
    backendGateway: "后端网关",
    modelCatalog: "模型目录",
    device: "设备",
    notConnected: "未接入",
    stopped: "未启动",
    notSynced: "未同步"
  },
  enhancements: {
    title: "Codex 增强项",
    modelPicker: "模型选择器",
    pluginAuthGate: "插件授权门禁",
    pluginMentionMarketplace: "插件市场提及",
    restartRequired: "需要重启 Codex App 后生效",
    unknown: "未知"
  },
  modelCatalog: {
    title: "模型目录",
    modelUnit: "个模型",
    searchPlaceholder: "搜索模型",
    allProviders: "全部供应商",
    allCapabilities: "全部能力",
    responses: "Responses",
    streaming: "流式",
    toolCalls: "工具调用",
    contextContinuation: "上下文延续",
    model: "模型",
    provider: "供应商",
    capabilities: "能力",
    pricing: "定价",
    status: "状态",
    empty: "暂无模型目录，请先同步或完成接入。",
    available: "可用",
    limited: "受限",
    pricingTrigger: "按模型定价"
  },
  status: {
    running: "运行中",
    configured: "已配置",
    repaired: "已修复",
    reauthorized: "已重新授权",
    not_connected: "未接入",
    not_configured: "未配置",
    degraded: "降级",
    error: "错误"
  },
  capabilities: {
    responses: "响应",
    streaming: "流式",
    tool_calls: "工具",
    context_continuation: "延续"
  },
  price: {
    price: "价格",
    input: "输入",
    output: "输出",
    cachedInput: "命中缓存",
    cacheWrite: "写入缓存",
    notConfigured: "未配置",
    perMillionTokens: "100万 tokens"
  }
} as const;

type DeepString<T> = {
  [K in keyof T]: T[K] extends string ? string : DeepString<T[K]>;
};

export type Language = "zh" | "en";
export type Translation = DeepString<typeof zh>;

export const translations: Record<Language, Translation> = {
  zh,
  en: {
    app: {
      name: "Zhumeng Injector",
      mark: "Z",
      subtitle: "Codex connection console"
    },
    nav: {
      overview: "Overview",
      apps: "Connected Apps",
      codex: "Codex App",
      wizard: "Setup Wizard",
      diagnostics: "Diagnostics",
      settings: "Settings",
      about: "Distribution",
      restart: "Restart",
      sectionLabel: "Navigation"
    },
    websiteCta: {
      sidebarVisit: "Visit Zhumeng",
      sidebarHint: "Releases and guides",
      overviewTitle: "Continue in your browser",
      overviewBody: "Open the Zhumeng console to grab an auth code, manage subscription, and review usage.",
      overviewAction: "Open Zhumeng console",
      wizardCta: "Open Zhumeng console for an auth code",
      distributionAction: "Downloads",
      distributionDocs: "Read install guide",
      aboutTitle: "Learn more",
      aboutBody: "Tutorials, plans, and release notes live on the Zhumeng website.",
      learnMore: "Learn more"
    },
    global: {
      subtitle: "Codex App · Zhumeng managed models · Local proxy",
      proxyPort: "Proxy port",
      proxyStopped: "Stopped",
      refresh: "Refresh status",
      toggleTheme: "Toggle theme"
    },
    actions: {
      repairCodex: "Repair Codex connection",
      openCodex: "Open Codex App",
      repair: "Repair connection",
      authorize: "Authorize",
      enableEnhancements: "Enable Codex enhancements",
      enableAllEnhancements: "Enable all enhancements",
      copyDiagnostics: "Generate and copy report",
      sync: "Sync",
      syncShort: "Sync"
    },
    overview: {
      title: "Overview",
      subtitle: "Check whether authorization, proxy, Codex enhancements, and the model catalog are ready.",
      globalStatus: "Global status",
      modelCatalog: "Model catalog",
      mainListModels: "Main list",
      missingPricing: "Missing pricing",
      quickActions: "Quick actions",
      repairHint: "Repair re-applies authorization, the local proxy, and Codex enhancements without touching other settings.",
      runtime: "Runtime",
      proxyEndpoint: "Local proxy",
      deviceId: "Authorized device",
      runtimeNotReady: "Stopped",
      deviceUnknown: "Not authorized"
    },
    apps: {
      title: "Connected Apps",
      subtitle: "Codex App is supported today, with more desktop apps on the way.",
      claude: "Claude Desktop",
      custom: "Custom target app",
      planned: "Coming soon",
      v2Reserved: "Coming soon"
    },
    codex: {
      title: "Codex App",
      subtitle: "Manage authorization, proxy, enhancements, and model catalog in one place.",
      emptyTitle: "Codex is not connected yet",
      emptyBody: "Run the setup wizard to authorize, then come back to enable enhancements and browse models.",
      goWizard: "Open setup wizard",
      learnAtSite: "Learn how Codex connects on zhumeng.example.com"
    },
    wizard: {
      title: "Setup Wizard",
      subtitle: "Finish authorization and enable enhancements so Codex App can use Zhumeng models.",
      receivedAuth: "Received web authorization",
      detectCodex: "Detect Codex App",
      injectAuth: "Inject authorization and config",
      startProxy: "Start local proxy",
      enableCodexEnhancements: "Enable Codex enhancements",
      healthCheck: "Health check",
      done: "Done",
      receivedAuthHint: "Click \"Open in Codex\" in your browser; the auth code arrives automatically.",
      detectCodexHint: "Confirm /Applications/Codex.app is installed and launchable.",
      injectAuthHint: "Write the auth code and server URL into the local config.",
      startProxyHint: "Start the local proxy and steer clear of common ports.",
      enableEnhancementsHint: "Turn on the model picker, plugin auth gate, and other enhancements.",
      healthCheckHint: "Verify the proxy, gateway, and model catalog look healthy.",
      doneHint: "All set. Open Codex App and start chatting.",
      statusDone: "Done",
      statusPending: "Pending",
      needAuthCode: "Need an auth code?"
    },
    diagnostics: {
      title: "Diagnostics",
      subtitle: "Inspect runtime status and copy a redacted report when reaching out to support.",
      reportTitle: "Redacted diagnostic report",
      calloutTitle: "Reports are redacted automatically",
      calloutBody: "Reports include runtime status and error codes only. They never contain auth tokens, device fingerprints, or your code."
    },
    settings: {
      title: "Settings",
      subtitle: "Manage language, proxy, and model gating preferences for the app.",
      languageTitle: "Language",
      languageDescription: "Chinese is the default. Your choice is saved locally for the next launch.",
      chinese: "中文",
      english: "English",
      proxyPolicy: "Proxy port policy",
      proxyPolicyValue: "Automatically avoids common proxy ports",
      strictGate: "Strict model gate",
      strictGateValue: "Only show models compatible with Codex Agent",
      autoUpdate: "Auto update",
      autoUpdateValue: "Coming soon"
    },
    distribution: {
      title: "Distribution",
      subtitle: "Review the install source, signing details, and the safety boundary of this tool.",
      releasePath: "Release path",
      releaseCopy: "Installed from the Zhumeng website rather than the Mac App Store. Stable releases are signed with an Apple Developer ID, notarized by Apple, and accompanied by a SHA256 checksum.",
      safetyBoundary: "Safety boundary",
      safetyCopy: "The app manages Codex connection, the local proxy, and configuration on your Mac only. It never uploads or reads your code.",
      websiteTitle: "Get the latest version",
      websiteCopy: "The download page lists every release, SHA256 checksum, and upgrade notes.",
      websiteAction: "Open downloads page"
    },
    health: {
      title: "Health checks",
      authorization: "Authorization",
      proxy: "Local proxy",
      backendGateway: "Backend gateway",
      modelCatalog: "Model catalog",
      device: "Device",
      notConnected: "Not connected",
      stopped: "Stopped",
      notSynced: "Not synced"
    },
    enhancements: {
      title: "Codex enhancements",
      modelPicker: "Model picker",
      pluginAuthGate: "Plugin auth gate",
      pluginMentionMarketplace: "Plugin marketplace mention",
      restartRequired: "Restart Codex App to apply changes",
      unknown: "Unknown"
    },
    modelCatalog: {
      title: "Model catalog",
      modelUnit: "models",
      searchPlaceholder: "Search models",
      allProviders: "All providers",
      allCapabilities: "All capabilities",
      responses: "Responses",
      streaming: "Streaming",
      toolCalls: "Tool calls",
      contextContinuation: "Context continuation",
      model: "Model",
      provider: "Provider",
      capabilities: "Capabilities",
      pricing: "Pricing",
      status: "Status",
      empty: "No model catalog yet. Sync models or complete setup first.",
      available: "Available",
      limited: "Limited",
      pricingTrigger: "Model pricing"
    },
    status: {
      running: "Running",
      configured: "Configured",
      repaired: "Repaired",
      reauthorized: "Reauthorized",
      not_connected: "Not connected",
      not_configured: "Not configured",
      degraded: "Degraded",
      error: "Error"
    },
    capabilities: {
      responses: "Responses",
      streaming: "Streaming",
      tool_calls: "Tools",
      context_continuation: "Continuation"
    },
    price: {
      price: "Price",
      input: "Input",
      output: "Output",
      cachedInput: "Cache hit",
      cacheWrite: "Cache write",
      notConfigured: "Not configured",
      perMillionTokens: "1M tokens"
    }
  }
};

export function isLanguage(value: unknown): value is Language {
  return value === "zh" || value === "en";
}
