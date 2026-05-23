import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import App from "./App";

vi.mock("@tauri-apps/api/event", () => ({
  listen: vi.fn(async () => vi.fn())
}));

vi.mock("@tauri-apps/plugin-deep-link", () => ({
  getCurrent: vi.fn(async () => null),
  onOpenUrl: vi.fn(async () => vi.fn())
}));

const openUrlMock: ReturnType<typeof vi.fn<(url: string) => Promise<void>>> = vi.fn(async (_url: string) => undefined);

vi.mock("@tauri-apps/plugin-opener", () => ({
  openUrl: openUrlMock
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
      adapters: { codex: { status: "not_configured", enhancements: {}, restart_required: false } },
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
    openUrlMock.mockClear();
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

  it("renders a website entry point in the sidebar that links to zhumeng.example.com", async () => {
    render(<App />);

    const link = await screen.findByRole("button", { name: "访问逐梦官网" });
    fireEvent.click(link);

    await waitFor(() => expect(openUrlMock).toHaveBeenCalledTimes(1));
    expect(openUrlMock.mock.calls[0]?.[0]).toMatch(/^https:\/\/zhumeng\.example\.com/);
  });

  it("uses a vertical stepper layout for the setup wizard", async () => {
    render(<App />);

    fireEvent.click(screen.getByText("接入向导"));

    const wizard = await screen.findByTestId("setup-wizard");
    expect(wizard.classList.contains("wizard-stepper")).toBe(true);
    expect(wizard.classList.contains("wizard")).toBe(false);
  });

  it("nudges users with a website CTA on the setup wizard", async () => {
    render(<App />);

    fireEvent.click(screen.getByText("接入向导"));

    const ctas = await screen.findAllByRole("button", { name: /前往逐梦控制台获取授权/ });
    expect(ctas.length).toBeGreaterThanOrEqual(1);
    fireEvent.click(ctas[0]!);

    await waitFor(() => expect(openUrlMock).toHaveBeenCalledTimes(1));
    expect(openUrlMock.mock.calls[0]?.[0]).toMatch(/^https:\/\/zhumeng\.example\.com\/codex/);
  });

  it("shows a Codex empty state when the app is not connected yet", async () => {
    render(<App />);

    fireEvent.click(screen.getByText("Codex App"));

    expect(await screen.findByTestId("codex-empty-state")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("renders distribution descriptions inside callout sections, not generic cards", async () => {
    render(<App />);

    fireEvent.click(screen.getByText("分发与安全"));

    const callouts = await screen.findAllByTestId("info-callout");
    expect(callouts.length).toBeGreaterThanOrEqual(2);
  });
});
