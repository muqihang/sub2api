import { describe, expect, it, vi } from "vitest";

import { createSidecarClient } from "./sidecar";

describe("sidecar client", () => {
  it("calls the stable desktop command family", async () => {
    const invoke = vi.fn(async () => ({
      ok: true,
      status: "configured",
      data: { global_status: "configured" }
    }));
    const client = createSidecarClient(invoke);

    await client.status();
    await client.modelsStatus();
    await client.openCodex();
    await client.setup("codex", "setup-code", "https://example.com");
    await client.reauth("codex", "reauth-code", "https://example.com");
    await client.patchEnhancements("/Applications/Codex.app");

    const calls = (invoke.mock.calls as unknown as Array<[string, { args: string[]; timeoutMs: number }]>).map(([, payload]) => payload.args);
    expect(calls).toEqual([
      ["desktop", "status", "--json"],
      ["desktop", "models", "status", "--client", "codex", "--json"],
      ["desktop", "open", "--app", "codex", "--json"],
      ["desktop", "setup", "--client", "codex", "--code", "setup-code", "--server", "https://example.com", "--json"],
      ["desktop", "reauth", "--client", "codex", "--code", "reauth-code", "--server", "https://example.com", "--json"],
      ["desktop", "codex-enhancements", "patch", "--app", "/Applications/Codex.app", "--item", "all", "--json"]
    ]);
    for (const args of calls) {
      expect(args[0]).toBe("desktop");
      expect(args).toContain("--json");
    }
    expect(invoke).toHaveBeenLastCalledWith("run_sidecar", {
      args: calls.at(-1),
      timeoutMs: 20000
    });
  });

  it("throws structured errors when the envelope is not ok", async () => {
    const client = createSidecarClient(async () => ({
      ok: false,
      status: "not_configured",
      error: { code: "not_configured", message: "missing state" }
    }));

    await expect(client.status()).rejects.toMatchObject({
      code: "not_configured",
      message: "missing state"
    });
  });
});
