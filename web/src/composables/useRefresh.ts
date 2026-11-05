import { apiFetch as fetch } from "../api";
import { t,tr } from "../i18n";
import { refreshErrors,applyRetryVisible,authenticated,csrfToken,error,deleteBusy,ddnsRefreshSequence,refreshing,hosts,revisions,providers,l4Rules,ddnsRecords,certificates,ipBans,certExpiring,upstreamHealth,online,lastRefreshedAt } from "./useState";

// ---- request sequencing (cross-domain) ----
type Scope = "core" | "sites" | "layer4" | "ddns" | "certs" | "dns" | "bans";
type Task = { scope: Scope; refresh: () => Promise<unknown> };
const domainRefreshers = new Map<string, Task>();
export function registerRefreshTask(name: string, refresh: () => Promise<unknown>, scope: Scope = "core") {
  domainRefreshers.set(name, { scope, refresh });
}
export let loadRequestSequence = 0;

type Target = { url: string; scope: Scope; apply: (data: any) => void };
const targets: Target[] = [
  { url: "/api/v1/health", scope: "core", apply: data => { online.value = !!(data.ok && data.caddy_ready) } },
  { url: "/api/v1/proxy-hosts", scope: "core", apply: data => { hosts.value = data } },
  { url: "/api/v1/config/revisions", scope: "core", apply: data => { revisions.value = data.slice(0, 10) } },
  { url: "/api/v1/proxy-hosts/health", scope: "sites", apply: data => { upstreamHealth.value = data && typeof data === "object" ? data : {} } },
  { url: "/api/v1/layer4-rules", scope: "layer4", apply: data => { l4Rules.value = data } },
  { url: "/api/v1/ddns-records", scope: "ddns", apply: data => { ddnsRecords.value = data } },
  { url: "/api/v1/custom-certificates", scope: "certs", apply: data => { certificates.value = data } },
  { url: "/api/v1/certificates/expiring?days=30", scope: "certs", apply: data => { certExpiring.value = Array.isArray(data?.certificates) ? data.certificates : [] } },
  { url: "/api/v1/dns-providers", scope: "dns", apply: data => { providers.value = data } },
  { url: "/api/v1/ip-bans", scope: "bans", apply: data => { ipBans.value = data } },
];
const refreshLabels: Record<string, string> = { '/api/v1/proxy-hosts': 'Web 服务', '/api/v1/health': '引擎状态', '/api/v1/config/revisions': '配置历史', '/api/v1/dns-providers': 'DNS 凭据', '/api/v1/layer4-rules': '端口转发', '/api/v1/ddns-records': '动态 DNS', '/api/v1/custom-certificates': 'SSL 证书', '/api/v1/ip-bans': 'IP 封禁', '/api/v1/certificates/expiring?days=30': '证书到期状态', '/api/v1/proxy-hosts/health': '上游状态' };

// What each page actually reads. Loading only these keeps the first paint to
// one request per data group instead of the whole console's worth of lists.
const sectionScopes: Record<string, Scope[]> = {
  overview: ["core", "sites", "layer4", "ddns", "certs", "dns", "bans"],
  "proxy-sites": ["core", "sites"],
  "file-services": ["core", "sites"],
  layer4: ["core", "layer4"],
  ddns: ["core", "ddns"],
  certificates: ["core", "certs"],
  "dns-providers": ["core", "dns"],
  "ip-bans": ["core", "bans"],
  "access-logs": ["core"],
  "config-history": ["core"],
};
const loadedScopes = new Set<Scope>();

async function execute(list: Target[], runTasks: boolean) {
  const requestSequence = ++loadRequestSequence;
  refreshing.value = true;
  error.value = "";
  const ddnsSequence = ddnsRefreshSequence.value;
  let succeeded = false;
  try {
    const results = await Promise.allSettled(list.map(async target => {
      const response = await fetch(target.url);
      if (response.status === 401) throw Error('unauthorized');
      if (!response.ok) throw Error(String(response.status));
      return response.json();
    }));
    if (requestSequence !== loadRequestSequence) return;
    if (results.some(result => result.status === 'rejected' && result.reason?.message === 'unauthorized')) {
      authenticated.value = false; csrfToken.value = ''; return;
    }
    const failures: string[] = [];
    results.forEach((result, index) => {
      const target = list[index]!;
      if (result.status === 'fulfilled') {
        // A DDNS write bumps the sequence on save; a response that started
        // before it would resurrect the row the user just changed.
        if (target.scope !== 'ddns' || ddnsSequence === ddnsRefreshSequence.value) target.apply(result.value);
        delete refreshErrors.value[target.url];
        loadedScopes.add(target.scope);
      } else {
        refreshErrors.value[target.url] = t('读取失败，请重试');
        failures.push(target.url);
        if (target.scope === 'core' && target.url.endsWith('/health')) online.value = false;
      }
    });
    if (runTasks) {
      const tasks = [...domainRefreshers.entries()].filter(([, task]) => list.some(target => target.scope === task.scope));
      const taskResults = await Promise.allSettled(tasks.map(([, task]) => task.refresh()));
      if (requestSequence !== loadRequestSequence) return;
      taskResults.forEach((result, index) => {
        const name = tasks[index]![0];
        if (result.status === 'rejected') { refreshErrors.value[name] = t('读取失败，请重试'); failures.push(name) }
        else delete refreshErrors.value[name];
      });
    }
    if (failures.length) error.value = t('部分数据刷新失败，已保留上次结果') + '：' + failures.map(name => t(refreshLabels[name] || name)).join('、');
    succeeded = failures.length === 0;
  } catch (e) {
    if (requestSequence === loadRequestSequence) {
      error.value = e instanceof Error ? tr`刷新失败：${e.message}` : t("刷新失败：后端暂不可用");
    }
  } finally {
    if (requestSequence === loadRequestSequence) {
      refreshing.value = false;
      if (succeeded) lastRefreshedAt.value = Date.now();
    }
  }
}

/** Full refresh: every data group, used after a write or a manual reload. */
export async function load() {
  loadedScopes.clear();
  await execute(targets, true);
}

/** First visit to a page: fetch only the groups it renders and are still missing. */
let sectionLoading = false;
export async function loadSection(section: string) {
  if (sectionLoading) return;
  const scopes = sectionScopes[section] ?? ["core"];
  const list = targets.filter(target => scopes.includes(target.scope) && !loadedScopes.has(target.scope));
  if (!list.length) return;
  sectionLoading = true;
  try {
    await execute(list, true);
  } finally {
    sectionLoading = false;
  }
}

export async function performDelete(url: string) {
  if (deleteBusy.value) return;
  deleteBusy.value = true;
  let failure = "";
  ddnsRefreshSequence.value++;
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
    ddnsRefreshSequence.value++;
    await load();
    if (failure) error.value = failure;
    deleteBusy.value = false;
  }
}
