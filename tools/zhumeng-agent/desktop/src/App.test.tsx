import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import App from "./App";
import type { CatalogModel } from "./lib/types";

vi.mock("@tauri-apps/api/event", () => ({
  listen: vi.fn(async () => vi.fn())
}));

const sidecarHoisted = vi.hoisted(() => ({
  status: vi.fn(async () => ({
    status: "configured",
    global_status: "configured",
    proxy: { status: "configured", port: 51793 },
    authorization: { status: "configured", device_id: 9 },
    adapters: { codex: { status: "not_configured", enhancements: {}, restart_required: false } },
    model_catalog: { model_count: 0, models: [] }
  })),
  modelsStatus: vi.fn(async (): Promise<Record<string, unknown> & { models: CatalogModel[] }> => ({ models: [] })),
  repair: vi.fn(),
  openCodex: vi.fn(async () => undefined),
  openClaude: vi.fn(async () => undefined),
  setup: vi.fn(),
  reauth: vi.fn(),
  patchEnhancements: vi.fn(),
  modelsSync: vi.fn(),
  diagnose: vi.fn()
}));
const deepLinkHoisted = vi.hoisted(() => ({
  initial: { current: null as string[] | null }
}));
const initialDeepLinks = deepLinkHoisted.initial;
const sidecarStatusMock = sidecarHoisted.status;
const sidecarModelsStatusMock = sidecarHoisted.modelsStatus;
const openCodexMock = sidecarHoisted.openCodex;
const openClaudeMock = sidecarHoisted.openClaude;
const sidecarSetupMock = sidecarHoisted.setup;

vi.mock("@tauri-apps/plugin-deep-link", () => ({
  getCurrent: vi.fn(async () => deepLinkHoisted.initial.current),
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
  sidecarErrorMessage: (error: unknown) => {
    if (error instanceof Error) return error.message;
    if (error && typeof error === "object") {
      const envelope = error as { status?: string; error?: { code?: string; message?: string } };
      const code = envelope.error?.code || envelope.status || "sidecar_error";
      const message = envelope.error?.message || envelope.status || "sidecar command failed";
      return `${code}: ${message}`;
    }
    return String(error);
  },
  sidecar: sidecarHoisted
}));

describe("App visual shell", () => {
  beforeEach(() => {
    document.documentElement.dataset.theme = "";
    window.localStorage.clear();
    openUrlMock.mockClear();
    initialDeepLinks.current = null;
    sidecarStatusMock.mockClear();
    sidecarModelsStatusMock.mockReset();
    sidecarModelsStatusMock.mockResolvedValue({ models: [] });
    openCodexMock.mockClear();
    openClaudeMock.mockClear();
    sidecarSetupMock.mockClear();
    sidecarStatusMock.mockImplementation(async () => ({
      status: "configured",
      global_status: "configured",
      proxy: { status: "configured", port: 51793 },
      authorization: { status: "configured", device_id: 9 },
      adapters: { codex: { status: "not_configured", enhancements: {}, restart_required: false } },
      model_catalog: { model_count: 0, models: [] }
    }));
  });

  it("renders inside the native window without a faux macOS shell", async () => {
    render(<App />);

    expect(await screen.findByText("逐梦 Agent 增强器")).toBeInTheDocument();
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
    fireEvent.click(screen.getByRole("button", { name: "语言" }));
    fireEvent.click(await screen.findByRole("option", { name: "English" }));

    expect(screen.getByRole("button", { name: /Overview/ })).toBeInTheDocument();
    expect(screen.getByText("Settings")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Language" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /概览/ })).not.toBeInTheDocument();
    expect(screen.queryByText("Download from website · No Mac App Store")).not.toBeInTheDocument();
    expect(screen.queryByText("Desktop Mac MVP")).not.toBeInTheDocument();
    expect(window.localStorage.getItem("zhumeng-agent-desktop-language")).toBe("en");
  });

  it("removes the old language control from settings after moving it to the top bar", async () => {
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /设置/ }));

    expect(screen.queryAllByRole("button", { name: "语言" })).toHaveLength(1);
    expect(screen.queryByText("默认中文；切换后会保存到本机，下次打开继续使用。")).not.toBeInTheDocument();
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

  it("apps hub lists Claude Code as a supported app with an open action", async () => {
    sidecarStatusMock.mockImplementation(async () => ({
      status: "configured",
      global_status: "configured",
      proxy: { status: "configured", port: 51793 },
      authorization: { status: "configured", device_id: 9 },
      adapters: {
        codex: { status: "not_configured", enhancements: {}, restart_required: false },
        claude_code: { status: "ready", configured: true, running: false }
      },
      model_catalog: { model_count: 0, models: [] }
    }));
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /应用/ }));
    expect(await screen.findByTestId("apps-hub")).toBeInTheDocument();
    expect(screen.getByTestId("app-card-codex")).toBeInTheDocument();
    expect(screen.getByTestId("app-card-claude")).toBeInTheDocument();
    expect(screen.getByTestId("app-card-custom")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("app-card-claude"));
    expect(await screen.findByTestId("app-detail")).toBeInTheDocument();
    expect(screen.queryByTestId("app-detail-coming-soon")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "打开" }));
    await waitFor(() => expect(openClaudeMock).toHaveBeenCalledTimes(1));
  });

  it("setup wizard exposes an app picker and shows a coming-soon empty state for Claude", async () => {
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /接入向导/ }));
    const claudeTab = await screen.findByRole("tab", { name: /Claude Desktop/ });
    fireEvent.click(claudeTab);

    expect(await screen.findByTestId("wizard-coming-soon")).toBeInTheDocument();
    expect(screen.queryByTestId("setup-wizard")).not.toBeInTheDocument();
  });

  it("wizard app picker uses bundled app logo images", async () => {
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /接入向导/ }));
    const codexTab = await screen.findByRole("tab", { name: /Codex App/ });
    const claudeTab = await screen.findByRole("tab", { name: /Claude Desktop/ });
    expect(codexTab.querySelector('img[alt=""][src*="codex"]')).not.toBeNull();
    expect(claudeTab.querySelector('img[alt=""][src*="claude"]')).not.toBeNull();
  });

  it("model catalog lives at the top level and the Codex App page no longer renders the table", async () => {
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /模型目录/ }));
    expect(await screen.findByTestId("catalog-page")).toBeInTheDocument();
  });

  it("deep link open?app=claude routes to Claude Code and launches zhumeng-claude, not Codex", async () => {
    initialDeepLinks.current = ["zhumeng-agent://open?app=claude"];

    render(<App />);

    expect(await screen.findByTestId("app-detail")).toBeInTheDocument();
    await waitFor(() => expect(openClaudeMock).toHaveBeenCalledTimes(1));
    expect(openCodexMock).not.toHaveBeenCalled();
  });

  it("deep link with an unknown client does not navigate to the Codex wizard", async () => {
    initialDeepLinks.current = ["zhumeng-agent://setup?client=unknown&code=abc&server=https://api.example.com"];

    render(<App />);

    // Wait for the initial status refresh to settle.
    await waitFor(() => expect(sidecarStatusMock).toHaveBeenCalled());
    expect(screen.queryByTestId("setup-wizard")).not.toBeInTheDocument();
    expect(screen.queryByTestId("wizard-coming-soon")).not.toBeInTheDocument();
    // The default overview page should still be visible.
    expect(screen.getByRole("button", { name: /概览/ })).toHaveClass("active");
  });

  it("shows progress and success feedback when executing web authorization", async () => {
    initialDeepLinks.current = ["zhumeng-agent://setup?client=codex&code=abc&server=http%3A%2F%2F127.0.0.1%3A3080"];
    let resolveSetup: ((value: Awaited<ReturnType<typeof sidecarSetupMock>>) => void) | undefined;
    sidecarSetupMock.mockImplementation(() => new Promise((resolve) => {
      resolveSetup = resolve;
    }));
    const configuredStatus = {
      status: "configured",
      global_status: "configured",
      proxy: { status: "configured", port: 64645 },
      authorization: { status: "configured", device_id: 18 },
      adapters: { codex: { status: "configured", enhancements: {}, restart_required: false } },
      model_catalog: { model_count: 14, models: [] }
    };

    render(<App />);

    const authorizeButton = await screen.findByRole("button", { name: /执行授权/ });
    fireEvent.click(authorizeButton);

    expect(await screen.findByTestId("wizard-action-feedback")).toHaveTextContent("正在执行授权");
    await waitFor(() => expect(sidecarSetupMock).toHaveBeenCalledWith("codex", "abc", "http://127.0.0.1:3080"));
    resolveSetup?.(configuredStatus);
    expect(await screen.findByTestId("wizard-action-feedback")).toHaveTextContent("授权已写入");
  });

  it("keeps the authorization flow usable when the model catalog refresh times out", async () => {
    const warnMock = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    initialDeepLinks.current = ["zhumeng-agent://setup?client=codex&code=abc&server=http%3A%2F%2F127.0.0.1%3A3080"];
    sidecarHoisted.modelsStatus.mockRejectedValue({
      ok: false,
      status: "error",
      error: { code: "timeout", message: "sidecar timed out after 5000ms; stderr=" }
    });
    const configuredStatus = {
      status: "configured",
      global_status: "configured",
      proxy: { status: "configured", port: 64645 },
      authorization: { status: "configured", device_id: 18 },
      adapters: { codex: { status: "configured", enhancements: {}, restart_required: false } },
      model_catalog: { model_count: 14, models: [] }
    };
    sidecarSetupMock.mockResolvedValue(configuredStatus);

    render(<App />);

    const authorizeButton = await screen.findByRole("button", { name: /执行授权/ });
    expect(screen.queryByText(/sidecar timed out/)).not.toBeInTheDocument();

    fireEvent.click(authorizeButton);

    await waitFor(() => expect(sidecarSetupMock).toHaveBeenCalledWith("codex", "abc", "http://127.0.0.1:3080"));
    expect(await screen.findByTestId("wizard-action-feedback")).toHaveTextContent("授权已写入");
    expect(screen.queryByText(/sidecar timed out/)).not.toBeInTheDocument();
    warnMock.mockRestore();
  });

  it("shows an inline error when web authorization fails", async () => {
    initialDeepLinks.current = ["zhumeng-agent://setup?client=codex&code=used&server=http%3A%2F%2F127.0.0.1%3A3080"];
    sidecarSetupMock.mockRejectedValue(new Error("setup grant is invalid, expired, or already used"));

    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /执行授权/ }));

    expect(await screen.findByTestId("wizard-action-feedback")).toHaveTextContent("授权失败");
    expect(screen.getByTestId("wizard-action-feedback")).toHaveTextContent("setup grant is invalid");
  });

  it("apps hub keyboard activation enters the detail without triggering pending repair shortcut", async () => {
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /应用/ }));
    const tile = await screen.findByTestId("app-card-codex");
    tile.focus();
    expect(tile).toHaveFocus();
    fireEvent.keyDown(tile, { key: "Enter" });
    fireEvent.click(tile);
    expect(await screen.findByTestId("app-detail-empty-state")).toBeInTheDocument();
  });

  it("apps hub displays 3 / 3 enhancements when sidecar wraps items in { items }", async () => {
    sidecarStatusMock.mockImplementation(async () => ({
      status: "configured",
      global_status: "configured",
      proxy: { status: "configured", port: 51793 },
      authorization: { status: "configured", device_id: 9 },
      adapters: {
        codex: {
          status: "configured",
          enhancements: {
            items: {
              "model-picker": { status: "patched" },
              "plugin-auth-gate": { status: "patched" },
              "plugin-mention-marketplace": { status: "patched" }
            }
          },
          restart_required: false
        }
      },
      model_catalog: { model_count: 0, models: [] }
    }));

    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /应用/ }));
    const meta = await screen.findByText(/增强项 3 \/ 3/);
    expect(meta).toBeInTheDocument();
  });

  it("English settings page exposes the Apps navigation in English copy", async () => {
    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /设置/ }));
    fireEvent.click(screen.getByRole("button", { name: "语言" }));
    fireEvent.click(await screen.findByRole("option", { name: "English" }));

    fireEvent.click(await screen.findByRole("button", { name: /Apps/ }));
    expect(await screen.findByText("Coming soon (1)")).toBeInTheDocument();
  });

  it("uses the custom listbox popover for language and model filters", async () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "语言" }));
    expect(await screen.findByRole("listbox", { name: "语言" })).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("option", { name: "English" }));

    fireEvent.click(await screen.findByRole("button", { name: /Model Catalog/ }));
    fireEvent.click(screen.getByRole("button", { name: "All capabilities" }));
    expect(await screen.findByRole("listbox", { name: "All capabilities" })).toBeInTheDocument();
  });

  it("updates the overview model catalog health check after refreshing models", async () => {
    sidecarModelsStatusMock.mockResolvedValue({
      source: "gateway",
      models: [
        { slug: "gpt-5.5", capabilities: { responses: true, streaming: true, tool_calls: true, context_continuation: true } },
        { slug: "deepseek-chat", capabilities: { responses: true, streaming: true, tool_calls: true, context_continuation: true } }
      ]
    });

    render(<App />);

    await waitFor(() => expect(sidecarModelsStatusMock).toHaveBeenCalled());
    await waitFor(() => {
      const healthCard = screen.getByText("健康检查").closest(".card");
      expect(healthCard).not.toBeNull();
      expect(healthCard).toHaveTextContent("模型目录");
      expect(healthCard).toHaveTextContent("2 个模型");
    });
  });

  it("renders compact pricing in table cells instead of inline popovers", async () => {
    sidecarModelsStatusMock.mockResolvedValue({
      models: [
        {
          slug: "gpt-5.5",
          display_name: "GPT-5.5",
          provider_id: "openai",
          capabilities: { responses: true, streaming: true, tool_calls: true, context_continuation: true },
          pricing: {
            input_price: "2.50",
            output_price: "15.00",
            cached_input_price: "0.25",
            currency: "USD",
            unit: "per_1m_tokens"
          }
        }
      ]
    });

    render(<App />);

    fireEvent.click(await screen.findByRole("button", { name: /模型目录/ }));
    expect(await screen.findByText("输入 $2.50")).toBeInTheDocument();
    expect(screen.getByText("输出 $15.00")).toBeInTheDocument();
    expect(document.querySelector(".price-popover")).toBeNull();
  });
});
