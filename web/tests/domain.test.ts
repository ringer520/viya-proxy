import { describe, expect, it, vi } from "vitest";

// i18n.ts touches localStorage/document at import time; keep the pure helpers
// testable in a node environment by stubbing the translation functions.
vi.mock("../src/i18n", () => ({
  t: (text: string) => text,
  tr: (parts: TemplateStringsArray, ...values: unknown[]) =>
    parts.reduce((out, part, i) => out + (i ? String(values[i - 1] ?? "") : "") + part, ""),
}));

import {
  canonicalDomain,
  isWithinDomain,
  registrableDomain,
  siteIconStyle,
} from "../src/utils/domain";

describe("domain helpers", () => {
  it("normalises wildcard prefixes and casing", () => {
    expect(canonicalDomain("*.Example.COM")).toBe("example.com");
    expect(canonicalDomain("NAS.Example.com")).toBe("nas.example.com");
    expect(canonicalDomain("example.com")).toBe("example.com");
  });

  it("matches a domain and its subdomains, but not lookalikes", () => {
    expect(isWithinDomain("app.example.com", "example.com")).toBe(true);
    expect(isWithinDomain("a.b.example.com", "example.com")).toBe(true);
    expect(isWithinDomain("example.com", "example.com")).toBe(true);
    expect(isWithinDomain("notexample.com", "example.com")).toBe(false);
    expect(isWithinDomain("example.com", "app.example.com")).toBe(false);
  });

  it("folds subdomains to the registrable domain, including two-label suffixes", () => {
    expect(registrableDomain("app.example.com")).toBe("example.com");
    expect(registrableDomain("a.b.example.co.uk")).toBe("example.co.uk");
    expect(registrableDomain("example.com")).toBe("example.com");
    expect(registrableDomain("*.Blog.Example.COM")).toBe("example.com");
  });

  it("falls back to the first swatch for unknown icon keys", () => {
    expect(siteIconStyle("blue").color).toBe("#3b82f6");
    expect(siteIconStyle("rose").backgroundColor).toBe("#f43f5e18");
    expect(siteIconStyle("does-not-exist").color).toBe(siteIconStyle("blue").color);
  });
});
