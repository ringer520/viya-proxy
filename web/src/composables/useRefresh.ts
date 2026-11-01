import { apiFetch as fetch } from "../api";
import { t,tr } from "../i18n";
import { applyRetryVisible,authenticated,csrfToken,error,deleteBusy,ddnsRefreshSequence,refreshing,hosts,revisions,providers,l4Rules,ddnsRecords,certificates,ipBans,certExpiring,upstreamHealth,online,lastRefreshedAt } from "./useState";
// ---- request sequencing (cross-domain) ----
const domainRefreshers=new Map<string,()=>Promise<unknown>>();
export function registerRefreshTask(name:string,refresh:()=>Promise<unknown>){domainRefreshers.set(name,refresh)}
export let loadRequestSequence = 0;


export async function load() {
  const requestSequence = ++loadRequestSequence,
    ddnsSequence = ddnsRefreshSequence.value;
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
    if (ddnsSequence === ddnsRefreshSequence.value) ddnsRecords.value = ddns;
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
    await Promise.all([...domainRefreshers.values()].map(refresh=>refresh()));
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
