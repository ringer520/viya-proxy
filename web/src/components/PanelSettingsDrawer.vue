<script setup lang="ts">
import { apiFetch as fetch } from "../api";

import {t,tr} from '../i18n'
import { confirmDeletion } from "../composables/useState";


import LanguagePicker from './LanguagePicker.vue'
import { ref, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { X, Settings, Palette, RefreshCw, Server, KeyRound, Copy, Bell, Trash2, Download, CheckCircle2, Loader2 } from '@lucide/vue'
const theme=defineModel<'system'|'light'|'dark'|'aurora'|'ember'|'cyber'>('theme',{required:true})
const compact=defineModel<boolean>('compact',{required:true})
const simpleDisplay=defineModel<boolean>('simpleDisplay',{required:true})
const interval=defineModel<number>('interval',{required:true})
const autoRefresh=defineModel<boolean>('autoRefresh',{required:true})
const pauseHidden=defineModel<boolean>('pauseHidden',{required:true})
const charts=defineModel<boolean>('charts',{required:true})
const previewLimit=defineModel<number>('previewLimit',{required:true})
const landing=defineModel<string>('landing',{required:true})
const port=defineModel<number>('port',{required:true})
const password=defineModel<{current:string;next:string;confirm:string}>('password',{required:true})
const props=defineProps<{busy:boolean;loading:boolean;error:string;message:string;accessAddress:string;accessPort:string;username:string;sessions:number|null;csrf:string;role:'admin'|'viewer';pages:{id:string;label:string}[]}>()
// Extra accounts are how a household shares the panel without handing out the
// administrator password; viewers can read every page but cannot save anything.
const users=ref<{id:number;username:string;role:string;created_at:string}[]>([])
const userForm=ref({username:'',password:'',role:'viewer'})
const userBusy=ref(false),userNote=ref(''),userError=ref('')
async function loadUsers(){
 if(props.role!=='admin')return
 try{const r=await fetch('/api/v1/users');if(r.ok)users.value=await r.json()}catch{userError.value=t('读取账号列表失败')}
}
async function addUser(){
 userBusy.value=true;userError.value='';userNote.value=''
 try{const r=await fetch('/api/v1/users',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify(userForm.value)});const d=await r.json().catch(()=>({}));if(!r.ok)throw Error(d.error||'create failed');userForm.value={username:'',password:'',role:'viewer'};userNote.value=t('账号已创建');await loadUsers()}catch(e){userError.value=e instanceof Error?t(e.message):t('创建失败')}finally{userBusy.value=false}
}
async function setUserRole(id:number,role:string){
 userBusy.value=true;userError.value='';userNote.value=''
 try{const r=await fetch(`/api/v1/users/${id}`,{method:'PUT',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify({role})});const d=await r.json().catch(()=>({}));if(!r.ok)throw Error(d.error||'update failed');userNote.value=t('权限已更新');await loadUsers()}catch(e){userError.value=e instanceof Error?t(e.message):t('更新失败')}finally{userBusy.value=false}
}
async function removeUser(id:number,username:string){
 if(!await confirmDeletion(tr`确定删除账号 ${username} 吗？`))return
 userBusy.value=true;userError.value='';userNote.value=''
 try{const r=await fetch(`/api/v1/users/${id}`,{method:'DELETE',headers:{'X-CSRF-Token':props.csrf}});const d=await r.json().catch(()=>({}));if(!r.ok)throw Error(d.error||'delete failed');userNote.value=t('账号已删除');await loadUsers()}catch(e){userError.value=e instanceof Error?t(e.message):t('删除失败')}finally{userBusy.value=false}
}
const emit=defineEmits<{close:[];savePort:[];changePassword:[];changeUsername:[username:string,password:string];revokeSessions:[];copyAddress:[];resetPreferences:[]}>()
const usernameForm=ref({username:props.username,password:''})
// Alert delivery is the only channel that reaches the operator when the panel
// is closed, so it is configured here next to the account it belongs to.
const alertForm=ref({enabled:false,webhook_url:'',notify_recovery:false}),alertBusy=ref(false),alertNote=ref(''),alertError=ref('')
async function loadAlerts(){
 try{const r=await fetch('/api/v1/alert-settings');if(!r.ok)throw Error(t('读取通知设置失败'));const d=await r.json();alertForm.value={enabled:!!d.enabled,webhook_url:typeof d.webhook_url==='string'?d.webhook_url:'',notify_recovery:!!d.notify_recovery}}catch{alertError.value=t('读取通知设置失败')}
}
async function saveAlerts(){
 alertBusy.value=true;alertError.value='';alertNote.value=''
 try{const r=await fetch('/api/v1/alert-settings',{method:'PUT',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify(alertForm.value)});const d=await r.json().catch(()=>({}));if(!r.ok)throw Error(d.error||'save failed');alertNote.value=t('通知设置已保存')}catch(e){alertError.value=e instanceof Error?t(e.message):t('保存失败')}finally{alertBusy.value=false}
}
async function testAlerts(){
 alertBusy.value=true;alertError.value='';alertNote.value=''
 try{const r=await fetch('/api/v1/alert-settings/test',{method:'POST',headers:{'X-CSRF-Token':props.csrf}});const d=await r.json().catch(()=>({}));if(!r.ok)throw Error(d.error||'send failed');alertNote.value=t('测试通知已发送，请检查接收端')}catch(e){alertError.value=e instanceof Error?t(e.message):t('发送失败')}finally{alertBusy.value=false}
}
// Version check / self-update. The drawer talks to /api/v1/version directly;
// the update endpoint swaps the running binary and the service manager brings
// the panel back up on the new build.
const verCurrent=ref(''),verLatest=ref(''),verUpdate=ref(false),verDocker=ref(false),verManaged=ref(false),verNotes=ref(''),verURL=ref('')
const verBusy=ref(false),verNote=ref(''),verError=ref(''),verChecking=ref(false)
async function loadVersion(){
 try{const r=await fetch('/api/v1/version');const d=await r.json();verCurrent.value=d.current||'';verLatest.value=d.latest||'';verUpdate.value=!!d.update_available;verDocker.value=!!d.in_docker;verManaged.value=!!d.managed;verNotes.value=d.notes||'';verURL.value=d.release_url||'';if(d.error)verError.value=d.error}catch{verError.value=t('读取版本信息失败')}
}
async function checkUpdate(){
 verChecking.value=true;verError.value='';verNote.value=''
 try{const r=await fetch('/api/v1/version');const d=await r.json();verCurrent.value=d.current||'';verLatest.value=d.latest||'';verUpdate.value=!!d.update_available;verDocker.value=!!d.in_docker;verManaged.value=!!d.managed;verNotes.value=d.notes||'';verURL.value=d.release_url||'';if(d.error){verError.value=d.error}else{verNote.value=d.update_available?t('发现新版本'):t('当前已是最新版本')}}catch{verError.value=t('检查更新失败，请检查服务器网络')}finally{verChecking.value=false}
}
async function applyUpdate(){
 verBusy.value=true;verError.value='';verNote.value=''
 try{const r=await fetch('/api/v1/version/update',{method:'POST',headers:{'X-CSRF-Token':props.csrf}});const d=await r.json().catch(()=>({}));if(!r.ok)throw Error(d.error||'update failed');verNote.value=t('更新完成，服务正在重启…');verUpdate.value=false}catch(e){verError.value=e instanceof Error?t(e.message):t('更新失败')}finally{verBusy.value=false}
}
onMounted(()=>{loadAlerts();loadUsers();loadVersion()})
watch(()=>props.username,value=>{usernameForm.value={username:value,password:''}})
const tab=ref('appearance'),root=ref<HTMLElement|null>(null),body=ref<HTMLElement|null>(null),restartConfirmed=ref(false)
const tabs=[{id:'appearance',label:t('外观与显示'),icon:Palette},{id:'refresh',label:t('刷新与日志'),icon:RefreshCw},{id:'access',label:t('面板访问'),icon:Server},{id:'account',label:t('账户与会话'),icon:KeyRound},{id:'alerts',label:t('通知'),icon:Bell}]
// label/description drive the picker; the swatches show the real palette.
const themeOptions=[
 {id:'system',label:t('跟随系统'),hint:t('随系统深浅切换'),base:'linear-gradient(135deg,#f4f6fb 0 50%,#0b1220 50% 100%)',accent:'#6fa3ff'},
 {id:'light',label:t('浅色'),hint:t('中性蓝灰'),base:'#f4f6fb',accent:'#2563eb'},
 {id:'dark',label:t('深色'),hint:t('午夜蓝'),base:'#0b1220',accent:'#6fa3ff'},
 {id:'aurora',label:t('极光'),hint:t('薄荷青绿'),base:'#071a1c',accent:'#34d399'},
 {id:'ember',label:t('琥珀'),hint:t('暖炭金'),base:'#1a1310',accent:'#f5a524'},
 {id:'cyber',label:t('赛博'),hint:t('霓虹紫'),base:'#0d0a1a',accent:'#c084fc'},
] as const
let previousFocus:HTMLElement|null=null,previousOverflow=''
function close(){if(!props.busy)emit('close')}
function keyboard(event:KeyboardEvent){
 if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close()}
 if(event.key!=='Tab')return
 const controls=Array.from(root.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href],[tabindex="0"]')||[]).filter(el=>el.getClientRects().length>0)
 const first=controls[0],last=controls.at(-1)
 if(!first||!last)return
 if(event.shiftKey&&(document.activeElement===first||document.activeElement===root.value)){event.preventDefault();last.focus()}
 else if(!event.shiftKey&&(document.activeElement===last||document.activeElement===root.value)){event.preventDefault();first.focus()}
}
watch(()=>[props.error,props.message],async values=>{if(values.some(Boolean)){await nextTick();body.value?.scrollTo({top:0})}})
onMounted(()=>{previousFocus=document.activeElement as HTMLElement;previousOverflow=document.body.style.overflow;document.body.style.overflow='hidden';root.value?.focus()})
onBeforeUnmount(()=>{document.body.style.overflow=previousOverflow;if(previousFocus?.isConnected)previousFocus.focus()})
</script>

<template>
 <div class="settings-drawer-backdrop">
  <section ref="root" class="settings-drawer" role="dialog" aria-modal="true" aria-labelledby="settings-title" tabindex="-1" @keydown="keyboard">
   <header class="settings-drawer-header"><span class="settings-title-icon"><Settings :size="22"/></span><div><span class="eyebrow">{{t('VIYA / 偏好与管理')}}</span><h2 id="settings-title">{{t('面板设置')}}</h2></div><button class="icon-btn" :disabled="busy" :aria-label="t('关闭面板设置')" @click="close"><X :size="20"/></button></header>
   <nav class="settings-drawer-tabs" role="tablist" :aria-label="t('设置分类')"><button v-for="item in tabs" :id="'settings-tab-'+item.id" :key="item.id" role="tab" :aria-selected="tab===item.id" :aria-controls="'settings-section-'+item.id" :class="{selected:tab===item.id}" @click="tab=item.id"><component :is="item.icon" :size="16"/>{{ item.label }}</button></nav>
   <div ref="body" class="settings-drawer-body">
    <p v-if="loading" class="settings-notice" role="status">{{t('正在读取面板设置…')}}</p>
    <p v-if="error" class="settings-alert" role="alert">{{ error }}</p><p v-if="message" class="settings-notice success" role="status">{{ message }}</p>
    <section v-show="tab==='appearance'" id="settings-section-appearance" role="tabpanel" aria-labelledby="settings-tab-appearance">
     <div class="settings-section-intro"><h3>{{t('让面板符合你的习惯')}}</h3><p>{{t('以下偏好即时生效，保存在当前浏览器。')}}</p></div>
     <div class="settings-card"><h4>{{t('语言 / Language')}}</h4><LanguagePicker/></div>     <div class="settings-card"><h4>{{t('界面主题')}}</h4><div class="drawer-theme-options"><button v-for="item in themeOptions" :key="item.id" :class="{selected:theme===item.id}" :aria-pressed="theme===item.id" :title="item.hint" @click="theme=item.id"><span class="theme-swatch" :style="{background:item.base,'--swatch-accent':item.accent}"><i></i><b></b></span><span class="theme-name">{{ item.label }}</span><small>{{ item.hint }}</small></button></div></div>
     <div class="settings-card"><label class="drawer-setting-row"><span><strong>{{t('简洁显示模式')}}</strong><small>{{t('自动隐藏界面中的说明文字，让页面更简洁')}}</small></span><input v-model="simpleDisplay" type="checkbox"/></label><label class="drawer-setting-row"><span><strong>{{t('紧凑布局')}}</strong><small>{{t('减少站点和任务列表的留白')}}</small></span><input v-model="compact" type="checkbox"/></label><label class="drawer-setting-row"><span><strong>{{t('显示流量趋势图')}}</strong><small>{{t('关闭后保留请求数与流量统计')}}</small></span><input v-model="charts" type="checkbox"/></label><label class="drawer-setting-row"><span><strong>{{t('默认进入页面')}}</strong><small>{{t('未指定页面地址时使用')}}</small></span><select v-model="landing"><option v-for="page in pages" :key="page.id" :value="page.id">{{ page.label }}</option></select></label></div>
     <button class="secondary" @click="emit('resetPreferences')">{{t('恢复浏览器默认偏好')}}</button><p class="drawer-field-help">{{t('仅重置外观、刷新和显示偏好。')}}</p>
    </section>
    <section v-show="tab==='refresh'" id="settings-section-refresh" role="tabpanel" aria-labelledby="settings-tab-refresh">
     <div class="settings-section-intro"><h3>{{t('保持信息更新')}}</h3><p>{{t('控制页面轮询；DDNS 定时任务由服务器独立运行。')}}</p></div>
     <div class="settings-card"><label class="drawer-setting-row"><span><strong>{{t('自动刷新页面数据')}}</strong><small>{{t('流量、DDNS 状态及证书进度')}}</small></span><input v-model="autoRefresh" type="checkbox"/></label><label class="drawer-setting-row"><span><strong>{{t('流量与日志刷新间隔')}}</strong><small>{{t('DDNS 与申请中证书每秒更新')}}</small></span><select v-model.number="interval" :disabled="!autoRefresh"><option :value="5">{{t('5 秒')}}</option><option :value="15">{{t('15 秒')}}</option><option :value="30">{{t('30 秒')}}</option><option :value="60">{{t('60 秒')}}</option></select></label><label class="drawer-setting-row"><span><strong>{{t('页面在后台时暂停刷新')}}</strong><small>{{t('回到页面后立即获取最新数据')}}</small></span><input v-model="pauseHidden" type="checkbox" :disabled="!autoRefresh"/></label></div>
     <div class="settings-card"><label class="drawer-setting-row"><span><strong>{{t('悬浮日志显示条数')}}</strong><small>{{t('鼠标停留在站点日志图标时预览')}}</small></span><select v-model.number="previewLimit"><option :value="3">{{t('3 条')}}</option><option :value="5">{{t('5 条')}}</option><option :value="10">{{t('10 条')}}</option></select></label></div>
    </section>
    <section v-show="tab==='access'" id="settings-section-access" role="tabpanel" aria-labelledby="settings-tab-access">
     <div class="settings-section-intro"><h3>{{t('面板访问')}}</h3><p>{{t('管理入口与 Web 反代监听分别配置。')}}</p></div>
     <div class="settings-card"><h4>{{t('当前访问地址')}}</h4><div class="drawer-address"><code>{{ accessAddress }}</code><button class="icon-btn" :aria-label="t('复制面板地址')" @click="emit('copyAddress')"><Copy :size="17"/></button></div><p class="drawer-field-help">{{t('浏览器访问端口：')}}{{ accessPort }}</p></div>
     <form class="settings-card" @submit.prevent="restartConfirmed&&emit('savePort')"><h4>{{t('面板监听端口')}}</h4><label class="drawer-input-label">{{t('服务器端口')}}<input v-model.number="port" type="number" min="1024" max="65535" required :disabled="busy||loading"/></label><p class="drawer-field-help">{{t('修改后面板服务重启。通过域名反代访问时，需要同步修改对应站点的上游端口。')}}</p><label class="drawer-check-line"><input v-model="restartConfirmed" type="checkbox" :disabled="busy"/>{{t('我已了解访问地址及反代上游需要同步调整')}}</label><button class="primary" :disabled="busy||loading||!restartConfirmed">{{ busy?t('正在处理…'):t('保存端口并重启') }}</button></form>
     <div class="settings-card version-card">
      <h4>{{t('版本与更新')}}</h4>
      <div class="version-row"><span class="version-label">{{t('当前版本')}}</span><code class="version-tag">{{ verCurrent||'—' }}</code></div>
      <div v-if="verLatest" class="version-row"><span class="version-label">{{t('最新版本')}}</span><code class="version-tag" :class="{'version-newer':verUpdate}">{{ verLatest }}</code></div>
      <p v-if="verError" class="settings-alert" role="alert">{{ verError }}</p>
      <p v-if="verNote" class="settings-notice success" role="status">{{ verNote }}</p>
      <p v-if="verDocker" class="drawer-field-help">{{t('检测到 Docker 环境：自动更新不可用，请拉取新镜像并重建容器。')}}</p>
      <p v-else-if="!verManaged" class="drawer-field-help">{{t('当前环境不支持自动更新，请手动替换二进制。')}}</p>
      <template v-else>
       <button v-if="verUpdate&&role==='admin'" class="primary version-update-btn" :disabled="verBusy||verChecking" @click="applyUpdate">
        <Loader2 v-if="verBusy" :size="16" class="spin"/><Download v-else :size="16"/>{{ verBusy?t('正在更新…'):t('更新并重启') }}
       </button>
       <p v-if="verUpdate&&role==='viewer'" class="drawer-field-help">{{t('只读账号不能执行更新，请联系管理员。')}}</p>
      </template>
      <button class="secondary" :disabled="verChecking||verBusy" @click="checkUpdate">
       <Loader2 v-if="verChecking" :size="16" class="spin"/><CheckCircle2 v-else :size="16"/>{{ verChecking?t('正在检查…'):t('检查更新') }}
      </button>
      <p v-if="verURL" class="drawer-field-help"><a :href="verURL" target="_blank" rel="noopener">{{t('查看更新日志')}}</a></p>
     </div>
    </section>
    <section v-show="tab==='account'" id="settings-section-account" role="tabpanel" aria-labelledby="settings-tab-account">
     <div class="settings-section-intro"><h3>{{t('账户与会话')}}</h3><p>{{t('当前管理员：')}}{{ username||t('管理员') }}</p></div>
     <form class="settings-card" @submit.prevent="emit('changeUsername',usernameForm.username,usernameForm.password);usernameForm.password=''"><h4>{{t('修改管理员账号')}}</h4><label class="drawer-input-label">{{t('新账号')}}<input v-model="usernameForm.username" minlength="3" maxlength="64" pattern="[A-Za-z0-9_.-]+" autocomplete="username" required :disabled="busy"/></label><label class="drawer-input-label">{{t('当前密码')}}<input v-model="usernameForm.password" type="password" autocomplete="current-password" required :disabled="busy"/></label><p class="drawer-field-help">{{t('支持字母、数字、点、横线和下划线，3–64 位。修改后当前会话保留，其他会话退出。')}}</p><button class="primary" :disabled="busy||usernameForm.username===username">{{t('修改账号')}}</button></form><form class="settings-card drawer-password-form" @submit.prevent="emit('changePassword')"><h4>{{t('修改管理员密码')}}</h4><label class="drawer-input-label">{{t('当前密码')}}<input v-model="password.current" type="password" autocomplete="current-password" required :disabled="busy"/></label><label class="drawer-input-label">{{t('新密码')}}<input v-model="password.next" type="password" autocomplete="new-password" minlength="8" maxlength="72" required :disabled="busy"/></label><label class="drawer-input-label">{{t('确认新密码')}}<input v-model="password.confirm" type="password" autocomplete="new-password" minlength="8" maxlength="72" required :disabled="busy"/></label><p class="drawer-field-help">{{t('至少 8 位。修改成功后其他会话退出，当前会话保留。')}}</p><button class="primary" :disabled="busy">{{t('修改密码')}}</button></form>
     <div v-if="role==='admin'" class="settings-card"><h4>{{t('面板账号')}}</h4><p class="drawer-field-help">{{t('只读账号可以查看所有页面，但不能保存任何修改。至少保留一个管理员账号。')}}</p><p v-if="userError" class="settings-alert" role="alert">{{ userError }}</p><p v-if="userNote" class="settings-notice success" role="status">{{ userNote }}</p><ul v-if="users.length" class="panel-user-list"><li v-for="item in users" :key="item.id"><span class="panel-user-name">{{ item.username }}<small>{{ item.role==='viewer'?t('只读'):t('管理员') }}</small></span><template v-if="item.username!==username"><select :value="item.role" :disabled="userBusy" @change="setUserRole(item.id,($event.target as HTMLSelectElement).value)"><option value="admin">{{t('管理员')}}</option><option value="viewer">{{t('只读')}}</option></select><button class="icon-btn" :disabled="userBusy" :aria-label="t('删除账号')" @click="removeUser(item.id,item.username)"><Trash2 :size="16"/></button></template><small v-else class="panel-user-self">{{t('当前登录')}}</small></li></ul><form class="panel-user-form" @submit.prevent="addUser"><label class="drawer-input-label">{{t('新账号')}}<input v-model="userForm.username" minlength="3" maxlength="64" pattern="[A-Za-z0-9_.-]+" autocomplete="username" required :disabled="userBusy"/></label><label class="drawer-input-label">{{t('密码')}}<input v-model="userForm.password" type="password" autocomplete="new-password" minlength="8" maxlength="72" required :disabled="userBusy"/></label><label class="drawer-input-label">{{t('权限')}}<select v-model="userForm.role" :disabled="userBusy"><option value="viewer">{{t('只读')}}</option><option value="admin">{{t('管理员')}}</option></select></label><button class="primary" :disabled="userBusy">{{t('添加账号')}}</button></form></div>
     <div class="settings-card"><h4>{{t('登录会话')}}</h4><p class="drawer-field-help">{{ sessions===null?t('读取当前会话数量后可管理其他登录。'):tr`当前有 ${sessions} 个有效会话（含当前会话）。` }}</p><button class="secondary" :disabled="busy||loading||sessions===null||sessions<=1" @click="emit('revokeSessions')">{{t('退出其他登录会话')}}</button></div>
    </section>
    <section v-show="tab==='alerts'" id="settings-section-alerts" role="tabpanel" aria-labelledby="settings-tab-alerts">
     <div class="settings-section-intro"><h3>{{t('通知')}}</h3><p>{{t('服务异常时主动推送，不必一直打开面板查看。')}}</p></div>
     <form class="settings-card" @submit.prevent="saveAlerts">
      <h4>{{t('告警 Webhook')}}</h4>
      <label class="drawer-setting-row"><span><strong>{{t('启用通知')}}</strong><small>{{t('覆盖上游故障、证书申请失败、DDNS 连续失败、数据目录磁盘接近写满、Caddy 进程退出')}}</small></span><input v-model="alertForm.enabled" type="checkbox" :disabled="alertBusy"/></label>
      <label class="drawer-input-label">{{t('Webhook 地址')}}<input v-model="alertForm.webhook_url" type="url" placeholder="https://" :disabled="alertBusy||!alertForm.enabled"/></label>
      <label class="drawer-setting-row"><span><strong>{{t('恢复时也通知')}}</strong><small>{{t('故障消除后再发送一条恢复消息')}}</small></span><input v-model="alertForm.notify_recovery" type="checkbox" :disabled="alertBusy||!alertForm.enabled"/></label>
      <p v-if="alertError" class="settings-alert" role="alert">{{ alertError }}</p><p v-if="alertNote" class="settings-notice success" role="status">{{ alertNote }}</p>
      <p class="drawer-field-help">{{t('以 JSON 形式 POST 到该地址，内容为 source / event / title / detail / at。同一事件 10 分钟内只推送一次，避免故障抖动造成刷屏。')}}</p>
      <button class="primary" :disabled="alertBusy">{{t('保存通知设置')}}</button><button type="button" class="secondary" :disabled="alertBusy||!alertForm.enabled" @click="testAlerts">{{t('发送测试通知')}}</button>
     </form>
    </section>
   </div>
   <footer class="settings-drawer-footer"><span>{{t('偏好即时保存 · 端口和密码需单独提交')}}</span><button class="secondary" :disabled="busy" @click="close">{{t('完成')}}</button></footer>
  </section>
 </div>
</template>

<style scoped>
.settings-drawer-backdrop{position:fixed;inset:0;z-index:180;background:#0b142b66;backdrop-filter:blur(4px);display:flex;justify-content:flex-end}.settings-drawer{display:flex;flex-direction:column;width:min(650px,100%);height:100dvh;background:var(--surface);color:var(--text);box-shadow:-20px 0 70px #0002;outline:none;animation:drawer-enter .22s ease-out}.settings-drawer-header{display:flex;align-items:center;gap:14px;padding:25px 28px 22px;border-bottom:1px solid var(--line)}.settings-drawer-header h2{font-size:23px;margin:4px 0 0}.settings-drawer-header>.icon-btn{margin-left:auto}.settings-title-icon{display:grid;place-items:center;width:45px;height:45px;border-radius:14px;background:color-mix(in srgb,var(--accent) 12%,var(--surface));color:var(--accent)}.settings-drawer-tabs{display:flex;gap:6px;padding:16px 24px 0;border-bottom:1px solid var(--line);overflow:auto;flex-shrink:0}.settings-drawer-tabs button{display:flex;gap:7px;align-items:center;justify-content:center;white-space:nowrap;flex:1;border:0;border-bottom:2px solid transparent;background:none;color:var(--muted);padding:12px 5px;font-size:12px;cursor:pointer}.settings-drawer-tabs button.selected{color:var(--accent);border-bottom-color:var(--accent)}.settings-drawer-body{padding:24px 28px;overflow-y:auto;flex:1;overscroll-behavior:contain}.settings-section-intro{margin-bottom:22px}.settings-section-intro h3{font-size:17px;margin:0 0 7px}.settings-section-intro p,.drawer-field-help{color:var(--muted);font-size:12px;line-height:1.7;margin:7px 0 14px}.settings-card{border:1px solid var(--line);border-radius:14px;padding:18px;margin-bottom:16px;background:var(--surface)}.settings-card h4{font-size:13px;margin:0 0 15px}.drawer-theme-options{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.drawer-theme-options button{display:flex;flex-direction:column;align-items:center;gap:12px;border:1px solid var(--line);border-radius:10px;padding:18px 8px;background:var(--surface-2);color:var(--muted);cursor:pointer;font-size:12px}.drawer-theme-options button.selected{border-color:var(--accent);color:var(--accent);background:color-mix(in srgb,var(--accent) 7%,var(--surface))}.drawer-setting-row{display:flex;align-items:center;justify-content:space-between;gap:22px;padding:15px 0;border-bottom:1px solid var(--line);font-size:12px}.drawer-setting-row:first-child{padding-top:0}.drawer-setting-row:last-child{padding-bottom:0;border-bottom:0}.drawer-setting-row strong{font-weight:600;display:block;font-size:13px}.drawer-setting-row small{display:block;font-size:11px;color:var(--muted);margin-top:5px;line-height:1.6}.drawer-setting-row select{max-width:155px}.drawer-setting-row input[type=checkbox],.drawer-check-line input{width:17px;height:17px;accent-color:var(--accent);flex-shrink:0}.settings-drawer select,.drawer-input-label input{border:1px solid var(--line);border-radius:8px;background:var(--surface-2);color:var(--text);padding:10px;font-size:12px}.drawer-input-label{display:grid;gap:8px;font-size:12px;margin:12px 0}.drawer-input-label input{width:100%;min-width:0}.drawer-check-line{display:flex;gap:9px;align-items:flex-start;font-size:12px;line-height:1.6;margin:18px 0}.drawer-check-line input{margin:2px 0}.drawer-address{display:flex;align-items:center;gap:8px;background:var(--surface-2);padding:9px 12px;border-radius:8px}.drawer-address code{flex:1;overflow-wrap:anywhere;font-size:12px}.settings-drawer-footer{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:18px 28px;border-top:1px solid var(--line);margin:0;color:var(--muted);font-size:10px}.settings-alert,.settings-notice{padding:12px 14px;border-radius:9px;font-size:12px;line-height:1.7;margin:0 0 18px}.settings-alert{background:#dc262612;color:#dc2626;border:1px solid #dc262633}.settings-notice{background:var(--surface-2);color:var(--muted)}.settings-notice.success{color:#15803d;background:#16a34a12}.settings-drawer button:focus-visible,.settings-drawer input:focus-visible,.settings-drawer select:focus-visible{outline:2px solid var(--accent);outline-offset:3px}@keyframes drawer-enter{from{transform:translateX(25px);opacity:0}to{transform:translateX(0);opacity:1}}@media(max-width:560px){.settings-drawer-header{padding:20px}.settings-drawer-tabs{padding:10px 10px 0}.settings-drawer-tabs button{font-size:11px;gap:5px}.settings-drawer-body{padding:20px 16px}.settings-drawer-footer{padding:15px 16px}.drawer-setting-row{gap:12px}.drawer-setting-row select{max-width:125px}}@media(prefers-reduced-motion:reduce){.settings-drawer{animation:none}}
.drawer-theme-options{gap:10px}
.drawer-theme-options button{align-items:stretch;gap:8px;padding:10px;text-align:left;border-radius:12px;transition:border-color .16s ease,box-shadow .16s ease,transform .16s ease}
.drawer-theme-options button:hover{transform:translateY(-1px);border-color:color-mix(in srgb,var(--accent) 42%,var(--line))}
.drawer-theme-options button.selected{border-color:var(--accent);box-shadow:0 0 0 3px var(--accent-soft),0 10px 24px -18px var(--glow)}
.theme-swatch{position:relative;display:block;height:44px;border-radius:8px;border:1px solid var(--line);overflow:hidden}
.theme-swatch i{position:absolute;left:9px;top:9px;width:24px;height:7px;border-radius:999px;background:var(--swatch-accent);opacity:.85}
.panel-user-list{list-style:none;margin:0 0 14px;padding:0;display:grid;gap:8px}.panel-user-list li{display:flex;align-items:center;gap:9px;padding:8px 10px;border:1px solid var(--line);border-radius:9px;background:var(--surface-2)}.panel-user-list select{margin-left:auto}.panel-user-name{display:flex;flex-direction:column;font-size:12.5px;font-weight:600}.panel-user-name small{font-weight:400;color:var(--muted);font-size:11px}.panel-user-self{margin-left:auto;font-size:11px;color:var(--muted)}.panel-user-form{display:grid;gap:11px}.panel-user-form .primary{margin-top:2px}
.theme-swatch b{position:absolute;right:8px;bottom:8px;width:10px;height:10px;border-radius:50%;background:var(--swatch-accent);box-shadow:0 0 12px var(--swatch-accent)}
.theme-name{font-size:12.5px;font-weight:650;color:var(--text);line-height:1.2}
.drawer-theme-options button small{font-size:10px;color:var(--muted);line-height:1.3}
@media(max-width:560px){.drawer-theme-options{grid-template-columns:repeat(2,minmax(0,1fr))}}
.version-card .version-row{display:flex;align-items:center;gap:12px;margin:8px 0}.version-label{font-size:12.5px;color:var(--muted);min-width:64px}.version-tag{font-family:var(--font-mono,ui-monospace,monospace);font-size:13px;padding:3px 10px;border-radius:7px;background:var(--surface-2);border:1px solid var(--line)}.version-tag.version-newer{color:var(--accent);border-color:var(--accent);background:color-mix(in srgb,var(--accent) 8%,var(--surface))}.version-update-btn{margin:10px 0 8px;display:flex;align-items:center;gap:8px}.spin{animation:ver-spin 1s linear infinite}@keyframes ver-spin{to{transform:rotate(360deg)}}
</style>
