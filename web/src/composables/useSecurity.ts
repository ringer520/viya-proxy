import { apiFetch as fetch } from "../api";
// Inherited site policy: one panel-wide default plus per-root-domain
// overrides. The dialog edits either layer; new subdomains pick up the
// nearest one. `resolveSitePolicy` mirrors the server suffix walk.
import { ref } from "vue";
import { t } from "../i18n";
import { emptySitePolicy, type SitePolicy } from "../policy";
import { csrfToken, hosts, readOnly } from "./useState";
import { load } from "./useRefresh";

export const securityDefaults = ref<{
  global: SitePolicy;
  domains: Record<string, SitePolicy>;
}>({ global: emptySitePolicy(), domains: {} });
export const securityDialog = ref<{
  open: boolean;
  scope: "global" | "domain";
  domain: string;
}>({ open: false, scope: "global", domain: "" });
export const securityForm = ref<SitePolicy>(emptySitePolicy());
export const securitySaving = ref(false);
export const securityError = ref("");
export const securityNotice = ref("");
export const securityApplyPrompt = ref<{ domain: string; count: number } | null>(
  null,
);

export async function loadSecurityDefaults() {
  try {
    const r = await fetch("/api/v1/security-defaults");
    if (!r.ok) return;
    const d = await r.json();
    securityDefaults.value = {
      global: { ...emptySitePolicy(), ...(d.global || {}) },
      domains: d.domains || {},
    };
  } catch {
    // A missing policy must never block the rest of the console.
  }
}

// resolveSitePolicy mirrors the server: walk suffixes, first hit wins, then
// fall back to the panel default. Marks the result as inherited.
export function resolveSitePolicy(domain: string): SitePolicy {
  let candidate = String(domain || "").trim().toLowerCase().replace(/\.$/, "");
  while (candidate.includes(".")) {
    const found = securityDefaults.value.domains[candidate];
    if (found?.enabled) return { ...emptySitePolicy(), ...found, inherit: true };
    candidate = candidate.slice(candidate.indexOf(".") + 1);
  }
  const global = securityDefaults.value.global;
  if (global?.enabled) return { ...emptySitePolicy(), ...global, inherit: true };
  return emptySitePolicy();
}

export function resetSecurityForm(source?: SitePolicy) {
  securityForm.value = { ...emptySitePolicy(), ...(source || {}) };
}

export function openSecurityDefaults(domain?: string) {
  if (readOnly.value) return;
  securityError.value = "";
  securityNotice.value = "";
  securityApplyPrompt.value = null;
  const root = (domain || "").trim().toLowerCase();
  securityDialog.value = root
    ? { open: true, scope: "domain", domain: root }
    : { open: true, scope: "global", domain: "" };
  resetSecurityForm(
    securityDialog.value.scope === "domain"
      ? securityDefaults.value.domains[root]
      : securityDefaults.value.global,
  );
}

export function switchSecurityScope(scope: "global" | "domain") {
  securityDialog.value.scope = scope;
  securityError.value = "";
  securityNotice.value = "";
  securityApplyPrompt.value = null;
  resetSecurityForm(
    scope === "domain"
      ? securityDefaults.value.domains[securityDialog.value.domain]
      : securityDefaults.value.global,
  );
}

export async function saveSecurityDefaults() {
  if (securitySaving.value) return;
  securitySaving.value = true;
  securityError.value = "";
  securityNotice.value = "";
  try {
    const scope = securityDialog.value.scope;
    const target =
      scope === "domain"
        ? `/api/v1/security-defaults/domain/${encodeURIComponent(securityDialog.value.domain)}`
        : "/api/v1/security-defaults/global";
    const r = await fetch(target, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify(securityForm.value),
    });
    const body = await r.json().catch(() => ({}));
    if (!r.ok) throw Error(body.error || t("保存失败"));
    await loadSecurityDefaults();
    securityNotice.value = t("已保存");
    if (scope === "domain" && securityForm.value.enabled) {
      const count = hosts.value.filter(
        (h) =>
          h.security_headers?.inherit &&
          String(h.domain).toLowerCase().endsWith(
            "." + securityDialog.value.domain,
          ),
      ).length;
      if (count > 0) {
        securityApplyPrompt.value = {
          domain: securityDialog.value.domain,
          count,
        };
      }
    }
  } catch (e) {
    securityError.value = e instanceof Error ? e.message : t("保存失败");
  } finally {
    securitySaving.value = false;
  }
}

export async function applySecurityToExisting() {
  const prompt = securityApplyPrompt.value;
  if (!prompt) return;
  securityApplyPrompt.value = null;
  securitySaving.value = true;
  try {
    const r = await fetch("/api/v1/security-defaults/apply", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({ domain: prompt.domain, scope: "inherited" }),
    });
    const body = await r.json().catch(() => ({}));
    if (!r.ok) throw Error(body.error || t("应用失败"));
    securityNotice.value = t("已应用到 ") + (body.updated ?? 0) + t(" 个站点");
    await load();
  } catch (e) {
    securityError.value = e instanceof Error ? e.message : t("应用失败");
  } finally {
    securitySaving.value = false;
  }
}

export function closeSecurityDialog() {
  securityDialog.value.open = false;
  securityApplyPrompt.value = null;
  securityError.value = "";
  securityNotice.value = "";
}
