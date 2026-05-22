import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import App from "./App";

vi.mock("@tauri-apps/api/event", () => ({
  listen: vi.fn(async () => vi.fn())
}));

vi.mock("@tauri-apps/plugin-deep-link", () => ({
  getCurrent: vi.fn(async () => null),
  onOpenUrl: vi.fn(async () => vi.fn())
}));

vi.mock("./lib/sidecar", () => ({
  SidecarError: class SidecarError extends Error {
    code = "mock_error";
    status = "mock_error";
  },
  sidecar: {
    status: vi.fn(async () => ({
      status: "configured",
      global_status: "configured",
      proxy: { status: "configured", port: 51793 },
      authorization: { status: "configured", device_id: 9 },
      adapters: { codex: { status: "configured", enhancements: {}, restart_required: false } },
      model_catalog: { model_count: 0, models: [] }
    })),
    modelsStatus: vi.fn(async () => ({ models: [] })),
    repair: vi.fn(),
    openCodex: vi.fn(),
    setup: vi.fn(),
    reauth: vi.fn(),
    patchEnhancements: vi.fn(),
    modelsSync: vi.fn(),
    diagnose: vi.fn()
  }
}));

describe("App visual shell", () => {
  beforeEach(() => {
    document.documentElement.dataset.theme = "";
  });

  it("renders a macOS style window frame for web preview and Tauri", async () => {
    render(<App />);

    expect(await screen.findAllByText("逐梦注入工具")).toHaveLength(2);
    expect(screen.getByTestId("mac-window-frame")).toBeInTheDocument();
    expect(screen.getByTestId("mac-window-titlebar")).toBeInTheDocument();
    expect(screen.getAllByTestId("mac-window-control")).toHaveLength(3);
  });
});
