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
  Globe,
  Info,
  KeyRound,
  ListChecks,
  LockKeyhole,
  Moon,
  PackageCheck,
  PlugZap,
  RefreshCw,
  Search,
  Settings,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  Sun,
  TerminalSquare,
  Wrench
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { parseZhumengDeepLink } from "./lib/deeplink";
import { isLanguage, translations, type Language, type Translation } from "./lib/i18n";
import {
  ZHUMENG_CONSOLE_URL,
  ZHUMENG_DOCS_URL,
  ZHUMENG_WEBSITE_URL,
  openExternal
} from "./lib/links";
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
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">{t.app.mark}</div>
          <div>
            <div className="brand-title">{t.app.name}</div>
            <div className="brand-subtitle">{t.app.subtitle}</div>
          </div>
        </div>
        <nav className="nav-list" aria-label={t.nav.sectionLabel}>
          <NavButton icon={<Gauge />} label={t.nav.overview} active={page === "overview"} onClick={() => setPage("overview")} />
          <NavButton icon={<Boxes />} label={t.nav.apps} active={page === "apps"} onClick={() => setPage("apps")} />
          <NavButton icon={<AppWindow />} label={t.nav.codex} active={page === "codex"} onClick={() => setPage("codex")} badge={status.adapters?.codex?.restart_required ? t.nav.restart : undefined} />
          <NavButton icon={<ListChecks />} label={t.nav.wizard} active={page === "wizard"} onClick={() => setPage("wizard")} />
          <NavButton icon={<FileWarning />} label={t.nav.diagnostics} active={page === "diagnostics"} onClick={() => setPage("diagnostics")} />
          <NavButton icon={<Settings />} label={t.nav.settings} active={page === "settings"} onClick={() => setPage("settings")} />
          <NavButton icon={<ShieldCheck />} label={t.nav.about} active={page === "about"} onClick={() => setPage("about")} />
        </nav>
        <SidebarWebsiteLink t={t} />
      </aside>

      <main className="main-panel">
        <GlobalStatusBar t={t} status={globalStatus} proxyPort={status.proxy?.port} busy={isBusy} onRefresh={() => void refreshStatus()} theme={theme} onTheme={setTheme} />
        {lastError ? <div className="error-strip"><AlertTriangle size={16} />{lastError}</div> : null}
        <div className="page-scroll">
          {page === "overview" && <OverviewPage t={t} status={status} summary={summary} onRepair={() => runAction(() => sidecar.repair())} onOpenCodex={() => runAction(() => sidecar.openCodex())} />}
          {page === "apps" && <ConnectedAppsPage t={t} status={status} onOpenCodex={() => setPage("codex")} />}
          {page === "codex" && <CodexDetailPage t={t} language={language} status={status} models={visibleModels} onRepair={() => runAction(() => sidecar.repair())} onPatch={() => runAction(() => sidecar.patchEnhancements("/Applications/Codex.app"))} onSyncModels={() => runAction(() => sidecar.modelsSync())} onGoWizard={() => setPage("wizard")} />}
          {page === "wizard" && <SetupWizardPage t={t} deepLink={deepLink} status={status} onAuthorize={() => void handleDeepLinkAuth()} onPatch={() => runAction(() => sidecar.patchEnhancements("/Applications/Codex.app"))} />}
          {page === "diagnostics" && <DiagnosticsPage t={t} onDiagnose={() => runAction(() => sidecar.diagnose())} status={status} />}
          {page === "settings" && <SettingsPage t={t} language={language} onLanguage={setLanguage} />}
          {page === "about" && <AboutDistributionPage t={t} />}
        </div>
      </main>
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

function SidebarWebsiteLink({ t }: { t: Translation }) {
  return (
    <button
      className="sidebar-link"
      onClick={() => void openExternal(ZHUMENG_WEBSITE_URL)}
      aria-label={t.websiteCta.sidebarVisit}
    >
      <span className="sidebar-link-icon">
        <Globe size={14} />
      </span>
      <span className="sidebar-link-body">
        <span className="sidebar-link-title">{t.websiteCta.sidebarVisit}</span>
        <span className="sidebar-link-hint">{t.websiteCta.sidebarHint}</span>
      </span>
      <ExternalLink size={13} className="sidebar-link-arrow" />
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
  const proxyPort = status.proxy?.port;
  const proxyEndpoint = proxyPort ? `127.0.0.1:${proxyPort}` : t.overview.runtimeNotReady;
  const deviceId = status.authorization?.device_id;
  const deviceLabel = deviceId ? `#${deviceId}` : t.overview.deviceUnknown;

  return (
    <section className="content">
      <PageHeader title={t.overview.title} subtitle={t.overview.subtitle} />
      <div className="metric-grid">
        <Metric title={t.overview.globalStatus} value={statusLabel(status.global_status || status.status || "not_connected", t)} icon={<Activity />} />
        <Metric title={t.overview.modelCatalog} value={modelCountText(summary.modelCount, t)} icon={<Boxes />} />
        <Metric title={t.overview.mainListModels} value={modelCountText(summary.mainListCount, t)} icon={<BadgeCheck />} />
        <Metric title={t.overview.missingPricing} value={modelCountText(summary.missingPricingCount, t)} icon={<AlertTriangle />} />
      </div>
      <div className="two-column overview-grid">
        <HealthCheckList t={t} status={status} />
        <div className="overview-side">
          <div className="card quick-actions-card">
            <div className="card-title">{t.overview.quickActions}</div>
            <button className="primary-action full" onClick={onRepair}>
              <Wrench size={14} />
              {t.actions.repairCodex}
            </button>
            <button className="secondary-action full" onClick={onOpenCodex}>
              <ExternalLink size={14} />
              {t.actions.openCodex}
            </button>
            <div className="hint">{t.overview.repairHint}</div>
            <div className="key-list">
              <div className="key-row">
                <span>{t.overview.proxyEndpoint}</span>
                <em>{proxyEndpoint}</em>
              </div>
              <div className="key-row">
                <span>{t.overview.deviceId}</span>
                <em>{deviceLabel}</em>
              </div>
            </div>
          </div>
          <WebsiteCallout
            t={t}
            title={t.websiteCta.overviewTitle}
            body={t.websiteCta.overviewBody}
            action={t.websiteCta.overviewAction}
            url={ZHUMENG_CONSOLE_URL}
          />
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

function CodexDetailPage({ t, language, status, models, onRepair, onPatch, onSyncModels, onGoWizard }: { t: Translation; language: Language; status: DesktopStatus; models: CatalogModel[]; onRepair: () => void; onPatch: () => void; onSyncModels: () => void; onGoWizard: () => void }) {
  const codexStatus = status.adapters?.codex?.status || "not_configured";
  const isConnected = codexStatus !== "not_configured";
  if (!isConnected) {
    return (
      <section className="content">
        <PageHeader title={t.codex.title} subtitle={t.codex.subtitle} />
        <div className="empty-state" data-testid="codex-empty-state">
          <div className="empty-state-icon"><AppWindow size={22} /></div>
          <div className="empty-state-title">{t.codex.emptyTitle}</div>
          <div className="empty-state-body">{t.codex.emptyBody}</div>
          <div className="empty-state-actions">
            <button className="primary-action" onClick={onGoWizard}>
              <ListChecks size={14} />
              {t.codex.goWizard}
            </button>
            <button className="secondary-action" onClick={() => void openExternal(ZHUMENG_WEBSITE_URL + "/docs/codex")}>
              <Globe size={14} />
              {t.codex.learnAtSite}
            </button>
          </div>
        </div>
      </section>
    );
  }

  return (
    <section className="content">
      <PageHeader title={t.codex.title} subtitle={t.codex.subtitle} />
      <div className="two-column">
        <CodexEnhancementsCard t={t} enhancements={status.adapters?.codex?.enhancements} restartRequired={Boolean(status.adapters?.codex?.restart_required)} onPatch={onPatch} />
        <HealthCheckList t={t} status={status} />
      </div>
      <ModelCatalogTable t={t} language={language} models={models} onSyncModels={onSyncModels} />
      <div className="action-row">
        <button className="primary-action" onClick={onRepair}><Wrench size={14} />{t.actions.repair}</button>
      </div>
    </section>
  );
}

function SetupWizardPage({ t, deepLink, status, onAuthorize, onPatch }: { t: Translation; deepLink: DeepLinkRoute | null; status: DesktopStatus; onAuthorize: () => void; onPatch: () => void }) {
  const steps: Array<{ label: string; hint: string; state: "done" | "pending" }> = [
    { label: t.wizard.receivedAuth, hint: t.wizard.receivedAuthHint, state: deepLink ? "done" : "pending" },
    { label: t.wizard.detectCodex, hint: t.wizard.detectCodexHint, state: status.adapters?.codex?.status && status.adapters.codex.status !== "not_configured" ? "done" : "pending" },
    { label: t.wizard.injectAuth, hint: t.wizard.injectAuthHint, state: status.authorization?.status === "configured" || status.status === "configured" ? "done" : "pending" },
    { label: t.wizard.startProxy, hint: t.wizard.startProxyHint, state: status.proxy?.port ? "done" : "pending" },
    { label: t.wizard.enableCodexEnhancements, hint: t.wizard.enableEnhancementsHint, state: status.adapters?.codex?.enhancements && Object.keys(status.adapters.codex.enhancements as Record<string, unknown>).length > 0 ? "done" : "pending" },
    { label: t.wizard.healthCheck, hint: t.wizard.healthCheckHint, state: status.global_status === "running" || status.global_status === "configured" ? "done" : "pending" },
    { label: t.wizard.done, hint: t.wizard.doneHint, state: status.status === "configured" ? "done" : "pending" }
  ];
  return (
    <section className="content">
      <PageHeader title={t.wizard.title} subtitle={t.wizard.subtitle} />
      <div className="wizard-stepper" data-testid="setup-wizard">
        {steps.map((step, index) => (
          <div className={`wizard-step ${step.state}`} key={step.label}>
            <div className="wizard-step-rail">
              <span className="wizard-step-index">{index + 1}</span>
              {index < steps.length - 1 ? <span className="wizard-step-connector" /> : null}
            </div>
            <div className="wizard-step-body">
              <div className="wizard-step-label">
                <span>{step.label}</span>
                <em>{step.state === "done" ? t.wizard.statusDone : t.wizard.statusPending}</em>
              </div>
              <div className="wizard-step-hint">{step.hint}</div>
            </div>
          </div>
        ))}
      </div>
      <div className="action-row">
        <button className="primary-action" disabled={!deepLink || deepLink.action === "open"} onClick={onAuthorize}>
          <KeyRound size={14} />
          {t.actions.authorize}
        </button>
        <button className="secondary-action" onClick={onPatch}>
          <PlugZap size={14} />
          {t.actions.enableEnhancements}
        </button>
      </div>
      <div className="wizard-help">
        <Info size={14} />
        <span>{t.wizard.needAuthCode}</span>
        <button className="inline-link" onClick={() => void openExternal(ZHUMENG_CONSOLE_URL)}>
          {t.websiteCta.wizardCta}
          <ExternalLink size={12} />
        </button>
      </div>
    </section>
  );
}

function DiagnosticsPage({ t, status, onDiagnose }: { t: Translation; status: DesktopStatus; onDiagnose: () => void }) {
  return (
    <section className="content">
      <PageHeader title={t.diagnostics.title} subtitle={t.diagnostics.subtitle} />
      <InfoCallout
        icon={<ShieldCheck size={16} />}
        title={t.diagnostics.calloutTitle}
        body={t.diagnostics.calloutBody}
      />
      <div className="card">
        <div className="card-title">{t.diagnostics.reportTitle}</div>
        <div className="code-block">{JSON.stringify({ status: status.status, proxy: status.proxy, authorization: status.authorization }, null, 2)}</div>
        <button className="secondary-action" onClick={onDiagnose}><Copy size={14} />{t.actions.copyDiagnostics}</button>
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
      <div className="callout-grid">
        <InfoCallout
          icon={<PackageCheck size={16} />}
          title={t.distribution.releasePath}
          body={t.distribution.releaseCopy}
        />
        <InfoCallout
          icon={<ShieldAlert size={16} />}
          title={t.distribution.safetyBoundary}
          body={t.distribution.safetyCopy}
        />
      </div>
      <div className="card website-card">
        <div className="website-card-icon"><Globe size={18} /></div>
        <div className="website-card-body">
          <div className="website-card-title">{t.distribution.websiteTitle}</div>
          <div className="website-card-text">{t.distribution.websiteCopy}</div>
        </div>
        <div className="website-card-actions">
          <button className="primary-action" onClick={() => void openExternal(ZHUMENG_WEBSITE_URL + "/download")}>
            <ExternalLink size={14} />
            {t.distribution.websiteAction}
          </button>
          <button className="secondary-action" onClick={() => void openExternal(ZHUMENG_DOCS_URL)}>
            <Info size={14} />
            {t.websiteCta.distributionDocs}
          </button>
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

function InfoCallout({ icon, title, body }: { icon: React.ReactNode; title: string; body: string }) {
  return (
    <div className="info-callout" data-testid="info-callout">
      <div className="info-callout-icon">{icon}</div>
      <div className="info-callout-body">
        <div className="info-callout-title">{title}</div>
        <div className="info-callout-text">{body}</div>
      </div>
    </div>
  );
}

function WebsiteCallout({ t, title, body, action, url }: { t: Translation; title: string; body: string; action: string; url: string }) {
  return (
    <div className="website-callout">
      <div className="website-callout-icon"><Globe size={16} /></div>
      <div className="website-callout-body">
        <div className="website-callout-title">{title}</div>
        <div className="website-callout-text">{body}</div>
        <button className="link-action compact" onClick={() => void openExternal(url)}>
          {action}
          <ExternalLink size={12} />
        </button>
      </div>
      <span className="sr-only">{t.websiteCta.learnMore}</span>
    </div>
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
      <button className="secondary-action full" onClick={onPatch}><PlugZap size={14} />{t.actions.enableAllEnhancements}</button>
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
        <div className="table-toolbar-heading">
          <div className="card-title">{t.modelCatalog.title}</div>
          <div className="muted-text">{filtered.length} / {models.length} {t.modelCatalog.modelUnit}</div>
        </div>
        <div className="table-toolbar-controls">
          <label className="search-field">
            <Search size={15} />
            <input value={filter.query} onChange={(event) => setFilter({ ...filter, query: event.target.value })} placeholder={t.modelCatalog.searchPlaceholder} />
          </label>
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
          <button className="icon-action" onClick={onSyncModels} aria-label={t.actions.sync} title={t.actions.sync}>
            <RefreshCw size={14} />
            <span className="icon-action-label">{t.actions.syncShort}</span>
          </button>
        </div>
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
