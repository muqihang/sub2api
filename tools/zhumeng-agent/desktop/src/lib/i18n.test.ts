import { describe, expect, it } from "vitest";

import { isLanguage, translations } from "./i18n";

describe("desktop i18n", () => {
  it("provides Chinese as the default-facing language and English as a full UI option", () => {
    expect(translations.zh.nav.overview).toBe("概览");
    expect(translations.en.nav.overview).toBe("Overview");
    expect(translations.en.settings.languageTitle).toBe("Language");
  });

  it("validates persisted language values", () => {
    expect(isLanguage("zh")).toBe(true);
    expect(isLanguage("en")).toBe(true);
    expect(isLanguage("fr")).toBe(false);
  });
});
