import type { DeepLinkRoute } from "./types";

export function parseZhumengDeepLink(rawUrl: string): DeepLinkRoute {
  const url = new URL(rawUrl);
  if (url.protocol !== "zhumeng-agent:") {
    throw new Error("unsupported deeplink scheme");
  }
  const action = url.hostname;
  if (action === "open") {
    const app = url.searchParams.get("app") || "";
    if (!app) {
      throw new Error("missing open parameters");
    }
    return { action: "open", app };
  }
  if (action === "setup" || action === "reauth") {
    const client = url.searchParams.get("client") || "";
    const code = url.searchParams.get("code") || "";
    const server = url.searchParams.get("server") || "";
    if (!client || !code || !server) {
      throw new Error("missing setup parameters");
    }
    return { action, client, code, server };
  }
  throw new Error("unsupported deeplink action");
}
