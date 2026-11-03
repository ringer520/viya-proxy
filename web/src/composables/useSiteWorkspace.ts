import { computed, ref } from 'vue'
import { apiFetch } from '../api'
import { t } from '../i18n'
import type {Host} from '../types'
import {hosts,ddnsRecords,readOnly,csrfToken} from './useState'
import {load} from './useRefresh'
import {managedCertificates} from './useState'
export async function saveSiteMetadata(host:Host,tags:string[]){if(readOnly.value)return;const r=await apiFetch(`/api/v1/proxy-hosts/${host.id}/metadata`,{method:'PATCH',headers:{'Content-Type':'application/json','X-CSRF-Token':csrfToken.value},body:JSON.stringify({tags})});const d=await r.json();if(!r.ok)throw Error(d.error);Object.assign(host,{tags:d.tags})}
export const detailID=ref<number|null>(null)
export const detailHost=computed(()=>hosts.value.find(h=>h.id===detailID.value))
export const diagnostic=ref<{checked_at:string;steps:{name:string;state:string;detail:string;advice?:string}[]}|null>(null)
export const diagnosticBusy=ref(false),detailError=ref('')
let diagnosticRequest:AbortController|null=null
export function closeSiteDetails(){detailID.value=null;diagnosticRequest?.abort();diagnosticBusy.value=false}
export function openSiteDetails(host:Host,run=false){diagnosticRequest?.abort();detailID.value=host.id;diagnostic.value=null;detailError.value='';if(run)void runDiagnostics()}
export async function runDiagnostics(){if(!detailHost.value)return;diagnosticRequest?.abort();const controller=new AbortController();diagnosticRequest=controller;const id=detailID.value;diagnosticBusy.value=true;detailError.value='';try{const r=await apiFetch(`/api/v1/proxy-hosts/${id}/diagnostics`,{signal:controller.signal});const d=await r.json();if(!r.ok)throw Error(d.error||t('诊断失败'));if(detailID.value===id&&!controller.signal.aborted)diagnostic.value=d}catch(e){if(!controller.signal.aborted)detailError.value=e instanceof Error?e.message:t('诊断失败')}finally{if(diagnosticRequest===controller)diagnosticBusy.value=false}}
export function ddnsDomain(record:{record_name:string;zone:string}){const zone=record.zone.toLowerCase().replace(/\.$/,'');const name=record.record_name.toLowerCase().replace(/\.$/,'');return name==='@'||name===zone?zone:name.endsWith('.'+zone)?name:name+'.'+zone}
export const relatedDDNS=computed(()=>ddnsRecords.value.filter(r=>ddnsDomain(r)===detailHost.value?.domain))
export function providerUsage(id:number){return [...hosts.value.filter(h=>h.dns_provider_id===id).map(h=>t('站点')+': '+h.domain),...ddnsRecords.value.filter(d=>d.dns_provider_id===id).map(d=>'DDNS: '+ddnsDomain(d)),...managedCertificates.value.filter(c=>c.dns_provider_id===id).map(c=>t('证书')+': '+c.domain)]}
export function certificateUsage(domain:string,id?:number){return hosts.value.filter(h=>id?h.custom_certificate_id===id:h.certificate_source?.domain===domain).map(h=>h.domain)}
