<script setup lang="ts">
import { apiFetch as fetch } from "../api";

import {computed,ref,watch} from 'vue'
import {Download,Upload,Archive,RefreshCw,KeyRound} from '@lucide/vue'
import {t} from '../i18n'
const props=defineProps<{csrf:string}>()
const emit=defineEmits<{restored:[preferences:Record<string,string>,message:string]}>()
const mode=ref<'export'|'import'>('export'),archive=ref<unknown>(null),busy=ref(false),error=ref(''),message=ref(''),confirmed=ref(false)
const password=ref(''),passwordConfirm=ref(''),importPassword=ref(''),archiveEncrypted=ref(false)
type Report={created:string;counts:Record<string,number>;files:number;preferences:number;encrypted?:boolean}
const report=ref<Report|null>(null)
const labels:Record<string,string>={dns_providers:t('DNS 凭据'),custom_certificates:t('上传证书'),managed_certificates:t('托管证书'),proxy_hosts:t('Web 站点'),layer4_rules:t('端口转发'),ddns_records:t('DDNS 任务'),ip_bans:t('IP 封禁'),panel_settings:t('面板与监听设置')}
const passwordValid=computed(()=>password.value.length>=8&&password.value===passwordConfirm.value)
const awaitingPassword=computed(()=>mode.value==='import'&&archiveEncrypted.value&&!report.value)
watch(mode,()=>{error.value='';message.value='';report.value=null;confirmed.value=false;archive.value=null;archiveEncrypted.value=false;importPassword.value=''})
function preferences(){const result:Record<string,string>={};for(let i=0;i<localStorage.length;i++){const key=localStorage.key(i);if(key?.startsWith('viya-'))result[key]=localStorage.getItem(key)||''}return result}
async function download(){if(busy.value||!passwordValid.value)return;busy.value=true;error.value='';try{const r=await fetch('/api/v1/config/export',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify({preferences:preferences(),password:password.value})});if(!r.ok)throw Error(t((await r.json()).error));const blob=await r.blob(),url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download=`viya-config-${new Date().toISOString().replace(/[:.]/g,'-')}.json`;a.click();setTimeout(()=>URL.revokeObjectURL(url),5000);message.value=t('配置文件已加密导出，不包含面板账号和密码。请牢记本次设置的备份密码。')}catch(e){error.value=e instanceof Error?t(e.message):t('导出失败')}finally{busy.value=false}}
async function inspect(event:Event){const input=event.target as HTMLInputElement,file=input.files?.[0];archive.value=null;report.value=null;confirmed.value=false;archiveEncrypted.value=false;error.value='';if(!file)return;busy.value=true;try{if(file.size>80*1024*1024)throw Error(t('备份文件不能超过 80 MiB'));const parsed=JSON.parse(await file.text());archive.value=parsed;archiveEncrypted.value=Boolean(parsed?.encrypted)}catch(e){error.value=e instanceof Error?t(e.message):t('无法读取备份')}finally{busy.value=false;input.value=''}
if(!archiveEncrypted.value&&archive.value)void readArchive()
}
async function readArchive(){if(!archive.value||busy.value)return;busy.value=true;error.value='';report.value=null;confirmed.value=false;try{const r=await fetch('/api/v1/config/import/inspect',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify({archive:archive.value,password:importPassword.value})});const body=await r.json();if(!r.ok)throw Error(t(body.error));report.value=body}catch(e){error.value=e instanceof Error?t(e.message):t('无法读取备份')}finally{busy.value=false}}
async function restore(){if(!report.value||!confirmed.value||busy.value)return;busy.value=true;error.value='';try{const r=await fetch('/api/v1/config/import',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify({archive:archive.value,password:importPassword.value,confirmed:true})});const body=await r.json();if(!r.ok)throw Error(t(body.error));emit('restored',body.preferences||{},t(body.message))}catch(e){error.value=e instanceof Error?t(e.message):t('导入失败')}finally{busy.value=false}}
</script>
<template>
<section class="config-transfer panel"><div class="transfer-heading"><span class="transfer-symbol"><Archive :size="25"/></span><div><h2>{{t('配置保存与恢复')}}</h2><p>{{t('保存业务配置与浏览器偏好，不包含面板管理员账号、密码及登录会话。')}}</p></div></div>
<div class="transfer-tabs"><button :class="{selected:mode==='export'}" :disabled="busy" @click="mode='export'"><Download :size="16"/>{{t('导出配置')}}</button><button :class="{selected:mode==='import'}" :disabled="busy" @click="mode='import'"><Upload :size="16"/>{{t('导入配置')}}</button></div>
<div class="transfer-content">
<template v-if="mode==='export'">
<p class="auth-hint">{{t('备份使用你设置的密码加密，文件中包含 DNS 凭据与证书私钥。密码不会保存在服务器，遗失后无法解密该文件。')}}</p>
<label>{{t('备份密码（至少 8 位）')}}<input v-model="password" type="text" class="credential-input" autocomplete="off" autocapitalize="none" spellcheck="false" data-lpignore="true" data-1p-ignore="true" maxlength="1024" :disabled="busy"/></label>
<label>{{t('确认备份密码')}}<input v-model="passwordConfirm" type="text" class="credential-input" autocomplete="off" autocapitalize="none" spellcheck="false" data-lpignore="true" data-1p-ignore="true" maxlength="1024" :disabled="busy"/></label>
<p v-if="password&&password.length<8" class="auth-hint">{{t('备份密码至少需要 8 个字符。')}}</p>
<p v-else-if="passwordConfirm&&password!==passwordConfirm" class="error">{{t('两次输入的备份密码不一致。')}}</p>
<button class="primary" :disabled="busy||!passwordValid" @click="download"><Download :size="16"/>{{busy?t('正在保存…'):t('下载加密配置文件')}}</button>
</template>
<template v-else>
<label>{{t('选择配置文件')}}<input type="file" accept=".json" :disabled="busy" @change="inspect"/></label>
<p v-if="awaitingPassword" class="auth-hint"><KeyRound :size="14"/>{{t('该备份已加密，请输入导出时设置的备份密码。')}}</p>
<template v-if="awaitingPassword">
<label>{{t('备份密码')}}<input v-model="importPassword" type="text" class="credential-input" autocomplete="off" autocapitalize="none" spellcheck="false" data-lpignore="true" data-1p-ignore="true" :disabled="busy" @keyup.enter="readArchive()"/></label>
<button class="secondary" :disabled="busy||importPassword.length<8" @click="readArchive()"><RefreshCw :size="15"/>{{t('解密并预览')}}</button>
</template>
<p v-if="busy" role="status"><RefreshCw :size="15" class="spinning"/>{{t('正在检查配置…')}}</p>
<div v-if="report" class="transfer-report"><h3>{{t('恢复预览')}} <small>{{new Date(report.created).toLocaleString()}}</small></h3><div class="transfer-counts"><span v-for="(count,key) in report.counts" :key="key">{{labels[key]||key}} <b>{{count}}</b></span><span>{{t('证书文件')}} <b>{{report.files}}</b></span></div><p>{{t('导入将替换业务配置，保留当前管理员账号与登录状态；操作前自动保存恢复快照。')}}</p><label class="transfer-confirm"><input v-model="confirmed" type="checkbox" :disabled="busy"/>{{t('我已核对恢复范围，确认替换业务配置')}}</label><button class="danger-button" :disabled="!confirmed||busy" @click="restore">{{busy?t('正在恢复，请稍候…'):t('确认导入并恢复')}}</button></div>
</template>
</div>
<p v-if="error" class="error" role="alert">{{error}}</p><p v-if="message" class="probe-ok" role="status">{{message}}</p></section>
</template>

<style scoped>
/* 表单改为“标签在上、输入框整行”，避免标签与输入框在同一行互相挤压 */
.transfer-content > label:not(.transfer-confirm) {
  display: grid;
  gap: 7px;
  max-width: 360px;
  margin: 12px 0 0;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.01em;
  color: var(--muted);
}
.transfer-content > label:not(.transfer-confirm) > input {
  width: 100%;
  min-height: 38px;
  padding: 8px 11px;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface-2);
  color: var(--text);
  font-size: 13px;
  font-weight: 500;
}
.transfer-content > label:not(.transfer-confirm) > input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
  outline: none;
}
.transfer-content > label:not(.transfer-confirm) > input[type="file"] {
  padding: 7px 9px;
  font-size: 12px;
  cursor: pointer;
}
.transfer-content > button {
  margin-top: 16px;
  justify-self: start;
}
.transfer-content > .error,
.transfer-content > .probe-ok {
  margin: 10px 0 0;
}
@media (max-width: 760px) {
  .transfer-content > label:not(.transfer-confirm) {
    max-width: none;
  }
  .transfer-content > button {
    width: 100%;
    justify-content: center;
  }
}
</style>
