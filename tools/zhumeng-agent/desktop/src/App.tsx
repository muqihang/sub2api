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
import { isLanguage, translations, type Language, type Translation } from "./lib/i18n";
import { filterCatalogModels, modelIsCompatible, modelPriceRows, providerOptions, summarizeCatalog } from "./lib/modelCatalog";
import { sidecar, SidecarError } from "./lib/sidecar";
import type { CatalogModel, DeepLinkRoute, DesktopStatus, ModelFilter } from "./lib/types";

type PageId = "overview" | "apps" | "codex" | "wizard" | "diagnostics" | "settings" | "about";

export const LANGUAGE_STORAGE_KEY = "zhumeng-agent-desktop-language";

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
  const [language, setLanguage] = useState<Language>(() => readInitialLanguage());

  const t = translations[language];
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

  useEffect(() => {
    document.documentElement.lang = language === "zh" ? "zh-CN" : "en";
    window.localStorage.setItem(LANGUAGE_STORAGE_KEY, language);
  }, [language]);

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
          <div className="window-title">{t.app.name}</div>
          <div className="titlebar-meta">{t.app.windowMeta}</div>
        </div>
        <div className="app-shell">
          <aside className="sidebar">
            <div className="brand">
              <div className="brand-mark">{t.app.mark}</div>
              <div>
                <div className="brand-title">{t.app.name}</div>
                <div className="brand-subtitle">{t.app.subtitle}</div>
              </div>
            </div>
            <NavButton icon={<Gauge />} label={t.nav.overview} active={page === "overview"} onClick={() => setPage("overview")} />
            <NavButton icon={<Boxes />} label={t.nav.apps} active={page === "apps"} onClick={() => setPage("apps")} />
            <NavButton icon={<AppWindow />} label={t.nav.codex} active={page === "codex"} onClick={() => setPage("codex")} badge={status.adapters?.codex?.restart_required ? t.nav.restart : undefined} />
            <NavButton icon={<ListChecks />} label={t.nav.wizard} active={page === "wizard"} onClick={() => setPage("wizard")} />
            <NavButton icon={<FileWarning />} label={t.nav.diagnostics} active={page === "diagnostics"} onClick={() => setPage("diagnostics")} />
            <NavButton icon={<Settings />} label={t.nav.settings} active={page === "settings"} onClick={() => setPage("settings")} />
            <NavButton icon={<ShieldCheck />} label={t.nav.about} active={page === "about"} onClick={() => setPage("about")} />
            <div className="sidebar-footer">{t.app.footer}</div>
          </aside>

          <main className="main-panel">
            <GlobalStatusBar t={t} status={globalStatus} proxyPort={status.proxy?.port} busy={isBusy} onRefresh={() => void refreshStatus()} theme={theme} onTheme={setTheme} />
            {lastError ? <div className="error-strip"><AlertTriangle size={16} />{lastError}</div> : null}
            {page === "overview" && <OverviewPage t={t} status={status} summary={summary} onRepair={() => runAction(() => sidecar.repair())} onOpenCodex={() => runAction(() => sidecar.openCodex())} />}
            {page === "apps" && <ConnectedAppsPage t={t} status={status} onOpenCodex={() => setPage("codex")} />}
            {page === "codex" && <CodexDetailPage t={t} language={language} status={status} models={visibleModels} onRepair={() => runAction(() => sidecar.repair())} onPatch={() => runAction(() => sidecar.patchEnhancements("/Applications/Codex.app"))} onSyncModels={() => runAction(() => sidecar.modelsSync())} />}
            {page === "wizard" && <SetupWizardPage t={t} deepLink={deepLink} status={status} onAuthorize={() => void handleDeepLinkAuth()} onPatch={() => runAction(() => sidecar.patchEnhancements("/Applications/Codex.app"))} />}
            {page === "diagnostics" && <DiagnosticsPage t={t} onDiagnose={() => runAction(() => sidecar.diagnose())} status={status} />}
            {page === "settings" && <SettingsPage t={t} language={language} onLanguage={setLanguage} />}
            {page === "about" && <AboutDistributionPage t={t} />}
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

function GlobalStatusBar({ t, status, proxyPort, busy, theme, onTheme, onRefresh }: { t: Translation; status: string; proxyPort?: number; busy: boolean; theme: "system" | "dark" | "light"; onTheme: (theme: "system" | "dark" | "light") => void; onRefresh: () => void }) {
  const tone = statusTone(status);
  return (
    <div className={`global-bar ${tone}`}>
      <span className={`status-dot ${tone}`} />
      <div>
        <div className="global-title">{statusLabel(status, t)}</div>
        <div className="global-subtitle">{t.global.subtitle}</div>
      </div>
      <div className="global-spacer" />
      <span className="pill">{t.global.proxyPort} {proxyPort || t.global.proxyStopped}</span>
      <button className="icon-button" onClick={onRefresh} aria-label={t.global.refresh}>
        <RefreshCw size={16} className={busy ? "spin" : ""} />
      </button>
      <button className="icon-button" onClick={() => onTheme(theme === "dark" ? "light" : "dark")} aria-label={t.global.toggleTheme}>
        {theme === "dark" ? <Sun size={16} /> : <Moon size={16} />}
      </button>
    </div>
  );
}

function OverviewPage({ t, status, summary, onRepair, onOpenCodex }: { t: Translation; status: DesktopStatus; summary: ReturnType<typeof summarizeCatalog>; onRepair: () => void; onOpenCodex: () => void }) {
  return (
    <section className="content">
      <PageHeader title={t.overview.title} subtitle={t.overview.subtitle} />
      <div className="metric-grid">
        <Metric title={t.overview.globalStatus} value={statusLabel(status.global_status || status.status || "not_connected", t)} icon={<Activity />} />
        <Metric title={t.overview.modelCatalog} value={modelCountText(summary.modelCount, t)} icon={<Boxes />} />
        <Metric title={t.overview.mainListModels} value={modelCountText(summary.mainListCount, t)} icon={<BadgeCheck />} />
        <Metric title={t.overview.missingPricing} value={modelCountText(summary.missingPricingCount, t)} icon={<AlertTriangle />} />
      </div>
      <div className="two-column">
        <HealthCheckList t={t} status={status} />
        <div className="card">
          <div className="card-title">{t.overview.quickActions}</div>
          <button className="primary-action" onClick={onRepair}><Wrench size={16} />{t.actions.repairCodex}</button>
          <button className="secondary-action" onClick={onOpenCodex}><ExternalLink size={16} />{t.actions.openCodex}</button>
          <div className="hint">{t.overview.repairHint}</div>
        </div>
      </div>
    </section>
  );
}

function ConnectedAppsPage({ t, status, onOpenCodex }: { t: Translation; status: DesktopStatus; onOpenCodex: () => void }) {
  return (
    <section className="content">
      <PageHeader title={t.apps.title} subtitle={t.apps.subtitle} />
      <button className="app-card" onClick={onOpenCodex}>
        <div className="app-icon">C</div>
        <div>
          <div className="app-title">Codex App</div>
          <div className="app-subtitle">{statusLabel(status.adapters?.codex?.status || "not_configured", t)}</div>
        </div>
        <ChevronRight size={18} />
      </button>
      <div className="disabled-app"><div className="app-icon muted">A</div><div><div className="app-title">{t.apps.claude}</div><div className="app-subtitle">{t.apps.planned}</div></div></div>
      <div className="disabled-app"><div className="app-icon muted">+</div><div><div className="app-title">{t.apps.custom}</div><div className="app-subtitle">{t.apps.v2Reserved}</div></div></div>
    </section>
  );
}

function CodexDetailPage({ t, language, status, models, onRepair, onPatch, onSyncModels }: { t: Translation; language: Language; status: DesktopStatus; models: CatalogModel[]; onRepair: () => void; onPatch: () => void; onSyncModels: () => void }) {
  return (
    <section className="content">
      <PageHeader title={t.codex.title} subtitle={t.codex.subtitle} />
      <div className="two-column">
        <CodexEnhancementsCard t={t} enhancements={status.adapters?.codex?.enhancements} restartRequired={Boolean(status.adapters?.codex?.restart_required)} onPatch={onPatch} />
        <HealthCheckList t={t} status={status} />
      </div>
      <ModelCatalogTable t={t} language={language} models={models} onSyncModels={onSyncModels} />
      <div className="action-row">
        <button className="primary-action inline" onClick={onRepair}><Wrench size={16} />{t.actions.repair}</button>
      </div>
    </section>
  );
}

function SetupWizardPage({ t, deepLink, status, onAuthorize, onPatch }: { t: Translation; deepLink: DeepLinkRoute | null; status: DesktopStatus; onAuthorize: () => void; onPatch: () => void }) {
  const steps = [
    [t.wizard.receivedAuth, deepLink ? "done" : "pending"],
    [t.wizard.detectCodex, status.adapters?.codex?.status !== "not_configured" ? "done" : "pending"],
    [t.wizard.injectAuth, status.authorization?.status === "configured" || status.status === "configured" ? "done" : "pending"],
    [t.wizard.startProxy, status.proxy?.port ? "done" : "pending"],
    [t.wizard.enableCodexEnhancements, status.adapters?.codex?.enhancements ? "done" : "pending"],
    [t.wizard.healthCheck, status.global_status === "running" || status.global_status === "configured" ? "done" : "pending"],
    [t.wizard.done, status.status === "configured" ? "done" : "pending"]
  ] as const;
  return (
    <section className="content">
      <PageHeader title={t.wizard.title} subtitle={t.wizard.subtitle} />
      <div className="wizard">
        {steps.map(([label, state], index) => (
          <div className={`wizard-step ${state}`} key={label}>
            <span>{index + 1}</span>
            <div>{label}</div>
          </div>
        ))}
      </div>
      <div className="action-row">
        <button className="primary-action inline" disabled={!deepLink || deepLink.action === "open"} onClick={onAuthorize}><KeyRound size={16} />{t.actions.authorize}</button>
        <button className="secondary-action inline" onClick={onPatch}><PlugZap size={16} />{t.actions.enableEnhancements}</button>
      </div>
    </section>
  );
}

function DiagnosticsPage({ t, status, onDiagnose }: { t: Translation; status: DesktopStatus; onDiagnose: () => void }) {
  return (
    <section className="content">
      <PageHeader title={t.diagnostics.title} subtitle={t.diagnostics.subtitle} />
      <div className="card">
        <div className="card-title">{t.diagnostics.reportTitle}</div>
        <div className="code-block">{JSON.stringify({ status: status.status, proxy: status.proxy, authorization: status.authorization }, null, 2)}</div>
        <button className="secondary-action inline" onClick={onDiagnose}><Copy size={16} />{t.actions.copyDiagnostics}</button>
      </div>
    </section>
  );
}

function SettingsPage({ t, language, onLanguage }: { t: Translation; language: Language; onLanguage: (language: Language) => void }) {
  return (
    <section className="content">
      <PageHeader title={t.settings.title} subtitle={t.settings.subtitle} />
      <div className="settings-list">
        <LanguageSetting t={t} language={language} onLanguage={onLanguage} />
        <SettingRow icon={<SlidersHorizontal />} title={t.settings.proxyPolicy} value={t.settings.proxyPolicyValue} />
        <SettingRow icon={<LockKeyhole />} title={t.settings.strictGate} value={t.settings.strictGateValue} />
        <SettingRow icon={<PackageCheck />} title={t.settings.autoUpdate} value={t.settings.autoUpdateValue} disabled />
      </div>
    </section>
  );
}

function AboutDistributionPage({ t }: { t: Translation }) {
  return (
    <section className="content">
      <PageHeader title={t.distribution.title} subtitle={t.distribution.subtitle} />
      <div className="two-column">
        <div className="card">
          <div className="card-title">{t.distribution.releasePath}</div>
          <p>{t.distribution.releaseCopy}</p>
        </div>
        <div className="card">
          <div className="card-title">{t.distribution.safetyBoundary}</div>
          <p>{t.distribution.safetyCopy}</p>
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

function HealthCheckList({ t, status }: { t: Translation; status: DesktopStatus }) {
  const checks = [
    [t.health.authorization, status.authorization?.device_id ? "ok" : "warn", status.authorization?.device_id ? `${t.health.device} ${status.authorization.device_id}` : t.health.notConnected],
    [t.health.proxy, status.proxy?.port ? "ok" : "warn", status.proxy?.port ? `127.0.0.1:${status.proxy.port}` : t.health.stopped],
    [t.health.backendGateway, status.backend?.gateway_base_url ? "ok" : "warn", status.backend?.gateway_base_url || t.health.notSynced],
    [t.health.modelCatalog, status.model_catalog?.model_count ? "ok" : "warn", modelCountText(status.model_catalog?.model_count || 0, t)]
  ] as const;
  return (
    <div className="card">
      <div className="card-title">{t.health.title}</div>
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

function CodexEnhancementsCard({ t, enhancements, restartRequired, onPatch }: { t: Translation; enhancements?: Record<string, unknown>; restartRequired: boolean; onPatch: () => void }) {
  const items = (enhancements?.items || enhancements || {}) as Record<string, { status?: string }>;
  return (
    <div className="card">
      <div className="card-title">{t.enhancements.title}</div>
      {["model-picker", "plugin-auth-gate", "plugin-mention-marketplace"].map((item) => (
        <div className="enhancement-row" key={item}>
          <TerminalSquare size={16} />
          <span>{enhancementName(item, t)}</span>
          <em>{statusLabel(items[item]?.status || "unknown", t)}</em>
        </div>
      ))}
      {restartRequired ? <div className="warning-note"><AlertTriangle size={15} />{t.enhancements.restartRequired}</div> : null}
      <button className="secondary-action inline" onClick={onPatch}><PlugZap size={16} />{t.actions.enableAllEnhancements}</button>
    </div>
  );
}

function ModelCatalogTable({ t, language, models, onSyncModels }: { t: Translation; language: Language; models: CatalogModel[]; onSyncModels: () => void }) {
  const [filter, setFilter] = useState<ModelFilter>({ query: "", provider: "all", capability: "all" });
  const filtered = useMemo(() => filterCatalogModels(models, filter), [models, filter]);
  const providers = providerOptions(models);
  return (
    <div className="card model-card">
      <div className="table-toolbar">
        <div>
          <div className="card-title">{t.modelCatalog.title}</div>
          <div className="muted-text">{filtered.length} / {models.length} {t.modelCatalog.modelUnit}</div>
        </div>
        <label className="search-field"><Search size={15} /><input value={filter.query} onChange={(event) => setFilter({ ...filter, query: event.target.value })} placeholder={t.modelCatalog.searchPlaceholder} /></label>
        <select value={filter.provider} onChange={(event) => setFilter({ ...filter, provider: event.target.value })}>
          <option value="all">{t.modelCatalog.allProviders}</option>
          {providers.map((provider) => <option value={provider} key={provider}>{provider}</option>)}
        </select>
        <select value={filter.capability} onChange={(event) => setFilter({ ...filter, capability: event.target.value as ModelFilter["capability"] })}>
          <option value="all">{t.modelCatalog.allCapabilities}</option>
          <option value="responses">{t.modelCatalog.responses}</option>
          <option value="streaming">{t.modelCatalog.streaming}</option>
          <option value="tool_calls">{t.modelCatalog.toolCalls}</option>
          <option value="context_continuation">{t.modelCatalog.contextContinuation}</option>
        </select>
        <button className="secondary-action inline" onClick={onSyncModels}><RefreshCw size={16} />{t.actions.sync}</button>
      </div>
      <div className="model-table-wrap">
        <table className="model-table">
          <thead>
            <tr><th>{t.modelCatalog.model}</th><th>{t.modelCatalog.provider}</th><th>{t.modelCatalog.capabilities}</th><th>{t.modelCatalog.pricing}</th><th>{t.modelCatalog.status}</th></tr>
          </thead>
          <tbody>
            {filtered.map((model) => <ModelRow t={t} language={language} model={model} key={model.slug} />)}
            {!filtered.length ? <tr><td colSpan={5} className="empty-cell">{t.modelCatalog.empty}</td></tr> : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ModelRow({ t, language, model }: { t: Translation; language: Language; model: CatalogModel }) {
  const compatible = modelIsCompatible(model);
  return (
    <tr>
      <td><strong>{model.display_name || model.slug}</strong><span>{model.slug}</span></td>
      <td>{model.provider_id || model.origin || "unknown"}</td>
      <td><div className="cap-list">{["responses", "streaming", "tool_calls", "context_continuation"].map((cap) => <span className={model.capabilities?.[cap] ? "on" : "off"} key={cap}>{capLabel(cap, t)}</span>)}</div></td>
      <td><ModelPriceTooltip t={t} language={language} model={model} /></td>
      <td><span className={`status-chip ${compatible ? "ok" : "warn"}`}>{compatible ? t.modelCatalog.available : t.modelCatalog.limited}</span></td>
    </tr>
  );
}

function ModelPriceTooltip({ t, language, model }: { t: Translation; language: Language; model: CatalogModel }) {
  const rows = modelPriceRows(model, language);
  return (
    <span className="price-cell">
      {t.modelCatalog.pricingTrigger}
      <span className="price-popover">
        {rows.map(([label, value]) => <span key={label}><b>{label}</b><em>{value}</em></span>)}
      </span>
    </span>
  );
}

function LanguageSetting({ t, language, onLanguage }: { t: Translation; language: Language; onLanguage: (language: Language) => void }) {
  return (
    <div className="setting-row language-setting">
      <SlidersHorizontal />
      <div>
        <div>{t.settings.languageTitle}</div>
        <span>{t.settings.languageDescription}</span>
      </div>
      <div className="segmented-control" aria-label={t.settings.languageTitle}>
        <button className={language === "zh" ? "selected" : ""} onClick={() => onLanguage("zh")}>{t.settings.chinese}</button>
        <button className={language === "en" ? "selected" : ""} onClick={() => onLanguage("en")}>{t.settings.english}</button>
      </div>
    </div>
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

function statusLabel(status: string, t: Translation) {
  if (status === "unknown") {
    return t.enhancements.unknown;
  }
  return t.status[status as keyof Translation["status"]] || status;
}

function enhancementName(item: string, t: Translation) {
  const labels: Record<string, string> = {
    "model-picker": t.enhancements.modelPicker,
    "plugin-auth-gate": t.enhancements.pluginAuthGate,
    "plugin-mention-marketplace": t.enhancements.pluginMentionMarketplace
  };
  return labels[item] || item;
}

function capLabel(cap: string, t: Translation) {
  return t.capabilities[cap as keyof Translation["capabilities"]] || cap;
}

function modelCountText(count: number, t: Translation) {
  return `${count} ${t.modelCatalog.modelUnit}`;
}

function readInitialLanguage(): Language {
  const stored = window.localStorage.getItem(LANGUAGE_STORAGE_KEY);
  return isLanguage(stored) ? stored : "zh";
}

export default App;
