<script setup lang="ts">
import { t, tr } from "./i18n";

import {
  computed,
  onBeforeUnmount,
  onMounted,
  nextTick,
  ref,
  watch,
} from "vue";
import AccessLogTable from "./components/AccessLogTable.vue";
import AttentionPanel from "./components/AttentionPanel.vue";
import TrafficRangeBar from "./components/TrafficRangeBar.vue";
import PanelSettingsDrawer from "./components/PanelSettingsDrawer.vue";
import ConfigTransfer from "./components/ConfigTransfer.vue";
import LogWorkspace from "./components/LogWorkspace.vue";
import SystemGauges from "./components/SystemGauges.vue";
import TrafficTrend from "./components/TrafficTrend.vue";
import { formatBytes } from "./format";
import LanguagePicker from "./components/LanguagePicker.vue";
import {
  Plus,
  Globe2,
  ShieldCheck,
  Trash2,
  ExternalLink,
  Server,
  RefreshCw,
  Pencil,
  History,
  LayoutDashboard,
  KeyRound,
  ScrollText,
  Ban,
  Cable,
  LogOut,
  Settings,
  Copy,
  X,
  Search,
  Network,
} from "@lucide/vue";

type CertificateSource = {
  kind: string;
  domain?: string;
  label: string;
  status: string;
  not_after?: string;
};
type AutomaticCertificate = {
  domain: string;
  source: CertificateSource;
  sites: string[];
};
type Host = {
  id: number;
  listener_id: string;
  certificate_source?: CertificateSource;
  name: string;
  domain: string;
  upstream: string;
  enabled: boolean;
  version: number;
  tls_mode: string;
  path: string;
  icon_color: string;
  dns_provider_id?: number;
  custom_certificate_id?: number;
  custom_headers: Record<string, string>;
  basic_auth_enabled: boolean;
  basic_auth_user?: string;
  ip_allowlist: string[];
  ip_denylist: string[];
  rate_limit_requests: number;
  rate_limit_window?: string;
};
type SiteGroup = { domain: string; hosts: Host[] };
type Revision = {
  id: number;
  status: string;
  error?: string;
  created_at: string;
};
type Provider = {
  id: number;
  name: string;
  provider_type: string;
  has_credentials: boolean;
};
type DNSZoneOption = { name: string; status?: string };
type Layer4Rule = {
  id: number;
  name: string;
  listen_address: string;
  listen_port: number;
  upstream_address: string;
  protocol: string;
  enabled: boolean;
  version: number;
};
type DDNSRecord = {
  id: number;
  name: string;
  comment: string;
  dns_provider_id: number;
  zone: string;
  record_name: string;
  record_type: "A" | "AAAA" | "CNAME";
  cname_target?: string;
  ip_source: "interface" | "http" | "";
  interface_name: string;
  interface_ip?: string;
  ttl_seconds: number;
  interval_minutes: number;
  enabled: boolean;
  last_ip: string;
  last_run_at: number;
  last_error: string;
};
type NetworkInterfaceAddress = {
  ip: string;
  family: "ipv4" | "ipv6";
  scope: string;
  temporary?: boolean;
};
type NetworkInterface = {
  name: string;
  index: number;
  addresses: NetworkInterfaceAddress[];
};
type Certificate = {
  id: number;
  name: string;
  dns_names: string[];
  not_after: string;
};
type ManagedCertificate = {
  id: number;
  name: string;
  domain: string;
  method: string;
  status: string;
  progress: number;
  progress_message: string;
  progress_attempt: number;
  last_error?: string;
  not_after?: string;
  issuer?: string;
  email?: string;
  dns_provider_id?: number;
};
type IPBan = {
  id: number;
  domain: string;
  ip: string;
  reason: string;
  expires_at: number;
  source?: string;
};
const managedCertificates = ref<ManagedCertificate[]>([]),
  showManagedCert = ref(false),
  managedCertError = ref(""),
  managedCertBusy = ref(false);
type ExpiringCertificate = {
  domain: string;
  kind: string;
  label: string;
  not_after: string;
  days_left: number;
  expired: boolean;
  sites: string[];
};
type UpstreamHealth = {
  ok: boolean;
  latency_ms: number;
  checked_at: number;
  target: string;
  error?: string;
  failures: number;
};
type Palette = "light" | "dark" | "aurora" | "ember" | "cyber";
type ThemeChoice = "system" | Palette;
const paletteChoices: Palette[] = [
  "light",
  "dark",
  "aurora",
  "ember",
  "cyber",
];
const savedTheme = localStorage.getItem("viya-theme");
const themeChoice = ref<ThemeChoice>(
  savedTheme === "system" || paletteChoices.includes(savedTheme as Palette)
    ? (savedTheme as ThemeChoice)
    : "system",
);
function storedNumber(key: string, allowed: number[], fallback: number) {
  const value = Number(localStorage.getItem(key));
  return allowed.includes(value) ? value : fallback;
}
const refreshInterval = ref(
  storedNumber("viya-refresh-seconds", [5, 15, 30, 60], 5),
);
const autoRefresh = ref(localStorage.getItem("viya-auto-refresh") !== "false"),
  pauseHidden = ref(localStorage.getItem("viya-pause-hidden") !== "false"),
  trafficCharts = ref(localStorage.getItem("viya-traffic-charts") !== "false"),
  previewLimit = ref(storedNumber("viya-log-preview-limit", [3, 5, 10], 5)),
  landingPage = ref(localStorage.getItem("viya-landing-page") || "overview");
const panelSettingsLoading = ref(false),
  panelUsername = ref(""),
  panelRole = ref<"admin" | "viewer">("admin"),
  panelSessions = ref<number | null>(null),
  storedPanelPort = ref(8181);
// Read-only accounts cannot save anything, so the panel starts in a sealed mode
// instead of letting every button fail one request later.
const readOnly = computed(() => panelRole.value === "viewer");
const documentHidden = ref(document.hidden);
const themeMedia = window.matchMedia("(prefers-color-scheme: dark)");
const panelAccessURL = window.location.origin;
const compactMode = ref(localStorage.getItem("viya-compact-mode") === "true");
const panelAccessPort =
  window.location.port ||
  (window.location.protocol === "https:" ? "443" : "80");
const showPanelSettings = ref(false);
const panelListenPort = ref(Number(panelAccessPort) || 8181),
  panelSettingsError = ref(""),
  panelSettingsMessage = ref(""),
  panelSettingsBusy = ref(false);
const passwordForm = ref({ current: "", next: "", confirm: "" });
const managedCertForm = ref({
  name: "",
  domain: "",
  method: "dns-01",
  email: "",
  dns_provider_id: "",
});
const showBan = ref(false),
  editingBanID = ref<number | null>(null),
  banError = ref(""),
  banKind = ref("ip"),
  banForm = ref({ domain: "", ip: "", reason: "" });
function openBan(ban?: IPBan) {
  banKind.value = ban?.ip === "foreign" ? "foreign" : "ip";
  editingBanID.value = ban?.id ?? null;
  banError.value = "";
  banForm.value = {
    domain: ban?.domain || hosts.value[0]?.domain || "",
    ip: ban?.ip || "",
    reason: ban?.reason || "",
  };
  showBan.value = true;
}
async function saveBan() {
  saving.value = true;
  banError.value = "";
  try {
    const r = await fetch(
      editingBanID.value
        ? `/api/v1/ip-bans/${editingBanID.value}`
        : "/api/v1/ip-bans",
      {
        method: editingBanID.value ? "PUT" : "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken.value,
        },
        body: JSON.stringify({
          ...banForm.value,
          ip: banKind.value === "foreign" ? "foreign" : banForm.value.ip,
          // Bans are permanent until an administrator lifts them manually.
          expires_at: 0,
        }),
      },
    );
    const data = await r.json();
    if (!r.ok) {
      if (r.status === 502) {
        editingBanID.value = data.id;
        applyRetryVisible.value = true;
        error.value = t(data.error);
        await load();
      }
      throw Error(t(data.error) || t("保存封禁失败"));
    }
    showBan.value = false;
    await load();
  } catch (e) {
    banError.value = e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    saving.value = false;
  }
}
const editingManagedID = ref<number | null>(null);
function openManagedCert(cert?: ManagedCertificate, clone = false) {
  editingManagedID.value = cert && !clone ? cert.id : null;
  managedCertForm.value = cert
    ? {
        name: cert.name + (clone ? t(" 副本") : ""),
        domain: clone ? "" : cert.domain,
        method: cert.method,
        email: cert.email || "",
        dns_provider_id: cert.dns_provider_id
          ? String(cert.dns_provider_id)
          : "",
      }
    : {
        name: "",
        domain: "",
        method: "dns-01",
        email: "",
        dns_provider_id: "",
      };
  managedCertError.value = "";
  showManagedCert.value = true;
}
async function refreshManagedCertificates() {
  try {
    const r = await fetch("/api/v1/managed-certificates");
    if (r.ok) managedCertificates.value = await r.json();
  } catch {}
}
async function requestCertificate() {
  managedCertBusy.value = true;
  managedCertError.value = "";
  try {
    const r = await fetch(
      editingManagedID.value
        ? `/api/v1/managed-certificates/${editingManagedID.value}`
        : "/api/v1/managed-certificates",
      {
        method: editingManagedID.value ? "PUT" : "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken.value,
        },
        body: JSON.stringify({
          ...managedCertForm.value,
          dns_provider_id:
            managedCertForm.value.method === "dns-01"
              ? Number(managedCertForm.value.dns_provider_id)
              : null,
        }),
      },
    );
    const data = await r.json();
    if (!r.ok) {
      if (r.status === 502) {
        showManagedCert.value = false;
        applyRetryVisible.value = true;
        await load();
        error.value = t(data.error);
        return;
      }
      throw Error(t(data.error) || t("证书申请失败"));
    }
    showManagedCert.value = false;
    error.value = "";
    await load();
  } catch (e) {
    managedCertError.value = e instanceof Error ? t(e.message) : t("申请失败");
  } finally {
    managedCertBusy.value = false;
  }
}
async function removeManagedCert(cert: ManagedCertificate) {
  if (await confirmDeletion(tr`停止“${cert.domain}”的证书托管和自动续期？`))
    await performDelete(`/api/v1/managed-certificates/${cert.id}`);
}
async function removeAutomaticCert(cert: AutomaticCertificate) {
  if (
    await confirmDeletion(
      tr`将彻底删除“${cert.domain}”的证书文件和申请记录，并由引擎重新应用配置。之后若站点仍选择自动 HTTPS，引擎会重新申请。确定继续吗？`,
    )
  )
    await performDelete(
      `/api/v1/certificate-inventory/${encodeURIComponent(cert.domain)}`,
    );
}
const hostToggling = ref<Record<number, boolean>>({});
async function toggleHost(host: Host) {
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
const hosts = ref<Host[]>([]),
  revisions = ref<Revision[]>([]),
  providers = ref<Provider[]>([]),
  l4Rules = ref<Layer4Rule[]>([]),
  ddnsRecords = ref<DDNSRecord[]>([]),
  certificates = ref<Certificate[]>([]),
  certExpiring = ref<ExpiringCertificate[]>([]),
  upstreamHealth = ref<Record<string, UpstreamHealth>>({}),
  networkInterfaces = ref<NetworkInterface[]>([]),
  online = ref(false),
  show = ref(false),
  showProvider = ref(false),
  showL4 = ref(false),
  showDDNS = ref(false),
  showCert = ref(false),
  error = ref(""),
  saving = ref(false),
  applyRetryVisible = ref(false),
  partialSave = ref(false),
  advancedOpen = ref(false),
  upstreamTesting = ref(false),
  upstreamCheck = ref<{ ok: boolean; message: string } | null>(null);
const domainMode = ref<"full" | "subdomain">("full"),
  domainParent = ref(""),
  domainPrefix = ref("");
const siteIconColors = [
  { key: "blue", label: t("海蓝"), value: "#3b82f6" },
  { key: "teal", label: t("青绿"), value: "#14b8a6" },
  { key: "green", label: t("草绿"), value: "#22c55e" },
  { key: "lime", label: t("浅绿"), value: "#84cc16" },
  { key: "amber", label: t("琥珀"), value: "#f59e0b" },
  { key: "orange", label: t("橙色"), value: "#f97316" },
  { key: "rose", label: t("玫红"), value: "#f43f5e" },
  { key: "pink", label: t("粉色"), value: "#ec4899" },
  { key: "violet", label: t("紫色"), value: "#8b5cf6" },
  { key: "slate", label: t("石墨"), value: "#64748b" },
] as const;
function siteIconStyle(key: string) {
  const color =
    siteIconColors.find((item) => item.key === key) || siteIconColors[0];
  return {
    color: color.value,
    backgroundColor: `${color.value}18`,
    borderColor: `${color.value}35`,
  };
}
function canonicalDomain(domain: string) {
  return domain.toLowerCase().replace(/^\*\./, "");
}
function isWithinDomain(domain: string, parent: string) {
  const child = canonicalDomain(domain),
    base = canonicalDomain(parent);
  return child === base || child.endsWith(`.${base}`);
}
type WebListener = {
  id: string;
  protocol: "http" | "https";
  port: number;
  copy_from?: string;
};
const webListeners = ref<WebListener[]>([
    { id: "https-main", protocol: "https", port: 443 },
    { id: "http-main", protocol: "http", port: 80 },
  ]),
  selectedListener = ref(0),
  listenerLoaded = ref(false),
  showListenerEditor = ref(false),
  listenerEditingIndex = ref<number | null>(null),
  listenerDraft = ref<WebListener>({ id: "", protocol: "https", port: 0 }),
  listenerChecking = ref(false),
  listenerAvailable = ref<boolean | null>(null),
  listenerMessage = ref(""),
  listenerSaving = ref(false);
const listenerCreationMode = ref<"empty" | "copy">("empty"),
  listenerCopyFrom = ref("");
const automaticCertificates = ref<AutomaticCertificate[]>([]);
async function refreshCertificateInventory() {
  try {
    const r = await fetch("/api/v1/certificate-inventory");
    if (r.ok) automaticCertificates.value = await r.json();
  } catch {}
}
const selectedPortState = ref<{
  checking: boolean;
  available: boolean | null;
  message: string;
}>({ checking: false, available: null, message: "" });
let selectedPortSequence = 0;
async function checkSelectedPort() {
  const sequence = ++selectedPortSequence,
    listener = webListeners.value[selectedListener.value];
  if (!listener) return;
  selectedPortState.value = {
    checking: true,
    available: null,
    message: t("正在检测端口…"),
  };
  try {
    const r = await fetch(
      `/api/v1/web-listeners/check?port=${listener.port}&protocol=${listener.protocol}`,
    );
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("检测失败"));
    if (sequence === selectedPortSequence)
      selectedPortState.value = {
        checking: false,
        available: body.available,
        message: t(body.message),
      };
  } catch (e) {
    if (sequence === selectedPortSequence)
      selectedPortState.value = {
        checking: false,
        available: false,
        message: e instanceof Error ? t(e.message) : t("检测失败"),
      };
  }
}
let listenerCheckSequence = 0;
async function loadWebListeners() {
  if (listenerLoaded.value) return;
  try {
    const r = await fetch("/api/v1/web-listeners");
    if (r.ok) {
      webListeners.value = await r.json();
      listenerLoaded.value = true;
      selectListener(0);
    }
  } catch {}
}
function selectListener(index: number) {
  selectedListener.value = index;
  siteExposureFilter.value = webListeners.value[index]?.protocol || "https";
  void checkSelectedPort();
}
// crypto.randomUUID only exists in secure contexts, so a plain-HTTP panel
// access would throw here and the listener dialog would never open.
function randomListenerID() {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function")
    return crypto.randomUUID();
  return (
    "listener-" +
    Date.now().toString(36) +
    "-" +
    Math.random().toString(36).slice(2, 10)
  );
}
function openListenerEditor(index: number | null) {
  listenerEditingIndex.value = index;
  listenerDraft.value =
    index === null
      ? { id: randomListenerID(), protocol: "https", port: 0 }
      : { ...webListeners.value[index]! };
  listenerCreationMode.value = "empty";
  listenerCopyFrom.value = "";
  listenerAvailable.value = null;
  listenerMessage.value = "";
  showListenerEditor.value = true;
  if (index !== null) void checkListenerPort();
}
async function checkListenerPort() {
  const sequence = ++listenerCheckSequence,
    port = listenerDraft.value.port;
  listenerAvailable.value = null;
  listenerChecking.value = true;
  try {
    if (!Number.isInteger(port) || port < 1 || port > 65535)
      throw Error(t("端口范围为 1–65535"));
    if (
      webListeners.value.some(
        (item, index) =>
          index !== listenerEditingIndex.value && item.port === port,
      )
    )
      throw Error(t("此端口已存在于其他监听"));
    const r = await fetch(
      `/api/v1/web-listeners/check?port=${port}&protocol=${listenerDraft.value.protocol}`,
    );
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("检测失败"));
    if (sequence === listenerCheckSequence) {
      listenerAvailable.value = body.available;
      listenerMessage.value = t(body.message);
    }
  } catch (e) {
    if (sequence === listenerCheckSequence) {
      listenerAvailable.value = false;
      listenerMessage.value = e instanceof Error ? t(e.message) : t("检测失败");
    }
  } finally {
    if (sequence === listenerCheckSequence) listenerChecking.value = false;
  }
}
async function persistListeners(
  next: WebListener[],
  target = selectedListener.value,
) {
  listenerSaving.value = true;
  try {
    const r = await fetch("/api/v1/web-listeners", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify(next),
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("监听保存失败"));
    webListeners.value = body;
    await load();
    selectListener(Math.min(target, body.length - 1));
    showListenerEditor.value = false;
    listenerMessage.value = "";
  } catch (e) {
    listenerAvailable.value = false;
    listenerMessage.value = e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    listenerSaving.value = false;
  }
}
async function saveListener() {
  if (
    listenerSaving.value ||
    listenerChecking.value ||
    listenerAvailable.value !== true
  )
    return;
  const next = webListeners.value.map((item) => ({ ...item }));
  if (listenerEditingIndex.value === null) {
    if (listenerCreationMode.value === "copy" && !listenerCopyFrom.value) {
      listenerMessage.value = t("请选择导入来源监听");
      return;
    }
    next.push({
      ...listenerDraft.value,
      copy_from:
        listenerCreationMode.value === "copy"
          ? listenerCopyFrom.value
          : undefined,
    });
  } else next[listenerEditingIndex.value] = { ...listenerDraft.value };
  await persistListeners(
    next,
    listenerEditingIndex.value === null
      ? next.length - 1
      : selectedListener.value,
  );
}
async function deleteListener() {
  if (listenerSaving.value) return;
  const index = listenerEditingIndex.value;
  if (
    index === null ||
    !(await confirmDeletion(
      tr`删除 ${webListeners.value[index]?.protocol.toUpperCase()} :${webListeners.value[index]?.port} 监听及其 ${hosts.value.filter((host) => host.listener_id === webListeners.value[index]?.id).length} 个站点？`,
    ))
  )
    return;
  await persistListeners(webListeners.value.filter((_, i) => i !== index));
}
function siteURL(host: Host) {
  const protocol = host.tls_mode === "none" ? "http" : "https";
  const listener = webListeners.value.find(
    (item) => item.id === host.listener_id,
  );
  const port = listener?.port || (protocol === "https" ? 443 : 80);
  return `${protocol}://${host.domain}${port === (protocol === "https" ? 443 : 80) ? "" : ":" + port}${host.path || ""}`;
}
const siteExposureFilter = ref<"all" | "https" | "http">("https");
const visibleHosts = computed(() =>
  searchedHosts.value.filter(
    (host) =>
      host.listener_id === webListeners.value[selectedListener.value]?.id,
  ),
);
const siteRootDomains = computed(() => {
  const owned = hosts.value.filter(
    (host) =>
      host.listener_id === webListeners.value[selectedListener.value]?.id,
  );
  const domains = [
    ...new Set(
      owned.flatMap((host) => [
        canonicalDomain(host.domain),
        ...(host.certificate_source?.kind === "wildcard" &&
        host.certificate_source.domain
          ? [canonicalDomain(host.certificate_source.domain)]
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
const siteGroups = computed<SiteGroup[]>(() =>
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
function suggestedParentDomain() {
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
const composedDomain = computed(() =>
  domainMode.value === "subdomain" &&
  domainPrefix.value.trim() &&
  domainParent.value
    ? `${domainPrefix.value.trim().replace(/\.$/, "")}.${domainParent.value}`
    : domainMode.value === "full"
      ? form.value.domain.trim()
      : "",
);
function setDomainMode(mode: "full" | "subdomain") {
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
const editingID = ref<number | null>(null);
const form = ref({
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
  rate_limit_requests: 0,
  rate_limit_window: "1s",
});
const deletePrompt = ref("");
let deleteResolver: ((result: boolean) => void) | null = null;
function confirmDeletion(message: string): Promise<boolean> {
  if (deleteResolver || deleteBusy.value) return Promise.resolve(false);
  deletePrompt.value = message;
  return new Promise((resolve) => {
    deleteResolver = resolve;
  });
}
function resolveDelete(result: boolean) {
  const resolve = deleteResolver;
  deleteResolver = null;
  deletePrompt.value = "";
  resolve?.(result);
}
const deleteBusy = ref(false);
async function performDelete(url: string) {
  if (deleteBusy.value) return;
  deleteBusy.value = true;
  let failure = "";
  ++ddnsRefreshSequence;
  try {
    const r = await fetch(url, {
      method: "DELETE",
      headers: { "X-CSRF-Token": csrfToken.value },
    });
    if (!r.ok) {
      const body = await r.json();
      if (r.status === 502) applyRetryVisible.value = true;
      throw Error(t(body.error) || t("删除失败"));
    }
    applyRetryVisible.value = false;
  } catch (e) {
    failure = e instanceof Error ? t(e.message) : t("删除失败");
  } finally {
    ++ddnsRefreshSequence;
    await load();
    if (failure) error.value = failure;
    deleteBusy.value = false;
  }
}
const refreshing = ref(false),
  lastRefreshedAt = ref(0);
let loadRequestSequence = 0;
const headerText = ref(""),
  allowlistText = ref(""),
  denylistText = ref("");
const editingProviderID = ref<number | null>(null),
  providerError = ref("");
const providerForm = ref({
  name: "",
  provider_type: "cloudflare",
  credentials: {} as Record<string, string>,
});
const l4EditingID = ref<number | null>(null),
  l4Error = ref(""),
  l4PartialSave = ref(false),
  l4Form = ref({
    name: "",
    listen_address: "0.0.0.0",
    listen_port: 2222,
    upstream_address: "127.0.0.1:22",
    protocol: "tcp",
    enabled: true,
  });
const ddnsEditingID = ref<number | null>(null),
  ddnsError = ref(""),
  ddnsForm = ref({
    name: "",
    comment: "viya",
    dns_provider_id: "",
    zone: "example.com",
    record_name: "@",
    record_type: "A" as "A" | "AAAA" | "CNAME",
    cname_target: "",
    ip_source: "interface" as "interface" | "http" | "",
    interface_name: "",
    interface_ip: "",
    ttl_seconds: 300,
    interval_minutes: 5,
    enabled: true,
  });
const ddnsZones = ref<DNSZoneOption[]>([]),
  ddnsZoneLoading = ref(false),
  ddnsZoneMessage = ref(""),
  manualDDNSZone = ref(false);
const selectedInterfaceAddresses = computed(
  () =>
    networkInterfaces.value
      .find((item) => item.name === ddnsForm.value.interface_name)
      ?.addresses.filter(
        (address) =>
          address.family ===
          (ddnsForm.value.record_type === "A" ? "ipv4" : "ipv6"),
      ) || [],
);
function preferredInterfaceIP(addresses: NetworkInterfaceAddress[]) {
  return (
    [...addresses].sort(
      (a, b) =>
        Number(a.temporary === true) - Number(b.temporary === true) ||
        Number(a.scope === "内网") - Number(b.scope === "内网") ||
        a.ip.localeCompare(b.ip),
    )[0]?.ip || ""
  );
}
watch(selectedInterfaceAddresses, (addresses) => {
  if (!addresses.some((address) => address.ip === ddnsForm.value.interface_ip))
    ddnsForm.value.interface_ip = preferredInterfaceIP(addresses);
});
watch(
  () => ddnsForm.value.interface_name,
  () => {
    if (
      !selectedInterfaceAddresses.value.some(
        (address) => address.ip === ddnsForm.value.interface_ip,
      )
    )
      ddnsForm.value.interface_ip = preferredInterfaceIP(
        selectedInterfaceAddresses.value,
      );
  },
);
watch(
  () => ddnsForm.value.record_type,
  () => {
    if (
      !selectedInterfaceAddresses.value.some(
        (address) => address.ip === ddnsForm.value.interface_ip,
      )
    )
      ddnsForm.value.interface_ip = preferredInterfaceIP(
        selectedInterfaceAddresses.value,
      );
  },
);
function formatDDNSTime(value: number) {
  return value ? new Date(value * 1000).toLocaleString() : t("尚未更新");
}
const certForm = ref({ name: "" }),
  certFile = ref<File | null>(null),
  keyFile = ref<File | null>(null),
  certError = ref("");
const ipBans = ref<IPBan[]>([]);
type Counters = {
  requests: number;
  errors: number;
  upload: number;
  download: number;
};
type SiteTraffic = {
  total: Counters;
  recent: Counters;
  hourly: ({ hour: number } & Counters)[];
  // Present only for the 7d and 30d ranges.
  daily?: ({ day: number } & Counters)[];
};
const traffic = ref<Record<string, SiteTraffic>>({}),
  trafficError = ref("");
const siteLogs = ref<
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
const viewingLogSite = ref<Host | null>(null);
type RuleLogState = {
  events: Record<string, any>[];
  loading: boolean;
  error: string;
  loadedAt: number;
};
const l4Logs = ref<Record<number, RuleLogState>>({});
const l4PreviewLayout = ref<Record<string, string>>({});
async function loadL4Logs(rule: Layer4Rule) {
  const current = l4Logs.value[rule.id];
  if (current?.loading) return;
  if (current && Date.now() - current.loadedAt < 4000) return;
  l4Logs.value = {
    ...l4Logs.value,
    [rule.id]: {
      events: current?.events || [],
      loading: true,
      error: "",
      loadedAt: current?.loadedAt || 0,
    },
  };
  try {
    const r = await fetch(`/api/v1/layer4-rules/${rule.id}/logs`);
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("读取转发日志失败"));
    l4Logs.value = {
      ...l4Logs.value,
      [rule.id]: { events: body, loading: false, error: "", loadedAt: Date.now() },
    };
  } catch (e) {
    l4Logs.value = {
      ...l4Logs.value,
      [rule.id]: {
        events: [],
        loading: false,
        error: e instanceof Error ? t(e.message) : t("读取转发日志失败"),
        loadedAt: Date.now(),
      },
    };
  }
}
function previewL4(rule: Layer4Rule, event: MouseEvent | FocusEvent) {
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
  const width = Math.min(540, window.innerWidth - 32);
  l4PreviewLayout.value = {
    position: "fixed",
    width: width + "px",
    left:
      Math.max(
        16,
        Math.min(rect.right - width, window.innerWidth - width - 16),
      ) + "px",
    ...(rect.bottom + 300 > window.innerHeight
      ? { bottom: window.innerHeight - rect.top + "px", top: "auto" }
      : { top: rect.bottom + "px", bottom: "auto" }),
  };
  void loadL4Logs(rule);
}
const previewLayout = ref<Record<string, string>>({});
function previewSite(host: Host, event: MouseEvent | FocusEvent) {
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
let trafficTimer: ReturnType<typeof setInterval> | undefined;
let certificateProgressTimer: ReturnType<typeof setInterval> | undefined;
function canAutoRefresh() {
  return (
    authenticated.value &&
    autoRefresh.value &&
    (!pauseHidden.value || !document.hidden)
  );
}
function resolvePalette(): Palette {
  if (themeChoice.value === "system")
    return themeMedia.matches ? "dark" : "light";
  return themeChoice.value;
}
function applyPanelSettings() {
  const palette = resolvePalette();
  const root = document.documentElement;
  // data-theme keeps the legacy light/dark rules alive; data-palette drives the
  // new token layer, so every palette is a real theme instead of a tint.
  root.dataset.theme = palette === "light" ? "light" : "dark";
  root.dataset.palette = palette;
  root.dataset.density = compactMode.value ? "compact" : "comfortable";
  const values: Record<string, string> = {
    "viya-theme": themeChoice.value,
    "viya-refresh-seconds": String(refreshInterval.value),
    "viya-compact-mode": String(compactMode.value),
    "viya-auto-refresh": String(autoRefresh.value),
    "viya-pause-hidden": String(pauseHidden.value),
    "viya-traffic-charts": String(trafficCharts.value),
    "viya-log-preview-limit": String(previewLimit.value),
    "viya-landing-page": landingPage.value,
  };
  for (const [key, value] of Object.entries(values))
    localStorage.setItem(key, value);
}
function restartTrafficTimer() {
  if (trafficTimer) clearInterval(trafficTimer);
  trafficTimer = setInterval(() => {
    if (!canAutoRefresh()) return;
    void refreshTraffic();
    if (viewingLogSite.value) void loadSiteLogs(viewingLogSite.value, true);
  }, refreshInterval.value * 1000);
}
function synchronizeAutoRefresh() {
  documentHidden.value = document.hidden;
  if (!canAutoRefresh()) return;
  void refreshTraffic();
  if (activeSection.value === "ddns") void refreshDDNS();
  if (activeSection.value === "certificates") void refreshManagedCertificates();
}
watch(
  [
    themeChoice,
    refreshInterval,
    compactMode,
    autoRefresh,
    pauseHidden,
    trafficCharts,
    previewLimit,
    landingPage,
  ],
  () => {
    applyPanelSettings();
    if (trafficTimer) restartTrafficTimer();
    synchronizeAutoRefresh();
  },
);
watch(showPanelSettings, (open) => {
  if (!open) {
    ++settingsReadSequence;
    panelSettingsLoading.value = false;
    passwordForm.value = { current: "", next: "", confirm: "" };
  }
});

const emptyCounters = (): Counters => ({
  requests: 0,
  errors: 0,
  upload: 0,
  download: 0,
});
const trafficTotals = computed(() =>
  Object.values(traffic.value).reduce(
    (sum, item) => ({
      requests: sum.requests + item.recent.requests,
      errors: sum.errors + item.recent.errors,
      upload: sum.upload + item.recent.upload,
      download: sum.download + item.recent.download,
    }),
    emptyCounters(),
  ),
);
// Stable zero-value buckets: the template reads stats many times per render,
// so allocating a fresh object for sites without traffic wasted both memory and
// reference equality (each call looked like a new value to Vue).
const ZERO_COUNTERS = Object.freeze(emptyCounters()) as Counters;
const EMPTY_SITE_TRAFFIC: SiteTraffic = {
  total: ZERO_COUNTERS,
  recent: ZERO_COUNTERS,
  hourly: [],
};
const siteTrafficIndex = computed(() => {
  const index: Record<number, SiteTraffic> = {};
  for (const host of hosts.value)
    index[host.id] = traffic.value[host.id] || EMPTY_SITE_TRAFFIC;
  return index;
});
function siteStats(host: Host): SiteTraffic {
  return siteTrafficIndex.value[host.id] || EMPTY_SITE_TRAFFIC;
}
// Precomputed once per traffic refresh: 24 aligned buckets plus the peak
// volume per site. The template previously recomputed both for every bar.
const trafficHourIndex = computed(() => {
  const current = Math.floor(Date.now() / 3600000) * 3600;
  const index: Record<
    number,
    { points: ({ hour: number } & Counters)[]; max: number }
  > = {};
  for (const [id, summary] of Object.entries(traffic.value)) {
    const byHour = new Map<number, { hour: number } & Counters>();
    for (const point of summary.hourly) byHour.set(point.hour, point);
    const points: ({ hour: number } & Counters)[] = [];
    let max = 0;
    for (let offset = 23; offset >= 0; offset--) {
      const hour = current - offset * 3600;
      const point = byHour.get(hour) || { hour, ...ZERO_COUNTERS };
      points.push(point);
      const volume = point.upload + point.download;
      if (volume > max) max = volume;
    }
    index[Number(id)] = { points, max };
  }
  return index;
});
// One shared fallback per clock hour instead of 24 fresh objects per call.
let emptyHourCache: { hour: number; points: ({ hour: number } & Counters)[] } = {
  hour: 0,
  points: [],
};
function emptyHourPoints() {
  const current = Math.floor(Date.now() / 3600000) * 3600;
  if (emptyHourCache.hour !== current)
    emptyHourCache = {
      hour: current,
      points: Array.from({ length: 24 }, (_, i) => ({
        hour: current - (23 - i) * 3600,
        ...ZERO_COUNTERS,
      })),
    };
  return emptyHourCache.points;
}
function hourlyTraffic(host: Host) {
  return trafficHourIndex.value[host.id]?.points || emptyHourPoints();
}
// Longer windows reuse the same card but aggregate by day: traffic_hours keeps
// 90 days, so 30d is the widest honest window.
const trafficRange = ref<"24h" | "7d" | "30d">("24h");
// The window is chosen by the backend, so the series has to be re-fetched.
watch(trafficRange, () => {
  void refreshTraffic();
});
const trafficDays = computed(() =>
  trafficRange.value === "30d" ? 30 : trafficRange.value === "7d" ? 7 : 0,
);
const trafficDayStart = computed(() =>
  Math.floor(Date.now() / 86400000) * 86400,
);
// Whole-site series for the overview trend card.
const trafficTrend = computed(() => {
  const days = trafficDays.value;
  if (!days) {
    const totals = Array.from({ length: 24 }, () => 0);
    for (const current of Object.values(trafficHourIndex.value))
      current.points.forEach((point, i) => {
        totals[i] = (totals[i] || 0) + point.upload + point.download;
      });
    const hourStart = Math.floor(Date.now() / 3600000) * 3600;
    return totals.map((bytes, i) => ({
      hour: hourStart - (23 - i) * 3600,
      bytes,
    }));
  }
  const totals = Array.from({ length: days }, () => 0);
  for (const summary of Object.values(traffic.value))
    for (const point of summary.daily || []) {
      const index = days - 1 - Math.round((trafficDayStart.value - point.day) / 86400);
      if (index >= 0 && index < days)
        totals[index] += point.upload + point.download;
    }
  return totals.map((bytes, i) => ({
    hour: trafficDayStart.value - (days - 1 - i) * 86400,
    bytes,
  }));
});
// Stored revisions are redacted, so the panel can only compare them, not replay
// them. Real rollback goes through the encrypted archive above this list.
const revisionDiff = ref<{
  id: number;
  from: number;
  to: number;
  added: { path: string; to: string }[];
  removed: { path: string; from: string }[];
  changed: { path: string; from: string; to: string }[];
  truncated: boolean;
} | null>(null);
const revisionDiffBusy = ref(false),
  revisionDiffError = ref("");
async function loadRevisionDiff(id: number, against = "current") {
  revisionDiffBusy.value = true;
  revisionDiffError.value = "";
  try {
    const r = await fetch(
      `/api/v1/config/revisions/${id}/diff?against=${encodeURIComponent(
        String(against),
      )}`,
    );
    const data = await r.json();
    if (!r.ok) throw Error(data.error || t("对比失败"));
    revisionDiff.value = {
      id,
      from: data.from,
      to: data.to,
      added: data.added || [],
      removed: data.removed || [],
      changed: data.changed || [],
      truncated: !!data.truncated,
    };
  } catch (e) {
    revisionDiffError.value =
      e instanceof Error ? t(e.message) : t("对比失败");
  } finally {
    revisionDiffBusy.value = false;
  }
}
function diffValue(value: string) {
  return value.length > 160 ? value.slice(0, 160) + "…" : value;
}
// Sites ranked by the volume inside the selected window.
const trafficRanking = computed(() => {
  const rows = hosts.value
    .map((host) => {
      const summary = traffic.value[host.id];
      const recent = summary?.recent;
      return {
        id: host.id,
        domain: host.domain,
        bytes: recent ? recent.upload + recent.download : 0,
        requests: recent ? recent.requests : 0,
      };
    })
    .filter((row) => row.bytes > 0);
  rows.sort((a, b) => b.bytes - a.bytes);
  return rows.slice(0, 6);
});
function barHeight(host: Host, bytes: number) {
  // Empty hours stay invisible so the sparkline reads as a chart instead of a
  // dashed divider; the track itself draws the baseline.
  if (bytes <= 0) return 0;
  return Math.max(
    3,
    (bytes / Math.max(1, trafficHourIndex.value[host.id]?.max || 0)) * 30,
  );
}
async function refreshTraffic() {
  if (!authenticated.value) return;
  try {
    const r = await fetch(
      `/api/v1/proxy-hosts/traffic?range=${trafficRange.value}`,
    );
    if (!r.ok) throw Error(t("流量统计暂不可用"));
    traffic.value = await r.json();
    trafficError.value = "";
  } catch (e) {
    trafficError.value = e instanceof Error ? t(e.message) : t("统计读取失败");
  }
}
async function loadSiteLogs(host: Host, force = false) {
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
function openSiteLogs(host: Host) {
  viewingLogSite.value = host;
  loadSiteLogs(host, true);
}
const providerFields: Record<string, string[]> = {
  cloudflare: ["api_token"],
  tencentcloud: ["secret_id", "secret_key"],
  alidns: ["access_key_id", "access_key_secret"],
};
const authChecked = ref(false),
  setupRequired = ref(false),
  authenticated = ref(false),
  csrfToken = ref(""),
  authBusy = ref(false),
  authError = ref("");
const authNotice = ref(sessionStorage.getItem("viya-restored-message") || "");
sessionStorage.removeItem("viya-restored-message");
const authForm = ref({ username: "", password: "", confirm: "" });
// Set after the password step when the account has two-factor enabled; while it
// holds a token the login card shows the verification code field instead.
const totpChallenge = ref("");
const totpCode = ref("");
const activeSection = ref("overview");
const navigationGroups = [
  t("概览"),
  t("网络服务"),
  t("安全与证书"),
  t("运行维护"),
];
const navigationItems = [
  {
    id: "overview",
    group: t("概览"),
    label: t("运行概览"),
    description: t("查看引擎状态和各项服务的配置情况。"),
    icon: LayoutDashboard,
  },
  {
    id: "proxy-sites",
    group: t("网络服务"),
    label: t("Web 服务"),
    description: t("选择监听入口，管理独立站点与证书来源。"),
    icon: Globe2,
    action: t("添加站点"),
    run: () => openCreate(),
  },
  {
    id: "layer4",
    group: t("网络服务"),
    label: t("端口转发"),
    description: t("管理独立监听的 TCP / UDP 转发规则。"),
    icon: Cable,
    action: t("添加转发"),
    run: () => openL4(),
  },
  {
    id: "ddns",
    group: t("网络服务"),
    label: t("动态 DNS"),
    description: t("定时检查公网地址并更新 DNS 记录。"),
    icon: RefreshCw,
    action: t("添加 DDNS"),
    run: () => openDDNS(),
  },
  {
    id: "certificates",
    group: t("安全与证书"),
    label: t("SSL 证书"),
    description: t("申请证书、查看签发状态和管理自定义证书。"),
    icon: ShieldCheck,
    action: t("申请证书"),
    run: openManagedCert,
  },
  {
    id: "dns-providers",
    group: t("安全与证书"),
    label: t("DNS 凭据"),
    description: t("管理用于证书验证与动态域名的 DNS 服务商凭据。"),
    icon: KeyRound,
    action: t("添加 DNS 凭据"),
    run: openProvider,
  },
  {
    id: "ip-bans",
    group: t("安全与证书"),
    label: t("IP 封禁"),
    description: t("新增、编辑或解除站点 IP / 网段封禁规则。"),
    icon: Ban,
    action: t("添加封禁"),
    run: () => openBan(),
  },
  {
    id: "access-logs",
    group: t("运行维护"),
    label: t("日志"),
    description: t("访问记录、程序日志、登录审计与轮转配置。"),
    icon: ScrollText,
  },
  {
    id: "config-history",
    group: t("运行维护"),
    label: t("配置历史"),
    description: t("查看 Caddy 配置应用结果与错误详情。"),
    icon: History,
  },
];
// Grouping and filtering used to run inside the template on every render.
function upstreamLabel(id: number) {
  const state = upstreamHealth.value[String(id)];
  if (!state) return "";
  return state.ok
    ? tr`上游 ${Math.round(state.latency_ms)} ms`
    : t("上游无响应");
}
// Sites whose upstream refused a TCP connection. Showing this on the overview
// is the whole point: the proxy itself is healthy, the thing behind it is not.
const attentionSites = computed(() =>
  hosts.value
    .map((host) => ({
      domain: host.domain,
      target: upstreamHealth.value[String(host.id)]?.target || host.upstream,
      error: upstreamHealth.value[String(host.id)]?.error || "",
      health: upstreamHealth.value[String(host.id)],
    }))
    .filter((entry) => entry.health && !entry.health.ok)
    .map(({ domain, target, error }) => ({ domain, target, error })),
);
const groupedNavigation = computed(() =>
  navigationGroups.map((group) => ({
    group,
    items: navigationItems.filter((item) => item.group === group),
  })),
);
const webSearchOpen = ref(false),
  webSearchInput = ref<HTMLInputElement | null>(null);
const listSearchOpen = ref(false),
  listSearchInput = ref<HTMLInputElement | null>(null);
async function toggleListSearch() {
  listSearchOpen.value = !listSearchOpen.value;
  if (listSearchOpen.value) {
    await nextTick();
    listSearchInput.value?.focus();
  } else listSearch.value = "";
}
async function toggleWebSearch() {
  webSearchOpen.value = !webSearchOpen.value;
  if (webSearchOpen.value) {
    await nextTick();
    webSearchInput.value?.focus();
  } else listSearch.value = "";
}
const listSearch = ref(""),
  statusFilter = ref<"all" | "enabled" | "disabled">("all");
function matchesSearch(...values: unknown[]) {
  const words = listSearch.value
      .trim()
      .toLowerCase()
      .split(/\s+/)
      .filter(Boolean),
    text = values
      .filter((value) => value != null)
      .join(" ")
      .toLowerCase();
  return words.every((word) => text.includes(word));
}
function matchesStatus(enabled: boolean) {
  return (
    statusFilter.value === "all" ||
    enabled === (statusFilter.value === "enabled")
  );
}
const searchedHosts = computed(() =>
  hosts.value.filter(
    (host) =>
      matchesSearch(host.name, host.domain, host.upstream, host.path) &&
      matchesStatus(host.enabled),
  ),
);
const visibleDDNS = computed(() =>
  ddnsRecords.value.filter(
    (record) =>
      matchesSearch(
        record.name,
        record.comment,
        record.zone,
        record.record_name,
        record.record_type,
        record.last_ip,
        record.interface_name,
      ) && matchesStatus(record.enabled),
  ),
);
const visibleL4 = computed(() =>
  l4Rules.value.filter(
    (rule) =>
      matchesSearch(
        rule.name,
        rule.protocol,
        rule.listen_address,
        rule.listen_port,
        rule.upstream_address,
      ) && matchesStatus(rule.enabled),
  ),
);
const visibleProviders = computed(() =>
  providers.value.filter((provider) =>
    matchesSearch(provider.name, provider.provider_type),
  ),
);
const visibleCerts = computed(() =>
  certificates.value.filter((cert) =>
    matchesSearch(cert.name, ...cert.dns_names),
  ),
);
const visibleManagedCerts = computed(() =>
  managedCertificates.value.filter((cert) =>
    matchesSearch(cert.name, cert.domain, cert.status, cert.method),
  ),
);
const visibleBans = computed(() =>
  ipBans.value.filter((ban) => matchesSearch(ban.domain, ban.ip, ban.reason)),
);
const searchablePage = computed(() =>
  ["ddns", "layer4", "ip-bans"].includes(activeSection.value),
);
const statusFilterAvailable = computed(() =>
  ["proxy-sites", "ddns", "layer4"].includes(activeSection.value),
);
const filteredCount = computed(
  () =>
    ({
      "proxy-sites": visibleHosts.value.length,
      ddns: visibleDDNS.value.length,
      layer4: visibleL4.value.length,
      "dns-providers": visibleProviders.value.length,
      certificates:
        visibleCerts.value.length + visibleManagedCerts.value.length,
      "ip-bans": visibleBans.value.length,
    })[activeSection.value] || 0,
);
const hasListFilter = computed(
  () => !!listSearch.value.trim() || statusFilter.value !== "all",
);
function clearListFilters() {
  listSearch.value = "";
  statusFilter.value = "all";
}
const pageInstructions: Record<string, string[]> = {
  overview: [
    t("先添加 DNS 凭据，再申请证书或创建 Web 服务。"),
    t("点击功能卡片进入管理；配置失败时可从最近配置状态查看原因。"),
  ],
  "proxy-sites": [
    t(
      "先选择 HTTP / HTTPS 监听，再添加域名和上游地址；各监听独立管理站点；新建监听默认为空，也可显式导入已有配置。",
    ),
    t("已有主域名可直接添加子域名；访问路径留空表示匹配整个域名。"),
    t("流量概览统计全部站点；搜索和状态筛选仅影响站点列表。"),
    t("日志图标支持悬浮预览；克隆可复用配置；删除前会再次确认。"),
  ],
  layer4: [
    t("填写 Debian 主机监听地址、端口和上游 host:port；选择 TCP、UDP 或两者。"),
    t("监听端口需可用并放行防火墙，不能占用面板或 Web 服务端口。"),
  ],
  ddns: [
    t("先选择 DNS 凭据和 Zone，再选择记录类型与地址来源。"),
    t("多个 IPv6 时优先选稳定公网地址；临时地址可能定期变化。"),
    t("自动更新复选框控制服务器定时任务；关闭后仍可手动更新。"),
  ],
  certificates: [
    t("公网证书推荐 DNS-01；可申请 *.example.com 泛域名证书。"),
    t("HTTP-01 需要公网 80 端口可达；内部 CA 证书需客户端自行信任。"),
    t("已有 PEM 证书使用下方上传入口；站点需选择匹配的 TLS 配置。"),
  ],
  "dns-providers": [
    t("保存服务商的 API 凭据，供证书申请和 DDNS 共用。"),
    t("列表不显示密钥；替换凭据会影响引用该凭据的任务。"),
  ],
  "ip-bans": [
    t("可按站点永久封禁单个 IP 或 CIDR 网段，随时手动解除。"),
    t("手动与自动封禁分别标记；解除封禁需要确认。"),
  ],
  "access-logs": [
    t("查看代理请求的时间、域名、路径和响应状态；查询参数与凭据已隐藏。"),
    t("「登录审计」记录每次管理员登录的成败、来源 IP 与客户端，便于事后追溯。"),
    t("自动刷新可在左下角设置中暂停；手动刷新始终可用。"),
  ],
  "config-history": [
    t("记录配置提交和 Caddy 应用结果；失败记录用于定位错误。"),
    t("配置保存与生效是两个步骤；应用失败时请查看原因并重新应用。"),
  ],
};
const currentPage = computed(
  () =>
    navigationItems.find((item) => item.id === activeSection.value) ||
    navigationItems[0]!,
);
const overviewItems = computed(() => [
  {
    id: "proxy-sites",
    label: t("Web 服务"),
    count: hosts.value.length,
    detail: tr`${hosts.value.filter((h) => h.enabled).length} 个站点已启用`,
    icon: Globe2,
  },
  {
    id: "layer4",
    label: t("端口转发"),
    count: l4Rules.value.length,
    detail: t("TCP / UDP 转发规则"),
    icon: Cable,
  },
  {
    id: "ddns",
    label: t("动态 DNS"),
    count: ddnsRecords.value.length,
    detail: t("公网地址更新任务"),
    icon: RefreshCw,
  },
  {
    id: "certificates",
    label: t("SSL 证书"),
    count:
      certificates.value.length +
      managedCertificates.value.length +
      automaticCertificates.value.length,
    detail: t("托管申请与自定义证书"),
    icon: ShieldCheck,
  },
]);

function configurationRestored(
  preferences: Record<string, string>,
  message: string,
) {
  const keys = Object.keys(localStorage).filter((key) =>
    key.startsWith("viya-"),
  );
  for (const key of keys) localStorage.removeItem(key);
  for (const [key, value] of Object.entries(preferences)) {
    if (
      key.startsWith("viya-") &&
      key.length < 100 &&
      typeof value === "string" &&
      value.length < 32768
    )
      localStorage.setItem(key, value);
  }
  sessionStorage.setItem("viya-restored-message", message);
  window.location.reload();
}
async function changeUsername(username: string, password: string) {
  if (panelSettingsBusy.value) return;
  panelSettingsBusy.value = true;
  panelSettingsError.value = "";
  panelSettingsMessage.value = "";
  try {
    const r = await fetch("/api/v1/auth/username", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({ username, password }),
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("账号修改失败"));
    panelUsername.value = body.username;
    panelSessions.value = 1;
    authForm.value.username = body.username;
    panelSettingsMessage.value = t("账号已修改，其他登录会话已退出。");
  } catch (e) {
    panelSettingsError.value =
      e instanceof Error ? t(e.message) : t("账号修改失败");
  } finally {
    panelSettingsBusy.value = false;
  }
}
async function initializeAuth() {
  try {
    const r = await fetch("/api/v1/auth/status");
    const status = await r.json();
    setupRequired.value = status.setup_required;
    if (!setupRequired.value) await refreshSession();
  } catch {
    authError.value = t("无法连接管理服务");
  } finally {
    authChecked.value = true;
  }
}
async function refreshSession() {
  const r = await fetch("/api/v1/auth/me");
  if (!r.ok) {
    authenticated.value = false;
    return;
  }
  const data = await r.json();
  panelUsername.value = data.username || authForm.value.username;
  panelRole.value = data.role === "viewer" ? "viewer" : "admin";
  csrfToken.value = data.csrf_token;
  authenticated.value = true;
  try {
    const settings = await fetch("/api/v1/panel/settings");
    if (settings.ok) {
      const data = await settings.json();
      panelListenPort.value = Number(data.listen_port) || 8181;
    }
  } catch {}
  await load();
  syncActiveSection();
}
let settingsReadSequence = 0;
async function openPanelSettings() {
  showPanelSettings.value = true;
  panelSettingsError.value = "";
  panelSettingsMessage.value = "";
  panelSettingsLoading.value = true;
  const sequence = ++settingsReadSequence;
  try {
    const r = await fetch("/api/v1/panel/settings");
    const data = await r.json();
    if (sequence !== settingsReadSequence || !showPanelSettings.value) return;
    if (!r.ok) throw Error(t(data.error) || t("读取设置失败"));
    storedPanelPort.value = Number(data.listen_port);
    panelListenPort.value = storedPanelPort.value;
    panelUsername.value = data.username || panelUsername.value;
    panelSessions.value = Number.isInteger(data.active_sessions)
      ? data.active_sessions
      : null;
  } catch (e) {
    if (sequence === settingsReadSequence)
      panelSettingsError.value =
        e instanceof Error ? t(e.message) : t("读取设置失败");
  } finally {
    if (sequence === settingsReadSequence) panelSettingsLoading.value = false;
  }
}
function settingsShortcut(event: KeyboardEvent) {
  if (
    (event.ctrlKey || event.metaKey) &&
    event.key === "," &&
    authenticated.value
  ) {
    event.preventDefault();
    if (showPanelSettings.value) {
      if (!panelSettingsBusy.value) showPanelSettings.value = false;
    } else if (!document.querySelector(".overlay")) void openPanelSettings();
  }
}
function resetPanelPreferences() {
  themeChoice.value = "system";
  compactMode.value = false;
  refreshInterval.value = 5;
  autoRefresh.value = true;
  pauseHidden.value = true;
  trafficCharts.value = true;
  previewLimit.value = 5;
  landingPage.value = "overview";
  panelSettingsError.value = "";
  panelSettingsMessage.value = t("浏览器偏好已恢复默认。");
}
async function copyPanelAddress() {
  panelSettingsError.value = "";
  try {
    await navigator.clipboard.writeText(panelAccessURL);
    panelSettingsMessage.value = t("面板地址已复制。");
  } catch {
    panelSettingsError.value = t("浏览器未允许复制，可手动选择上方地址。");
  }
}
async function revokeOtherSessions() {
  if (panelSettingsBusy.value) return;
  panelSettingsBusy.value = true;
  panelSettingsError.value = "";
  panelSettingsMessage.value = "";
  try {
    const r = await fetch("/api/v1/auth/sessions/revoke-others", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken.value },
    });
    const data = await r.json();
    if (!r.ok) throw Error(t(data.error) || t("会话退出失败"));
    panelSessions.value = 1;
    panelSettingsMessage.value = tr`已退出 ${data.revoked} 个其他会话，当前登录保留。`;
  } catch (e) {
    panelSettingsError.value =
      e instanceof Error ? t(e.message) : t("会话退出失败");
  } finally {
    panelSettingsBusy.value = false;
  }
}
async function savePanelPort() {
  if (panelSettingsBusy.value || panelSettingsLoading.value) return;
  panelSettingsError.value = "";
  panelSettingsMessage.value = "";
  if (
    !Number.isInteger(panelListenPort.value) ||
    panelListenPort.value < 1024 ||
    panelListenPort.value > 65535
  ) {
    panelSettingsError.value = t("端口范围为 1024–65535");
    return;
  }
  panelSettingsBusy.value = true;
  try {
    const r = await fetch("/api/v1/panel/settings/port", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({ port: panelListenPort.value }),
    });
    const data = await r.json();
    if (!r.ok) throw Error(t(data.error) || t("端口保存失败"));
    if (data.unchanged) {
      panelSettingsMessage.value = t("端口未变化。");
      return;
    }
    const directHTTP =
      window.location.protocol === "http:" &&
      Number(panelAccessPort) === storedPanelPort.value;
    storedPanelPort.value = Number(data.new_port);
    panelSettingsMessage.value = directHTTP
      ? tr`服务即将重启，稍后跳转至新端口 ${data.new_port}。`
      : tr`服务即将重启，新端口为 ${data.new_port}。请同步调整反代上游后重新连接当前域名。`;
    if (directHTTP)
      setTimeout(() => {
        const address = new URL(window.location.href);
        address.port = String(data.new_port);
        window.location.assign(address.href);
      }, 4500);
  } catch (e) {
    panelSettingsError.value =
      e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    panelSettingsBusy.value = false;
  }
}

async function changeAdminPassword() {
  if (panelSettingsBusy.value) return;
  panelSettingsError.value = "";
  panelSettingsMessage.value = "";
  if (passwordForm.value.next !== passwordForm.value.confirm) {
    panelSettingsError.value = t("两次输入的新密码不一致");
    return;
  }
  panelSettingsBusy.value = true;
  try {
    const r = await fetch("/api/v1/auth/password", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({
        current_password: passwordForm.value.current,
        new_password: passwordForm.value.next,
        confirm_password: passwordForm.value.confirm,
      }),
    });
    const data = await r.json();
    if (!r.ok) throw Error(t(data.error) || t("密码修改失败"));
    passwordForm.value = { current: "", next: "", confirm: "" };
    panelSessions.value = 1;
    panelSettingsMessage.value = t("管理员密码已更新，其他登录会话已退出。");
  } catch (e) {
    panelSettingsError.value =
      e instanceof Error ? t(e.message) : t("密码修改失败");
  } finally {
    panelSettingsBusy.value = false;
  }
}
async function authenticate() {
  authBusy.value = true;
  authError.value = "";
  try {
    if (
      setupRequired.value &&
      authForm.value.password !== authForm.value.confirm
    )
      throw Error(t("两次输入的密码不一致"));
    const endpoint = setupRequired.value ? "setup" : "login";
    const r = await fetch(`/api/v1/auth/${endpoint}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        username: authForm.value.username,
        password: authForm.value.password,
      }),
    });
    const data = await r.json();
    if (!r.ok) throw Error(t(data.error) || t("认证失败"));
    if (data.totp_required) {
      totpChallenge.value = String(data.challenge || "");
      totpCode.value = "";
      return;
    }
    await refreshSession();
  } catch (e) {
    authError.value = e instanceof Error ? t(e.message) : t("认证失败");
  } finally {
    authBusy.value = false;
  }
}
async function submitTOTP() {
  authBusy.value = true;
  authError.value = "";
  try {
    const r = await fetch("/api/v1/auth/login/totp", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        challenge: totpChallenge.value,
        code: totpCode.value.trim(),
      }),
    });
    const data = await r.json();
    if (!r.ok) throw Error(t(data.error) || t("验证失败"));
    totpChallenge.value = "";
    totpCode.value = "";
    await refreshSession();
  } catch (e) {
    authError.value = e instanceof Error ? t(e.message) : t("验证失败");
  } finally {
    authBusy.value = false;
  }
}
function cancelTOTP() {
  totpChallenge.value = "";
  totpCode.value = "";
  authError.value = "";
  authForm.value.password = "";
}
async function logout() {
  await fetch("/api/v1/auth/logout", {
    method: "POST",
    headers: { "X-CSRF-Token": csrfToken.value },
  });
  authenticated.value = false;
  csrfToken.value = "";
  authForm.value.password = "";
  authForm.value.confirm = "";
}

async function load() {
  const requestSequence = ++loadRequestSequence,
    ddnsSequence = ddnsRefreshSequence;
  refreshing.value = true;
  error.value = "";
  let succeeded = false;
  try {
    const urls = [
      "/api/v1/proxy-hosts",
      "/api/v1/health",
      "/api/v1/config/revisions",
      "/api/v1/dns-providers",
      "/api/v1/layer4-rules",
      "/api/v1/ddns-records",
      "/api/v1/custom-certificates",
      "/api/v1/ip-bans",
      "/api/v1/certificates/expiring?days=30",
      "/api/v1/proxy-hosts/health",
    ];
    const responses = await Promise.all(urls.map((url) => fetch(url)));
    if (requestSequence !== loadRequestSequence) return;
    if (responses.some((r) => r.status === 401)) {
      authenticated.value = false;
      csrfToken.value = "";
      return;
    }
    if (responses.some((r) => !r.ok)) throw Error(t("后端暂不可用"));
    const [h, health, history, dns, l4, ddns, certs, bans, expiring, upstream] =
      await Promise.all(responses.map((r) => r.json()));
    if (requestSequence !== loadRequestSequence) return;
    hosts.value = h;
    revisions.value = history.slice(0, 10);
    providers.value = dns;
    l4Rules.value = l4;
    if (ddnsSequence === ddnsRefreshSequence) ddnsRecords.value = ddns;
    certificates.value = certs;
    ipBans.value = bans;
    certExpiring.value = Array.isArray(expiring?.certificates)
      ? expiring.certificates
      : [];
    upstreamHealth.value =
      upstream && typeof upstream === "object" ? upstream : {};
    online.value = health.ok && health.caddy_ready;
    // These four used to run one after another, adding their latencies to every
    // refresh. They are independent reads, so fan them out instead.
    await Promise.all([
      refreshTraffic(),
      refreshManagedCertificates(),
      refreshCertificateInventory(),
      loadWebListeners(),
    ]);
    succeeded = true;
  } catch (e) {
    if (requestSequence === loadRequestSequence) {
      online.value = false;
      error.value =
        e instanceof Error
          ? tr`刷新失败：${e.message}`
          : t("刷新失败：后端暂不可用");
    }
  } finally {
    if (requestSequence === loadRequestSequence) {
      refreshing.value = false;
      if (succeeded) lastRefreshedAt.value = Date.now();
    }
  }
}

function openL4(rule?: Layer4Rule) {
  l4EditingID.value = rule?.id ?? null;
  l4PartialSave.value = false;
  l4Form.value = rule
    ? { ...rule }
    : {
        name: "",
        listen_address: "0.0.0.0",
        listen_port: 2222,
        upstream_address: "127.0.0.1:22",
        protocol: "tcp",
        enabled: true,
      };
  l4Error.value = "";
  applyRetryVisible.value = false;
  showL4.value = true;
}
async function saveL4() {
  l4Error.value = "";
  saving.value = true;
  try {
    const creating = l4EditingID.value === null;
    const url = creating
      ? "/api/v1/layer4-rules"
      : `/api/v1/layer4-rules/${l4EditingID.value}`;
    const r = await fetch(url, {
      method: creating ? "POST" : "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify(l4Form.value),
    });
    const body = r.status === 204 ? {} : await r.json();
    if (!r.ok) {
      if (r.status === 409) {
        await load();
        throw Error(
          t(body.error) || t("该转发已被其他会话修改，请关闭后重新打开"),
        );
      }
      if (r.status === 502) {
        applyRetryVisible.value = true;
        l4PartialSave.value = true;
        await load();
        if (creating) {
          const saved = l4Rules.value.find(
            (rule) =>
              rule.listen_port === l4Form.value.listen_port &&
              rule.listen_address === l4Form.value.listen_address &&
              rule.protocol === l4Form.value.protocol,
          );
          if (saved) {
            l4EditingID.value = saved.id;
            l4Form.value = { ...saved };
          }
        }
        l4Error.value = tr`${t(body.error) || t("Caddy 未应用配置")}。规则已保存，请勿重复新建；可重新应用或修正后再次保存。`;
        return;
      }
      throw Error(t(body.error) || t("保存失败"));
    }
    applyRetryVisible.value = false;
    l4PartialSave.value = false;
    showL4.value = false;
    await load();
  } catch (e) {
    l4Error.value = e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    saving.value = false;
  }
}
const l4Toggling = ref<Record<number, boolean>>({});
async function toggleL4(rule: Layer4Rule) {
  if (l4Toggling.value[rule.id]) return;
  l4Toggling.value[rule.id] = true;
  try {
    const r = await fetch(`/api/v1/layer4-rules/${rule.id}/enabled`, {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({ enabled: !rule.enabled, version: rule.version }),
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error));
    await load();
  } catch (e) {
    const message = e instanceof Error ? t(e.message) : t("修改失败");
    await load();
    error.value = message;
  } finally {
    l4Toggling.value[rule.id] = false;
  }
}
async function removeL4(rule: Layer4Rule) {
  if (
    await confirmDeletion(
      tr`删除 ${rule.protocol.toUpperCase()} ${rule.listen_address}:${rule.listen_port} 转发？`,
    )
  )
    await performDelete(`/api/v1/layer4-rules/${rule.id}`);
}
async function refreshNetworkInterfaces() {
  try {
    const response = await fetch("/api/v1/network-interfaces");
    if (response.ok) networkInterfaces.value = await response.json();
  } catch {}
}
function interfaceLabel(item: NetworkInterface) {
  const family = ddnsForm.value.record_type === "A" ? "ipv4" : "ipv6";
  const addresses = item.addresses.filter(
    (address) => address.family === family,
  );
  return `${item.name} · ${addresses.length ? tr`${addresses.length} 个${family === "ipv6" ? "IPv6" : "IPv4"} 地址` : t("无对应地址")}`;
}
let ddnsZoneSequence = 0;
async function loadDDNSZones(clearZone = false) {
  const sequence = ++ddnsZoneSequence,
    providerID = ddnsForm.value.dns_provider_id;
  ddnsZones.value = [];
  ddnsZoneMessage.value = "";
  if (clearZone) ddnsForm.value.zone = "";
  if (!providerID) {
    manualDDNSZone.value = true;
    ddnsZoneMessage.value = t("请先选择 DNS 服务商。");
    ddnsZoneLoading.value = false;
    return;
  }
  ddnsZoneLoading.value = true;
  try {
    const response = await fetch(`/api/v1/dns-providers/${providerID}/zones`);
    const data = await response.json();
    if (sequence !== ddnsZoneSequence) return;
    if (!response.ok) throw Error(t(data.error) || t("读取 DNS Zone 失败"));
    if (data.supported) {
      ddnsZones.value = data.zones || [];
      const exists = ddnsZones.value.some(
        (zone) => zone.name === ddnsForm.value.zone,
      );
      manualDDNSZone.value =
        !ddnsZones.value.length ||
        (!clearZone && !!ddnsForm.value.zone && !exists);
      ddnsZoneMessage.value = ddnsZones.value.length
        ? tr`已读取 ${ddnsZones.value.length} 个可管理 Zone。`
        : t(data.message) || t("此账号下没有可用 Zone，请手动输入。");
    } else {
      manualDDNSZone.value = true;
      ddnsZoneMessage.value =
        t(data.message) || t("此服务商暂不支持自动列出 Zone，请手动输入。");
    }
  } catch (e) {
    if (sequence === ddnsZoneSequence) {
      manualDDNSZone.value = true;
      ddnsZoneMessage.value =
        e instanceof Error ? t(e.message) : t("读取 Zone 失败，可手动输入。");
    }
  } finally {
    if (sequence === ddnsZoneSequence) ddnsZoneLoading.value = false;
  }
}

async function openDDNS(record?: DDNSRecord) {
  await refreshNetworkInterfaces();
  ddnsEditingID.value = record?.id ?? null;
  const firstIPv4 = networkInterfaces.value.find((item) =>
    item.addresses.some((address) => address.family === "ipv4"),
  );
  ddnsForm.value = record
    ? {
        ...record,
        comment: record.comment || "viya",
        cname_target: record.cname_target || "",
        interface_ip: record.interface_ip || "",
        dns_provider_id: String(record.dns_provider_id),
        ip_source: record.ip_source || "http",
        interface_name: record.interface_name || "",
      }
    : {
        name: "",
        comment: "viya",
        dns_provider_id: providers.value[0]
          ? String(providers.value[0].id)
          : "",
        zone: "",
        record_name: "@",
        record_type: "A",
        cname_target: "",
        ip_source: firstIPv4 ? "interface" : "http",
        interface_name: firstIPv4?.name || "",
        interface_ip: "",
        ttl_seconds: 300,
        interval_minutes: 5,
        enabled: true,
      };
  ddnsError.value = "";
  ddnsZones.value = [];
  manualDDNSZone.value = false;
  showDDNS.value = true;
  void loadDDNSZones(false);
}
async function saveDDNS() {
  saving.value = true;
  ddnsError.value = "";
  try {
    const url =
      ddnsEditingID.value === null
        ? "/api/v1/ddns-records"
        : `/api/v1/ddns-records/${ddnsEditingID.value}`;
    const r = await fetch(url, {
      method: ddnsEditingID.value === null ? "POST" : "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({
        ...ddnsForm.value,
        dns_provider_id: Number(ddnsForm.value.dns_provider_id),
      }),
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("保存失败"));
    showDDNS.value = false;
    await load();
  } catch (e) {
    ddnsError.value = e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    saving.value = false;
  }
}
const ddnsRunning = ref<Record<number, boolean>>({}),
  ddnsToggling = ref<Record<number, boolean>>({}),
  ddnsFeedback = ref<Record<number, string>>({});
let ddnsRefreshSequence = 0;
let ddnsRefreshPromise: Promise<void> | null = null;
async function refreshDDNS(force = false) {
  while (ddnsRefreshPromise) {
    await ddnsRefreshPromise;
    if (!force) return;
  }
  const sequence = ++ddnsRefreshSequence;
  const task = (async () => {
    try {
      const r = await fetch("/api/v1/ddns-records");
      if (r.ok) {
        const records = await r.json();
        if (sequence === ddnsRefreshSequence) ddnsRecords.value = records;
      }
    } catch {}
  })();
  ddnsRefreshPromise = task;
  try {
    await task;
  } finally {
    if (ddnsRefreshPromise === task) ddnsRefreshPromise = null;
  }
}
function patchDDNS(id: number, patch: Partial<DDNSRecord>) {
  const current = ddnsRecords.value.find((item) => item.id === id);
  if (current) Object.assign(current, patch);
}
async function toggleDDNS(record: DDNSRecord, enabled: boolean) {
  if (ddnsToggling.value[record.id]) return;
  ddnsToggling.value[record.id] = true;
  ddnsFeedback.value[record.id] = "";
  ++ddnsRefreshSequence;
  try {
    const r = await fetch(`/api/v1/ddns-records/${record.id}/enabled`, {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({ enabled }),
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("修改失败"));
    ++ddnsRefreshSequence;
    patchDDNS(record.id, { enabled: body.enabled });
    await refreshDDNS(true);
  } catch (e) {
    ddnsFeedback.value[record.id] =
      e instanceof Error ? t(e.message) : t("修改失败");
    await refreshDDNS(true);
  } finally {
    ddnsToggling.value[record.id] = false;
  }
}
async function runDDNS(record: DDNSRecord) {
  if (ddnsRunning.value[record.id]) return;
  ddnsRunning.value[record.id] = true;
  ddnsFeedback.value[record.id] = "";
  try {
    const r = await fetch(`/api/v1/ddns-records/${record.id}/run`, {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken.value },
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("更新失败"));
    ++ddnsRefreshSequence;
    patchDDNS(record.id, {
      last_run_at: body.last_run_at,
      last_ip: body.ip,
      last_error: "",
    });
    ddnsFeedback.value[record.id] = t("更新完成");
  } catch (e) {
    ddnsFeedback.value[record.id] =
      e instanceof Error ? t(e.message) : t("更新失败");
  } finally {
    await refreshDDNS(true);
    ddnsRunning.value[record.id] = false;
  }
}

async function removeDDNS(record: DDNSRecord) {
  if (await confirmDeletion(tr`删除 DDNS 任务“${record.name}”？`))
    await performDelete(`/api/v1/ddns-records/${record.id}`);
}
async function saveCert() {
  certError.value = "";
  saving.value = true;
  try {
    if (!certFile.value || !keyFile.value)
      throw Error(t("请选择证书和私钥 PEM 文件"));
    const data = new FormData();
    data.set("name", certForm.value.name);
    data.set("certificate", certFile.value);
    data.set("private_key", keyFile.value);
    const r = await fetch("/api/v1/custom-certificates", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken.value },
      body: data,
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("上传失败"));
    showCert.value = false;
    certFile.value = null;
    keyFile.value = null;
    certForm.value.name = "";
    await load();
  } catch (e) {
    certError.value = e instanceof Error ? t(e.message) : t("上传失败");
  } finally {
    saving.value = false;
  }
}
async function removeCert(cert: Certificate) {
  if (await confirmDeletion(tr`删除证书“${cert.name}”？`))
    await performDelete(`/api/v1/custom-certificates/${cert.id}`);
}

async function removeBan(id: number) {
  if (await confirmDeletion(tr`确认解除这条 IP 封禁？`))
    await performDelete(`/api/v1/ip-bans/${id}`);
}
function openCreate(parentDomain?: string) {
  editingID.value = null;
  partialSave.value = false;
  advancedOpen.value = false;
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
    basic_auth_enabled: false,
    basic_auth_user: "",
    basic_auth_password: "",
    rate_limit_requests: 0,
    rate_limit_window: "1s",
  };
  headerText.value = "";
  allowlistText.value = "";
  denylistText.value = "";
  error.value = "";
  show.value = true;
}
function openEdit(host: Host) {
  editingID.value = host.id;
  partialSave.value = false;
  advancedOpen.value =
    Object.keys(host.custom_headers || {}).length > 0 ||
    host.ip_allowlist.length > 0 ||
    host.ip_denylist.length > 0 ||
    host.rate_limit_requests > 0;
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
    rate_limit_requests: host.rate_limit_requests || 0,
    rate_limit_window: host.rate_limit_window || "1s",
  };
  headerText.value = Object.entries(host.custom_headers || {})
    .map(([key, value]) => `${key}: ${value}`)
    .join("\n");
  allowlistText.value = (host.ip_allowlist || []).join("\n");
  denylistText.value = (host.ip_denylist || []).join("\n");
  error.value = "";
  show.value = true;
}
async function testUpstream() {
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
async function save() {
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
    const headers: Record<string, string> = {};
    for (const line of headerText.value
      .split("\n")
      .map((value) => value.trim())
      .filter(Boolean)) {
      const split = line.indexOf(":");
      if (split < 1) throw Error(t("请求头请按“名称: 值”格式填写"));
      headers[line.slice(0, split).trim()] = line.slice(split + 1).trim();
    }
    const lines = (value: string) =>
      value
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean);
    const payload = {
      ...form.value,
      domain: hostDomain,
      dns_provider_id:
        form.value.tls_mode === "dns-01" && form.value.dns_provider_id
          ? Number(form.value.dns_provider_id)
          : null,
      custom_certificate_id:
        form.value.tls_mode === "custom" && form.value.custom_certificate_id
          ? Number(form.value.custom_certificate_id)
          : null,
      custom_headers: headers,
      ip_allowlist: lines(allowlistText.value),
      ip_denylist: lines(denylistText.value),
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
async function retryApply() {
  saving.value = true;
  try {
    const r = await fetch("/api/v1/config/apply", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken.value },
    });
    const body = r.status === 204 ? {} : await r.json();
    if (!r.ok) throw Error(t(body.error) || t("重新应用失败"));
    applyRetryVisible.value = false;
    advancedOpen.value = false;
    form.value.basic_auth_password = "";
    providerError.value = "";
    l4Error.value = "";
    error.value = t("当前配置已成功重新应用到 Caddy。");
    await load();
    if (partialSave.value && editingID.value === null) {
      const saved = hosts.value.find(
        (host) =>
          host.domain === form.value.domain.trim().toLowerCase() &&
          host.listener_id === form.value.listener_id,
      );
      if (saved) editingID.value = saved.id;
    }
    if (l4PartialSave.value && l4EditingID.value === null) {
      const saved = l4Rules.value.find(
        (rule) =>
          rule.listen_port === l4Form.value.listen_port &&
          rule.listen_address === l4Form.value.listen_address &&
          rule.protocol === l4Form.value.protocol,
      );
      if (saved) l4EditingID.value = saved.id;
    }
    if (editingID.value !== null) partialSave.value = false;
    if (l4EditingID.value !== null) l4PartialSave.value = false;
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t("重新应用失败");
  } finally {
    saving.value = false;
  }
}
function openProvider() {
  editingProviderID.value = null;
  providerForm.value = {
    name: "",
    provider_type: "cloudflare",
    credentials: {},
  };
  providerError.value = "";
  applyRetryVisible.value = false;
  showProvider.value = true;
}
function editProvider(provider: Provider) {
  editingProviderID.value = provider.id;
  providerForm.value = {
    name: provider.name,
    provider_type: provider.provider_type,
    credentials: {},
  };
  providerError.value = "";
  showProvider.value = true;
}
async function saveProvider() {
  saving.value = true;
  providerError.value = "";
  try {
    const url =
      editingProviderID.value === null
        ? "/api/v1/dns-providers"
        : `/api/v1/dns-providers/${editingProviderID.value}`;
    const r = await fetch(url, {
      method: editingProviderID.value === null ? "POST" : "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify(providerForm.value),
    });
    const data = await r.json();
    if (!r.ok) {
      if (r.status === 502) {
        applyRetryVisible.value = true;
        await load();
        throw Error(
          tr`${t(data.error) || t("Caddy 未重新载入 DNS 凭据")}。凭据已保存，可重新应用配置。`,
        );
      }
      throw Error(t(data.error) || t("保存 DNS 凭据失败"));
    }
    applyRetryVisible.value = false;
    showProvider.value = false;
    await load();
  } catch (e) {
    providerError.value = e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    saving.value = false;
  }
}
async function removeProvider(provider: Provider) {
  if (await confirmDeletion(tr`删除 DNS 凭据“${provider.name}”？`))
    await performDelete(`/api/v1/dns-providers/${provider.id}`);
}

async function remove(host: Host) {
  if (await confirmDeletion(tr`确认删除 Web 服务“${host.domain}”？`))
    await performDelete(`/api/v1/proxy-hosts/${host.id}`);
}

function cloneHost(host: Host) {
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
    host.rate_limit_requests > 0;
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
    rate_limit_requests: host.rate_limit_requests || 0,
    rate_limit_window: host.rate_limit_window || "1s",
  };
  headerText.value = Object.entries(host.custom_headers || {})
    .map(([key, value]) => `${key}: ${value}`)
    .join("\n");
  allowlistText.value = (host.ip_allowlist || []).join("\n");
  denylistText.value = (host.ip_denylist || []).join("\n");
  error.value = "";
  show.value = true;
}
function focusMain() {
  document.getElementById("main-content")?.focus();
}
function syncActiveSection() {
  listSearch.value = "";
  webSearchOpen.value = false;
  statusFilter.value = "all";
  let section = window.location.hash.slice(1);
  if (section === "dns-ddns") section = "dns-providers";
  activeSection.value = navigationItems.some((item) => item.id === section)
    ? section
    : navigationItems.some((item) => item.id === landingPage.value)
      ? landingPage.value
      : "overview";
  if (section !== activeSection.value)
    history.replaceState(null, "", `#${activeSection.value}`);
  document.title = `${currentPage.value.label} · Viya`;
  window.scrollTo({ top: 0, behavior: "instant" });
}
onMounted(() => {
  if (!navigationItems.some((item) => item.id === landingPage.value))
    landingPage.value = "overview";
  applyPanelSettings();
  syncActiveSection();
  window.addEventListener("hashchange", syncActiveSection);
  window.addEventListener("keydown", settingsShortcut);
  document.addEventListener("visibilitychange", synchronizeAutoRefresh);
  themeMedia.addEventListener("change", applyPanelSettings);
  initializeAuth();
  restartTrafficTimer();
  // Progress/DNS polling: 2s is still perceived as live and halves the request
  // volume compared with the old 1s tick.
  certificateProgressTimer = setInterval(() => {
    if (!canAutoRefresh()) return;
    if (activeSection.value === "ddns") void refreshDDNS();
    if (activeSection.value === "certificates")
      void refreshCertificateInventory();
    if (
      activeSection.value === "certificates" &&
      managedCertificates.value.some(
        (cert) => !["issued", "expired"].includes(cert.status),
      )
    )
      refreshManagedCertificates();
  }, 2000);
});
onBeforeUnmount(() => {
  window.removeEventListener("hashchange", syncActiveSection);
  window.removeEventListener("keydown", settingsShortcut);
  document.removeEventListener("visibilitychange", synchronizeAutoRefresh);
  themeMedia.removeEventListener("change", applyPanelSettings);
  if (trafficTimer) clearInterval(trafficTimer);
  if (certificateProgressTimer) clearInterval(certificateProgressTimer);
  resolveDelete(false);
});
</script>

<template>
  <div v-if="authChecked && !authenticated" class="auth-screen">
    <LanguagePicker class="login-language" />
    <form
      class="modal auth-modal"
      @submit.prevent="totpChallenge ? submitTOTP() : authenticate()"
    >
      <img
        class="brand-logo auth-logo"
        src="/viya-fox-icon.png"
        alt="Viya"
      /><span class="eyebrow">{{ t("Viya 管理面板") }}</span>
      <h1>
        {{
          totpChallenge
            ? t("两步验证")
            : setupRequired
              ? t("创建管理员")
              : t("管理员登录")
        }}
      </h1>
      <p class="auth-hint">
        {{
          totpChallenge
            ? t("请输入验证器应用中当前显示的 6 位验证码。")
            : setupRequired
              ? t("首次使用请创建唯一管理员账户，密码至少 8 位。")
              : t("登录以管理代理站点和 Caddy 配置。")
        }}
      </p>
      <template v-if="!totpChallenge">
        <label
          >{{ t("用户名")
          }}<input
            v-model="authForm.username"
            autocomplete="username"
            minlength="3"
            maxlength="64"
            required /></label
        ><label
          >{{ t("密码")
          }}<input
            v-model="authForm.password"
            :autocomplete="setupRequired ? 'new-password' : 'current-password'"
            type="password"
            minlength="8"
            required /></label
        ><label v-if="setupRequired"
          >{{ t("确认密码")
          }}<input
            v-model="authForm.confirm"
            type="password"
            autocomplete="new-password"
            minlength="8"
            required
        /></label>
      </template>
      <template v-else>
        <label
          >{{ t("验证码")
          }}<input
            v-model="totpCode"
            class="totp-code"
            inputmode="numeric"
            autocomplete="one-time-code"
            pattern="[0-9]{6}"
            maxlength="6"
            minlength="6"
            required
            autofocus
        /></label>
        <button type="button" class="auth-back" @click="cancelTOTP">
          {{ t("返回上一步") }}
        </button>
      </template>
      <p v-if="authNotice" class="auth-notice" role="status">
        {{ authNotice }}
      </p>
      <p v-if="authError" class="error">{{ authError }}</p>
      <button class="primary auth-submit" :disabled="authBusy">
        {{
          authBusy
            ? t("请稍候…")
            : totpChallenge
              ? t("验证并登录")
              : setupRequired
                ? t("创建并登录")
                : t("登录")
        }}
      </button>
    </form>
  </div>
  <div v-else-if="authenticated" class="shell">
    <a class="skip-link" href="#main-content" @click.prevent="focusMain">{{
      t("跳到主要内容")
    }}</a>
    <aside class="sidebar">
      <div class="brand">
        <img
          class="brand-logo"
          src="/viya-fox-icon.png"
          alt=""
          aria-hidden="true"
        />
        <div>
          <strong>Viya</strong><small>{{ t("反向代理控制台") }}</small>
        </div>
      </div>
      <nav class="nav-menu" :aria-label="t('主导航')">
        <div v-for="group in groupedNavigation" :key="group.group" class="nav-group">
          <div class="nav-label">{{ group.group }}</div>
          <a
            v-for="item in group.items"
            :key="item.id"
            :href="'#' + item.id"
            class="nav"
            :class="{ active: activeSection === item.id }"
            :aria-current="activeSection === item.id ? 'page' : undefined"
            ><component :is="item.icon" :size="18" /><span>{{
              item.label
            }}</span
            ><span v-if="item.id === 'proxy-sites'" class="count">{{
              hosts.length
            }}</span></a
          >
        </div>
      </nav>
      <div class="side-bottom">
        <div class="panel-settings-wrap">
          <button
            class="floating-settings settings-launcher"
            :aria-expanded="showPanelSettings"
            aria-haspopup="dialog"
            aria-controls="panel-settings-dialog"
            :aria-label="t('面板设置')"
            :title="t('面板设置（Ctrl / ⌘ + ,）')"
            @click="openPanelSettings"
          >
            <Settings :size="19" /><span>{{ t("面板设置") }}</span
            ><kbd>,</kbd>
          </button>
        </div>
        <button
          class="icon-btn"
          :title="t('退出登录')"
          :aria-label="t('退出登录')"
          @click="logout"
        >
          <LogOut :size="16" />
        </button>
      </div>
    </aside>
    <PanelSettingsDrawer
      v-if="showPanelSettings"
      id="panel-settings-dialog"
      v-model:theme="themeChoice"
      v-model:compact="compactMode"
      v-model:interval="refreshInterval"
      v-model:auto-refresh="autoRefresh"
      v-model:pause-hidden="pauseHidden"
      v-model:charts="trafficCharts"
      v-model:preview-limit="previewLimit"
      v-model:landing="landingPage"
      v-model:port="panelListenPort"
      v-model:password="passwordForm"
      :busy="panelSettingsBusy"
      :loading="panelSettingsLoading"
      :error="panelSettingsError"
      :message="panelSettingsMessage"
      :access-address="panelAccessURL"
      :access-port="panelAccessPort"
      :username="panelUsername"
      :sessions="panelSessions"
      :csrf="csrfToken"
      :role="panelRole"
      :pages="
        navigationItems.map((item) => ({ id: item.id, label: item.label }))
      "
      @close="showPanelSettings = false"
      @save-port="savePanelPort"
      @change-password="changeAdminPassword"
      @change-username="changeUsername"
      @revoke-sessions="revokeOtherSessions"
      @copy-address="copyPanelAddress"
      @reset-preferences="resetPanelPreferences"
    />
    <main id="main-content" tabindex="-1" :aria-busy="refreshing">
      <div class="workspace-bar">
        <span
          >{{ t("Viya 控制台 /") }} {{ currentPage.group }} /
          {{ currentPage.label }}</span
        >
        <div class="workspace-bar-actions">
          <span class="engine-pill"
            ><i class="dot" :class="{ ok: online }" />{{
              online ? t("引擎正常") : t("引擎未就绪")
            }}</span
          >
          <button
            class="icon-btn refresh-btn"
            type="button"
            :disabled="refreshing"
            :aria-busy="refreshing"
            :title="refreshing ? t('刷新中…') : t('刷新数据')"
            :aria-label="t('刷新数据')"
            @click="load"
          >
            <RefreshCw :size="16" :class="{ spinning: refreshing }" />
          </button>
        </div>
      </div>
      <header class="page-header">
        <div class="page-header-title">
          <span class="eyebrow">{{ currentPage.group }}</span>
          <h1>{{ currentPage.label }}</h1>
          <p>{{ currentPage.description }}</p>
        </div>
        <details :key="activeSection" class="workspace-help">
          <summary :aria-label="t('如何使用')" :title="t('如何使用')">
            ?
          </summary>
          <div>
            <strong>{{ currentPage.label }}</strong>
            <ol>
              <li v-for="step in pageInstructions[activeSection]" :key="step">
                {{ step }}
              </li>
            </ol>
          </div>
        </details>
      </header>
      <div class="workspace-toolbar">
        <button
          v-if="currentPage.action"
          class="primary toolbar-add"
          type="button"
          @click="currentPage.run?.()"
        >
          <Plus :size="17" />{{ currentPage.action }}
        </button>
        <div class="page-actions">
          <div
            v-if="activeSection === 'proxy-sites'"
            class="web-total-stats"
            :aria-label="t('全部站点近 24 小时统计')"
            :title="
              t('全部站点近 24 小时 · 错误 ') + trafficTotals.errors + t(' 次')
            "
          >
            <span
              ><small>{{ t("全部站点 · 24h") }}</small
              ><b
                >{{ trafficTotals.requests.toLocaleString() }}
                <em>{{ t("次") }}</em></b
              ></span
            ><span
              ><small>{{ t("↑ 上行") }}</small
              ><b>{{ formatBytes(trafficTotals.upload) }}</b></span
            ><span
              ><small>{{ t("↓ 下行") }}</small
              ><b>{{ formatBytes(trafficTotals.download) }}</b></span
            >
          </div>
        </div>
      </div>
      <p v-if="authNotice" class="probe-ok">
        {{ authNotice }}
        <button class="icon-btn" @click="authNotice = ''">×</button>
      </p>

      <div v-if="searchablePage" class="list-toolbar">
        <button
          class="icon-btn list-search-toggle"
          type="button"
          :aria-expanded="listSearchOpen"
          aria-controls="list-search-input"
          :aria-label="t('搜索当前列表')"
          :title="t('搜索当前列表')"
          @click="toggleListSearch"
        >
          <Search :size="17" />
        </button>
        <input
          v-if="listSearchOpen"
          id="list-search-input"
          ref="listSearchInput"
          v-model="listSearch"
          class="list-search-input"
          type="search"
          :placeholder="t('输入域名、名称或地址，支持多个关键词')"
          :aria-label="t('搜索当前列表')"
          @keydown.esc="toggleListSearch"
        /><label v-if="statusFilterAvailable" class="list-status"
          ><span>{{ t("状态") }}</span
          ><select v-model="statusFilter">
            <option value="all">{{ t("全部状态") }}</option>
            <option value="enabled">
              {{ activeSection === "ddns" ? t("自动更新开启") : t("已启用") }}
            </option>
            <option value="disabled">
              {{ activeSection === "ddns" ? t("自动更新关闭") : t("已停用") }}
            </option>
          </select></label
        ><span class="list-result-count" role="status"
          >{{ filteredCount }} {{ t("项") }}</span
        ><button
          v-if="hasListFilter"
          class="secondary"
          @click="clearListFilters"
        >
          {{ t("清除筛选") }}
        </button>
      </div>
      <div v-if="activeSection === 'overview'" class="overview-telemetry">
        <SystemGauges
          :auto-refresh="autoRefresh && (!pauseHidden || !documentHidden)"
        />
        <TrafficTrend
          :points="trafficTrend"
          :granularity="trafficRange === '24h' ? 'hour' : 'day'"
        />
      </div>
      <AttentionPanel
        v-if="activeSection === 'overview'"
        :certificates="certExpiring"
        :sites="attentionSites"
      />
      <section v-if="activeSection === 'overview'" class="stats kpi-row">
        <article>
          <span>{{ t("代理站点") }}</span
          ><strong>{{ hosts.length }}</strong
          ><small>{{ t("已配置规则") }}</small>
        </article>
        <article>
          <span>{{ t("运行状态") }}</span
          ><strong class="status-text"
            ><i class="dot" :class="{ ok: online }"></i
            >{{ online ? t("正常") : t("未连接") }}</strong
          ><small>Caddy Admin API</small>
        </article>
        <article>
          <span>{{ t("最近应用") }}</span
          ><strong>{{
            revisions.find((r) => r.status === "applied")?.id ?? "—"
          }}</strong
          ><small>{{ t("配置版本") }}</small>
        </article>
      </section>
      <p v-if="readOnly" class="readonly-banner" role="status">
        {{ t("当前账号为只读，可以查看全部配置，但不能保存任何修改。") }}
      </p>
      <div v-if="error" class="error global-error" role="alert">
        <span>{{ error }}</span
        ><button
          v-if="applyRetryVisible"
          class="secondary"
          type="button"
          :disabled="saving"
          @click="retryApply"
        >
          {{ saving ? t("正在应用…") : t("重新应用到 Caddy") }}
        </button>
      </div>
      <section v-if="activeSection === 'overview'" class="traffic-summary">
        <div class="traffic-summary-heading">
          <div>
            <h2>{{ t("站点流量") }}</h2>
            <p>
              {{ t("近 24 小时按整点汇总，")
              }}{{
                autoRefresh
                  ? t("每 ") + refreshInterval + t(" 秒刷新")
                  : t("自动刷新已暂停")
              }}{{
                t(
                  "。上行/下行分别为客户端请求体/响应体字节数，不含协议开销；累计统计从本地保留日志接入开始，重启后保留。",
                )
              }}
            </p>
          </div>
          <span class="badge" :class="{ disabled: !!trafficError }">{{
            trafficError || (autoRefresh ? t("自动更新") : t("手动刷新"))
          }}</span>
        </div>
        <div class="traffic-metrics">
          <article>
            <span>{{ t("请求数") }}</span
            ><strong
              >{{ trafficTotals.requests.toLocaleString()
              }}<small>{{ t("次") }}</small></strong
            >
          </article>
          <article>
            <span>{{ t("上行流量") }}</span
            ><strong>{{ formatBytes(trafficTotals.upload) }}</strong>
          </article>
          <article>
            <span>{{ t("下行流量") }}</span
            ><strong>{{ formatBytes(trafficTotals.download) }}</strong>
          </article>
          <article>
            <span>{{ t("错误请求（4xx / 5xx）") }}</span
            ><strong
              >{{ trafficTotals.errors.toLocaleString()
              }}<small>{{ t("次") }}</small></strong
            >
          </article>
        </div>
        <TrafficRangeBar v-model:range="trafficRange" :ranking="trafficRanking" />
      </section>
      <section v-if="activeSection === 'proxy-sites'" class="panel site-panel">
        <div class="panel-head">
          <div class="site-list-heading">
            <h2>{{ t("站点列表") }}</h2>
            <button
              class="icon-btn"
              :aria-expanded="webSearchOpen"
              aria-controls="web-site-search"
              :aria-label="t('搜索 Web 服务')"
              :title="t('搜索 Web 服务')"
              @click="toggleWebSearch"
            >
              <Search :size="17" /></button
            ><input
              v-if="webSearchOpen"
              id="web-site-search"
              ref="webSearchInput"
              v-model="listSearch"
              type="search"
              :placeholder="t('搜索域名、上游或路径')"
              :aria-label="t('搜索 Web 站点')"
              @keydown.esc="
                webSearchOpen = false;
                listSearch = '';
              "
            />
          </div>
        </div>

        <div v-if="activeSection === 'proxy-sites'" class="listener-workspace">
          <nav
            class="site-exposure-filters"
            :aria-label="t('Web 端口监听选项卡')"
            role="tablist"
          >
            <button
              v-for="(listener, index) in webListeners"
              :key="listener.id"
              role="tab"
              :aria-selected="selectedListener === index"
              :class="{ selected: selectedListener === index }"
              @click="selectListener(index)"
            >
              {{ listener.protocol.toUpperCase() }} · {{ listener.port }}
              <b>{{
                hosts.filter((host) => host.listener_id === listener.id).length
              }}</b></button
            ><button @click="openListenerEditor(null)">
              <Plus :size="14" />{{ t("添加端口监听") }}
            </button>
          </nav>
          <div class="listener-selected">
            <span
              >{{ webListeners[selectedListener]?.protocol.toUpperCase() }}
              {{ t("监听端口") }}
              <strong>:{{ webListeners[selectedListener]?.port }}</strong></span
            ><button
              class="secondary"
              @click="openListenerEditor(selectedListener)"
            >
              {{ t("修改端口 / 检测") }}</button
            ><span
              role="status"
              :class="
                selectedPortState.available === false ? 'error' : 'probe-ok'
              "
              >{{ selectedPortState.message }}</span
            >
          </div>
        </div>
        <div class="site-list-tools">
          <span>{{ filteredCount }} {{ t("个匹配站点") }}</span
          ><label
            >{{ t("状态")
            }}<select v-model="statusFilter">
              <option value="all">{{ t("全部状态") }}</option>
              <option value="enabled">{{ t("已启用") }}</option>
              <option value="disabled">{{ t("已停用") }}</option>
            </select></label
          ><button
            v-if="hasListFilter"
            class="inline-action"
            @click="clearListFilters"
          >
            {{ t("清除筛选") }}
          </button>
        </div>
        <div v-if="visibleHosts.length" class="site-groups">
          <section
            v-for="group in siteGroups"
            :key="group.domain"
            class="site-group"
          >
            <div class="site-group-heading">
              <div>
                <strong>{{ group.domain }}</strong
                ><span>{{ group.hosts.length }} {{ t("个代理服务") }}</span>
              </div>
              <button
                class="secondary add-subdomain-button"
                @click="openCreate(group.domain)"
              >
                <Plus :size="14" />{{ t("添加子域名") }}
              </button>
            </div>
            <div class="site-group-sites">
              <div
                v-for="host in group.hosts"
                :key="host.id"
                role="group"
                :aria-label="host.domain"
                class="host-row"
              >
                <div
                  class="host-icon site-icon"
                  :style="siteIconStyle(host.icon_color)"
                >
                  <Globe2 :size="18" />
                </div>
                <div class="host-main site-main">
                  <div class="site-title">
                    <strong class="entity-name">{{ host.domain }}</strong
                    ><small
                      class="proto-tag"
                      :class="{ plain: host.tls_mode === 'none' }"
                      >{{ host.tls_mode === "none" ? "HTTP" : "HTTPS" }}</small
                    ><small v-if="host.name" class="site-alias">{{
                      host.name
                    }}</small>
                  </div>
                  <span class="site-upstream">{{ host.upstream }}</span>
                  <div class="site-badges">
                    <span
                      v-if="upstreamHealth[String(host.id)]"
                      class="upstream-state"
                      :class="
                        upstreamHealth[String(host.id)].ok ? 'ok' : 'down'
                      "
                      :title="upstreamHealth[String(host.id)].error || ''"
                      ><i class="dot"></i>{{ upstreamLabel(host.id) }}</span
                    >
                    <span
                      v-if="host.certificate_source"
                      class="cert-line"
                      :class="host.certificate_source.kind"
                      :title="
                        [
                          t(host.certificate_source.label),
                          host.certificate_source.domain,
                          host.certificate_source.not_after
                            ? t('到期 ') +
                              new Date(
                                host.certificate_source.not_after,
                              ).toLocaleDateString()
                            : '',
                        ]
                          .filter(Boolean)
                          .join(' · ')
                      "
                      ><ShieldCheck :size="14" />{{
                        t(host.certificate_source.label)
                      }}<small
                        v-if="host.certificate_source.kind === 'wildcard'"
                        >{{ host.certificate_source.domain }}</small
                      ></span
                    >
                    <div class="site-log-preview-wrap">
                      <button
                        class="site-log-trigger"
                        :aria-label="
                          t('预览 ') + host.domain + t(' 的访问日志')
                        "
                        :aria-describedby="'site-log-' + host.id"
                        :title="t('悬浮预览访问日志；点击查看完整记录')"
                        @mouseenter="previewSite(host, $event)"
                        @click="openSiteLogs(host)"
                      >
                        <ScrollText :size="15" />
                      </button>
                      <div
                        :id="'site-log-' + host.id"
                        role="tooltip"
                        class="site-log-tooltip"
                        :style="previewLayout"
                      >
                        <div class="site-log-heading">
                          <b>{{ host.domain }} {{ t("· 最近访问") }}</b
                          ><span
                            >{{ t("最近") }} {{ previewLimit }}
                            {{ t("条") }}</span
                          >
                        </div>
                        <AccessLogTable
                          :events="
                            (siteLogs[host.id]?.events || []).slice(
                              0,
                              previewLimit,
                            )
                          "
                          :loading="siteLogs[host.id]?.loading"
                          :error="siteLogs[host.id]?.error"
                          :limit="previewLimit"
                          compact
                        /><button
                          class="inline-action"
                          @click="openSiteLogs(host)"
                        >
                          {{ t("查看最近 50 条记录 →") }}
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="site-traffic">
                  <div>
                    <span>{{ t("近24小时") }}</span
                    ><strong
                      >{{ siteStats(host).recent.requests.toLocaleString() }}
                      {{ t("次") }}</strong
                    ><span
                      >↑ {{ formatBytes(siteStats(host).recent.upload) }} · ↓
                      {{ formatBytes(siteStats(host).recent.download) }}</span
                    >
                  </div>
                  <div
                    v-if="trafficCharts"
                    class="traffic-spark"
                    :aria-label="t('最近24小时流量趋势')"
                  >
                    <i
                      v-for="point in hourlyTraffic(host)"
                      :key="point.hour"
                      :style="{
                        height:
                          barHeight(host, point.upload + point.download) + 'px',
                      }"
                      :title="
                        new Date(point.hour * 1000).toLocaleString() +
                        ' · ' +
                        point.requests +
                        t(' 次 · ') +
                        formatBytes(point.upload + point.download)
                      "
                    ></i>
                  </div>
                  <small
                    >{{ t("累计") }}
                    {{ siteStats(host).total.requests.toLocaleString() }}
                    {{ t("次 ·") }}
                    {{
                      formatBytes(
                        siteStats(host).total.upload +
                          siteStats(host).total.download,
                      )
                    }}</small
                  >
                </div>
                <button
                  class="site-switch"
                  type="button"
                  role="switch"
                  :aria-checked="host.enabled"
                  :aria-label="
                    (host.enabled ? t('停用站点 ') : t('启用站点 ')) +
                    host.domain
                  "
                  :class="{ on: host.enabled }"
                  :disabled="!!hostToggling[host.id]"
                  :title="
                    host.enabled ? t('已启用，点击停用') : t('已停用，点击启用')
                  "
                  @click="toggleHost(host)"
                >
                  <span /></button
                ><a
                  class="icon-btn"
                  :href="siteURL(host)"
                  target="_blank"
                  rel="noopener noreferrer"
                  :title="
                    host.tls_mode === 'none'
                      ? t('通过 HTTP 打开 ') + host.domain
                      : t('通过 HTTPS 打开 ') + host.domain
                  "
                  ><ExternalLink :size="16" /></a
                ><button
                  class="icon-btn"
                  :aria-label="t('克隆站点')"
                  :title="t('克隆站点')"
                  @click="cloneHost(host)"
                >
                  <Copy :size="15" /></button
                ><button
                  class="icon-btn"
                  :aria-label="t('编辑')"
                  :title="t('编辑')"
                  @click="openEdit(host)"
                >
                  <Pencil :size="15" /></button
                ><button
                  class="icon-btn danger"
                  :aria-label="t('删除')"
                  :title="t('删除')"
                  @click="remove(host)"
                >
                  <Trash2 :size="16" />
                </button>
              </div>
            </div>
          </section>
        </div>
        <div v-else class="empty">
          <div class="empty-icon"><Globe2 :size="23" /></div>
          <strong>{{
            hasListFilter
              ? t("未找到匹配站点")
              : hosts.length
                ? t("此监听下没有站点")
                : t("还没有代理站点")
          }}</strong
          ><span>{{ t("点击添加站点，将流量转发到你的服务。") }}</span>
        </div>
      </section>
      <section
        v-if="activeSection === 'dns-providers'"
        id="dns-providers"
        class="panel provider-panel"
      >
        <div class="panel-head">
          <div>
            <h2>{{ t("DNS 凭据") }}</h2>
            <p>{{ t("密钥以 AES-256-GCM 加密后保存在本机数据库。") }}</p>
          </div>
        </div>
        <div v-if="visibleProviders.length" class="host-list">
          <div
            v-for="provider in visibleProviders"
            :key="provider.id"
            role="group"
            :aria-label="provider.name"
            class="host-row"
          >
            <div class="host-icon"><ShieldCheck :size="17" /></div>
            <div class="host-main">
              <strong>{{ provider.name }}</strong
              ><span
                >{{ provider.provider_type }}
                {{ t("· 凭据已保存，不会在列表中显示") }}</span
              >
            </div>
            <button
              class="icon-btn"
              :title="t('替换凭据')"
              @click="editProvider(provider)"
            >
              <Pencil :size="15" /></button
            ><button
              class="icon-btn danger"
              :title="t('删除凭据')"
              @click="removeProvider(provider)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="revision-empty">
          {{
            hasListFilter
              ? t("未找到匹配项目，可清除筛选后重试。")
              : t("尚未添加 DNS 服务商凭据")
          }}
        </div>
      </section>
      <section
        v-if="activeSection === 'certificates'"
        class="panel managed-cert-panel"
      >
        <div class="panel-head">
          <div>
            <h2>{{ t("申请与自动续期") }}</h2>
            <p>
              {{
                t("申请记录独立管理；相同域名的站点选择“自动 HTTPS”即可使用。")
              }}
            </p>
          </div>
        </div>
        <div v-if="visibleManagedCerts.length" class="host-list">
          <div
            v-for="cert in visibleManagedCerts"
            :key="cert.id"
            class="host-row cert-host-row"
          >
            <div class="host-icon"><ShieldCheck :size="18" /></div>
            <div class="host-main">
              <strong>{{ cert.name }} · {{ cert.domain }}</strong
              ><span
                >{{
                  cert.method === "internal"
                    ? t("内部 CA")
                    : cert.method.toUpperCase()
                }}
                · {{ cert.issuer || t("等待验证和签发") }} ·
                {{
                  cert.not_after
                    ? t("到期 ") + new Date(cert.not_after).toLocaleString()
                    : t("签发完成后显示有效期")
                }}</span
              >
              <div
                v-if="cert.status !== 'issued' && cert.status !== 'expired'"
                class="cert-progress"
              >
                <div class="cert-progress-heading">
                  <span>{{
                    cert.progress_message || t("等待 Caddy 状态")
                  }}</span
                  ><strong>{{ cert.progress || 0 }}%</strong>
                </div>
                <div
                  class="cert-progress-track"
                  role="progressbar"
                  :aria-valuenow="cert.progress || 0"
                  aria-valuemin="0"
                  aria-valuemax="100"
                >
                  <i
                    :class="{ retrying: cert.status === 'retrying' }"
                    :style="{ width: Math.max(4, cert.progress || 0) + '%' }"
                  />
                </div>
                <small v-if="cert.last_error">{{ cert.last_error }}</small>
              </div>
            </div>
            <span
              class="badge"
              :class="{ disabled: cert.status !== 'issued' }"
              >{{
                cert.status === "issued"
                  ? t("已签发 · 自动续期")
                  : cert.status === "expired"
                    ? t("已到期")
                    : cert.status === "validating"
                      ? t("域名验证中")
                      : cert.status === "obtaining"
                        ? t("正在申请")
                        : cert.status === "retrying"
                          ? t("自动重试中")
                          : cert.status === "config-error"
                            ? t("待重新应用")
                            : t("准备签发")
              }}</span
            ><button
              class="icon-btn"
              :title="t('编辑证书')"
              :aria-label="t('编辑证书')"
              @click="openManagedCert(cert)"
            >
              <Pencil :size="16" /></button
            ><button
              class="icon-btn"
              :title="t('克隆证书')"
              :aria-label="t('克隆证书')"
              @click="openManagedCert(cert, true)"
            >
              <Copy :size="16" /></button
            ><button
              class="icon-btn danger"
              :title="t('删除证书')"
              :aria-label="t('删除证书')"
              @click="removeManagedCert(cert)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="revision-empty">
          {{
            hasListFilter
              ? t("未找到匹配的托管证书。")
              : t("尚未申请证书，可从右上角申请证书。")
          }}
        </div>
        <p class="certificate-note">
          {{
            t(
              "申请期间实时显示 Caddy 的验证与签发进度；签发后自动读取有效期并显示续期状态。DNS 传播或验证失败时，Caddy 会在后台重试。",
            )
          }}
        </p>
      </section>
      <section
        v-if="activeSection === 'certificates'"
        class="panel automatic-certificates"
      >
        <div class="panel-head">
          <div>
            <h2>{{ t("站点自动证书") }}</h2>
            <p>
              {{
                t(
                  "未匹配泛域名证书的 HTTPS 站点由 Caddy 单独申请并续期，自动同步到此列表。",
                )
              }}
            </p>
          </div>
          <span class="badge"
            >{{ automaticCertificates.length }} {{ t("张") }}</span
          >
        </div>
        <div v-if="automaticCertificates.length" class="host-list">
          <div
            v-for="cert in automaticCertificates"
            :key="cert.domain"
            class="host-row"
          >
            <div class="host-icon"><ShieldCheck :size="18" /></div>
            <div class="host-main">
              <strong>{{ cert.domain }}</strong
              ><span
                >{{ t(cert.source.label) }} ·
                {{
                  cert.source.not_after
                    ? t("到期 ") +
                      new Date(cert.source.not_after).toLocaleString()
                    : cert.source.status === "disabled"
                      ? t("站点停用，启用后自动申请")
                      : t("等待引擎签发，查看申请进度或引擎日志")
                }}</span
              >
            </div>
            <span class="certificate-origin individual">{{
              cert.source.kind === "internal"
                ? t("内部证书")
                : t("单独自动申请")
            }}</span
            ><span
              class="badge"
              :class="{ disabled: cert.source.status !== 'issued' }"
              >{{
                cert.source.status === "issued"
                  ? t("已签发")
                  : cert.source.status === "expired"
                    ? t("已过期")
                    : cert.source.status === "disabled"
                      ? t("站点已停用")
                      : cert.source.status === "pending"
                        ? t("等待签发")
                        : t("申请处理中")
              }}</span
            ><a class="secondary" href="#proxy-sites">{{
              t("管理关联站点")
            }}</a
            ><button
              class="icon-btn danger"
              :title="t('删除证书和申请')"
              :aria-label="t('删除证书和申请')"
              :disabled="deleteBusy"
              @click="removeAutomaticCert(cert)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="revision-empty">
          {{
            t(
              "暂无独立申请的站点证书；匹配到的泛域名证书可在 Web 站点查看复用标记。",
            )
          }}
        </div>
      </section>
      <section
        v-if="activeSection === 'certificates'"
        id="certificates"
        class="panel provider-panel"
      >
        <div class="panel-head">
          <div>
            <h2>{{ t("自定义 TLS 证书") }}</h2>
            <p>
              {{
                t("PEM 证书和匹配的私钥保存在受限权限目录，可绑定到代理站点。")
              }}
            </p>
          </div>
          <button
            class="secondary"
            @click="
              certError = '';
              certForm.name = '';
              showCert = true;
            "
          >
            <Plus :size="15" />{{ t("上传证书") }}
          </button>
        </div>
        <div v-if="visibleCerts.length" class="host-list">
          <div v-for="cert in visibleCerts" :key="cert.id" class="host-row">
            <div class="host-icon"><ShieldCheck :size="17" /></div>
            <div class="host-main">
              <strong>{{ cert.name }}</strong
              ><span
                >{{ cert.dns_names.join(", ") }} {{ t("· 到期") }}
                {{ new Date(cert.not_after).toLocaleDateString() }}</span
              >
            </div>
            <button
              class="icon-btn danger"
              :title="t('删除证书')"
              @click="removeCert(cert)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="revision-empty">
          {{
            hasListFilter
              ? t("未找到匹配项目，可清除筛选后重试。")
              : t("尚未上传自定义证书")
          }}
        </div>
      </section>
      <section v-if="activeSection === 'layer4'" class="panel provider-panel">
        <div class="panel-head">
          <div>
            <h2>{{ t("TCP / UDP 端口转发") }}</h2>
            <p>
              {{ t("由 Caddy Layer4 直接监听主机端口，请确认防火墙已放行。") }}
            </p>
          </div>
        </div>
        <div v-if="visibleL4.length" class="host-list">
          <div
            v-for="rule in visibleL4"
            :key="rule.id"
            role="group"
            :aria-label="rule.name || String(rule.listen_port)"
            class="host-row l4-row"
          >
            <div class="host-icon l4-icon"><Server :size="18" /></div>
            <div class="host-main site-main">
              <div class="site-title">
                <strong class="entity-name">{{
                  rule.name || t("未命名转发")
                }}</strong
                ><small class="l4-proto">{{
                  rule.protocol.toUpperCase()
                }}</small>
              </div>
              <div class="l4-detail">
                <span class="l4-port"
                  ><small>{{ t("监听端口") }}</small
                  ><b>{{ rule.listen_port }}</b></span
                ><span class="l4-target"
                  ><small>{{ t("监听目标") }}</small
                  ><code>{{ rule.upstream_address }}</code></span
                ><span class="l4-state" :class="{ off: !rule.enabled }"
                  ><i />{{ rule.enabled ? t("已启用") : t("已停用") }}</span
                ><div class="site-log-preview-wrap">
                  <button
                    class="site-log-trigger"
                    :aria-label="t('预览转发日志')"
                    :title="t('悬浮预览转发日志')"
                    @mouseenter="previewL4(rule, $event)"
                    @focus="previewL4(rule, $event)"
                  >
                    <ScrollText :size="15" />
                  </button>
                  <div role="tooltip" class="site-log-tooltip" :style="l4PreviewLayout">
                    <div class="site-log-heading">
                      <b
                        >{{ rule.name || t("端口转发") }} ·
                        {{ rule.listen_port }}</b
                      ><span>{{ t("最近") }} 50 {{ t("条") }}</span>
                    </div>
                    <p v-if="l4Logs[rule.id]?.loading" class="site-log-empty">
                      {{ t("正在读取转发日志…") }}
                    </p>
                    <p
                      v-else-if="l4Logs[rule.id]?.error"
                      class="site-log-empty"
                    >
                      {{ l4Logs[rule.id]?.error }}
                    </p>
                    <p
                      v-else-if="!l4Logs[rule.id]?.events?.length"
                      class="site-log-empty"
                    >
                      {{ t("暂无转发日志；连接失败时会记录到这里。") }}
                    </p>
                    <ul v-else class="l4-log-list">
                      <li
                        v-for="(log, index) in l4Logs[rule.id].events"
                        :key="index"
                      >
                        <time>{{
                          new Date((log.ts || 0) * 1000).toLocaleString()
                        }}</time>
                        <b
                          class="log-level"
                          :class="String(log.level).toLowerCase()"
                          >{{ log.level }}</b
                        ><span
                          >{{ log.msg
                          }}<small v-if="log.error"> · {{ log.error }}</small></span
                        ><small v-if="log.remote"
                          >{{ t("来源") }} {{ log.remote }}</small
                        >
                      </li>
                    </ul>
                  </div>
                </div>
              </div>
            </div>
            <button
              class="site-switch"
              type="button"
              role="switch"
              :aria-checked="rule.enabled"
              :aria-label="
                t('启用转发') + ' ' + (rule.name || rule.listen_port)
              "
              :class="{ on: rule.enabled }"
              :disabled="!!l4Toggling[rule.id]"
              @click="toggleL4(rule)"
            >
              <span /></button
            ><button
              class="icon-btn"
              :aria-label="t('编辑')"
              :title="t('编辑')"
              @click="openL4(rule)"
            >
              <Pencil :size="15" /></button
            ><button
              class="icon-btn danger"
              :aria-label="t('删除')"
              :title="t('删除')"
              @click="removeL4(rule)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="revision-empty">
          {{
            hasListFilter
              ? t("未找到匹配项目，可清除筛选后重试。")
              : t("尚未配置四层端口转发")
          }}
        </div>
      </section>
      <section v-if="activeSection === 'ddns'" class="panel provider-panel">
        <div class="panel-head">
          <div>
            <h2>{{ t("动态 DNS") }}</h2>
            <p>
              {{
                t("每个任务独立按设定间隔探测公网 IPv4 或 IPv6 并更新记录。")
              }}
            </p>
          </div>
        </div>
        <div v-if="visibleDDNS.length" class="host-list">
          <div
            v-for="record in visibleDDNS"
            :key="record.id"
            role="group"
            :aria-label="record.name"
            class="host-row ddns-record-row"
          >
            <div class="host-icon ddns-record-icon"><Network :size="19" /></div>
            <div class="host-main">
              <strong
                >{{
                  record.record_name === "@"
                    ? record.zone
                    : `${record.record_name}.${record.zone}`
                }}
                · {{ record.record_type }}</strong
              ><span
                >{{ record.name }} ·
                {{
                  record.record_type === "CNAME"
                    ? t("别名 → ") + (record.cname_target || t("未设置目标"))
                    : record.ip_source === "interface"
                      ? t("网卡 · ") + record.interface_name
                      : t("公网 IP 查询")
                }}
                {{ t("· 每") }} {{ record.interval_minutes }}
                {{ t("分钟") }}</span
              ><span v-if="record.record_type !== 'CNAME'" class="ddns-address"
                ><b>{{ t("当前解析") }}</b
                ><code>{{ record.last_ip || t("尚未更新") }}</code></span
              ><small
                v-if="record.last_error"
                class="ddns-error"
                role="status"
                >{{ record.last_error }}</small
              ><small>{{ t("备注：") }}{{ record.comment || "viya" }}</small
              ><small class="ddns-last-run"
                >{{ t("上次执行：")
                }}{{ formatDDNSTime(record.last_run_at) }}</small
              >
            </div>
            <div class="switch-with-label">
              <span>{{ t("自动更新") }}</span
              ><button
                class="site-switch"
                type="button"
                role="switch"
                :aria-checked="record.enabled"
                :aria-label="t('自动更新') + ' ' + record.name"
                :class="{ on: record.enabled }"
                :disabled="!!ddnsToggling[record.id]"
                @click="toggleDDNS(record, !record.enabled)"
              >
                <span />
              </button>
            </div>
            <span
              v-if="ddnsFeedback[record.id]"
              class="ddns-feedback"
              role="status"
              >{{ ddnsFeedback[record.id] }}</span
            ><button
              class="secondary ddns-run-button"
              :disabled="ddnsRunning[record.id]"
              :aria-busy="ddnsRunning[record.id]"
              :title="t('立即手动更新')"
              @click="runDDNS(record)"
            >
              <RefreshCw
                :size="14"
                :class="{ spinning: ddnsRunning[record.id] }"
              />{{
                ddnsRunning[record.id] ? t("更新中…") : t("手动更新")
              }}</button
            ><button
              class="icon-btn"
              :aria-label="t('编辑')"
              :title="t('编辑')"
              @click="openDDNS(record)"
            >
              <Pencil :size="15" /></button
            ><button
              class="icon-btn danger"
              :aria-label="t('删除')"
              :title="t('删除')"
              @click="removeDDNS(record)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="revision-empty">
          {{
            hasListFilter
              ? t("未找到匹配项目，可清除筛选后重试。")
              : t("尚未配置 DDNS 任务")
          }}
        </div>
      </section>
      <ConfigTransfer
        v-if="activeSection === 'config-history'"
        :csrf="csrfToken"
        @restored="configurationRestored"
      />
      <section
        v-if="activeSection === 'config-history'"
        class="panel revisions"
      >
        <div class="panel-head">
          <div>
            <h2><History :size="16" />{{ t("配置历史") }}</h2>
            <p>
              {{ t("最近 10 条配置应用记录；业务配置可从上方保存或恢复。") }}
            </p>
          </div>
        </div>
        <div v-if="revisions.length" class="revision-list">
          <div
            v-for="revision in revisions.slice(0, 10)"
            :key="revision.id"
            class="revision-row"
          >
            <span class="revision-id">#{{ revision.id }}</span
            ><span class="revision-state" :class="revision.status">{{
              revision.status === "applied" ? t("已应用") : t("失败")
            }}</span
            ><span class="revision-error" v-if="revision.error">{{
              revision.error
            }}</span
            ><time>{{ revision.created_at }}</time
            ><button
              class="secondary revision-diff-btn"
              type="button"
              :disabled="revisionDiffBusy"
              @click="loadRevisionDiff(revision.id)"
            >
              {{ t("与当前对比") }}
            </button>
          </div>
        </div>
        <div v-else class="revision-empty">{{ t("尚无配置变更记录") }}</div>
        <p v-if="revisionDiffError" class="error" role="alert">
          {{ revisionDiffError }}
        </p>
        <div v-if="revisionDiff" class="revision-diff">
          <div class="revision-diff-head">
            <strong>{{
              tr`版本 #${revisionDiff.from} → #${revisionDiff.to}`
            }}</strong
            ><button
              type="button"
              class="secondary"
              @click="revisionDiff = null"
            >
              {{ t("关闭对比") }}
            </button>
          </div>
          <p class="drawer-field-help">
            {{
              t(
                "历史快照已脱敏（密码与凭据被替换），因此只能用于对比，不能直接回滚。需要恢复到旧配置请使用上方的配置恢复。",
              )
            }}
          </p>
          <p v-if="revisionDiff.truncated" class="drawer-field-help">
            {{ t("差异较多，每组仅显示前 200 项。") }}
          </p>
          <p
            v-if="
              !revisionDiff.added.length &&
              !revisionDiff.removed.length &&
              !revisionDiff.changed.length
            "
            class="revision-diff-empty"
          >
            {{ t("两个版本内容相同。") }}
          </p>
          <div v-if="revisionDiff.added.length" class="diff-group added">
            <h5>{{ t("新增") }} · {{ revisionDiff.added.length }}</h5>
            <ul>
              <li v-for="item in revisionDiff.added" :key="'a' + item.path">
                <code>{{ item.path }}</code
                ><span>{{ diffValue(item.to) }}</span>
              </li>
            </ul>
          </div>
          <div v-if="revisionDiff.removed.length" class="diff-group removed">
            <h5>{{ t("移除") }} · {{ revisionDiff.removed.length }}</h5>
            <ul>
              <li v-for="item in revisionDiff.removed" :key="'r' + item.path">
                <code>{{ item.path }}</code
                ><span>{{ diffValue(item.from) }}</span>
              </li>
            </ul>
          </div>
          <div v-if="revisionDiff.changed.length" class="diff-group changed">
            <h5>{{ t("变更") }} · {{ revisionDiff.changed.length }}</h5>
            <ul>
              <li v-for="item in revisionDiff.changed" :key="'c' + item.path">
                <code>{{ item.path }}</code
                ><span>{{ diffValue(item.from) }}</span
                ><span class="diff-arrow">→</span
                ><span>{{ diffValue(item.to) }}</span>
              </li>
            </ul>
          </div>
        </div>
      </section>
      <LogWorkspace
        v-if="activeSection === 'access-logs'"
        :csrf="csrfToken"
        :auto-refresh="autoRefresh && (!pauseHidden || !documentHidden)"
      />
      <section v-if="activeSection === 'ip-bans'" class="panel provider-panel">
        <div class="panel-head">
          <div>
            <h2>{{ t("IP 封禁") }}</h2>
            <p>
              {{
                t(
                  "手动封禁 IP 或 CIDR 网段，长期有效，需手动解除。自动防护仍会封禁重复触发 400/401/403 的来源。",
                )
              }}
            </p>
          </div>
        </div>
        <div v-if="visibleBans.length" class="host-list">
          <div
            v-for="ban in visibleBans"
            :key="ban.id"
            role="group"
            :aria-label="ban.ip"
            class="host-row"
          >
            <div class="host-icon"><ShieldCheck :size="17" /></div>
            <div class="host-main">
              <strong
                >{{ ban.ip === "foreign" ? t("禁止境外 IP") : ban.ip }} ·
                {{ ban.domain }}</strong
              ><span
                >{{ ban.source === "manual" ? t("手动") : t("自动") }} ·
                {{ ban.reason || t("未填写原因") }}
                {{
                  ban.expires_at
                    ? t("· 到期 ") +
                      new Date(ban.expires_at * 1000).toLocaleString()
                    : t("· 永久封禁")
                }}</span
              >
            </div>
            <button class="secondary" @click="openBan(ban)">
              <Pencil :size="14" />{{ t("编辑") }}</button
            ><button class="secondary" @click="removeBan(ban.id)">
              {{ t("解除封禁") }}
            </button>
          </div>
        </div>
        <div v-else class="revision-empty">
          {{
            hasListFilter
              ? t("未找到匹配项目，可清除筛选后重试。")
              : t("当前没有封禁记录")
          }}
        </div>
      </section>

      <section v-if="activeSection === 'overview'" class="overview-grid">
        <a
          v-for="item in overviewItems"
          :key="item.id"
          :href="'#' + item.id"
          class="overview-card"
          ><div class="overview-card-top">
            <span class="host-icon"
              ><component :is="item.icon" :size="21" /></span
            ><ExternalLink :size="15" />
          </div>
          <strong>{{ item.count }}</strong>
          <h2>{{ item.label }}</h2>
          <p>{{ item.detail }}</p>
          <span class="overview-link">{{ t("进入管理 →") }}</span></a
        >
      </section>
      <section
        v-if="activeSection === 'overview'"
        class="panel overview-status"
      >
        <div class="panel-head">
          <div>
            <h2>{{ t("最近配置状态") }}</h2>
            <p>{{ t("配置应用结果可在配置历史中查看。") }}</p>
          </div>
          <a href="#config-history" class="secondary">{{
            t("查看配置历史")
          }}</a>
        </div>
        <div class="overview-health">
          <span class="dot" :class="{ ok: online }" /><span>{{
            online
              ? t("Caddy 已连接，服务正在运行")
              : t("Caddy 尚未就绪，请检查配置历史和服务状态")
          }}</span
          ><span
            v-if="revisions[0]"
            class="badge"
            :class="{ disabled: revisions[0].status !== 'applied' }"
            >{{ t("最近提交 #") }}{{ revisions[0].id }} ·
            {{
              revisions[0].status === "applied" ? t("已应用") : t("失败")
            }}</span
          >
        </div>
      </section>
      <footer>Viya <span>·</span> {{ t("本地部署的 Caddy 管理工具") }}</footer>
    </main>
    <div v-if="showListenerEditor" class="overlay" v-dialog>
      <form class="modal" @submit.prevent="saveListener">
        <div class="modal-top">
          <h2>
            {{
              listenerEditingIndex === null
                ? t("添加端口监听")
                : t("修改端口监听")
            }}
          </h2>
          <button
            type="button"
            class="icon-btn"
            :disabled="listenerSaving"
            :aria-label="t('关闭监听设置')"
            @click="showListenerEditor = false"
          >
            ×
          </button>
        </div>
        <fieldset
          v-if="listenerEditingIndex === null"
          class="listener-create-options"
        >
          <legend>{{ t("配置来源") }}</legend>
          <label
            ><input
              v-model="listenerCreationMode"
              type="radio"
              value="empty"
            />{{ t("完全新建") }}
            <small>{{ t("空站点列表，不继承任何监听配置") }}</small></label
          ><label
            ><input
              v-model="listenerCreationMode"
              type="radio"
              value="copy"
            />{{ t("从已有监听完全导入") }}
            <small>{{ t("复制站点及全部规则，后续独立编辑") }}</small></label
          ><label v-if="listenerCreationMode === 'copy'"
            >{{ t("导入来源")
            }}<select
              v-model="listenerCopyFrom"
              required
              @change="
                listenerDraft.protocol =
                  webListeners.find((item) => item.id === listenerCopyFrom)
                    ?.protocol || 'https';
                if (listenerDraft.port) checkListenerPort();
              "
            >
              <option value="">{{ t("请选择已有监听") }}</option>
              <option
                v-for="listener in webListeners"
                :key="listener.id"
                :value="listener.id"
              >
                {{ listener.protocol.toUpperCase() }} :{{ listener.port }} ·
                {{
                  hosts.filter((host) => host.listener_id === listener.id)
                    .length
                }}
                个站点
              </option>
            </select></label
          >
        </fieldset>
        <label
          >{{ t("协议")
          }}<select
            v-model="listenerDraft.protocol"
            :disabled="listenerEditingIndex !== null || listenerSaving"
            @change="checkListenerPort"
          >
            <option value="https">HTTPS</option>
            <option value="http">HTTP</option>
          </select></label
        ><label
          >{{ t("监听端口")
          }}<input
            :value="listenerDraft.port || ''"
            @input="
              listenerDraft.port = Number(
                ($event.target as HTMLInputElement).value,
              );
              checkListenerPort();
            "
            :disabled="listenerSaving"
            type="number"
            min="1"
            max="65535"
            required
        /></label>
        <p
          role="status"
          :class="listenerAvailable === false ? 'error' : 'probe-ok'"
        >
          {{ listenerChecking ? t("正在检测端口…") : listenerMessage }}
        </p>
        <button
          type="button"
          class="secondary"
          :disabled="listenerChecking"
          @click="checkListenerPort"
        >
          {{ t("重新检测") }}
        </button>
        <p class="auth-hint">
          {{
            t(
              "端口检测针对服务器本地绑定，不代表公网防火墙已放行。HTTP-01 验证仍要求公网 80 端口可达。",
            )
          }}
        </p>
        <div class="modal-actions">
          <button
            v-if="
              listenerEditingIndex !== null &&
              webListeners.filter(
                (item) => item.protocol === listenerDraft.protocol,
              ).length > 1
            "
            type="button"
            class="danger-button"
            :disabled="listenerSaving"
            @click="deleteListener"
          >
            {{ t("删除监听") }}</button
          ><button
            type="button"
            class="secondary"
            :disabled="listenerSaving"
            @click="showListenerEditor = false"
          >
            {{ t("取消") }}</button
          ><button
            class="primary"
            :disabled="
              listenerChecking || listenerAvailable !== true || listenerSaving
            "
          >
            {{ listenerSaving ? t("保存中…") : t("保存监听") }}
          </button>
        </div>
      </form>
    </div>
    <!-- Form overlays close only through their explicit close, cancel, or save controls. Keep this behavior for future edit/create forms. -->
    <div v-if="showBan" class="overlay" v-dialog>
      <form class="modal" @submit.prevent="saveBan">
        <div class="modal-top">
          <div>
            <span class="eyebrow">{{ t("IP 访问控制") }}</span>
            <h2>{{ editingBanID ? t("编辑封禁") : t("添加封禁") }}</h2>
          </div>
          <button
            type="button"
            class="icon-btn"
            :aria-label="t('关闭封禁表单')"
            @click="showBan = false"
          >
            ×
          </button>
        </div>
        <label
          >{{ t("站点域名")
          }}<select v-model="banForm.domain" required>
            <option value="" disabled>{{ t("选择站点") }}</option>
            <option
              v-for="domain in [...new Set(hosts.map((host) => host.domain))]"
              :key="domain"
              :value="domain"
            >
              {{ domain }}
            </option>
          </select></label
        ><label
          >{{ t("封禁类型")
          }}<select v-model="banKind">
            <option value="ip">{{ t("指定 IP / 网段") }}</option>
            <option value="foreign">
              {{ t("禁止境外 IP（仅允许中国大陆及内网）") }}
            </option>
          </select></label
        ><label v-if="banKind === 'ip'"
          >{{ t("IP 或网段")
          }}<input
            v-model="banForm.ip"
            required
            :placeholder="t('192.0.2.10 或 192.0.2.0/24')"
        /></label>
        <p v-else class="auth-hint">
          {{
            t(
              "同时覆盖 IPv4 / IPv6，保留内网及回环访问。依据 APNIC 的 CN 分配数据近似判断归属地，不含港澳台。按直接连接 IP 判断；位于 CDN / 代理后时会判断代理地址，请先确认访问链路。此规则不替代身份验证。",
            )
          }}
        </p>
        <label
          >{{ t("封禁原因")
          }}<textarea
            v-model="banForm.reason"
            rows="2"
            maxlength="500"
          /></label>
        <p class="auth-hint">
          {{
            t(
              "默认为永久封禁，需手动解除。保存后立即应用；手动规则不会被自动防护覆盖。",
            )
          }}
        </p>
        <p v-if="banError" class="error">{{ banError }}</p>
        <button
          v-if="applyRetryVisible"
          type="button"
          class="secondary"
          @click="retryApply"
        >
          {{ t("重新应用到 Caddy") }}
        </button>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="showBan = false">
            {{ t("取消") }}</button
          ><button class="primary" :disabled="saving">
            {{ saving ? t("应用中…") : t("保存并应用") }}
          </button>
        </div>
      </form>
    </div>
    <div v-if="showManagedCert" class="overlay" v-dialog>
      <form class="modal" @submit.prevent="requestCertificate">
        <div class="modal-top">
          <div>
            <span class="eyebrow">{{ t("SSL 证书") }}</span>
            <h2>{{ editingManagedID ? t("编辑证书") : t("申请证书") }}</h2>
          </div>
          <button
            type="button"
            class="icon-btn"
            :aria-label="t('关闭证书申请')"
            @click="showManagedCert = false"
          >
            ×
          </button>
        </div>
        <label
          >{{ t("申请名称")
          }}<input
            v-model="managedCertForm.name"
            maxlength="100"
            required
            :placeholder="t('我的站点证书')" /></label
        ><label
          >{{ t("证书域名")
          }}<input
            v-model="managedCertForm.domain"
            :readonly="!!editingManagedID"
            required
            :placeholder="t('app.example.com 或 *.example.com')"
            autocapitalize="none" /></label
        ><label
          >{{ t("验证方式")
          }}<select v-model="managedCertForm.method">
            <option value="dns-01">{{ t("DNS-01（支持泛域名）") }}</option>
            <option value="http-01">HTTP-01</option>
            <option value="internal">{{ t("内部 CA（内网使用）") }}</option>
          </select></label
        >
        <p class="inline-help">
          {{
            managedCertForm.method === "dns-01"
              ? t(
                  "通过 DNS API 添加验证记录，不需要开放验证端口；凭据需要对应区域的写入权限。",
                )
              : managedCertForm.method === "http-01"
                ? t("域名须解析至此服务器，公网 80 端口须可达；不支持泛域名。")
                : t(
                    "使用本机 CA 签发。客户端需自行信任 Caddy 根证书，不能作为公网受信任证书。",
                  )
          }}
        </p>
        <label v-if="managedCertForm.method !== 'internal'"
          >{{ t("联系邮箱（可选）")
          }}<input
            v-model="managedCertForm.email"
            type="email"
            placeholder="admin@example.com" /></label
        ><label v-if="managedCertForm.method === 'dns-01'"
          >{{ t("DNS 凭据")
          }}<select v-model="managedCertForm.dns_provider_id" required>
            <option value="" disabled>{{ t("选择服务商凭据") }}</option>
            <option
              v-for="provider in providers"
              :key="provider.id"
              :value="String(provider.id)"
            >
              {{ provider.name }} · {{ provider.provider_type }}
            </option>
          </select></label
        >
        <div
          v-if="managedCertForm.method === 'dns-01' && !providers.length"
          class="dependency-help"
        >
          <span>{{ t("请先添加 DNS 凭据。") }}</span
          ><button type="button" class="inline-action" @click="openProvider">
            {{ t("添加 DNS 凭据") }}
          </button>
        </div>
        <p class="auth-hint">
          {{
            t(
              "提交后由 Caddy 在后台申请并自动续期；同域名站点选择“自动 HTTPS”即可使用。已有其他 TLS 配置时请先修改站点。",
            )
          }}
        </p>
        <p v-if="managedCertError" class="error">{{ managedCertError }}</p>
        <div class="modal-actions">
          <button
            type="button"
            class="secondary"
            @click="showManagedCert = false"
          >
            {{ t("取消") }}</button
          ><button class="primary" :disabled="managedCertBusy">
            {{
              managedCertBusy
                ? t("提交中…")
                : editingManagedID
                  ? t("保存修改")
                  : t("提交申请")
            }}
          </button>
        </div>
      </form>
    </div>
    <div
      v-if="viewingLogSite"
      class="overlay"
      v-dialog
      @click.self="viewingLogSite = null"
      @keydown.esc="viewingLogSite = null"
    >
      <section
        class="modal site-log-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="t('站点访问日志')"
      >
        <div class="modal-top">
          <div>
            <span class="eyebrow">{{ t("站点访问日志") }}</span>
            <h2>{{ viewingLogSite.domain }}</h2>
            <p class="form-intro">{{ viewingLogSite.upstream }}</p>
          </div>
          <button
            class="icon-btn"
            :aria-label="t('关闭访问日志')"
            @click="viewingLogSite = null"
          >
            ×
          </button>
        </div>
        <div class="site-log-toolbar">
          <span
            >{{ t("最近 50 条 ·") }}
            {{
              autoRefresh
                ? t("每 ") + refreshInterval + t(" 秒刷新")
                : t("自动刷新已暂停")
            }}
            {{ t("· 查询参数和凭据已隐藏") }}</span
          ><button
            class="secondary"
            @click="loadSiteLogs(viewingLogSite, true)"
          >
            <RefreshCw :size="14" />{{ t("刷新") }}
          </button>
        </div>
        <AccessLogTable
          :events="siteLogs[viewingLogSite.id]?.events || []"
          :loading="siteLogs[viewingLogSite.id]?.loading"
          :error="siteLogs[viewingLogSite.id]?.error"
        />
        <div class="modal-actions">
          <button class="secondary" @click="viewingLogSite = null">
            {{ t("关闭") }}
          </button>
        </div>
      </section>
    </div>
    <div v-if="deletePrompt" class="overlay" v-dialog>
      <section
        class="modal confirm-delete-modal"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="delete-title"
      >
        <div class="modal-top">
          <h2 id="delete-title">{{ t("确认删除") }}</h2>
          <button
            type="button"
            class="icon-btn"
            :aria-label="t('关闭删除确认')"
            @click="resolveDelete(false)"
          >
            <X :size="17" />
          </button>
        </div>
        <p>{{ deletePrompt }}</p>
        <p class="auth-hint">{{ t("确认后立即执行，请核对要操作的项目。") }}</p>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="resolveDelete(false)">
            {{ t("取消") }}</button
          ><button
            type="button"
            class="danger-button"
            @click="resolveDelete(true)"
          >
            {{ t("确认") }}
          </button>
        </div>
      </section>
    </div>
    <div v-if="show" class="overlay" v-dialog>
      <form class="modal proxy-modal" @submit.prevent="save">
        <div class="modal-top">
          <div>
            <span class="eyebrow">{{
              editingID === null ? t("新建反代") : t("修改反代")
            }}</span>
            <h2>
              {{ editingID === null ? t("添加代理站点") : t("编辑代理站点") }}
            </h2>
          </div>
          <button
            type="button"
            class="icon-btn"
            :aria-label="t('关闭')"
            @click="show = false"
          >
            ×
          </button>
        </div>
        <p class="form-intro">
          {{
            t(
              "填写域名与上游地址，再选择证书方式。访问路径选填；IP 规则和限速位于高级设置。",
            )
          }}
        </p>
        <div v-if="editingID === null" class="domain-entry-mode">
          <button
            type="button"
            :class="{ selected: domainMode === 'subdomain' }"
            :disabled="!siteRootDomains.length"
            @click="setDomainMode('subdomain')"
          >
            {{ t("添加到已有主域名") }}</button
          ><button
            type="button"
            :class="{ selected: domainMode === 'full' }"
            @click="setDomainMode('full')"
          >
            {{ t("填写完整域名") }}
          </button>
        </div>
        <label
          >{{ t("站点名称（选填）")
          }}<input
            v-model="form.name"
            maxlength="100"
            :placeholder="t('为站点起一个便于识别的名称')"
          /><small>{{ t("仅用于面板中识别，不影响访问域名。") }}</small></label
        >
        <template v-if="editingID === null && domainMode === 'subdomain'"
          ><label
            >{{ t("主域名")
            }}<select v-model="domainParent" required>
              <option value="" disabled>{{ t("选择已有主域名") }}</option>
              <option
                v-for="domain in siteRootDomains"
                :key="domain"
                :value="domain"
              >
                {{ domain }}
              </option>
            </select></label
          ><label
            >{{ t("子域名前缀")
            }}<input
              v-model="domainPrefix"
              placeholder="qb"
              autocomplete="off"
              autocapitalize="none"
              spellcheck="false"
              required
            /><small
              >{{ t("只需填写前缀，例如 qb；最终访问域名为") }}
              <code>{{
                composedDomain || "qb." + (domainParent || t("主域名"))
              }}</code
              >。</small
            ></label
          ></template
        >
        <label v-else
          >{{ t("前端域名")
          }}<input
            v-model="form.domain"
            placeholder="app.example.com"
            autocomplete="off"
            autocapitalize="none"
            spellcheck="false"
            required
          /><small>{{
            t("填写完整域名，不包含 http:// 或路径。")
          }}</small></label
        >
        <label
          >{{ t("上游地址")
          }}<input
            v-model="form.upstream"
            type="url"
            placeholder="http://192.168.1.20:8080"
            autocomplete="url"
            required
          /><small>{{
            t(
              "填写 http(s):// 地址。端口可省略，HTTP 默认 80，HTTPS 默认 443。",
            )
          }}</small></label
        >
        <label
          >{{ t("访问路径（选填）")
          }}<input
            v-model="form.path"
            :placeholder="t('留空匹配整个域名，例如 /api')"
            autocapitalize="none"
            spellcheck="false"
          /><small>{{
            t("留空时整个域名都转发；填写路径后仅匹配该路径和它下面的子路径。")
          }}</small></label
        >
        <div class="site-icon-setting">
          <span>{{ t("站点图标颜色") }}</span>
          <div
            class="site-icon-picker"
            role="group"
            :aria-label="t('选择站点图标颜色')"
          >
            <button
              v-for="color in siteIconColors"
              :key="color.key"
              type="button"
              :aria-label="color.label"
              :aria-pressed="form.icon_color === color.key"
              :title="color.label"
              :class="{ selected: form.icon_color === color.key }"
              :style="siteIconStyle(color.key)"
              @click="form.icon_color = color.key"
            >
              <Globe2 :size="16" />
            </button>
          </div>
          <small>{{ t("默认使用海蓝色图标。") }}</small>
        </div>
        <div class="probe-row">
          <button
            type="button"
            class="secondary"
            :disabled="upstreamTesting || !form.upstream"
            @click="testUpstream"
          >
            {{ upstreamTesting ? t("正在测试…") : t("测试上游连通性") }}</button
          ><span
            v-if="upstreamCheck"
            :class="upstreamCheck.ok ? 'probe-ok' : 'probe-error'"
            >{{ upstreamCheck.message }}</span
          >
        </div>
        <label
          >TLS / HTTPS<select v-model="form.tls_mode">
            <option value="auto">{{ t("自动 HTTPS（推荐）") }}</option>
            <option value="http-01">ACME HTTP-01</option>
            <option value="dns-01">{{ t("DNS-01（支持泛域名）") }}</option>
            <option value="internal">{{ t("Caddy 内部 CA") }}</option>
            <option value="custom">{{ t("自定义证书") }}</option>
            <option value="none">{{ t("仅 HTTP") }}</option>
          </select></label
        >
        <p class="inline-help" v-if="form.tls_mode === 'auto'">
          {{
            t(
              "Caddy 自动申请和续期证书。公网域名需解析到此服务器，并确保 HTTP/HTTPS 验证端口可达。",
            )
          }}
        </p>
        <p class="inline-help" v-else-if="form.tls_mode === 'http-01'">
          {{
            t("HTTP-01 需要公网域名和可从互联网访问的 80 端口；不支持泛域名。")
          }}
        </p>
        <p class="inline-help" v-else-if="form.tls_mode === 'dns-01'">
          {{
            t(
              "DNS-01 可签发泛域名证书。先选择已保存的 DNS 凭据，首次使用可在这里添加。",
            )
          }}
        </p>
        <p class="inline-help" v-else-if="form.tls_mode === 'internal'">
          {{ t("使用 Caddy 本地 CA。客户端需手动信任该 CA 根证书。") }}
        </p>
        <p class="inline-help" v-else-if="form.tls_mode === 'custom'">
          {{ t("使用已上传且 SAN 覆盖此域名的证书。") }}
        </p>
        <p class="inline-help" v-else>
          {{ t("此站点只通过 HTTP 提供服务，不会申请 TLS 证书。") }}
        </p>
        <label v-if="form.tls_mode === 'dns-01'"
          >{{ t("DNS 服务商")
          }}<select v-model="form.dns_provider_id" required>
            <option value="" disabled>{{ t("选择已保存的服务商凭据") }}</option>
            <option
              v-for="provider in providers"
              :key="provider.id"
              :value="String(provider.id)"
            >
              {{ provider.name }} · {{ provider.provider_type }}
            </option>
          </select></label
        >
        <div
          v-if="form.tls_mode === 'dns-01' && providers.length === 0"
          class="dependency-help"
        >
          <span>{{ t("还没有 DNS 凭据。添加后会保留当前站点表单。") }}</span
          ><button type="button" class="inline-action" @click="openProvider">
            {{ t("添加 DNS 凭据") }}
          </button>
        </div>
        <label v-if="form.tls_mode === 'custom'"
          >{{ t("自定义证书")
          }}<select v-model="form.custom_certificate_id" required>
            <option value="" disabled>{{ t("选择覆盖此域名的证书") }}</option>
            <option
              v-for="cert in certificates"
              :key="cert.id"
              :value="String(cert.id)"
            >
              {{ cert.name }} · {{ cert.dns_names.join(", ") }}
            </option>
          </select></label
        >
        <div
          v-if="form.tls_mode === 'custom' && certificates.length === 0"
          class="dependency-help"
        >
          <span>{{ t("还没有上传证书。上传后会保留当前站点表单。") }}</span
          ><button
            type="button"
            class="inline-action"
            @click="
              certError = '';
              certForm.name = '';
              showCert = true;
            "
          >
            {{ t("上传自定义证书") }}
          </button>
        </div>
        <label class="enabled-label"
          ><input v-model="form.enabled" type="checkbox" />{{
            t("启用此站点")
          }}</label
        >
        <section class="security-section">
          <h3>{{ t("访问安全") }}</h3>
          <label class="enabled-label"
            ><input v-model="form.basic_auth_enabled" type="checkbox" />{{
              t("启用 Basic Auth")
            }}</label
          ><template v-if="form.basic_auth_enabled"
            ><label
              >{{ t("Basic Auth 用户名")
              }}<input
                v-model="form.basic_auth_user"
                autocomplete="off"
                required /></label
            ><label
              >{{
                editingID === null
                  ? t("Basic Auth 密码（至少 8 位）")
                  : t("替换密码（留空保留原密码）")
              }}<input
                v-model="form.basic_auth_password"
                type="password"
                autocomplete="new-password"
                :required="editingID === null"
                minlength="8" /></label
          ></template>
        </section>
        <button
          type="button"
          class="advanced-toggle"
          :aria-expanded="advancedOpen"
          @click="advancedOpen = !advancedOpen"
        >
          {{ advancedOpen ? t("收起高级设置") : t("高级设置")
          }}<span>{{
            advancedOpen
              ? t("隐藏请求头、IP 过滤和限速")
              : t("请求头、IP 白名单/黑名单、限速")
          }}</span>
        </button>
        <div v-if="advancedOpen" class="advanced-fields">
          <label
            >{{ t("上游请求头")
            }}<textarea
              v-model="headerText"
              rows="3"
              placeholder="X-Environment: production"
            /><small>{{ t("每行一条，格式为“名称: 值”。") }}</small></label
          ><label
            >{{ t("IP 白名单（每行一个 IP 或 CIDR）")
            }}<textarea
              v-model="allowlistText"
              rows="2"
              placeholder="192.168.1.0/24"
            /></label
          ><label
            >{{ t("IP 黑名单（每行一个 IP 或 CIDR）")
            }}<textarea
              v-model="denylistText"
              rows="2"
              placeholder="203.0.113.10"
            />
          </label>
          <div class="rate-limit-fields">
            <label
              >{{ t("速率限制请求数")
              }}<input
                v-model.number="form.rate_limit_requests"
                type="number"
                min="0"
                max="1000000"
              /><small>{{ t("设为 0 表示关闭。") }}</small></label
            ><label
              >{{ t("时间窗口")
              }}<select v-model="form.rate_limit_window">
                <option value="1s">{{ t("1 秒") }}</option>
                <option value="10s">{{ t("10 秒") }}</option>
                <option value="1m">{{ t("1 分钟") }}</option>
                <option value="1h">{{ t("1 小时") }}</option>
              </select></label
            >
          </div>
        </div>
        <p v-if="error" class="error">{{ error }}</p>
        <button
          v-if="applyRetryVisible"
          type="button"
          class="secondary retry-button"
          :disabled="saving"
          @click="retryApply"
        >
          {{ saving ? t("正在应用…") : t("重新应用到 Caddy") }}
        </button>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="show = false">
            {{ t("取消") }}</button
          ><button
            class="primary"
            :disabled="saving || (partialSave && editingID === null)"
          >
            {{
              saving
                ? t("应用中…")
                : partialSave && editingID !== null
                  ? t("修正后重新应用")
                  : t("保存并应用")
            }}
          </button>
        </div>
      </form>
    </div>
    <div v-if="showProvider" class="overlay overlay-top">
      <form class="modal" @submit.prevent="saveProvider">
        <div class="modal-top">
          <div>
            <span class="eyebrow">{{ t("DNS 提供商") }}</span>
            <h2>
              {{
                editingProviderID === null
                  ? t("添加 DNS 凭据")
                  : t("替换 DNS 凭据")
              }}
            </h2>
          </div>
          <button
            type="button"
            class="icon-btn"
            :aria-label="t('关闭')"
            @click="showProvider = false"
          >
            ×
          </button>
        </div>
        <label
          >{{ t("名称")
          }}<input
            v-model="providerForm.name"
            :placeholder="t('我的 Cloudflare')"
            maxlength="100"
            required /></label
        ><label
          >{{ t("服务商")
          }}<select
            v-model="providerForm.provider_type"
            @change="providerForm.credentials = {}"
          >
            <option value="cloudflare">Cloudflare</option>
            <option value="tencentcloud">{{ t("腾讯云 DNSPod") }}</option>
            <option value="alidns">{{ t("阿里云 DNS") }}</option>
          </select></label
        ><label
          v-for="field in providerFields[providerForm.provider_type]"
          :key="field"
          >{{ field
          }}<input
            v-model="providerForm.credentials[field]"
            type="password"
            autocomplete="new-password"
            required
        /></label>
        <p class="auth-hint">
          {{
            t(
              "使用仅具备所需 DNS 区域写入权限的 API 凭据。密钥不会再次回显。替换凭据时请重新输入全部必要字段。",
            )
          }}
        </p>
        <p v-if="providerError" class="error">{{ providerError }}</p>
        <button
          v-if="applyRetryVisible"
          type="button"
          class="secondary retry-button"
          :disabled="saving"
          @click="retryApply"
        >
          {{ t("重新应用到 Caddy") }}
        </button>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="showProvider = false">
            {{ t("取消") }}</button
          ><button class="primary" :disabled="saving">
            {{ saving ? t("加密保存中…") : t("加密并保存") }}
          </button>
        </div>
      </form>
    </div>
    <div v-if="showL4" class="overlay" v-dialog>
      <form class="modal" @submit.prevent="saveL4">
        <div class="modal-top">
          <div>
            <span class="eyebrow">Layer4</span>
            <h2>
              {{ l4EditingID === null ? t("添加端口转发") : t("编辑端口转发") }}
            </h2>
          </div>
          <button
            type="button"
            class="icon-btn"
            :aria-label="t('关闭')"
            @click="showL4 = false"
          >
            ×
          </button>
        </div>
        <label
          >{{ t("名称")
          }}<input
            v-model="l4Form.name"
            maxlength="100"
            :placeholder="t('SSH 转发')" /></label
        ><label
          >{{ t("监听 IP")
          }}<input
            v-model="l4Form.listen_address"
            required
            :placeholder="t('0.0.0.0 或 ::')" /></label
        ><label
          >{{ t("监听端口")
          }}<input
            v-model.number="l4Form.listen_port"
            type="number"
            min="1"
            max="65535"
            required /></label
        ><label
          >{{ t("上游 host:port")
          }}<input
            v-model="l4Form.upstream_address"
            required
            placeholder="192.168.1.20:22" /></label
        ><label
          >{{ t("协议")
          }}<select v-model="l4Form.protocol">
            <option value="tcp">TCP</option>
            <option value="udp">UDP</option>
            <option value="both">TCP + UDP</option>
          </select></label
        ><label class="enabled-label"
          ><input v-model="l4Form.enabled" type="checkbox" />{{
            t("启用转发")
          }}</label
        >
        <p class="auth-hint">
          {{
            t(
              "监听端口会直接绑定 Debian 主机。低于 1024 的端口需相应系统权限；请勿占用管理接口或 HTTP/HTTPS 端口。",
            )
          }}
        </p>
        <p v-if="l4Error" class="error">{{ l4Error }}</p>
        <button
          v-if="applyRetryVisible"
          type="button"
          class="secondary retry-button"
          :disabled="saving"
          @click="retryApply"
        >
          {{ t("重新应用到 Caddy") }}
        </button>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="showL4 = false">
            {{ t("取消") }}</button
          ><button class="primary" :disabled="saving">
            {{ saving ? t("应用中…") : t("保存并应用") }}
          </button>
        </div>
      </form>
    </div>
    <div v-if="showDDNS" class="overlay" v-dialog>
      <form class="modal" @submit.prevent="saveDDNS">
        <div class="modal-top">
          <div>
            <span class="eyebrow">DDNS</span>
            <h2>
              {{
                ddnsEditingID === null
                  ? t("添加 DDNS 任务")
                  : t("编辑 DDNS 任务")
              }}
            </h2>
          </div>
          <button
            type="button"
            class="icon-btn"
            :aria-label="t('关闭 DDNS 表单')"
            @click="showDDNS = false"
          >
            ×
          </button>
        </div>
        <label
          >{{ t("任务名称")
          }}<input
            v-model="ddnsForm.name"
            maxlength="100"
            required
            :placeholder="t('家庭宽带 IPv4')" /></label
        ><label
          >{{ t("备注")
          }}<input
            v-model="ddnsForm.comment"
            maxlength="500"
            placeholder="viya" /></label
        ><label
          >{{ t("DNS 服务商")
          }}<select
            v-model="ddnsForm.dns_provider_id"
            required
            @change="loadDDNSZones(true)"
          >
            <option value="" disabled>{{ t("选择凭据") }}</option>
            <option v-for="p in providers" :key="p.id" :value="String(p.id)">
              {{ p.name }} · {{ p.provider_type }}
            </option>
          </select></label
        >
        <div v-if="providers.length === 0" class="dependency-help">
          <span>{{ t("还没有 DNS 凭据。添加后会保留当前 DDNS 表单。") }}</span
          ><button type="button" class="inline-action" @click="openProvider">
            {{ t("添加 DNS 凭据") }}
          </button>
        </div>
        <label v-if="ddnsZones.length && !manualDDNSZone"
          >DNS Zone<select v-model="ddnsForm.zone" required>
            <option value="" disabled>{{ t("请选择服务商中的 Zone") }}</option>
            <option
              v-for="zone in ddnsZones"
              :key="zone.name"
              :value="zone.name"
            >
              {{ zone.name }}{{ zone.status ? ` · ${zone.status}` : "" }}
            </option>
          </select></label
        ><label v-else
          >DNS Zone<input
            v-model="ddnsForm.zone"
            required
            placeholder="example.com"
            autocapitalize="none" /></label
        ><button
          v-if="ddnsZones.length"
          type="button"
          class="inline-action zone-mode-toggle"
          @click="manualDDNSZone = !manualDDNSZone"
        >
          {{ manualDDNSZone ? t("从 Zone 列表选择") : t("手动输入 Zone") }}
        </button>
        <p class="zone-fetch-hint">
          {{ ddnsZoneLoading ? t("正在读取服务商 Zone…") : ddnsZoneMessage }}
        </p>
        <label
          >{{ t("相对记录名")
          }}<input
            v-model="ddnsForm.record_name"
            required
            :placeholder="t('@ 或 home')" /></label
        ><label
          >{{ t("记录类型")
          }}<select v-model="ddnsForm.record_type">
            <option value="A">A (IPv4)</option>
            <option value="AAAA">AAAA (IPv6)</option>
            <option value="CNAME">{{ t("CNAME（别名）") }}</option>
          </select></label
        ><label v-if="ddnsForm.record_type === 'CNAME'"
          >{{ t("CNAME 目标域名")
          }}<input
            v-model="ddnsForm.cname_target"
            required
            placeholder="target.example.net"
            autocapitalize="none"
          /><small>{{
            t("填写目标主机名，不是 IP 地址。任务会按间隔将别名指向此域名。")
          }}</small></label
        ><label v-if="ddnsForm.record_type !== 'CNAME'"
          >{{ t("公网 IP 获取方式")
          }}<select v-model="ddnsForm.ip_source">
            <option value="interface">{{ t("通过网卡获取") }}</option>
            <option value="http">{{ t("通过公网接口查询") }}</option>
          </select></label
        >
        <div
          v-if="
            ddnsForm.record_type !== 'CNAME' &&
            ddnsForm.ip_source === 'interface'
          "
          class="ddns-interface-fields"
        >
          <label
            >{{ t("网络接口")
            }}<select v-model="ddnsForm.interface_name" required>
              <option value="" disabled>{{ t("选择接口") }}</option>
              <option
                v-for="item in networkInterfaces"
                :key="item.name"
                :value="item.name"
              >
                {{ interfaceLabel(item) }}
              </option>
            </select></label
          ><label
            >{{ t("要发布的") }}
            {{ ddnsForm.record_type === "A" ? "IPv4" : "IPv6" }} {{ t("地址")
            }}<select v-model="ddnsForm.interface_ip" required>
              <option value="" disabled>
                {{ t("选择这条 DNS 记录使用的 IP") }}
              </option>
              <option
                v-for="address in selectedInterfaceAddresses"
                :key="address.ip"
                :value="address.ip"
              >
                {{ address.ip }} · {{ address.scope
                }}{{ address.temporary ? t(" · 临时隐私地址") : "" }}
              </option></select
            ><small>{{
              t(
                "有多个 IPv6 时请选公网全局地址，并优先选择长期稳定、不会定期变化的地址。系统会固定使用此地址；若网卡上不再存在会报错，不会自动改用另一个。",
              )
            }}</small></label
          >
          <div class="interface-current-ip">
            <span>{{ t("网卡当前可用地址") }}</span
            ><button
              type="button"
              class="inline-action"
              @click="refreshNetworkInterfaces"
            >
              {{ t("刷新网卡") }}
            </button>
            <div v-if="selectedInterfaceAddresses.length">
              <code
                v-for="address in selectedInterfaceAddresses"
                :key="address.ip"
                >{{ address.ip }}
                <small
                  >{{ address.scope
                  }}{{ address.temporary ? t(" · 临时隐私地址") : "" }}</small
                ></code
              >
            </div>
            <small v-else class="interface-warning">{{
              t(
                "此接口暂时没有对应 IP 版本的地址。可换一个接口或改用公网接口查询。",
              )
            }}</small>
          </div>
        </div>
        <p v-else-if="ddnsForm.record_type !== 'CNAME'" class="auth-hint">
          {{
            t(
              "通过多个公网 IP 查询接口轮询获取地址，适用于路由器 NAT 或网卡上没有公网地址的情况。",
            )
          }}
        </p>
        <label
          >{{ t("检查间隔（分钟）")
          }}<input
            v-model.number="ddnsForm.interval_minutes"
            type="number"
            min="5"
            max="1440"
            required /></label
        ><label
          >{{ t("TTL 秒数")
          }}<input
            v-model.number="ddnsForm.ttl_seconds"
            type="number"
            min="60"
            max="86400"
            required /></label
        ><label class="enabled-label"
          ><input v-model="ddnsForm.enabled" type="checkbox" />{{
            t("启用并自动更新")
          }}</label
        >
        <p class="auth-hint">
          {{
            ddnsForm.record_type === "CNAME"
              ? t(
                  "CNAME 任务会定时把记录指向上方目标域名；CNAME 记录不能与同名的 A/AAAA 等记录同时存在。",
                )
              : t(
                  "网卡方式会在每次更新时精确使用选择的 IP 地址；建议选网卡上用于公网解析的长期稳定地址。若网卡地址是内网地址，公网域名建议使用公网接口查询。",
                )
          }}
        </p>
        <p v-if="ddnsError" class="error">{{ ddnsError }}</p>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="showDDNS = false">
            {{ t("取消") }}</button
          ><button
            class="primary"
            :disabled="
              saving ||
              (ddnsForm.record_type !== 'CNAME' &&
                ddnsForm.ip_source === 'interface' &&
                !selectedInterfaceAddresses.length)
            "
          >
            {{ saving ? t("保存中…") : t("保存任务") }}
          </button>
        </div>
      </form>
    </div>
    <div v-if="showCert" class="overlay overlay-top">
      <form class="modal" @submit.prevent="saveCert">
        <div class="modal-top">
          <div>
            <span class="eyebrow">{{ t("自定义证书") }}</span>
            <h2>{{ t("上传 PEM 证书") }}</h2>
          </div>
          <button type="button" class="icon-btn" @click="showCert = false">
            ×
          </button>
        </div>
        <label
          >{{ t("证书名称")
          }}<input
            v-model="certForm.name"
            maxlength="100"
            required
            :placeholder="t('example.com 证书')" /></label
        ><label
          >{{ t("证书链文件")
          }}<input
            type="file"
            accept=".pem,.crt,.cer"
            required
            @change="
              certFile = ($event.target as HTMLInputElement).files?.[0] || null
            " /></label
        ><label
          >{{ t("私钥文件")
          }}<input
            type="file"
            accept=".pem,.key"
            required
            @change="
              keyFile = ($event.target as HTMLInputElement).files?.[0] || null
            "
        /></label>
        <p class="auth-hint">
          {{
            t(
              "只接受未加密 PEM 私钥；服务端会校验证书和私钥匹配、SAN 与有效期。上传后选择站点域名匹配的证书。",
            )
          }}
        </p>
        <p v-if="certError" class="error">{{ certError }}</p>
        <div class="modal-actions">
          <button type="button" class="secondary" @click="showCert = false">
            {{ t("取消") }}</button
          ><button class="primary" :disabled="saving">
            {{ saving ? t("校验并保存…") : t("上传证书") }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
