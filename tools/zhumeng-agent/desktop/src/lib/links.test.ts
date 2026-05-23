import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const openerHoisted = vi.hoisted(() => ({
  openUrl: vi.fn(async (_url: string) => undefined)
}));

vi.mock("@tauri-apps/plugin-opener", () => openerHoisted);

describe("openExternal", () => {
  const originalWindowOpen = window.open;

  beforeEach(() => {
    openerHoisted.openUrl.mockReset();
    openerHoisted.openUrl.mockImplementation(async (_url: string) => undefined);
    window.open = vi.fn();
  });

  afterEach(() => {
    window.open = originalWindowOpen;
  });

  it("uses the Tauri opener plugin when available", async () => {
    const { openExternal } = await import("./links");

    await openExternal("https://zhumeng.example.com");

    expect(openerHoisted.openUrl).toHaveBeenCalledWith("https://zhumeng.example.com");
    expect(window.open).not.toHaveBeenCalled();
  });

  it("falls back to window.open when the opener plugin throws", async () => {
    openerHoisted.openUrl.mockImplementationOnce(async () => {
      throw new Error("bridge unavailable");
    });
    const { openExternal } = await import("./links");

    await openExternal("https://zhumeng.example.com/docs");

    expect(openerHoisted.openUrl).toHaveBeenCalledWith("https://zhumeng.example.com/docs");
    expect(window.open).toHaveBeenCalledWith(
      "https://zhumeng.example.com/docs",
      "_blank",
      "noopener,noreferrer"
    );
  });
});
