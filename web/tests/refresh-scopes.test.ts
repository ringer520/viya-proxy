import { beforeEach, describe, expect, it, vi } from "vitest";

const { calls } = vi.hoisted(() => ({ calls: [] as string[] }));

vi.mock("../src/api", () => ({
  apiFetch: (url: string) => {
    calls.push(url);
    return Promise.resolve({ ok: true, status: 200, json: async () => [] });
  },
}));

vi.mock("../src/i18n", () => ({
  t: (text: string) => text,
  tr: (parts: TemplateStringsArray, ...values: unknown[]) =>
    parts.reduce((out, part, i) => out + (i ? String(values[i - 1] ?? "") : "") + part, ""),
}));

beforeEach(() => {
  calls.length = 0;
  vi.resetModules();
});

// A fresh module instance per test resets the private loadedScopes set.
async function fresh() {
  return import("../src/composables/useRefresh");
}

describe("loadSection / per-page lazy loading", () => {
  it("loads only the scopes a page needs, including its dialog dependencies", async () => {
    const { loadSection } = await fresh();
    await loadSection("ddns");

    // core(3) + ddns(1) + dns(1): the DDNS form picks from DNS credentials.
    expect(calls.length).toBe(5);
    expect(calls).toContain("/api/v1/health");
    expect(calls).toContain("/api/v1/ddns-records");
    expect(calls).toContain("/api/v1/dns-providers");
    expect(calls).not.toContain("/api/v1/ip-bans");
    expect(calls).not.toContain("/api/v1/layer4-rules");
    expect(new Set(calls).size).toBe(calls.length);
  });

  it("does not refetch scopes already loaded by another page", async () => {
    const { loadSection } = await fresh();
    await loadSection("overview");
    expect(calls.length).toBe(10);

    calls.length = 0;
    await loadSection("ddns");
    expect(calls).toEqual([]);
  });

  it("does not drop a second page when two pages load concurrently", async () => {
    // Regression: one global loading flag made a quick A -> B switch skip
    // B's requests entirely and leave the page empty.
    const { loadSection } = await fresh();
    await Promise.all([loadSection("ddns"), loadSection("ip-bans")]);
    expect(calls).toContain("/api/v1/ddns-records");
    expect(calls).toContain("/api/v1/ip-bans");
  });
});
