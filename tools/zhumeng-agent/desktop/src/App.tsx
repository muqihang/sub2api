import { listen } from "@tauri-apps/api/event";
import { getCurrent, onOpenUrl } from "@tauri-apps/plugin-deep-link";
import {
  Activity,
  AlertTriangle,
  AppWindow,
  BadgeCheck,
  Boxes,
  ChevronRight,
  Copy,
  ExternalLink,
  FileWarning,
  Gauge,
  KeyRound,
  ListChecks,
  LockKeyhole,
  Moon,
  PackageCheck,
  PlugZap,
  RefreshCw,
  Search,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  Sun,
  TerminalSquare,
  Wrench
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { parseZhumengDeepLink } from "./lib/deeplink";
import { filterCatalogModels, modelIsCompatible, modelPriceRows, providerOptions, summarizeCatalog } from "./lib/modelCatalog";
import { sidecar, SidecarError } from "./lib/sidecar";
import type { CatalogModel, DeepLinkRoute, DesktopStatus, ModelFilter } from "./lib/types";

type PageId = "overview" | "apps" | "codex" | "wizard" | "diagnostics" | "settings" | "about";

const emptyStatus: DesktopStatus = {
  status: "not_connected",
  global_status: "not_connected",
  proxy: { status: "not_configured" },
  authorization: { status: "not_connected" },
  adapters: { codex: { status: "not_configured", enhancements: {}, restart_required: false } },
  model_catalog: { model_count: 0, models: [] }
};

function App() {
  const [page, setPage] = useState<PageId>("overview");
  const [status, setStatus] = useState<DesktopStatus>(emptyStatus);
  const [models, setModels] = useState<CatalogModel[]>([]);
  const [lastError, setLastError] = useState<string>("");
  const [isBusy, setIsBusy] = useState(false);
  const [deepLink, setDeepLink] = useState<DeepLinkRoute | null>(null);
  const [theme, setTheme] = useState<"system" | "dark" | "light">("system");

  const visibleModels = models;
  const summary = useMemo(() => summarizeCatalog(visibleModels), [visibleModels]);
  const globalStatus = status.global_status || status.status || "not_connected";

  useEffect(() => {
    void refreshStatus();
    const timer = window.setInterval(() => void refreshStatus({ quiet: true }), 3000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const disposers: Array<() => void> = [];
    const handleUrls = (urls: string[]) => {
      const first = urls[0];
      if (!first) return;
      try {
        const parsed = parseZhumengDeepLink(first);
        setDeepLink(parsed);
        setPage(parsed.action === "open" ? "codex" : "wizard");
        if (parsed.action === "open") {
          void runAction(() => sidecar.openCodex());
        }
      } catch (error) {
        setLastError(error instanceof Error ? error.message : String(error));
      }
    };

    void getCurrent().then((urls) => {
      if (urls?.length) handleUrls(urls);
    }).catch(() => undefined);
    void onOpenUrl(handleUrls).then((dispose) => disposers.push(dispose)).catch(() => undefined);
    void listen<string[]>("deep-link", (event) => handleUrls(event.payload)).then((dispose) => disposers.push(dispose)).catch(() => undefined);

    return () => {
      disposers.forEach((dispose) => dispose());
    };
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  async function refreshStatus(options: { quiet?: boolean } = {}) {
    try {
      if (!options.quiet) setIsBusy(true);
      const nextStatus = await sidecar.status();
      setStatus(nextStatus);
      const catalog = await sidecar.modelsStatus();
      const catalogModels = Array.isArray(catalog.models) ? (catalog.models as CatalogModel[]) : [];
      setModels(catalogModels);
      setLastError("");
    } catch (error) {
      if (!options.quiet) {
        setLastError(error instanceof SidecarError ? `${error.code}: ${error.message}` : String(error));
      }
    } finally {
      if (!options.quiet) setIsBusy(false);
    }
  }

  async function runAction<T>(action: () => Promise<T>) {
    setIsBusy(true);
    setLastError("");
    try {
      const result = await action();
      if (result && typeof result === "object" && "global_status" in result) {
        setStatus(result as DesktopStatus);
      }
      await refreshStatus({ quiet: true });
    } catch (error) {
      setLastError(error instanceof SidecarError ? `${error.code}: ${error.message}` : String(error));
    } finally {
      setIsBusy(false);
    }
  }

  async function handleDeepLinkAuth() {
    if (!deepLink || deepLink.action === "open") return;
    const action = deepLink.action === "reauth" ? sidecar.reauth : sidecar.setup;
    await runAction(() => action(deepLink.client, deepLink.code, deepLink.server));
  }

  return (
    <div className="preview-stage">
      <div className="mac-window" data-testid="mac-window-frame">
        <div className="mac-titlebar" data-testid="mac-window-titlebar">
          <div className="traffic-lights" aria-hidden="true">
            <span className="traffic-light close" data-testid="mac-window-control" />
            <span className="traffic-light minimize" data-testid="mac-window-control" />
            <span className="traffic-light zoom" data-testid="mac-window-control" />
          </div>
          <div className="window-title">逐梦注入工具</div>
          <div className="titlebar-meta">Mac MVP</div>
        </div>
        <div className="app-shell">
          <aside className="sidebar">
            <div className="brand">
              <div className="brand-mark">逐</div>
              <div>
                <div className="brand-title">逐梦注入工具</div>
                <div className="brand-subtitle">桌面版 Mac MVP</div>
              </div>
            </div>
            <NavButton icon={<Gauge />} label="概览" active={page === "overview"} onClick={() => setPage("overview")} />
            <NavButton icon={<Boxes />} label="已接入应用" active={page === "apps"} onClick={() => setPage("apps")} />
            <NavButton icon={<AppWindow />} label="Codex App" active={page === "codex"} onClick={() => setPage("codex")} badge={status.adapters?.codex?.restart_required ? "重启" : undefined} />
            <NavButton icon={<ListChecks />} label="接入向导" active={page === "wizard"} onClick={() => setPage("wizard")} />
            <NavButton icon={<FileWarning />} label="诊断与日志" active={page === "diagnostics"} onClick={() => setPage("diagnostics")} />
            <NavButton icon={<Settings />} label="设置" active={page === "settings"} onClick={() => setPage("settings")} />
            <NavButton icon={<ShieldCheck />} label="分发与安全" active={page === "about"} onClick={() => setPage("about")} />
            <div className="sidebar-footer">官网下载安装 · 不走 Mac App Store</div>
          </aside>

          <main className="main-panel">
            <GlobalStatusBar status={globalStatus} proxyPort={status.proxy?.port} busy={isBusy} onRefresh={() => void refreshStatus()} theme={theme} onTheme={setTheme} />
            {lastError ? <div className="error-strip"><AlertTriangle size={16} />{lastError}</div> : null}
            {page === "overview" && <OverviewPage status={status} summary={summary} onRepair={() => runAction(() => sidecar.repair())} onOpenCodex={() => runAction(() => sidecar.openCodex())} />}
            {page === "apps" && <ConnectedAppsPage status={status} onOpenCodex={() => setPage("codex")} />}
            {page === "codex" && <CodexDetailPage status={status} models={visibleModels} onRepair={() => runAction(() => sidecar.repair())} onPatch={() => runAction(() => sidecar.patchEnhancements("/Applications/Codex.app"))} onSyncModels={() => runAction(() => sidecar.modelsSync())} />}
            {page === "wizard" && <SetupWizardPage deepLink={deepLink} status={status} onAuthorize={() => void handleDeepLinkAuth()} onPatch={() => runAction(() => sidecar.patchEnhancements("/Applications/Codex.app"))} />}
            {page === "diagnostics" && <DiagnosticsPage onDiagnose={() => runAction(() => sidecar.diagnose())} status={status} />}
            {page === "settings" && <SettingsPage />}
            {page === "about" && <AboutDistributionPage />}
          </main>
        </div>
      </div>
    </div>
  );
}

function NavButton({ icon, label, active, badge, onClick }: { icon: React.ReactNode; label: string; active: boolean; badge?: string; onClick: () => void }) {
  return (
    <button className={`nav-item ${active ? "active" : ""}`} onClick={onClick}>
      {icon}
      <span>{label}</span>
      {badge ? <span className="nav-badge">{badge}</span> : null}
    </button>
  );
}

function GlobalStatusBar({ status, proxyPort, busy, theme, onTheme, onRefresh }: { status: string; proxyPort?: number; busy: boolean; theme: "system" | "dark" | "light"; onTheme: (theme: "system" | "dark" | "light") => void; onRefresh: () => void }) {
  const tone = statusTone(status);
  return (
    <div className={`global-bar ${tone}`}>
      <span className={`status-dot ${tone}`} />
      <div>
        <div className="global-title">{statusLabel(status)}</div>
        <div className="global-subtitle">Codex App · 逐梦托管模型 · 本机代理</div>
      </div>
      <div className="global-spacer" />
      <span className="pill">代理端口 {proxyPort || "未启动"}</span>
      <button className="icon-button" onClick={onRefresh} aria-label="刷新状态">
        <RefreshCw size={16} className={busy ? "spin" : ""} />
      </button>
      <button className="icon-button" onClick={() => onTheme(theme === "dark" ? "light" : "dark")} aria-label="切换主题">
        {theme === "dark" ? <Sun size={16} /> : <Moon size={16} />}
      </button>
    </div>
  );
}

function OverviewPage({ status, summary, onRepair, onOpenCodex }: { status: DesktopStatus; summary: ReturnType<typeof summarizeCatalog>; onRepair: () => void; onOpenCodex: () => void }) {
  return (
    <section className="content">
      <PageHeader title="概览" subtitle="检查授权、代理、Codex 增强项和模型目录是否处于可用状态。" />
      <div className="metric-grid">
        <Metric title="全局状态" value={statusLabel(status.global_status || status.status || "not_connected")} icon={<Activity />} />
        <Metric title="模型目录" value={`${summary.modelCount} 个模型`} icon={<Boxes />} />
        <Metric title="主列表模型" value={`${summary.mainListCount} 个`} icon={<BadgeCheck />} />
        <Metric title="缺少定价" value={`${summary.missingPricingCount} 个`} icon={<AlertTriangle />} />
      </div>
      <div className="two-column">
        <HealthCheckList status={status} />
        <div className="card">
          <div className="card-title">快速操作</div>
          <button className="primary-action" onClick={onRepair}><Wrench size={16} />一键修复 Codex 接入</button>
          <button className="secondary-action" onClick={onOpenCodex}><ExternalLink size={16} />打开 Codex App</button>
          <div className="hint">修复动作只调用 `zhumeng-agent desktop repair`，不会在桌面壳里直接改 `~/.codex` 或 `app.asar`。</div>
        </div>
      </div>
    </section>
  );
}

function ConnectedAppsPage({ status, onOpenCodex }: { status: DesktopStatus; onOpenCodex: () => void }) {
  return (
    <section className="content">
      <PageHeader title="已接入应用" subtitle="MVP 先支持 Codex App，后续 Adapter 可以接 Claude Desktop 和自研工具。" />
      <button className="app-card" onClick={onOpenCodex}>
        <div className="app-icon">C</div>
        <div>
          <div className="app-title">Codex App</div>
          <div className="app-subtitle">{status.adapters?.codex?.status || "not_configured"}</div>
        </div>
        <ChevronRight size={18} />
      </button>
      <div className="disabled-app"><div className="app-icon muted">A</div><div><div className="app-title">Claude Desktop</div><div className="app-subtitle">规划中</div></div></div>
      <div className="disabled-app"><div className="app-icon muted">+</div><div><div className="app-title">自定义目标应用</div><div className="app-subtitle">第二版预留</div></div></div>
    </section>
  );
}

function CodexDetailPage({ status, models, onRepair, onPatch, onSyncModels }: { status: DesktopStatus; models: CatalogModel[]; onRepair: () => void; onPatch: () => void; onSyncModels: () => void }) {
  return (
    <section className="content">
      <PageHeader title="Codex App" subtitle="授权、代理、增强项和模型目录都在这里集中管理。" />
      <div className="two-column">
        <CodexEnhancementsCard enhancements={status.adapters?.codex?.enhancements} restartRequired={Boolean(status.adapters?.codex?.restart_required)} onPatch={onPatch} />
        <HealthCheckList status={status} />
      </div>
      <ModelCatalogTable models={models} onSyncModels={onSyncModels} />
      <div className="action-row">
        <button className="primary-action inline" onClick={onRepair}><Wrench size={16} />修复接入</button>
      </div>
    </section>
  );
}

function SetupWizardPage({ deepLink, status, onAuthorize, onPatch }: { deepLink: DeepLinkRoute | null; status: DesktopStatus; onAuthorize: () => void; onPatch: () => void }) {
  const steps = [
    ["收到网页授权", deepLink ? "done" : "pending"],
    ["检测 Codex App", status.adapters?.codex?.status !== "not_configured" ? "done" : "pending"],
    ["授权与配置注入", status.authorization?.status === "configured" || status.status === "configured" ? "done" : "pending"],
    ["启动本机代理", status.proxy?.port ? "done" : "pending"],
    ["启用 Codex 增强项", status.adapters?.codex?.enhancements ? "done" : "pending"],
    ["健康检查", status.global_status === "running" || status.global_status === "configured" ? "done" : "pending"],
    ["完成", status.status === "configured" ? "done" : "pending"]
  ] as const;
  return (
    <section className="content">
      <PageHeader title="接入向导" subtitle="深链会进入这里，重新授权和打开 Codex 也走同一套路由。" />
      <div className="wizard">
        {steps.map(([label, state], index) => (
          <div className={`wizard-step ${state}`} key={label}>
            <span>{index + 1}</span>
            <div>{label}</div>
          </div>
        ))}
      </div>
      <div className="action-row">
        <button className="primary-action inline" disabled={!deepLink || deepLink.action === "open"} onClick={onAuthorize}><KeyRound size={16} />执行授权</button>
        <button className="secondary-action inline" onClick={onPatch}><PlugZap size={16} />启用 Codex 增强项</button>
      </div>
    </section>
  );
}

function DiagnosticsPage({ status, onDiagnose }: { status: DesktopStatus; onDiagnose: () => void }) {
  return (
    <section className="content">
      <PageHeader title="诊断与日志" subtitle="诊断报告由 sidecar 生成并脱敏，桌面壳不展示 token。" />
      <div className="card">
        <div className="card-title">脱敏诊断报告</div>
        <div className="code-block">{JSON.stringify({ status: status.status, proxy: status.proxy, authorization: status.authorization }, null, 2)}</div>
        <button className="secondary-action inline" onClick={onDiagnose}><Copy size={16} />生成并复制报告</button>
      </div>
    </section>
  );
}

function SettingsPage() {
  return (
    <section className="content">
      <PageHeader title="设置" subtitle="MVP 保留关键开关位置，真实策略由 sidecar 和后端控制。" />
      <div className="settings-list">
        <SettingRow icon={<SlidersHorizontal />} title="代理端口策略" value="自动避开常见代理端口" />
        <SettingRow icon={<LockKeyhole />} title="严格模型门禁" value="只展示兼容 Codex Agent 的模型" />
        <SettingRow icon={<PackageCheck />} title="自动更新" value="第二版支持" disabled />
      </div>
    </section>
  );
}

function AboutDistributionPage() {
  return (
    <section className="content">
      <PageHeader title="分发与安全" subtitle="首版从官网下载，不上架 Mac App Store。" />
      <div className="two-column">
        <div className="card">
          <div className="card-title">发布路径</div>
          <p>内测包可以先未签名分发；正式官网发布前需要 `Developer ID` 签名、苹果公证和 `SHA256` 校验。</p>
        </div>
        <div className="card">
          <div className="card-title">安全边界</div>
          <p>桌面壳只调用稳定 JSON 接口；写配置、启动代理、修补和恢复都由 Python sidecar 执行。</p>
        </div>
      </div>
    </section>
  );
}

function PageHeader({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <header className="page-header">
      <h1>{title}</h1>
      <p>{subtitle}</p>
    </header>
  );
}

function Metric({ title, value, icon }: { title: string; value: string; icon: React.ReactNode }) {
  return <div className="metric"><div className="metric-icon">{icon}</div><div><div className="metric-title">{title}</div><div className="metric-value">{value}</div></div></div>;
}

function HealthCheckList({ status }: { status: DesktopStatus }) {
  const checks = [
    ["授权", status.authorization?.device_id ? "ok" : "warn", status.authorization?.device_id ? `设备 ${status.authorization.device_id}` : "未接入"],
    ["本机代理", status.proxy?.port ? "ok" : "warn", status.proxy?.port ? `127.0.0.1:${status.proxy.port}` : "未启动"],
    ["后端网关", status.backend?.gateway_base_url ? "ok" : "warn", status.backend?.gateway_base_url || "未同步"],
    ["模型目录", status.model_catalog?.model_count ? "ok" : "warn", `${status.model_catalog?.model_count || 0} 个模型`]
  ] as const;
  return (
    <div className="card">
      <div className="card-title">健康检查</div>
      {checks.map(([label, state, detail]) => (
        <div className="check-row" key={label}>
          <span className={`status-dot small ${state}`} />
          <span>{label}</span>
          <em>{detail}</em>
        </div>
      ))}
    </div>
  );
}

function CodexEnhancementsCard({ enhancements, restartRequired, onPatch }: { enhancements?: Record<string, unknown>; restartRequired: boolean; onPatch: () => void }) {
  const items = (enhancements?.items || enhancements || {}) as Record<string, { status?: string }>;
  return (
    <div className="card">
      <div className="card-title">Codex 增强项</div>
      {["model-picker", "plugin-auth-gate", "plugin-mention-marketplace"].map((item) => (
        <div className="enhancement-row" key={item}>
          <TerminalSquare size={16} />
          <span>{enhancementName(item)}</span>
          <em>{items[item]?.status || "unknown"}</em>
        </div>
      ))}
      {restartRequired ? <div className="warning-note"><AlertTriangle size={15} />需要重启 Codex App 后生效</div> : null}
      <button className="secondary-action inline" onClick={onPatch}><PlugZap size={16} />启用全部增强项</button>
    </div>
  );
}

function ModelCatalogTable({ models, onSyncModels }: { models: CatalogModel[]; onSyncModels: () => void }) {
  const [filter, setFilter] = useState<ModelFilter>({ query: "", provider: "all", capability: "all" });
  const filtered = useMemo(() => filterCatalogModels(models, filter), [models, filter]);
  const providers = providerOptions(models);
  return (
    <div className="card model-card">
      <div className="table-toolbar">
        <div>
          <div className="card-title">模型目录</div>
          <div className="muted-text">{filtered.length} / {models.length} 个模型</div>
        </div>
        <label className="search-field"><Search size={15} /><input value={filter.query} onChange={(event) => setFilter({ ...filter, query: event.target.value })} placeholder="搜索模型" /></label>
        <select value={filter.provider} onChange={(event) => setFilter({ ...filter, provider: event.target.value })}>
          <option value="all">全部供应商</option>
          {providers.map((provider) => <option value={provider} key={provider}>{provider}</option>)}
        </select>
        <select value={filter.capability} onChange={(event) => setFilter({ ...filter, capability: event.target.value as ModelFilter["capability"] })}>
          <option value="all">全部能力</option>
          <option value="responses">Responses</option>
          <option value="streaming">流式</option>
          <option value="tool_calls">工具调用</option>
          <option value="context_continuation">上下文延续</option>
        </select>
        <button className="secondary-action inline" onClick={onSyncModels}><RefreshCw size={16} />同步</button>
      </div>
      <div className="model-table-wrap">
        <table className="model-table">
          <thead>
            <tr><th>模型</th><th>供应商</th><th>能力</th><th>定价</th><th>状态</th></tr>
          </thead>
          <tbody>
            {filtered.map((model) => <ModelRow model={model} key={model.slug} />)}
            {!filtered.length ? <tr><td colSpan={5} className="empty-cell">暂无模型目录，请先同步或完成接入。</td></tr> : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ModelRow({ model }: { model: CatalogModel }) {
  const compatible = modelIsCompatible(model);
  return (
    <tr>
      <td><strong>{model.display_name || model.slug}</strong><span>{model.slug}</span></td>
      <td>{model.provider_id || model.origin || "unknown"}</td>
      <td><div className="cap-list">{["responses", "streaming", "tool_calls", "context_continuation"].map((cap) => <span className={model.capabilities?.[cap] ? "on" : "off"} key={cap}>{capLabel(cap)}</span>)}</div></td>
      <td><ModelPriceTooltip model={model} /></td>
      <td><span className={`status-chip ${compatible ? "ok" : "warn"}`}>{compatible ? "可用" : "受限"}</span></td>
    </tr>
  );
}

function ModelPriceTooltip({ model }: { model: CatalogModel }) {
  const rows = modelPriceRows(model);
  return (
    <span className="price-cell">
      按模型定价
      <span className="price-popover">
        {rows.map(([label, value]) => <span key={label}><b>{label}</b><em>{value}</em></span>)}
      </span>
    </span>
  );
}

function SettingRow({ icon, title, value, disabled }: { icon: React.ReactNode; title: string; value: string; disabled?: boolean }) {
  return <div className={`setting-row ${disabled ? "disabled" : ""}`}>{icon}<div><div>{title}</div><span>{value}</span></div></div>;
}

function statusTone(status: string) {
  if (["running", "configured", "reauthorized", "repaired"].includes(status)) return "ok";
  if (["degraded", "not_connected", "not_configured"].includes(status)) return "warn";
  return "err";
}

function statusLabel(status: string) {
  const labels: Record<string, string> = {
    running: "运行中",
    configured: "已配置",
    repaired: "已修复",
    reauthorized: "已重新授权",
    not_connected: "未接入",
    not_configured: "未配置",
    degraded: "降级",
    error: "错误"
  };
  return labels[status] || status;
}

function enhancementName(item: string) {
  const labels: Record<string, string> = {
    "model-picker": "模型选择器",
    "plugin-auth-gate": "插件授权门禁",
    "plugin-mention-marketplace": "插件市场提及"
  };
  return labels[item] || item;
}

function capLabel(cap: string) {
  const labels: Record<string, string> = {
    responses: "响应",
    streaming: "流式",
    tool_calls: "工具",
    context_continuation: "延续"
  };
  return labels[cap] || cap;
}

export default App;
