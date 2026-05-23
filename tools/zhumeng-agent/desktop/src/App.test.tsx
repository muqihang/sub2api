import { fireEvent, render, screen } from "@testing-library/react";
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
    window.localStorage.clear();
  });

  it("renders inside the native window without a faux macOS shell", async () => {
    render(<App />);

    expect(await screen.findByText("逐梦注入工具")).toBeInTheDocument();
    expect(screen.queryByTestId("mac-window-frame")).not.toBeInTheDocument();
    expect(screen.queryByTestId("mac-window-titlebar")).not.toBeInTheDocument();
    expect(screen.queryAllByTestId("mac-window-control")).toHaveLength(0);
    expect(screen.queryByText("Mac MVP")).not.toBeInTheDocument();
    expect(screen.queryByText("桌面版 Mac MVP")).not.toBeInTheDocument();
    expect(screen.queryByText("官网下载安装 · 不走 Mac App Store")).not.toBeInTheDocument();
  });

  it("defaults to Chinese and switches the full shell to English from settings", async () => {
    render(<App />);

    expect(await screen.findAllByText("概览")).toHaveLength(2);
    expect(screen.getByText("设置")).toBeInTheDocument();

    fireEvent.click(screen.getByText("设置"));
    fireEvent.click(screen.getByRole("button", { name: "English" }));

    expect(screen.getByText("Overview")).toBeInTheDocument();
    expect(screen.getAllByText("Settings")).toHaveLength(2);
    expect(screen.getByText("Language")).toBeInTheDocument();
    expect(screen.queryByText("概览")).not.toBeInTheDocument();
    expect(screen.queryByText("Download from website · No Mac App Store")).not.toBeInTheDocument();
    expect(screen.queryByText("Desktop Mac MVP")).not.toBeInTheDocument();
    expect(window.localStorage.getItem("zhumeng-agent-desktop-language")).toBe("en");
  });
});
