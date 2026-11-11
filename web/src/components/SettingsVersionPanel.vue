<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Download, ChevronDown, RefreshCw, Loader2 } from '@lucide/vue'
import { apiFetch } from '../api'
import { t } from '../i18n'
const props=defineProps<{role:'admin'|'viewer';csrf:string;blocked:boolean}>()
const emit=defineEmits<{busy:[value:boolean];reloadReady:[]}>()
const identity=ref({commit:'',built_at:'',source:'',schema_version:0})
const current=ref(''),latest=ref(''),available=ref(false),docker=ref(false),managed=ref(false),notes=ref(''),url=ref('')
const checking=ref(false),updating=ref(false),expanded=ref(false),error=ref(''),message=ref(''),progress=ref(0),stage=ref('')
const canUpdate=computed(()=>props.role==='admin' && managed.value && !docker.value && available.value && !props.blocked)
const requests=new AbortController()
let alive=true,poll:ReturnType<typeof setInterval>|undefined,restarting=false,pollBusy=false
async function check(manual=false){
  if(checking.value || updating.value)return
  checking.value=true;error.value='';message.value=''
  try {
    const response=await apiFetch('/api/v1/version',{signal:requests.signal})
    if(!response.ok)throw Error(t('读取版本信息失败'))
    const data=await response.json();if(!alive)return
    identity.value=data;current.value=data.current||'';latest.value=data.latest||'';available.value=!!data.update_available;docker.value=!!data.in_docker;managed.value=!!data.managed;notes.value=data.notes||'';url.value=data.release_url||''
    if(t(data.error))error.value=t(data.error)
    else if(manual)message.value=data.update_available?t('发现新版本'):t('当前已是最新版本')
    if(manual)expanded.value=true
  }catch(e){if(alive)error.value=e instanceof Error?t(e.message):t('检查更新失败，请检查服务器网络')}
  finally{if(alive)checking.value=false}
}
async function pollProgress(){
  if(pollBusy||!alive)return;pollBusy=true
  try {
    const response=await apiFetch('/api/v1/version/update/status',{signal:requests.signal});if(!response.ok)return
    const data=await response.json();if(!alive||(!data.active&&!restarting))return
    progress.value=Math.max(progress.value,Math.min(100,Number(data.percent)||0))
    if(data.stage)stage.value=t(data.stage)
    if(data.stage==='正在重启')restarting=true
  }catch{ /* The old process disappears during restart. */ }
  finally{pollBusy=false}
}
async function waitForVersion(target:string){
  const deadline=Date.now()+120000
  while(alive&&Date.now()<deadline){
    await new Promise(resolve=>setTimeout(resolve,1500));if(!alive)return false
    try {
      const response=await apiFetch('/api/v1/version?local=1',{signal:requests.signal});if(!response.ok)continue
      const data=await response.json()
      if(String(data.current)!==target)continue
      const health=await apiFetch('/api/v1/health',{signal:requests.signal});if(!health.ok)continue
      const status=await health.json();if(status.ok&&status.caddy_ready)return true
    }catch{ /* Keep waiting for the new process and engine. */ }
  }
  return false
}
async function update(){
  if(!canUpdate.value||updating.value)return
  updating.value=true;emit('busy',true);expanded.value=true;error.value='';message.value='';progress.value=0;stage.value=t('准备更新');restarting=false
  const target=latest.value
  poll=setInterval(()=>void pollProgress(),600)
  try {
    let response:Response|undefined
    try {response=await apiFetch('/api/v1/version/update',{method:'POST',headers:{'X-CSRF-Token':props.csrf},signal:requests.signal})}
    catch(e){if(!(e instanceof TypeError)||!alive)throw e;restarting=true}
    if(response){
      const data=await response.json().catch(()=>({}))
      if(!response.ok)throw Error(t(data.error)||t('更新失败'))
      if(data.restart_required){
        progress.value=100;stage.value=t('更新已完成')
        message.value=t('新版本已安装，请在服务器上手动重启 Viya 后刷新页面')
        return
      }
    }
    restarting=true;stage.value=t('正在等待服务重启');progress.value=Math.max(progress.value,95)
    if(!await waitForVersion(target))throw Error(t('服务尚未恢复，请稍后手动刷新页面'))
    if(!alive)return
    progress.value=100;stage.value=t('更新完成');message.value=t('更新完成，正在刷新页面…')
    await new Promise(resolve=>setTimeout(resolve,700));if(alive){emit('reloadReady');window.location.reload()}
  }catch(e){if(alive)error.value=e instanceof Error?t(e.message):t('更新失败')}
  finally{clearInterval(poll);if(alive){updating.value=false;emit('busy',false)}}
}
onMounted(()=>void check())
onBeforeUnmount(()=>{alive=false;requests.abort();clearInterval(poll)})
</script>
<template>
  <section class="settings-version-panel" :class="{available,expanded,updating}">
    <div class="settings-version-strip"><span class="settings-version-mark"><Download :size="20"/></span>
      <button type="button" class="settings-version-toggle" :aria-expanded="expanded" aria-controls="settings-version-details" :disabled="updating" @click="expanded=!expanded"><span class="settings-version-name">Viya <b>{{current||'—'}}</b></span><small v-if="available">{{t('可更新至')}} {{latest}}</small><small v-else>{{checking?t('正在检查…'):t('版本与更新')}}</small><ChevronDown :size="14" :class="{rotated:expanded}"/></button>
      <button type="button" class="settings-version-check" :disabled="checking||updating" @click="check(true)"><Loader2 v-if="checking" :size="13" class="spinning"/><RefreshCw v-else :size="13"/>{{checking?t('正在检查…'):t('检查更新')}}</button>
    </div>
    <div v-if="available&&!expanded&&!updating" class="settings-version-offer"><span>{{t('最新版本')}} <b>{{latest}}</b></span><button v-if="managed&&!docker&&role==='admin'" class="primary" type="button" :disabled="!canUpdate||checking" @click="update"><Download :size="14"/>{{t('更新并重启')}}</button></div>
    <div v-if="expanded||updating" id="settings-version-details" class="settings-version-details">
      <div v-if="updating" class="settings-update-progress" role="status"><div><span>{{stage}}</span><b>{{progress}}%</b></div><progress :value="progress" max="100">{{progress}}%</progress></div>
      <p v-if="error" class="settings-alert" role="alert">{{error}}</p><p v-if="message" class="settings-notice success" role="status">{{message}}</p>
      <p v-if="docker" class="drawer-field-help">{{t('检测到 Docker 环境：自动更新不可用，请拉取新镜像并重建容器。')}}</p>
      <p v-else-if="current&&!managed" class="drawer-field-help">{{t('当前环境不支持自动更新，请手动替换二进制。')}}</p>
      <p v-if="available&&role==='viewer'" class="drawer-field-help">{{t('只读账号不能执行更新，请联系管理员。')}}</p>
      <p v-if="available&&blocked" class="drawer-field-help">{{t('请先保存或放弃未保存的修改，再执行更新。')}}</p>
      <div v-if="available&&managed&&!docker&&role==='admin'" class="settings-update-action"><span>{{t('最新版本')}} <b>{{latest}}</b></span><button class="primary" type="button" :disabled="!canUpdate||updating||checking" @click="update"><Download :size="14"/>{{updating?t('正在更新…'):t('更新并重启')}}</button></div>
      <details v-if="identity.commit" class="settings-release-notes"><summary>{{t('构建信息')}}</summary><p>Commit: <code>{{identity.commit}}</code></p><p>{{t('构建时间')}}: {{identity.built_at}}</p><p>Source: <code style="overflow-wrap:anywhere">{{identity.source}}</code></p><p>{{t('数据库版本')}}: {{identity.schema_version}}</p></details>
      <details v-if="notes" class="settings-release-notes"><summary>{{t('更新说明')}}</summary><pre>{{notes}}</pre></details>
      <a v-if="url" :href="url" target="_blank" rel="noopener noreferrer" class="settings-release-link">{{t('查看更新日志')}} ↗</a>
    </div>
  </section>
</template>
