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

    expect(await screen.findByRole("button", { name: /概览/ })).toBeInTheDocument();
    expect(screen.getByText("设置")).toBeInTheDocument();

    fireEvent.click(screen.getByText("设置"));
    fireEvent.click(screen.getByRole("button", { name: "English" }));

    expect(screen.getByRole("button", { name: /Overview/ })).toBeInTheDocument();
    expect(screen.getAllByText("Settings")).toHaveLength(2);
    expect(screen.getByText("Language")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /概览/ })).not.toBeInTheDocument();
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

    fireEvent.click(screen.getByRole("button", { name: /接入向导/ }));

    const wizard = await screen.findByTestId("setup-wizard");
    expect(wizard.classList.contains("wizard-stepper")).toBe(true);
    expect(wizard.classList.contains("wizard")).toBe(false);
  });

  it("nudges users with a website CTA on the setup wizard", async () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: /接入向导/ }));

    const ctas = await screen.findAllByRole("button", { name: /前往逐梦控制台获取授权/ });
    expect(ctas.length).toBeGreaterThanOrEqual(1);
    fireEvent.click(ctas[0]!);

    await waitFor(() => expect(openUrlMock).toHaveBeenCalledTimes(1));
    expect(openUrlMock.mock.calls[0]?.[0]).toMatch(/^https:\/\/zhumeng\.example\.com\/codex/);
  });

  it("shows the Codex pending empty state from the apps hub when not connected", async () => {
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /应用/ }));
    fireEvent.click(await screen.findByTestId("app-card-codex"));

    expect(await screen.findByTestId("app-detail-empty-state")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("renders distribution descriptions inside callout sections, not generic cards", async () => {
    render(<App />);

    fireEvent.click(screen.getByText("分发与安全"));

    const callouts = await screen.findAllByTestId("info-callout");
    expect(callouts.length).toBeGreaterThanOrEqual(2);
  });

  it("apps hub lists every registered app and exposes a coming-soon state for Claude", async () => {
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /应用/ }));
    expect(await screen.findByTestId("apps-hub")).toBeInTheDocument();
    expect(screen.getByTestId("app-card-codex")).toBeInTheDocument();
    expect(screen.getByTestId("app-card-claude")).toBeInTheDocument();
    expect(screen.getByTestId("app-card-custom")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("app-card-claude"));
    expect(await screen.findByTestId("app-detail-coming-soon")).toBeInTheDocument();
  });

  it("setup wizard exposes an app picker and shows a coming-soon empty state for Claude", async () => {
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /接入向导/ }));
    const claudeTab = await screen.findByRole("tab", { name: /Claude Desktop/ });
    fireEvent.click(claudeTab);

    expect(await screen.findByTestId("wizard-coming-soon")).toBeInTheDocument();
    expect(screen.queryByTestId("setup-wizard")).not.toBeInTheDocument();
  });

  it("model catalog lives at the top level and the Codex App page no longer renders the table", async () => {
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /模型目录/ }));
    expect(await screen.findByTestId("catalog-page")).toBeInTheDocument();
  });
});
