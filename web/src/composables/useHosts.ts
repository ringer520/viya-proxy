export * from "../utils/domain";
export * from "./useListeners";
import { canonicalDomain, isWithinDomain, registrableDomain } from "../utils/domain";
import { loadWebListeners, selectedListener, siteExposureFilter, webListeners } from "./useListeners";
import {registerRefreshTask} from "./useRefresh";
import { apiFetch as fetch } from "../api";
// Web services: reverse proxy, static sites, URL redirects. Owns the site
// form/dialog, the HTTP(S) listener editor, static-site file management, and
// per-site access-log preview. Depends on the shared store in useState.
import { computed, nextTick, ref } from "vue";
import { t, tr } from "../i18n";
import { emptySecurityHeaders, type SitePolicy } from "../policy";
import type { Host, SiteGroup } from "../types";
import { applyRetryVisible, confirmDeletion, csrfToken, deleteConfirmOptionResult, error, guardWrite, hosts, listSearch, matchesSearch, matchesStatus, saving } from "./useState";
import { load, performDelete } from "./useRefresh";
import { ArrowRight, Folder, FolderOpen, Globe2 } from "@lucide/vue";

// The three site kinds, each with a stable theme-aware accent color so the
// type picker (and the list rows) can tell them apart at a glance.
export const siteTypeOptions = [
  {
    value: "proxy",
    label: t("反向代理"),
    desc: t("转发到后端服务"),
    color: "var(--accent)",
    icon: Globe2,
  },
  {
    value: "static",
    label: t("静态站点"),
    desc: t("托管静态文件"),
    color: "var(--accent-2)",
    icon: Folder,
  },
  {
    value: "redirect",
    label: t("URL 重定向"),
    desc: t("跳转到其他地址"),
    color: "var(--warning)",
    icon: ArrowRight,
  },
] as const;

// File services live on their own page: a shared directory published for
// browser browsing plus optional WebDAV, so they are not offered as a Web
// service type any more.
export const fileServiceType = {
  value: "files",
  label: t("文件服务"),
  desc: t("共享目录 · WebDAV"),
  color: "var(--metric-memory)",
  icon: FolderOpen,
} as const;

/** Web services (proxy / static / redirect) — everything except file services. */
export const webHosts = computed(() =>
  hosts.value.filter((host) => host.type !== "files"),
);

// ---- enable/disable toggle ----
export const hostToggling = ref<Record<number, boolean>>({});
export async function toggleHost(host: Host) {
  if (!guardWrite()) return;
  if (hostToggling.value[host.id]) return;
  hostToggling.value[host.id] = true;
  try {
    const r = await fetch(`/api/v1/proxy-hosts/${host.id}/enabled`, {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({ enabled: !host.enabled, version: host.version }),
    });
    const body = await r.json();
    if (!r.ok) {
      if (r.status === 502) applyRetryVisible.value = true;
      throw Error(t(body.error));
    }
    await load();
  } catch (e) {
    const message = e instanceof Error ? t(e.message) : t("修改失败");
    await load();
    error.value = message;
  } finally {
    hostToggling.value[host.id] = false;
  }
}

// ---- domain composition ----
export const domainMode = ref<"full" | "subdomain">("full");
export const domainParent = ref("");
export const domainPrefix = ref("");
// ---- HTTP(S) listeners ----
export function siteURL(host: Host) {
  const protocol = host.tls_mode === "none" ? "http" : "https";
  const listener = webListeners.value.find(
    (item) => item.id === host.listener_id,
  );
  const port = listener?.port || (protocol === "https" ? 443 : 80);
  return `${protocol}://${host.domain}${port === (protocol === "https" ? 443 : 80) ? "" : ":" + port}${host.path || ""}`;
}

// ---- listing / grouping ----
export const searchedHosts = computed(() =>
  webHosts.value.filter(
    (host) =>
      matchesSearch(host.name, host.domain, host.upstream, host.path,...(host.tags||[])) &&
      matchesStatus(host.enabled),
  ),
);
export const visibleHosts = computed(() =>
  searchedHosts.value.filter(
    (host) =>
      host.listener_id === webListeners.value[selectedListener.value]?.id,
  ),
);
export const siteRootDomains = computed(() => {
  const owned = webHosts.value.filter(
    (host) =>
      host.listener_id === webListeners.value[selectedListener.value]?.id,
  );
  const domains = [
    ...new Set(
      owned.flatMap((host) => [
        registrableDomain(host.domain),
        ...(host.certificate_source?.kind === "wildcard" &&
        host.certificate_source.domain
          ? [registrableDomain(host.certificate_source.domain)]
          : []),
      ]),
    ),
  ];
  return domains
    .filter(
      (domain) =>
        !domains.some(
          (parent) => parent !== domain && isWithinDomain(domain, parent),
        ),
    )
    .sort((a, b) => a.localeCompare(b));
});
export const siteGroups = computed<SiteGroup[]>(() =>
  siteRootDomains.value
    .map((domain) => ({
      domain,
      hosts: visibleHosts.value
        .filter((host) => isWithinDomain(host.domain, domain))
        .sort((a, b) =>
          canonicalDomain(a.domain).localeCompare(canonicalDomain(b.domain)),
        ),
    }))
    .filter((group) => group.hosts.length),
);

// ---- file services page ----
export const fileSearch = ref("");
export const fileStatusFilter = ref<"all" | "enabled" | "disabled">("all");
export function suggestedParentDomain() {
  const roots = siteRootDomains.value;
  if (!roots.length) return "";
  const counts = roots
    .map((domain) => ({
      domain,
      count: hosts.value.filter(
        (host) =>
          canonicalDomain(host.domain) !== domain &&
          isWithinDomain(host.domain, domain),
      ).length,
    }))
    .sort((a, b) => b.count - a.count);
  return counts[0]?.count
    ? counts[0].domain
    : roots.length === 1
      ? roots[0]
      : "";
}
export const composedDomain = computed(() =>
  domainMode.value === "subdomain" &&
  domainPrefix.value.trim() &&
  domainParent.value
    ? `${domainPrefix.value.trim().replace(/\.$/, "")}.${domainParent.value}`
    : domainMode.value === "full"
      ? form.value.domain.trim()
      : "",
);
export function setDomainMode(mode: "full" | "subdomain") {
  if (mode === domainMode.value) return;
  if (mode === "subdomain") {
    domainParent.value = domainParent.value || suggestedParentDomain();
    const current = form.value.domain.trim().toLowerCase(),
      parent = domainParent.value.toLowerCase();
    domainPrefix.value =
      parent && current.endsWith(`.${parent}`)
        ? current.slice(0, -parent.length - 1)
        : "";
  } else {
    if (domainPrefix.value.trim() && domainParent.value)
      form.value.domain = `${domainPrefix.value.trim().replace(/\.$/, "")}.${domainParent.value}`;
    domainPrefix.value = "";
  }
  domainMode.value = mode;
}

// ---- site form ----
export const show = ref(false);
export const editingID = ref<number | null>(null);
export const partialSave = ref(false);
export const advancedOpen = ref(false);
export const upstreamTesting = ref(false);
export const upstreamCheck = ref<{ ok: boolean; message: string } | null>(null);
export const form = ref({
  version: 0,
  listener_id: "",
  name: "",
  domain: "",
  upstream: "http://127.0.0.1:9000",
  enabled: true,
  tls_mode: "auto",
  path: "",
  icon_color: "blue",
  dns_provider_id: "",
  custom_certificate_id: "",
  custom_headers: {} as Record<string, string>,
  basic_auth_enabled: false,
  basic_auth_user: "",
  basic_auth_password: "",
  ip_allowlist: [] as string[],
  ip_denylist: [] as string[],
  rate_limit_requests: 0,
  rate_limit_window: "1s",
  type: "proxy",
  root_path: "",
  browse: false,
  webdav: false,
  webdav_write: false,
  redirect_target: "",
  redirect_status: 301,
  redirect_keep_path: true,
  security_headers: emptySecurityHeaders(),
  max_body_bytes: 0,
  allowed_methods: [] as string[],
  hide_server: false,
  strip_prefix: false,
  rewrite_to: "",
  streaming: false,
  dial_timeout: "",
  response_header_timeout: "",
  maintenance: false,
  maintenance_message: "",
});

// Collapsed summary so operators can see what is configured without expanding
// the whole access-control panel.
export const advancedSummary = computed(() => {
  const parts: string[] = [];
  if (form.value.security_headers.mode === 'follow' || form.value.security_headers.inherit) parts.push(t('跟随域名策略'));
  else if (form.value.security_headers.mode === 'none') parts.push(t('不使用默认策略'));
  else parts.push(t('独立配置'));
  if (form.value.security_headers.enabled) parts.push(t("安全响应头"));
  if (form.value.basic_auth_enabled) parts.push("Basic Auth");
  if (Object.keys(form.value.custom_headers || {}).length) parts.push(t("请求头"));
  if ((form.value.ip_allowlist || []).length) parts.push(t("IP 白名单"));
  if ((form.value.ip_denylist || []).length) parts.push(t("IP 黑名单"));
  if (form.value.rate_limit_requests > 0) parts.push(t("限速"));
  return parts.length ? parts.join(" · ") : t("未启用额外保护");
});

// Short guidance under the site type selector so the choice is self-explanatory.
export const siteTypeHint = computed(() => {
  switch (form.value.type) {
    case "static":
      return t("直接托管静态文件，上传后由 Caddy 实时对外服务。");
    case "files":
      return t(
        "共享一个目录：浏览器可看文件列表，WebDAV 客户端可挂载同一地址。",
      );
    case "redirect":
      return t("把访问者重定向到另一个地址，不转发请求。");
    default:
      return t("把请求转发到后端服务，支持 HTTPS 上游与请求头改写。");
  }
});

// Bridges the site form's spread-out policy fields to the flat SitePolicy shape
// the policy editor component expects.
export const siteFormPolicy = computed<SitePolicy>({
  get: () => ({
    mode: form.value.security_headers.mode,
    enabled: form.value.security_headers.enabled,
    inherit: form.value.security_headers.inherit,
    frame_options: form.value.security_headers.frame_options,
    nosniff: form.value.security_headers.nosniff,
    hsts: form.value.security_headers.hsts,
    referrer_policy: form.value.security_headers.referrer_policy,
    csp: form.value.security_headers.csp,
    basic_auth_user: form.value.basic_auth_user,
    basic_auth_enabled: form.value.basic_auth_enabled,
    custom_headers: form.value.custom_headers,
    ip_allowlist: form.value.ip_allowlist,
    ip_denylist: form.value.ip_denylist,
    rate_limit_requests: form.value.rate_limit_requests,
    rate_limit_window: form.value.rate_limit_window,
    max_body_bytes: form.value.max_body_bytes,
    allowed_methods: form.value.allowed_methods,
    hide_server: form.value.hide_server,
  }),
  set: (value) => {
    form.value.security_headers = {
      ...form.value.security_headers,
      mode: value.mode,
      enabled: value.enabled,
      inherit: value.inherit,
      frame_options: value.frame_options,
      nosniff: value.nosniff,
      hsts: value.hsts,
      referrer_policy: value.referrer_policy,
      csp: value.csp,
    };
    form.value.basic_auth_user = value.basic_auth_user;
    form.value.basic_auth_enabled = value.basic_auth_enabled;
    form.value.custom_headers = value.custom_headers;
    form.value.ip_allowlist = value.ip_allowlist;
    form.value.ip_denylist = value.ip_denylist;
    form.value.rate_limit_requests = value.rate_limit_requests;
    form.value.rate_limit_window = value.rate_limit_window;
    form.value.max_body_bytes = value.max_body_bytes;
    form.value.allowed_methods = value.allowed_methods;
    form.value.hide_server = value.hide_server;
  },
});

// ---- per-site content stats + access logs + file drawer ----
export const hostContentStats = ref<
  Record<number, { files: number; total_size: number; empty: boolean }>
>({});
export const siteLogs = ref<
  Record<
    number,
    {
      events: Record<string, any>[];
      loading: boolean;
      error: string;
      loadedAt: number;
    }
  >
>({});
export const viewingLogSite = ref<Host | null>(null);
export const siteFilesHost = ref<Host | null>(null);
export const previewLayout = ref<Record<string, string>>({});
export async function loadSiteLogs(host: Host, force = false) {
  const cached = siteLogs.value[host.id];
  if (
    cached?.loading ||
    (!force && cached && Date.now() - cached.loadedAt < 5000)
  )
    return;
  siteLogs.value[host.id] = {
    events: cached?.events || [],
    loading: true,
    error: "",
    loadedAt: 0,
  };
  try {
    const r = await fetch(`/api/v1/proxy-hosts/${host.id}/logs`);
    if (!r.ok) throw Error(t("访问日志读取失败，请重试"));
    siteLogs.value[host.id] = {
      events: await r.json(),
      loading: false,
      error: "",
      loadedAt: Date.now(),
    };
  } catch (e) {
    siteLogs.value[host.id] = {
      events: [],
      loading: false,
      error: e instanceof Error ? t(e.message) : t("访问日志读取失败"),
      loadedAt: 0,
    };
  }
}
export function openSiteLogs(host: Host) {
  viewingLogSite.value = host;
  loadSiteLogs(host, true);
}
export function previewSite(host: Host, event: MouseEvent | FocusEvent) {
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
  const width = Math.min(720, window.innerWidth - 32);
  previewLayout.value = {
    position: "fixed",
    width: width + "px",
    left:
      Math.max(16, Math.min(rect.left, window.innerWidth - width - 16)) + "px",
    ...(rect.bottom + 330 > window.innerHeight
      ? { bottom: window.innerHeight - rect.top + "px", top: "auto" }
      : { top: rect.bottom + "px", bottom: "auto" }),
  };
  loadSiteLogs(host);
}
export function openSiteFiles(
  host: Host | { id: number; domain: string; root_path?: string },
) {
  siteFilesHost.value = host as Host;
}
export async function refreshSiteStat(id: number) {
  try {
    const r = await fetch(`/api/v1/proxy-hosts/${id}/files/stat`, {
      credentials: "same-origin",
    });
    if (!r.ok) return;
    hostContentStats.value = { ...hostContentStats.value, [id]: await r.json() };
  } catch {
    // A single site's stat failing must not disturb the rest of the dashboard.
  }
}
export function refreshStaticSiteStats() {
  for (const h of hosts.value) {
    // Static sites and file services both serve a directory worth counting.
    if (h.type === "static" || h.type === "files") void refreshSiteStat(h.id);
  }
}
export function closeSiteFiles() {
  const host = siteFilesHost.value;
  siteFilesHost.value = null;
  if (host?.type === "static" || host?.type === "files") void refreshSiteStat(host.id);
}
export function openSiteFilesForForm() {
  if (editingID.value === null) return;
  openSiteFiles({
    id: editingID.value,
    domain: composedDomain.value || form.value.domain,
    root_path: form.value.root_path,
  });
}

// ---- create / edit / clone / save / delete ----
export function openCreate(parentDomain?: string, type: Host["type"] = "proxy") {
  if (!guardWrite()) return;
  editingID.value = null;
  partialSave.value = false;
  // A file-service site requires authentication by default, and those inputs
  // live in the advanced section. Opening it avoids a save failure the user
  // cannot see the cause of.
  advancedOpen.value = type === "files";
  upstreamCheck.value = null;
  applyRetryVisible.value = false;
  domainParent.value = parentDomain ?? suggestedParentDomain();
  domainMode.value = domainParent.value ? "subdomain" : "full";
  domainPrefix.value = "";
  form.value = {
    version: 0,
    listener_id: webListeners.value[selectedListener.value]?.id || "",
    name: "",
    domain: "",
    upstream: "http://127.0.0.1:9000",
    enabled: true,
    tls_mode: siteExposureFilter.value === "http" ? "none" : "auto",
    path: "",
    icon_color: "blue",
    dns_provider_id: "",
    custom_certificate_id: "",
    custom_headers: {},
    basic_auth_enabled: type === "files",
    basic_auth_user: "",
    basic_auth_password: "",
    ip_allowlist: [] as string[],
    ip_denylist: [] as string[],
    rate_limit_requests: 0,
    rate_limit_window: "1s",
    type,
    root_path: "",
    browse: type === "files",
    webdav: type === "files",
    webdav_write: false,
    redirect_target: "",
    redirect_status: 301,
    redirect_keep_path: true,
    security_headers: emptySecurityHeaders(),
    max_body_bytes: 0,
    allowed_methods: [] as string[],
    hide_server: false,
    strip_prefix: false,
    rewrite_to: "",
    streaming: false,
    dial_timeout: "",
    response_header_timeout: "",
    maintenance: false,
    maintenance_message: "",
  };
  error.value = "";
  show.value = true;
}
export function openEdit(host: Host) {
  if (!guardWrite()) return;
  editingID.value = host.id;
  partialSave.value = false;
  advancedOpen.value =
    Object.keys(host.custom_headers || {}).length > 0 ||
    host.ip_allowlist.length > 0 ||
    host.ip_denylist.length > 0 ||
    host.rate_limit_requests > 0 ||
    host.basic_auth_enabled;
  upstreamCheck.value = null;
  applyRetryVisible.value = false;
  domainMode.value = "full";
  domainParent.value = "";
  domainPrefix.value = "";
  form.value = {
    version: host.version,
    name: host.name || "",
    listener_id: host.listener_id,
    domain: host.domain,
    upstream: host.upstream,
    enabled: host.enabled,
    tls_mode: host.tls_mode || "auto",
    path: host.path || "",
    icon_color: host.icon_color || "blue",
    dns_provider_id: host.dns_provider_id ? String(host.dns_provider_id) : "",
    custom_certificate_id: host.custom_certificate_id
      ? String(host.custom_certificate_id)
      : "",
    custom_headers: host.custom_headers || {},
    basic_auth_enabled: host.basic_auth_enabled,
    basic_auth_user: host.basic_auth_user || "",
    basic_auth_password: "",
    ip_allowlist: host.ip_allowlist || [],
    ip_denylist: host.ip_denylist || [],
    rate_limit_requests: host.rate_limit_requests || 0,
    rate_limit_window: host.rate_limit_window || "1s",
    type: host.type || "proxy",
    root_path: host.root_path || "",
    browse: !!host.browse,
    webdav: !!host.webdav,
    webdav_write: !!host.webdav_write,
    redirect_target: host.redirect_target || "",
    redirect_status: host.redirect_status || 301,
    redirect_keep_path: host.redirect_keep_path !== false,
    security_headers: {
      ...emptySecurityHeaders(),
      ...(host.security_headers || {}),
      mode: host.security_headers?.mode || (host.security_headers?.inherit ? "follow" : "custom"),
    },
    max_body_bytes: host.max_body_bytes || 0,
    allowed_methods: host.allowed_methods || [],
    hide_server: !!host.hide_server,
    strip_prefix: !!host.strip_prefix,
    rewrite_to: host.rewrite_to || "",
    streaming: !!host.streaming,
    dial_timeout: host.dial_timeout || "",
    response_header_timeout: host.response_header_timeout || "",
    maintenance: !!host.maintenance,
    maintenance_message: host.maintenance_message || "",
  };
  error.value = "";
  show.value = true;
}
export function cloneHost(host: Host) {
  if (!guardWrite()) return;
  const parent =
    siteRootDomains.value.find((domain) =>
      isWithinDomain(host.domain, domain),
    ) || "";
  editingID.value = null;
  partialSave.value = false;
  advancedOpen.value =
    Object.keys(host.custom_headers || {}).length > 0 ||
    host.ip_allowlist.length > 0 ||
    host.ip_denylist.length > 0 ||
    host.rate_limit_requests > 0 ||
    host.basic_auth_enabled;
  upstreamCheck.value = null;
  applyRetryVisible.value = false;
  domainMode.value = parent ? "subdomain" : "full";
  domainParent.value = parent;
  domainPrefix.value = parent
    ? canonicalDomain(host.domain) === parent
      ? "copy"
      : `${canonicalDomain(host.domain).slice(0, -parent.length - 1)}-copy`
    : "";
  form.value = {
    version: 0,
    listener_id: host.listener_id,
    name: host.name || "",
    domain: "",
    upstream: host.upstream,
    enabled: host.enabled,
    tls_mode: host.tls_mode || "auto",
    path: host.path || "",
    icon_color: host.icon_color || "blue",
    dns_provider_id: host.dns_provider_id ? String(host.dns_provider_id) : "",
    custom_certificate_id: host.custom_certificate_id
      ? String(host.custom_certificate_id)
      : "",
    custom_headers: host.custom_headers || {},
    basic_auth_enabled: host.basic_auth_enabled,
    basic_auth_user: host.basic_auth_user || "",
    basic_auth_password: "",
    ip_allowlist: host.ip_allowlist || [],
    ip_denylist: host.ip_denylist || [],
    rate_limit_requests: host.rate_limit_requests || 0,
    rate_limit_window: host.rate_limit_window || "1s",
    type: host.type || "proxy",
    root_path: host.root_path || "",
    browse: !!host.browse,
    webdav: !!host.webdav,
    webdav_write: !!host.webdav_write,
    redirect_target: host.redirect_target || "",
    redirect_status: host.redirect_status || 301,
    redirect_keep_path: host.redirect_keep_path !== false,
    security_headers: {
      ...emptySecurityHeaders(),
      ...(host.security_headers || {}),
      mode: host.security_headers?.mode || (host.security_headers?.inherit ? "follow" : "custom"),
    },
    max_body_bytes: host.max_body_bytes || 0,
    allowed_methods: host.allowed_methods || [],
    hide_server: !!host.hide_server,
    strip_prefix: !!host.strip_prefix,
    rewrite_to: host.rewrite_to || "",
    streaming: !!host.streaming,
    dial_timeout: host.dial_timeout || "",
    response_header_timeout: host.response_header_timeout || "",
    maintenance: !!host.maintenance,
    maintenance_message: host.maintenance_message || "",
  };
  error.value = "";
  show.value = true;
}
export async function testUpstream() {
  upstreamTesting.value = true;
  upstreamCheck.value = null;
  try {
    const r = await fetch("/api/v1/proxy-hosts/test-upstream", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({ upstream: form.value.upstream }),
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("上游连接测试失败"));
    upstreamCheck.value = {
      ok: true,
      message: tr`TCP 端口可连通（${body.duration_ms} ms）`,
    };
  } catch (e) {
    upstreamCheck.value = {
      ok: false,
      message: e instanceof Error ? t(e.message) : t("上游连接测试失败"),
    };
  } finally {
    upstreamTesting.value = false;
  }
}
export async function save() {
  error.value = "";
  saving.value = true;
  try {
    const hostDomain = composedDomain.value.trim().toLowerCase();
    if (!hostDomain)
      throw Error(
        domainMode.value === "subdomain"
          ? t("请填写子域名前缀")
          : t("请填写前端域名"),
      );
    const url =
      editingID.value === null
        ? "/api/v1/proxy-hosts"
        : `/api/v1/proxy-hosts/${editingID.value}`;
    const payload = {
      ...form.value,
      domain: hostDomain,
      // These legacy fields remain in storage for import compatibility, but
      // the UI no longer exposes SPA fallback or public directory browsing.
      dns_provider_id:
        form.value.tls_mode === "dns-01" && form.value.dns_provider_id
          ? Number(form.value.dns_provider_id)
          : null,
      custom_certificate_id:
        form.value.tls_mode === "custom" && form.value.custom_certificate_id
          ? Number(form.value.custom_certificate_id)
          : null,
      custom_headers: form.value.custom_headers,
      ip_allowlist: form.value.ip_allowlist || [],
      ip_denylist: form.value.ip_denylist || [],
    };
    const r = await fetch(url, {
      method: editingID.value === null ? "POST" : "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify(payload),
    });
    const body = await r.json();
    if (!r.ok) {
      if (r.status === 409) {
        await load();
        throw Error(
          t(body.error) || t("站点已被其他会话修改，请关闭后重新打开编辑"),
        );
      }
      if (r.status === 502) {
        applyRetryVisible.value = true;
        partialSave.value = true;
        await load();
        if (editingID.value === null) {
          const saved = hosts.value.find(
            (host) =>
              host.domain === hostDomain &&
              host.listener_id === form.value.listener_id,
          );
          if (saved) {
            editingID.value = saved.id;
            form.value.version = saved.version;
          }
        }
        error.value = tr`${t(body.error) || t("Caddy 未应用配置")}。数据库中的规则已保存，请勿重复新建；可先重新应用，或修正此规则后再次保存。`;
        return;
      }
      throw Error(t(body.error) || t("保存失败"));
    }
    form.value.basic_auth_password = "";
    partialSave.value = false;
    applyRetryVisible.value = false;
    show.value = false;
    await load();
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t("请求失败");
  } finally {
    saving.value = false;
  }
}
export async function remove(host: Host) {
  if (!guardWrite()) return;
  const staticSite = host.type === "static";
  const fileService = host.type === "files";
  const message = fileService
    ? tr`确认删除文件服务“${host.domain}”？共享目录里的文件会保留。`
    : tr`确认删除 Web 服务“${host.domain}”？`;
  if (!(await confirmDeletion(
    message,
    "确认删除",
    staticSite ? { label: "不要删除静态站点文件和文件夹", checked: false } : undefined,
  ))) return;
  const deleteFiles = staticSite && !deleteConfirmOptionResult.value;
  await performDelete(`/api/v1/proxy-hosts/${host.id}${staticSite ? `?delete_files=${deleteFiles ? "1" : "0"}` : ""}`);
}

// ---- site-list search box (web page) ----
export const webSearchOpen = ref(false);
export const webSearchInput = ref<HTMLInputElement | null>(null);
export async function toggleWebSearch() {
  webSearchOpen.value = !webSearchOpen.value;
  if (webSearchOpen.value) {
    await nextTick();
    webSearchInput.value?.focus();
  } else listSearch.value = "";
}

registerRefreshTask("hosts",async()=>{await loadWebListeners();void refreshStaticSiteStats()},"sites");
