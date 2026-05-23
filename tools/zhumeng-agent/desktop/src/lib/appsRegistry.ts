import type { DesktopStatus } from "./types";

export type AppId = "codex" | "claude" | "custom";

export type AppIconVariant = "primary" | "claude" | "muted";

export interface AppDefinition {
  id: AppId;
  initial: string;
  iconVariant: AppIconVariant;
  supported: boolean;
  hasEnhancements: boolean;
  hasModelPreview: boolean;
  hasOpenAction: boolean;
  /** Path probe shown as fallback when the sidecar has not provided one. */
  defaultAppPath?: string;
  /** External URL used for "learn more / subscribe" buttons. */
  learnMorePath: string;
}

/** Apps shown in the hub. Order matters for default selection. */
export const APPS: AppDefinition[] = [
  {
    id: "codex",
    initial: "C",
    iconVariant: "primary",
    supported: true,
    hasEnhancements: true,
    hasModelPreview: true,
    hasOpenAction: true,
    defaultAppPath: "/Applications/Codex.app",
    learnMorePath: "/docs/codex"
  },
  {
    id: "claude",
    initial: "A",
    iconVariant: "claude",
    supported: false,
    hasEnhancements: false,
    hasModelPreview: false,
    hasOpenAction: false,
    learnMorePath: "/docs/claude"
  },
  {
    id: "custom",
    initial: "+",
    iconVariant: "muted",
    supported: false,
    hasEnhancements: false,
    hasModelPreview: false,
    hasOpenAction: false,
    learnMorePath: "/docs/custom"
  }
];

export type AppConnectionStatus = "connected" | "pending" | "planned" | "error";

/** Distil a unified connection state per app from the sidecar status payload. */
export function appConnectionStatus(app: AppDefinition, status: DesktopStatus): AppConnectionStatus {
  if (!app.supported) {
    return "planned";
  }
  if (app.id === "codex") {
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
  return "pending";
}

export function findApp(id: AppId | string | null | undefined): AppDefinition {
  const match = APPS.find((app) => app.id === id);
  return match || APPS[0]!;
}

export function isAppId(value: unknown): value is AppId {
  return value === "codex" || value === "claude" || value === "custom";
}

export function countConnected(status: DesktopStatus): { connected: number; total: number } {
  const total = APPS.length;
  const connected = APPS.filter((app) => appConnectionStatus(app, status) === "connected").length;
  return { connected, total };
}
