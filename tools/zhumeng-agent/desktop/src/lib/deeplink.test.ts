import { describe, expect, it } from "vitest";

import { parseZhumengDeepLink } from "./deeplink";

describe("parseZhumengDeepLink", () => {
  it("parses setup and reauth links", () => {
    expect(parseZhumengDeepLink("zhumeng-agent://setup?client=codex&code=abc&server=https%3A%2F%2Fexample.com")).toEqual({
      action: "setup",
      client: "codex",
      code: "abc",
      server: "https://example.com"
    });

    expect(parseZhumengDeepLink("zhumeng-agent://reauth?client=codex&code=def&server=https%3A%2F%2Fexample.com").action).toBe("reauth");
  });

  it("parses open links", () => {
    expect(parseZhumengDeepLink("zhumeng-agent://open?app=codex")).toEqual({
      action: "open",
      app: "codex"
    });
  });

  it("rejects unsupported schemes and incomplete links", () => {
    expect(() => parseZhumengDeepLink("https://example.com")).toThrow("unsupported deeplink scheme");
    expect(() => parseZhumengDeepLink("zhumeng-agent://setup?client=codex")).toThrow("missing setup parameters");
  });
});
