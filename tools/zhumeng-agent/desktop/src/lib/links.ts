// Centralised entry points to the Zhumeng website. The hostnames below are
// placeholders that match the docs (`tools/zhumeng-agent/docs/*`) and should be
// pointed at the real production domain before the public release.
export const ZHUMENG_WEBSITE_URL = "https://zhumeng.example.com";
export const ZHUMENG_CONSOLE_URL = "https://zhumeng.example.com/codex";
export const ZHUMENG_DOCS_URL = "https://zhumeng.example.com/docs";

// Open a URL in the user's default browser. Uses the Tauri opener plugin when
// available (running inside the .app, where the IPC bridge is wired up) and
// falls back to `window.open` for the browser dev preview. Errors from Tauri
// (for example when the IPC bridge is missing during dev preview) are caught
// so the browser fallback can take over instead of surfacing platform errors.
export async function openExternal(url: string): Promise<void> {
  try {
    const opener = await import("@tauri-apps/plugin-opener");
    if (opener && typeof opener.openUrl === "function") {
      await opener.openUrl(url);
      return;
    }
  } catch {
    /* fall through to web fallback */
  }
  if (typeof window !== "undefined" && typeof window.open === "function") {
    window.open(url, "_blank", "noopener,noreferrer");
  }
}
