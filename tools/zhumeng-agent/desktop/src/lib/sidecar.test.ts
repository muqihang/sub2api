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
    await client.patchEnhancements("/Applications/Codex.app");

    expect(invoke).toHaveBeenNthCalledWith(1, "run_sidecar", {
      args: ["desktop", "status", "--json"],
      timeoutMs: 5000
    });
    expect(invoke).toHaveBeenNthCalledWith(2, "run_sidecar", {
      args: ["desktop", "models", "status", "--client", "codex", "--json"],
      timeoutMs: 5000
    });
    expect(invoke).toHaveBeenNthCalledWith(3, "run_sidecar", {
      args: ["desktop", "codex-enhancements", "patch", "--app", "/Applications/Codex.app", "--item", "all", "--json"],
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
