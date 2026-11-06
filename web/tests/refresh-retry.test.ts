import { beforeEach, describe, expect, it, vi } from "vitest";

const { calls, reply } = vi.hoisted(() => ({
  calls: [] as string[],
  reply: { status: 200, body: {} as { error?: string } },
}));

vi.mock("../src/api", () => ({
  apiFetch: (url: string, init?: { method?: string }) => {
    calls.push(url);
    if (init?.method === "DELETE") {
      return Promise.resolve({
        ok: reply.status < 400,
        status: reply.status,
        json: async () => reply.body,
      });
    }
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
  reply.status = 200;
  reply.body = {};
  vi.resetModules();
});

async function fresh() {
  const [refresh, state] = await Promise.all([
    import("../src/composables/useRefresh"),
    import("../src/composables/useState"),
  ]);
  return { refresh, state };
}

describe("performDelete / partial-save feedback", () => {
  it("shows the apply-retry banner when the engine rejects the reload with 502", async () => {
    reply.status = 502;
    reply.body = { error: "configuration_pending" };
    const { refresh, state } = await fresh();

    await refresh.performDelete("/api/v1/proxy-hosts/1");

    expect(state.applyRetryVisible.value).toBe(true);
    expect(state.error.value).toContain("configuration_pending");
    expect(state.deleteBusy.value).toBe(false);
    expect(calls.some((url) => url === "/api/v1/health")).toBe(true);
  });

  it("clears the banner after a successful delete", async () => {
    const { refresh, state } = await fresh();
    state.applyRetryVisible.value = true;

    await refresh.performDelete("/api/v1/proxy-hosts/1");

    expect(state.applyRetryVisible.value).toBe(false);
    expect(state.error.value).toBe("");
    expect(state.deleteBusy.value).toBe(false);
  });
});
