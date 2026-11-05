import { apiFetch as fetch } from "../api";
import { computed, ref, watch } from "vue";
import { t, tr } from "../i18n";
import { emptySitePolicy, type SitePolicy } from "../policy";
import { csrfToken, readOnly, error as pageError } from "./useState";
import { load, registerRefreshTask } from "./useRefresh";

export function normalizeDefault(source?: Partial<SitePolicy>, domain = false): SitePolicy {
  const p = { ...emptySitePolicy(), ...source };
  p.mode = source?.mode || (source?.enabled ? "custom" : domain ? "inherit" : "none");
  return p;
}
export const securityDefaults = ref<{global:SitePolicy;domains:Record<string,SitePolicy>}>({global:normalizeDefault(),domains:{}});
export const securityDialog = ref<{open:boolean;scope:"global"|"domain";domain:string}>({open:false,scope:"global",domain:""});
export const securityForm = ref<SitePolicy>(emptySitePolicy());
export const securitySaving = ref(false), securityError = ref(""), securityNotice = ref("");
export const securityImpact = ref<{updated:number;independent:number;overridden:number;sites:string[]}|null>(null);
export const securityPreviewPending = ref(false), securityPreviewError = ref("");

export async function loadSecurityDefaults() {
  try {
    const r=await fetch("/api/v1/security-defaults"); if(!r.ok) return false;
    const d=await r.json();
    securityDefaults.value={global:normalizeDefault(d.global),domains:Object.fromEntries(Object.entries(d.domains || {}).map(([key,value])=>[key,normalizeDefault(value as SitePolicy,true)]))};
    return true;
  } catch { return false; }
}
export function resolveSecurityPolicy(domain:string, draft?:{domain:string;policy:SitePolicy}) {
  const domains={...securityDefaults.value.domains}; let global=securityDefaults.value.global;
  if(draft){if(draft.domain)domains[draft.domain]=draft.policy;else global=draft.policy}
  let candidate=domain.trim().toLowerCase().replace(/\.$/,"");
  while(candidate.includes(".")){
    const p=domains[candidate];
    if(p?.mode==="none")return {policy:{...emptySitePolicy(),mode:"none" as const},source:candidate};
    if(p?.mode==="custom")return {policy:{...emptySitePolicy(),...p,inherit:true},source:candidate};
    candidate=candidate.slice(candidate.indexOf(".")+1);
  }
  return {policy:global.mode==="none"?{...emptySitePolicy(),mode:"none" as const}:{...emptySitePolicy(),...global,inherit:true},source:"global"};
}
export const securityEffective = computed(()=>securityForm.value.mode==="custom"?securityForm.value:securityForm.value.mode==="none"?{...emptySitePolicy(),mode:"none" as const}:resolveSecurityPolicy(securityDialog.value.domain,{domain:securityDialog.value.domain,policy:securityForm.value}).policy);
export const securityEffectiveSource = computed(()=>resolveSecurityPolicy(securityDialog.value.domain,{domain:securityDialog.value.domain,policy:securityForm.value}).source);
let securityOpening=0;
export async function openSecurityDefaults(domain?:string){
  if(readOnly.value || securitySaving.value)return;
  const seq=++securityOpening;
  const loaded=await loadSecurityDefaults();
  if(seq!==securityOpening)return;
  if(!loaded){pageError.value=t('读取安全策略失败，请刷新后重试。');return;}
  securityError.value="";securityNotice.value="";
  const root=(domain||"").trim().toLowerCase();
  securityDialog.value={open:true,scope:root?"domain":"global",domain:root};
  securityForm.value=JSON.parse(JSON.stringify(root?securityDefaults.value.domains[root] || normalizeDefault(undefined,true):securityDefaults.value.global));
}
// Debounced server preview uses the same resolver as compilation, including nested overrides.
let previewTimer:ReturnType<typeof setTimeout>|undefined, previewSequence=0;
let previewRequest:AbortController|undefined;
watch([securityForm,securityDialog],()=>{
  clearTimeout(previewTimer);previewRequest?.abort();const seq=++previewSequence;
  securityImpact.value=null;securityPreviewError.value="";
  if(!securityDialog.value.open){securityPreviewPending.value=false;return}
  securityPreviewPending.value=true;
  previewTimer=setTimeout(async()=>{
    previewRequest=new AbortController();
    try{
      const r=await fetch("/api/v1/security-defaults/preview",{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":csrfToken.value},body:JSON.stringify({domain:securityDialog.value.domain,policy:securityForm.value}),signal:previewRequest.signal});
      const data=await r.json();if(!r.ok)throw Error(t(data.error)||t("读取影响范围失败"));
      if(seq===previewSequence)securityImpact.value=data;
    }catch(e){if(seq===previewSequence)securityPreviewError.value=e instanceof Error?e.message:t("读取影响范围失败")}
    finally{if(seq===previewSequence)securityPreviewPending.value=false}
  },200);
},{deep:true,flush:"sync"});
export async function saveSecurityDefaults(){
  if(securitySaving.value || securityPreviewPending.value || !securityImpact.value)return;
  securitySaving.value=true;securityError.value="";securityNotice.value="";
  try{
    const target=securityDialog.value.scope==="domain"?`/api/v1/security-defaults/domain/${encodeURIComponent(securityDialog.value.domain)}`:"/api/v1/security-defaults/global";
    const r=await fetch(target,{method:"PUT",headers:{"Content-Type":"application/json","X-CSRF-Token":csrfToken.value},body:JSON.stringify(securityForm.value)});
    const body=await r.json().catch(()=>({}));if(!r.ok)throw Error(t(body.error)||t("保存失败"));
    await loadSecurityDefaults();await load();
    securityNotice.value=tr`已保存，${body.updated ?? 0} 个跟随站点已同步。`;
  }catch(e){securityError.value=e instanceof Error?e.message:t("保存失败")}
  finally{securitySaving.value=false}
}
export function closeSecurityDialog(){if(securitySaving.value)return;securityDialog.value.open=false;securityError.value="";securityNotice.value=""}

registerRefreshTask("security-defaults",loadSecurityDefaults,"bans");
