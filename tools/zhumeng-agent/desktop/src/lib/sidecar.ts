import { invoke as tauriInvoke } from "@tauri-apps/api/core";

import type { DesktopStatus, SidecarEnvelope } from "./types";

export type InvokeFn = (command: string, args?: Record<string, unknown>) => Promise<SidecarEnvelope<unknown>>;

export interface SidecarClient {
  status(): Promise<DesktopStatus>;
  repair(): Promise<DesktopStatus>;
  diagnose(): Promise<Record<string, unknown>>;
  modelsStatus(): Promise<Record<string, unknown>>;
  modelsSync(): Promise<Record<string, unknown>>;
  openCodex(): Promise<Record<string, unknown>>;
  setup(client: string, code: string, server: string): Promise<DesktopStatus>;
  reauth(client: string, code: string, server: string): Promise<DesktopStatus>;
  enhancementsStatus(appPath: string): Promise<Record<string, unknown>>;
  patchEnhancements(appPath: string): Promise<Record<string, unknown>>;
}

export class SidecarError extends Error {
  code: string;
  status: string;

  constructor(envelope: SidecarEnvelope) {
    super(envelope.error?.message || envelope.status || "sidecar command failed");
    this.name = "SidecarError";
    this.code = envelope.error?.code || envelope.status;
    this.status = envelope.status;
  }
}

function isSidecarEnvelope(value: unknown): value is SidecarEnvelope {
  return !!value && typeof value === "object" && "ok" in value && "status" in value;
}

export function sidecarErrorMessage(error: unknown): string {
  if (error instanceof SidecarError) {
    return `${error.code}: ${error.message}`;
  }
  if (isSidecarEnvelope(error)) {
    const code = error.error?.code || error.status || "sidecar_error";
    const message = error.error?.message || error.status || "sidecar command failed";
    return `${code}: ${message}`;
  }
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
  try {
    return JSON.stringify(error);
  } catch {
    return "sidecar command failed";
  }
}

export function createSidecarClient(invokeFn: InvokeFn = tauriInvoke as InvokeFn): SidecarClient {
  const run = async <T>(args: string[], timeoutMs = 5000): Promise<T> => {
    let envelope: SidecarEnvelope<unknown>;
    try {
      envelope = await invokeFn("run_sidecar", { args, timeoutMs });
    } catch (error) {
      if (isSidecarEnvelope(error)) {
        throw new SidecarError(error);
      }
      throw error;
    }
    if (!envelope.ok) {
      throw new SidecarError(envelope);
    }
    return (envelope.data || {}) as T;
  };

  return {
    status: () => run<DesktopStatus>(["desktop", "status", "--json"], 10000),
    repair: () => run<DesktopStatus>(["desktop", "repair", "--client", "codex", "--json"], 20000),
    diagnose: () => run<Record<string, unknown>>(["desktop", "diagnose", "--redacted", "--json"], 10000),
    modelsStatus: () => run<Record<string, unknown>>(["desktop", "models", "status", "--client", "codex", "--json"], 15000),
    modelsSync: () => run<Record<string, unknown>>(["desktop", "models", "sync", "--client", "codex", "--json"], 20000),
    openCodex: () => run<Record<string, unknown>>(["desktop", "open", "--app", "codex", "--json"]),
    setup: (client, code, server) => run<DesktopStatus>(["desktop", "setup", "--client", client, "--code", code, "--server", server, "--json"], 30000),
    reauth: (client, code, server) => run<DesktopStatus>(["desktop", "reauth", "--client", client, "--code", code, "--server", server, "--json"], 30000),
    enhancementsStatus: (appPath) =>
      run<Record<string, unknown>>(["desktop", "codex-enhancements", "status", "--app", appPath, "--json"], 10000),
    patchEnhancements: (appPath) =>
      run<Record<string, unknown>>(["desktop", "codex-enhancements", "patch", "--app", appPath, "--item", "all", "--json"], 20000)
  };
}

export const sidecar = createSidecarClient();
