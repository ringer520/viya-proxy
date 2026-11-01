<script setup lang="ts">
import { apiFetch as fetch } from "./api";

import { t, tr } from "./i18n";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import PanelSettingsDrawer from "./components/PanelSettingsDrawer.vue";
import SiteFilesDrawer from "./components/SiteFilesDrawer.vue";
import LogWorkspace from "./components/LogWorkspace.vue";
import LanguagePicker from "./components/LanguagePicker.vue";
import ListenerEditorModal from "./components/ListenerEditorModal.vue";
import BanModal from "./components/BanModal.vue";
import ManagedCertModal from "./components/ManagedCertModal.vue";
import SiteLogModal from "./components/SiteLogModal.vue";
import SecurityDefaultsDialog from "./components/SecurityDefaultsDialog.vue";
import DeleteConfirmDialog from "./components/DeleteConfirmDialog.vue";
import ConfigApplyBar from "./components/ConfigApplyBar.vue";
import SiteDetailsDrawer from "./components/SiteDetailsDrawer.vue";
import SiteFormModal from "./components/SiteFormModal.vue";
import ProviderModal from "./components/ProviderModal.vue";
import L4Modal from "./components/L4Modal.vue";
import DDNSModal from "./components/DDNSModal.vue";
import CertUploadModal from "./components/CertUploadModal.vue";
import OverviewTopSection from "./components/OverviewTopSection.vue";
import OverviewTrafficSection from "./components/OverviewTrafficSection.vue";
import OverviewBottomSection from "./components/OverviewBottomSection.vue";
import ProxySitesPage from "./components/ProxySitesPage.vue";
import DnsProvidersPage from "./components/DnsProvidersPage.vue";
import CertificatesPage from "./components/CertificatesPage.vue";
import Layer4Page from "./components/Layer4Page.vue";
import Ddnspage from "./components/Ddnspage.vue";
import ConfigHistoryPage from "./components/ConfigHistoryPage.vue";
import IpBansPage from "./components/IpBansPage.vue";
import { formatBytes } from "./format";
import {
  Plus,
  Globe2,
  ShieldCheck,
  RefreshCw,
  History,
  LayoutDashboard,
  KeyRound,
  ScrollText,
  Ban,
  Cable,
  LogOut,
  Settings,
  Search,
} from "@lucide/vue";

import { authenticated, csrfToken, authChecked, setupRequired, panelUsername, panelRole, readOnly, error, saving, applyRetryVisible, hosts, online, refreshing, resolveDelete, listSearch, statusFilter, hasListFilter, clearListFilters } from "./composables/useState";
import { load } from "./composables/useRefresh";
import { retryApply } from "./composables/useApply";
import { trafficTotals, refreshTraffic } from "./composables/useTraffic";
import {
  managedCertificates,
  openManagedCert,
  refreshManagedCertificates,
  refreshCertificateInventory,
  visibleCerts,
  visibleManagedCerts,
} from "./composables/useCertificates";
import {
  visibleHosts,
  viewingLogSite,
  siteFilesHost,
  loadSiteLogs,
  closeSiteFiles,
  openCreate,
  show as siteFormOpen,
  webSearchOpen,
} from "./composables/useHosts";
import {
  openBan,
  openL4,
  openDDNS,
  refreshDDNS,
  openProvider,
  visibleDDNS,
  visibleL4,
  visibleProviders,
  visibleBans,
} from "./composables/useNetwork";
import { loadSecurityDefaults } from "./composables/useSecurity";
import {
  autoRefresh,
  compactMode,
  simpleDisplay,
  documentHidden,
  landingPage,
  pauseHidden,
  previewLimit,
  refreshInterval,
  themeChoice,
  themeMedia,
  trafficCharts,
  applyPanelSettings,
  panelAccessPort,
  panelAccessURL,
} from "./composables/usePreferences";

// ---- theme & preferences (state lives in usePreferences) ----
function resetPanelPreferences() {
  themeChoice.value = "system";
  compactMode.value = false;
  simpleDisplay.value = false;
  refreshInterval.value = 5;
  autoRefresh.value = true;
  pauseHidden.value = true;
  trafficCharts.value = true;
  previewLimit.value = 5;
  landingPage.value = "overview";
  panelSettingsError.value = "";
  panelSettingsMessage.value = t("浏览器偏好已恢复默认。");
}

// ---- panel settings (account, port, sessions) ----
const appVersion = ref("");
const showPanelSettings = ref(false);
const panelListenPort = ref(Number(panelAccessPort) || 8181);
const panelSettingsError = ref("");
const panelSettingsMessage = ref("");
const panelSettingsBusy = ref(false);
const panelSettingsLoading = ref(false);
const panelSessions = ref<number | null>(null);
const storedPanelPort = ref(8181);
const passwordForm = ref({ current: "", next: "", confirm: "" });
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

// ---- auth UI (login / setup / logout) ----
const authForm = ref({ username: "", password: "", confirm: "" });
const authNotice = ref(sessionStorage.getItem("viya-restored-message") || "");
sessionStorage.removeItem("viya-restored-message");
const authBusy = ref(false);
const authError = ref("");

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
  // Fetch the build version once for the sidebar footer.
  fetch("/api/v1/version")
    .then((r) => (r.ok ? r.json() : Promise.resolve({})))
    .then((d: Record<string, unknown>) => {
      if (d && typeof d.current === "string") appVersion.value = d.current;
    })
    .catch(() => {});
  await Promise.all([load(), loadSecurityDefaults()]);
  syncActiveSection();
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
    await refreshSession();
  } catch (e) {
    authError.value = e instanceof Error ? t(e.message) : t("认证失败");
  } finally {
    authBusy.value = false;
  }
}
async function logout() {
  try {
    await fetch("/api/v1/auth/logout", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken.value },
    });
  } catch {
    // Even if the server is unreachable, clear the local session.
  } finally {
    authenticated.value = false;
    csrfToken.value = "";
    authForm.value.password = "";
    authForm.value.confirm = "";
  }
}
function onSessionExpired() {
  authenticated.value = false;
  csrfToken.value = "";
}

// ---- navigation ----
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
    description: t("管理反向代理、静态站点与 URL 重定向，支持证书与访问控制。"),
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
    description: t("管理 Cloudflare、阿里云、腾讯云、GoDaddy 等 DNS 服务商凭据，供证书申请与 DDNS 共用。"),
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
const groupedNavigation = computed(() =>
  navigationGroups.map((group) => ({
    group,
    items: navigationItems.filter((item) => item.group === group),
  })),
);
const currentPage = computed(
  () =>
    navigationItems.find((item) => item.id === activeSection.value) ||
    navigationItems[0]!,
);

// ---- search box UI ----
const listSearchOpen = ref(false);
const listSearchInput = ref<HTMLInputElement | null>(null);
async function toggleListSearch() {
  listSearchOpen.value = !listSearchOpen.value;
  if (listSearchOpen.value) {
    await nextTick();
    listSearchInput.value?.focus();
  } else listSearch.value = "";
}
const searchablePage = computed(() => activeSection.value === "ip-bans");
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

// ---- page instructions ----
const pageInstructions: Record<string, string[]> = {
  overview: [
    t("先添加 DNS 凭据，再申请证书或创建 Web 服务。"),
    t("点击功能卡片进入管理；配置失败时可从最近配置状态查看原因。"),
  ],
  "proxy-sites": [
    t("站点分三类：反向代理（转发到上游）、静态站点（由 Caddy 直接托管文件，可上传管理）、URL 重定向。"),
    t("先选择 HTTP / HTTPS 监听，再添加域名；已有主域名可直接添加子域名；访问路径留空表示匹配整个域名。"),
    t("静态站点文件上传后立即对外生效、无需重启；开启目录浏览可公开文件列表。"),
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
    t("支持 Cloudflare、腾讯云 DNSPod、阿里云 DNS、GoDaddy、Namecheap、DigitalOcean、Vultr 等厂商。"),
    t("保存的 API 凭据供证书 DNS-01 验证和 DDNS 共用；列表不显示密钥。"),
    t("替换凭据会影响引用该凭据的证书与 DDNS 任务。"),
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

// ---- re-apply after a partial save ----
// (retryApply lives in useCore and is imported above)

// ---- timers + auto refresh ----
let trafficTimer: ReturnType<typeof setInterval> | undefined;
let certificateProgressTimer: ReturnType<typeof setInterval> | undefined;
function canAutoRefresh() {
  return (
    authenticated.value &&
    autoRefresh.value &&
    (!pauseHidden.value || !document.hidden)
  );
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
    simpleDisplay,
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

onMounted(() => {
  if (!navigationItems.some((item) => item.id === landingPage.value))
    landingPage.value = "overview";
  applyPanelSettings();
  syncActiveSection();
  window.addEventListener("hashchange", syncActiveSection);
  window.addEventListener("keydown", settingsShortcut);
  window.addEventListener("viya:session-expired", onSessionExpired);
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
  window.removeEventListener("viya:session-expired", onSessionExpired);
  document.removeEventListener("visibilitychange", synchronizeAutoRefresh);
  themeMedia.removeEventListener("change", applyPanelSettings);
  if (trafficTimer) clearInterval(trafficTimer);
  if (certificateProgressTimer) clearInterval(certificateProgressTimer);
  resolveDelete(false);
});
</script>


<template>
  <div v-if="authChecked && !authenticated" class="auth-screen">
    <form class="modal auth-modal" @submit.prevent="authenticate()">
      <img
        class="brand-logo auth-logo"
        src="/viya-fox-icon.png"
        alt="Viya"
      /><span class="eyebrow">{{ t("Viya 管理面板") }}</span>
      <div class="auth-title-row">
        <h1>{{ setupRequired ? t("创建管理员") : t("管理员登录") }}</h1>
        <LanguagePicker />
      </div>
      <p class="auth-hint">
        {{
          setupRequired
            ? t("首次使用请创建唯一管理员账户，密码至少 8 位。")
            : t("登录以管理代理站点和 Caddy 配置。")
        }}
      </p>
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
      <p v-if="authNotice" class="auth-notice" role="status">
        {{ authNotice }}
      </p>
      <p v-if="authError" class="error">{{ authError }}</p>
      <button class="primary auth-submit" :disabled="authBusy">
        {{
          authBusy
            ? t("请稍候…")
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
        <span v-if="appVersion" class="sidebar-version">{{ appVersion }}</span>
      </div>
    </aside>
    <PanelSettingsDrawer
      v-if="showPanelSettings"
      id="panel-settings-dialog"
      v-model:theme="themeChoice"
      v-model:compact="compactMode"
      v-model:simple-display="simpleDisplay"
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
    <SiteFilesDrawer
      v-if="siteFilesHost"
      :host="{
        id: siteFilesHost.id,
        domain: siteFilesHost.domain,
        root_path: siteFilesHost.root_path,
        tls_mode: siteFilesHost.tls_mode,
      }"
      :csrf="csrfToken"
      @close="closeSiteFiles"
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
        <a
          v-if="activeSection === 'overview'"
          class="github-link"
          href="https://github.com/ringer520/viya-proxy"
          target="_blank"
          rel="noopener noreferrer"
          :title="t('在 GitHub 上查看 Viya 项目')"
          :aria-label="t('GitHub 仓库')"
        >
          <svg
            viewBox="0 0 24 24"
            width="19"
            height="19"
            fill="currentColor"
            aria-hidden="true"
          >
            <path
              d="M12 0C5.37 0 0 5.37 0 12c0 5.3 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61-.546-1.385-1.335-1.755-1.335-1.755-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 21.795 24 17.295 24 12c0-6.63-5.37-12-12-12z"
            />
          </svg>
        </a>
      </header>
      <div class="workspace-toolbar">
        <button
          v-if="currentPage.action"
          class="primary toolbar-add"
          type="button"
          :disabled="readOnly"
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
      <ConfigApplyBar />
      <OverviewTopSection v-if="activeSection === 'overview'" />
      <p v-if="readOnly" class="readonly-banner" role="status">
        {{ t("当前账号为只读，可以查看全部配置，但不能保存任何修改。") }}
      </p>
      <div v-if="error && !siteFormOpen" class="error global-error" role="alert">
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
      <OverviewTrafficSection v-if="activeSection === 'overview'" />
      <ProxySitesPage v-if="activeSection === 'proxy-sites'" />
      <DnsProvidersPage v-if="activeSection === 'dns-providers'" />
      <CertificatesPage v-if="activeSection === 'certificates'" />
      <Layer4Page v-if="activeSection === 'layer4'" />
      <Ddnspage v-if="activeSection === 'ddns'" />
      <ConfigHistoryPage v-if="activeSection === 'config-history'" />
      <LogWorkspace
        v-if="activeSection === 'access-logs'"
        :csrf="csrfToken"
        :auto-refresh="autoRefresh && (!pauseHidden || !documentHidden)"
      />
      <IpBansPage v-if="activeSection === 'ip-bans'" />

      <OverviewBottomSection v-if="activeSection === 'overview'" />
      <footer>Viya <span>·</span> {{ t("本地部署的 Caddy 管理工具") }}</footer>
    </main>
    <ListenerEditorModal />
    <!-- Form overlays close only through their explicit close, cancel, or save controls. Keep this behavior for future edit/create forms. -->
    <BanModal />
    <ManagedCertModal />
    <SiteLogModal :auto-refresh="autoRefresh" :refresh-interval="refreshInterval" />
    <SecurityDefaultsDialog />
    <DeleteConfirmDialog />
    <SiteFormModal />
    <SiteDetailsDrawer />
    <ProviderModal />
    <L4Modal />
    <DDNSModal />
    <CertUploadModal />
  </div>
</template>
