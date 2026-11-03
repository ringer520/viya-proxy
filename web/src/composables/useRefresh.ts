import { apiFetch as fetch } from "../api";
import { t,tr } from "../i18n";
import { refreshErrors,applyRetryVisible,authenticated,csrfToken,error,deleteBusy,ddnsRefreshSequence,refreshing,hosts,revisions,providers,l4Rules,ddnsRecords,certificates,ipBans,certExpiring,upstreamHealth,online,lastRefreshedAt } from "./useState";
// ---- request sequencing (cross-domain) ----
const domainRefreshers=new Map<string,()=>Promise<unknown>>();
export function registerRefreshTask(name:string,refresh:()=>Promise<unknown>){domainRefreshers.set(name,refresh)}
export let loadRequestSequence = 0;
const refreshLabels:Record<string,string>={'/api/v1/proxy-hosts':'Web 服务','/api/v1/health':'引擎状态','/api/v1/config/revisions':'配置历史','/api/v1/dns-providers':'DNS 凭据','/api/v1/layer4-rules':'端口转发','/api/v1/ddns-records':'动态 DNS','/api/v1/custom-certificates':'SSL 证书','/api/v1/ip-bans':'IP 封禁','/api/v1/certificates/expiring?days=30':'证书到期状态','/api/v1/proxy-hosts/health':'上游状态'};


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
    const setters: ((data:any)=>void)[] = [
      data=>{hosts.value=data},
      data=>{online.value=!!(data.ok&&data.caddy_ready)},
      data=>{revisions.value=data.slice(0,10)},
      data=>{providers.value=data},
      data=>{l4Rules.value=data},
      data=>{if(ddnsSequence===ddnsRefreshSequence.value)ddnsRecords.value=data},
      data=>{certificates.value=data},
      data=>{ipBans.value=data},
      data=>{certExpiring.value=Array.isArray(data?.certificates)?data.certificates:[]},
      data=>{upstreamHealth.value=data&&typeof data==='object'?data:{}},
    ];
    const results=await Promise.allSettled(urls.map(async url=>{
      const response=await fetch(url);
      if(response.status===401)throw Error('unauthorized');
      if(!response.ok)throw Error(String(response.status));
      return response.json();
    }));
    if(requestSequence!==loadRequestSequence)return;
    if(results.some(result=>result.status==='rejected'&&result.reason?.message==='unauthorized')){
      authenticated.value=false;csrfToken.value='';return;
    }
    const failures:string[]=[];
    results.forEach((result,index)=>{
      const name=urls[index]!;
      if(result.status==='fulfilled'){setters[index]!(result.value);delete refreshErrors.value[name]}
      else{refreshErrors.value[name]=t('读取失败，请重试');failures.push(name);if(index===1)online.value=false}
    });
    const tasks=[...domainRefreshers.entries()];
    const taskResults=await Promise.allSettled(tasks.map(([,refresh])=>refresh()));
    if(requestSequence!==loadRequestSequence)return;
    taskResults.forEach((result,index)=>{const name=tasks[index]![0];if(result.status==='rejected'){refreshErrors.value[name]=t('读取失败，请重试');failures.push(name)}else delete refreshErrors.value[name]});
    if(failures.length)error.value=t('部分数据刷新失败，已保留上次结果')+'：'+failures.map(name=>t(refreshLabels[name]||name)).join('、');
    succeeded = failures.length===0;
  } catch (e) {
    if (requestSequence === loadRequestSequence) {
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
