import {computed,ref,onMounted,onBeforeUnmount} from 'vue'
import {apiFetch} from '../api'
import {csrfToken,readOnly,confirmDeletion} from './useState'
import {load} from './useRefresh'
import {t} from '../i18n'
export type ConfigStatus={stage:string;message:string;updated:string;recovery:boolean;port_confirmation?:{old_port:string;new_port:string;deadline:number}|null}
export const configStatus=ref<ConfigStatus>({stage:'',message:'',updated:'',recovery:false}),recoveryBusy=ref(false),recoveryError=ref('')
export const applying=computed(()=>['compiling','validating','applying'].includes(configStatus.value.stage))
export async function refreshConfigStatus(){try{const r=await apiFetch('/api/v1/config/status');if(r.ok)configStatus.value=await r.json()}catch{}}
export async function recoverConfig(checkpoint=""){if(readOnly.value||recoveryBusy.value||applying.value)return;if(!await confirmDeletion(t('恢复最近可用配置会替换站点、监听、安全策略等业务设定；账号保持不变。确认继续？'),t('确认恢复配置')))return;recoveryBusy.value=true;recoveryError.value='';try{const r=await apiFetch('/api/v1/config/rollback',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrfToken.value},body:JSON.stringify({confirmed:true,checkpoint})});const d=await r.json();if(!r.ok)throw Error(d.error);await load()}catch(e){recoveryError.value=e instanceof Error?e.message:t('恢复失败')}finally{recoveryBusy.value=false;await refreshConfigStatus()}}
export async function confirmPanelAccess(){const r=await apiFetch('/api/v1/panel/settings/confirm',{method:'POST',headers:{'X-CSRF-Token':csrfToken.value}});if(!r.ok){const d=await r.json();recoveryError.value=d.error}await refreshConfigStatus()}
export function useConfigStatusPolling(){let timer:ReturnType<typeof setInterval>|undefined;onMounted(()=>{void refreshConfigStatus();timer=setInterval(()=>{if(!document.hidden)void refreshConfigStatus()},1200)});onBeforeUnmount(()=>{if(timer)clearInterval(timer)})}
