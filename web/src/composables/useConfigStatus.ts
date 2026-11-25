import {computed,ref,watch,onMounted,onBeforeUnmount} from 'vue'
import {apiFetch} from '../api'
import {csrfToken,readOnly,confirmDeletion} from './useState'
import {load} from './useRefresh'
import {loadWebListeners} from './useListeners'
import {t} from '../i18n'
import {autoRefresh,pauseHidden,documentHidden,refreshInterval} from './usePreferences'
export type ConfigStatus={access_confirmation?:{domain:string;deadline:number}|null;stage:string;message:string;updated:string;recovery:boolean;port_confirmation?:{old_port:string;new_port:string;deadline:number}|null}
export const configStatus=ref<ConfigStatus>({stage:'',message:'',updated:'',recovery:false}),recoveryBusy=ref(false),recoveryError=ref('')
export const applying=computed(()=>['compiling','validating','applying'].includes(configStatus.value.stage))
export async function refreshConfigStatus(){try{const r=await apiFetch('/api/v1/config/status');if(r.ok)configStatus.value=await r.json()}catch{}}
export async function recoverConfig(checkpoint=""){if(readOnly.value||recoveryBusy.value||applying.value)return;if(!await confirmDeletion(t('恢复最近可用配置会替换站点、监听、安全策略等业务设定；账号保持不变。确认继续？'),t('确认恢复配置')))return;recoveryBusy.value=true;recoveryError.value='';try{const r=await apiFetch('/api/v1/config/rollback',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrfToken.value},body:JSON.stringify({confirmed:true,checkpoint})});const d=await r.json();if(!r.ok)throw Error(t(d.error));await load();await loadWebListeners(true)}catch(e){recoveryError.value=e instanceof Error?e.message:t('恢复失败')}finally{recoveryBusy.value=false;await refreshConfigStatus()}}
export async function finishAccessRule(cancel=false){if(readOnly.value||recoveryBusy.value)return;recoveryError.value='';try{if(cancel&&!await confirmDeletion(t('取消本次访问限制修改并恢复原规则？'),t('确认恢复')))return;recoveryBusy.value=true;const r=await apiFetch('/api/v1/ip-bans/guard/'+(cancel?'cancel':'confirm'),{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrfToken.value},body:JSON.stringify({confirmed:true})});const d=await r.json();if(!r.ok)throw Error(t(d.error));await load()}catch(e){recoveryError.value=e instanceof Error?t(e.message):t('操作失败')}finally{recoveryBusy.value=false;await refreshConfigStatus()}}
export async function confirmPanelAccess(){const r=await apiFetch('/api/v1/panel/settings/confirm',{method:'POST',headers:{'X-CSRF-Token':csrfToken.value}});if(!r.ok){const d=await r.json();recoveryError.value=t(d.error)}await refreshConfigStatus()}
export function useConfigStatusPolling(){
 let polling=false;let timer:ReturnType<typeof setInterval>|undefined;
 const start=()=>{
  if(timer)clearInterval(timer);
  timer=setInterval(()=>{
   if(polling||!autoRefresh.value||(pauseHidden.value&&documentHidden.value))return;
   polling=true;void refreshConfigStatus().finally(()=>{polling=false});
  },Math.max(5,refreshInterval.value)*1000);
 };
 onMounted(()=>{void refreshConfigStatus();start();watch(refreshInterval,start)});
 onBeforeUnmount(()=>{if(timer)clearInterval(timer)});
}
