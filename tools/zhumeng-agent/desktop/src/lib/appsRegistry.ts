import type { DesktopStatus } from "./types";

export type AppId = "codex" | "claude" | "custom";

export type AppIconVariant = "primary" | "claude" | "muted";

export type AppWizardKind = "codex" | "coming-soon";

export type AppConnectionStatus = "connected" | "pending" | "planned" | "error";

export interface AppDefinition {
  id: AppId;
  initial: string;
  iconSrc?: string;
  iconVariant: AppIconVariant;
  supported: boolean;
  hasEnhancements: boolean;
  hasModelPreview: boolean;
  hasOpenAction: boolean;
  /** Path probe shown as fallback when the sidecar has not provided one. */
  defaultAppPath?: string;
  /** External URL used for "learn more / subscribe" buttons. */
  learnMorePath: string;
  /** Which wizard implementation should run for this app. */
  wizardKind: AppWizardKind;
  /**
   * Resolves a unified connection status from the sidecar payload. Apps
   * declared `supported: false` always return "planned".
   */
  resolveConnectionStatus(status: DesktopStatus): AppConnectionStatus;
}

function resolveCodexConnection(status: DesktopStatus): AppConnectionStatus {
  const adapter = status.adapters?.codex;
  const adapterStatus = adapter?.status;
  if (!adapterStatus || adapterStatus === "not_configured") {
    return "pending";
  }
  if (adapterStatus === "error") {
    return "error";
  }
  return "connected";
}

function resolveClaudeCodeConnection(status: DesktopStatus): AppConnectionStatus {
  const adapter = status.adapters?.claude_code;
  const adapterStatus = adapter?.status;
  if (!adapterStatus || adapterStatus === "not_configured") {
    return "pending";
  }
  if (adapterStatus === "error" || adapterStatus === "quarantined" || adapterStatus === "guard_bypass") {
    return "error";
  }
  return "connected";
}

function plannedConnection(): AppConnectionStatus {
  return "planned";
}

/** Apps shown in the hub. Order matters for default selection. */
const APP_LIST = [
  {
    id: "codex" as const,
    initial: "C",
    iconVariant: "primary" as const,
    supported: true,
    hasEnhancements: true,
    hasModelPreview: true,
    hasOpenAction: true,
    defaultAppPath: "/Applications/Codex.app",
    learnMorePath: "/docs/codex",
    wizardKind: "codex" as const,
    resolveConnectionStatus: resolveCodexConnection
  },
  {
    id: "claude" as const,
    initial: "A",
    iconVariant: "claude" as const,
    supported: true,
    hasEnhancements: false,
    hasModelPreview: true,
    hasOpenAction: true,
    defaultAppPath: "zhumeng-claude",
    learnMorePath: "/docs/claude-code",
    wizardKind: "coming-soon" as const,
    resolveConnectionStatus: resolveClaudeCodeConnection
  },
  {
    id: "custom" as const,
    initial: "+",
    iconVariant: "muted" as const,
    supported: false,
    hasEnhancements: false,
    hasModelPreview: false,
    hasOpenAction: false,
    learnMorePath: "/docs/custom",
    wizardKind: "coming-soon" as const,
    resolveConnectionStatus: plannedConnection
  }
] satisfies readonly AppDefinition[];

export const APPS: readonly AppDefinition[] = APP_LIST;

const APP_IDS: ReadonlySet<AppId> = new Set(APP_LIST.map((app) => app.id));

/** Distil a unified connection state per app from the sidecar status payload. */
export function appConnectionStatus(app: AppDefinition, status: DesktopStatus): AppConnectionStatus {
  if (!app.supported) {
    return "planned";
  }
  return app.resolveConnectionStatus(status);
}

export function findApp(id: AppId | string | null | undefined): AppDefinition {
  const match = APPS.find((app) => app.id === id);
  return match || APPS[0]!;
}

export function isAppId(value: unknown): value is AppId {
  return typeof value === "string" && APP_IDS.has(value as AppId);
}

/** Total counts apps that can become connected; planners are excluded. */
export function countConnected(status: DesktopStatus): { connected: number; total: number } {
  const supported = APPS.filter((app) => app.supported);
  const total = supported.length;
  const connected = supported.filter((app) => app.resolveConnectionStatus(status) === "connected").length;
  return { connected, total };
}
