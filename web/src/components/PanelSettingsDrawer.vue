<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Check, X, Settings, Palette, RefreshCw, ShieldCheck, KeyRound, Bell, Copy, Trash2, ChevronRight, UserRound, AlertCircle, HardDrive } from '@lucide/vue'
import { apiFetch } from '../api'
import { t, tr } from '../i18n'
import { openSecurityDefaults } from '../composables/useSecurity'
import { confirmDeletion, deletePrompt } from '../composables/useState'
import LanguagePicker from './LanguagePicker.vue'
import SettingsVersionPanel from './SettingsVersionPanel.vue'
import StorageSettings from './StorageSettings.vue'
import { animeThemes } from '../animeThemes'
import type { ThemeChoice } from '../types'
const theme=defineModel<ThemeChoice>('theme',{required:true})
const compact=defineModel<boolean>('compact',{required:true})
const simpleDisplay=defineModel<boolean>('simpleDisplay',{required:true})
const interval=defineModel<number>('interval',{required:true})
const autoRefresh=defineModel<boolean>('autoRefresh',{required:true})
const pauseHidden=defineModel<boolean>('pauseHidden',{required:true})
const charts=defineModel<boolean>('charts',{required:true})
const previewLimit=defineModel<number>('previewLimit',{required:true})
const landing=defineModel<string>('landing',{required:true})
const showFileServices=defineModel<boolean>('showFileServices',{required:true})
const port=defineModel<number>('port',{required:true})
const password=defineModel<{current:string;next:string;confirm:string}>('password',{required:true})
const props=defineProps<{busy:boolean;loading:boolean;error:string;message:string;savedPort:number;accessAddress:string;accessPort:string;username:string;sessions:number|null;csrf:string;role:'admin'|'viewer';pages:{id:string;label:string}[]}>()
const emit=defineEmits<{close:[];savePort:[];changePassword:[];changeUsername:[username:string,password:string];revokeSessions:[];copyAddress:[];resetPreferences:[]}>()
const tabs=[{id:'appearance',label:t('外观与显示'),icon:Palette},{id:'refresh',label:t('自动刷新'),icon:RefreshCw},{id:'access',label:t('访问与安全'),icon:ShieldCheck},{id:'account',label:t('账户'),icon:KeyRound},{id:'alerts',label:t('通知'),icon:Bell},{id:'storage',label:t('存储'),icon:HardDrive}] as const
type Tab=typeof tabs[number]['id']
const savedTab=localStorage.getItem('viya-settings-tab')
const tab=ref<Tab>(tabs.some(item=>item.id===savedTab)?savedTab as Tab:'appearance')
const storageVisited=ref(tab.value==='storage')
const root=ref<HTMLElement|null>(null),body=ref<HTMLElement|null>(null)
const storageBusy=ref(false),storageDirty=ref(false)
const feedbackScope=ref<Tab|'general'>('general'),restartConfirmed=ref(false),versionBusy=ref(false),discardOpen=ref(false),allowReload=ref(false)
const admin=computed(()=>props.role==='admin')
const usernameForm=ref({username:props.username,password:''})
type PanelUser={id:number;username:string;role:'admin'|'viewer';created_at:string}
const users=ref<PanelUser[]>([]),userForm=ref({username:'',password:'',role:'viewer'})
const userBusy=ref(false),userError=ref(''),userNote=ref(''),usersReady=ref(false)
const alertForm=ref({enabled:false,webhook_url:'',notify_recovery:false}),alertSaved=ref(''),alertReady=ref(false)
const alertBusy=ref(false),alertError=ref(''),alertNote=ref('')
const alertDirty=computed(()=>alertReady.value && JSON.stringify(alertForm.value)!==alertSaved.value)
const accessForm=ref({block_public_ip_host:false,hosts:'',ips:'',https_only:false}),accessSaved=ref(''),accessReady=ref(false),accessCurrent=ref({host:'',ip:''})
const accessBusy=ref(false),accessError=ref(''),accessNote=ref('')
const accessDirty=computed(()=>accessReady.value && JSON.stringify(accessForm.value)!==accessSaved.value)
const edited=ref({port:false,username:false,password:false,user:false})
function markEdited(kind:keyof typeof edited.value,event:Event){
 const form=event.currentTarget as HTMLElement
 if(form.contains(document.activeElement))edited.value[kind]=true
}
const portDirty=computed(()=>edited.value.port&&!props.loading&&Number(port.value)!==Number(props.savedPort))
const dirty=computed(()=>storageDirty.value||portDirty.value || (edited.value.password&&!!(password.value.current||password.value.next||password.value.confirm)) || (edited.value.username&&(usernameForm.value.username!==props.username||!!usernameForm.value.password)) || (edited.value.user&&!!(userForm.value.username||userForm.value.password||userForm.value.role!=='viewer')) || alertDirty.value || accessDirty.value)
const locked=computed(()=>storageBusy.value||props.busy||userBusy.value||alertBusy.value||accessBusy.value||versionBusy.value||!!deletePrompt.value)
const showFeedback=computed(()=>feedbackScope.value==='general'||feedbackScope.value===tab.value)
const reads=new AbortController()
let mounted=true
async function loadUsers(){
 if(!admin.value)return
 userError.value=''
 try{const response=await apiFetch('/api/v1/users',{signal:reads.signal});if(!response.ok)throw Error(t('读取账号列表失败'));const data=await response.json();if(mounted){users.value=data;usersReady.value=true}}
 catch(e){if(mounted)userError.value=e instanceof Error?t(e.message):t('读取账号列表失败')}
}
async function addUser(){
 if(!admin.value||locked.value)return
 userBusy.value=true;userError.value='';userNote.value=''
 try{const response=await apiFetch('/api/v1/users',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify(userForm.value)});const data=await response.json().catch(()=>({}));if(!response.ok)throw Error(data.error||t('创建失败'));userForm.value={username:'',password:'',role:'viewer'};userNote.value=t('账号已创建');await loadUsers()}
 catch(e){userError.value=e instanceof Error?t(e.message):t('创建失败')}
 finally{userBusy.value=false}
}
async function setUserRole(user:PanelUser,event:Event){
 const target=event.target as HTMLSelectElement
 if(!admin.value||locked.value){target.value=user.role;return}
 userBusy.value=true;userError.value='';userNote.value=''
 try{const response=await apiFetch(`/api/v1/users/${user.id}`,{method:'PUT',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify({role:target.value})});const data=await response.json().catch(()=>({}));if(!response.ok)throw Error(data.error||t('更新失败'));userNote.value=t('权限已更新');await loadUsers()}
 catch(e){userError.value=e instanceof Error?t(e.message):t('更新失败')}
 finally{target.value=users.value.find(item=>item.id===user.id)?.role||user.role;userBusy.value=false}
}
async function removeUser(user:PanelUser){
 if(!admin.value||locked.value)return
 if(!await confirmDeletion(tr`确定删除账号 ${user.username} 吗？`))return
 userBusy.value=true;userError.value='';userNote.value=''
 try{const response=await apiFetch(`/api/v1/users/${user.id}`,{method:'DELETE',headers:{'X-CSRF-Token':props.csrf}});const data=await response.json().catch(()=>({}));if(!response.ok)throw Error(data.error||t('删除失败'));userNote.value=t('账号已删除');await loadUsers()}
 catch(e){userError.value=e instanceof Error?t(e.message):t('删除失败')}
 finally{userBusy.value=false}
}
async function loadAlerts(){
 if(!admin.value)return
 alertError.value=''
 try{const response=await apiFetch('/api/v1/alert-settings',{signal:reads.signal});if(!response.ok)throw Error(t('读取通知设置失败'));const data=await response.json();if(!mounted)return;alertForm.value={enabled:!!data.enabled,webhook_url:typeof data.webhook_url==='string'?data.webhook_url:'',notify_recovery:!!data.notify_recovery};alertSaved.value=JSON.stringify(alertForm.value);alertReady.value=true}
 catch(e){if(mounted)alertError.value=e instanceof Error?t(e.message):t('读取通知设置失败')}
}
async function saveAlerts(){
 if(!admin.value||locked.value||!alertReady.value||!alertDirty.value)return
 alertBusy.value=true;alertError.value='';alertNote.value=''
 const draft={...alertForm.value,webhook_url:alertForm.value.webhook_url.trim()}
 try{const response=await apiFetch('/api/v1/alert-settings',{method:'PUT',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify(draft)});const data=await response.json().catch(()=>({}));if(!response.ok)throw Error(data.error||t('保存失败'));alertForm.value=draft;alertSaved.value=JSON.stringify(draft);alertNote.value=t('通知设置已保存')}
 catch(e){alertError.value=e instanceof Error?t(e.message):t('保存失败')}
 finally{alertBusy.value=false}
}
async function testAlerts(){
 if(!admin.value||locked.value||!alertReady.value||alertDirty.value||!JSON.parse(alertSaved.value).enabled)return
 alertBusy.value=true;alertError.value='';alertNote.value=''
 try{const response=await apiFetch('/api/v1/alert-settings/test',{method:'POST',headers:{'X-CSRF-Token':props.csrf}});const data=await response.json().catch(()=>({}));if(!response.ok)throw Error(data.error||t('发送失败'));alertNote.value=t('测试通知已发送，请检查接收端')}
 catch(e){alertError.value=e instanceof Error?t(e.message):t('发送失败')}
 finally{alertBusy.value=false}
}
function splitRules(raw:string){return raw.split(/[\n,]/).map(item=>item.trim()).filter(Boolean)}
async function loadAccess(){
 if(!admin.value)return
 try{const response=await apiFetch('/api/v1/panel/access',{signal:reads.signal});const data=await response.json().catch(()=>({}));if(!response.ok)throw Error(data.error||t('读取访问规则失败'));if(!mounted)return
  const next={block_public_ip_host:!!data.block_public_ip_host,hosts:(data.hosts||[]).join('\n'),ips:(data.ips||[]).join('\n'),https_only:!!data.https_only}
  accessForm.value=next;accessSaved.value=JSON.stringify(next);accessCurrent.value={host:data.current_host||'',ip:data.current_ip||''};accessReady.value=true}
 catch(e){if(mounted&&(e as Error).name!=='AbortError')accessError.value=e instanceof Error?t(e.message):t('读取访问规则失败')}
}
async function saveAccess(){
 if(!admin.value||locked.value||!accessReady.value||!accessDirty.value)return
 accessBusy.value=true;accessError.value='';accessNote.value=''
 try{const response=await apiFetch('/api/v1/panel/access',{method:'PUT',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify({block_public_ip_host:accessForm.value.block_public_ip_host,hosts:splitRules(accessForm.value.hosts),ips:splitRules(accessForm.value.ips),https_only:accessForm.value.https_only})})
  const data=await response.json().catch(()=>({}));if(!response.ok)throw Error(data.error||t('保存失败'))
  const next={block_public_ip_host:!!data.block_public_ip_host,hosts:(data.hosts||[]).join('\n'),ips:(data.ips||[]).join('\n'),https_only:!!data.https_only}
  accessForm.value=next;accessSaved.value=JSON.stringify(next);accessNote.value=t('面板访问规则已保存')}
 catch(e){accessError.value=e instanceof Error?t(e.message):t('保存失败')}
 finally{accessBusy.value=false}
}
function submitPort(){if(admin.value&&!locked.value&&portDirty.value&&restartConfirmed.value){feedbackScope.value='access';emit('savePort')}}
function submitUsername(){if(admin.value&&!locked.value){feedbackScope.value='account';emit('changeUsername',usernameForm.value.username,usernameForm.value.password)}}
function submitPassword(){if(!locked.value){feedbackScope.value='account';emit('changePassword')}}
function revokeSessions(){if(admin.value&&!locked.value){feedbackScope.value='account';emit('revokeSessions')}}
function copyAddress(){feedbackScope.value='access';emit('copyAddress')}
function finishClose(){port.value=props.savedPort;password.value={current:'',next:'',confirm:''};emit('close')}
function close(){if(locked.value)return;if(dirty.value){discardOpen.value=true;return}finishClose()}
async function discard(){if(locked.value)return;discardOpen.value=false;await nextTick();finishClose()}
defineExpose({requestClose:close})
function beforeUnload(event:BeforeUnloadEvent){if(!allowReload.value&&(dirty.value||locked.value)){event.preventDefault();event.returnValue=''}}
function selectTab(next:Tab){tab.value=next;body.value?.scrollTo({top:0,behavior:'instant'})}
async function tabKeyboard(event:KeyboardEvent){
 if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return
 event.preventDefault();event.stopPropagation()
 const index=tabs.findIndex(item=>item.id===tab.value)
 const next=event.key==='Home'?0:event.key==='End'?tabs.length-1:(index+(event.key==='ArrowRight'?1:-1)+tabs.length)%tabs.length
 selectTab(tabs[next]!.id);await nextTick();root.value?.querySelector<HTMLElement>(`#settings-tab-${tab.value}`)?.focus()
}
function keyboard(event:KeyboardEvent){
 if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close();return}
 if(event.key!=='Tab')return
 const controls=Array.from(root.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href],summary,[tabindex="0"]')||[]).filter(el=>el.checkVisibility?el.checkVisibility():el.getClientRects().length>0)
 const first=controls[0],last=controls.at(-1);if(!first||!last)return
 if(event.shiftKey&&(document.activeElement===first||document.activeElement===root.value)){event.preventDefault();last.focus()}
 else if(!event.shiftKey&&(document.activeElement===last||document.activeElement===root.value)){event.preventDefault();first.focus()}
}
watch(tab,next=>{localStorage.setItem('viya-settings-tab',next);if(next==='storage')storageVisited.value=true})
watch(port,()=>{restartConfirmed.value=false})
watch(()=>props.username,value=>{usernameForm.value={username:value,password:''}})
watch(()=>[props.error,props.message],async values=>{if(values.some(Boolean)){await nextTick();body.value?.scrollTo({top:0})}})
let previousFocus:HTMLElement|null=null,previousOverflow=''
onMounted(()=>{previousFocus=document.activeElement as HTMLElement;previousOverflow=document.body.style.overflow;document.body.style.overflow='hidden';root.value?.focus();window.addEventListener('beforeunload',beforeUnload);void loadUsers();void loadAlerts();void loadAccess()})
onBeforeUnmount(()=>{mounted=false;reads.abort();window.removeEventListener('beforeunload',beforeUnload);document.body.style.overflow=previousOverflow;if(previousFocus?.isConnected)previousFocus.focus()})
const themeOptions=[
 {id:'system',label:t('跟随系统'),hint:t('随系统深浅切换'),base:'linear-gradient(135deg,#f4f6f9 0 50%,#10151e 50% 100%)',accent:'#91b4fb'},
 {id:'light',label:t('浅色'),hint:t('中性蓝灰'),base:'#f4f6f9',accent:'#416be0'},
 {id:'clay',label:t('陶土'),hint:t('暖米白'),base:'#f7f3ed',accent:'#c25534'},
 {id:'dark',label:t('深色'),hint:t('午夜蓝'),base:'#10151e',accent:'#91b4fb'},
 {id:'aurora',label:t('极光'),hint:t('薄荷青绿'),base:'#101b1d',accent:'#80d9be'},
 {id:'ember',label:t('琥珀'),hint:t('暖炭金'),base:'#1c1916',accent:'#dfba7f'},
 {id:'cyber',label:t('赛博'),hint:t('雾紫'),base:'#191621',accent:'#c4acf2'},
] as const
</script>

<template>
  <div class="settings-drawer-backdrop">
    <section ref="root" class="settings-drawer panel-settings" role="dialog" aria-modal="true" aria-labelledby="settings-title" tabindex="-1" @keydown="keyboard">
      <header class="settings-drawer-header"><span class="settings-title-icon"><Settings :size="20"/></span><div><h2 id="settings-title">{{t('面板设置')}}</h2><span>{{t('偏好与管理')}}</span></div><button type="button" class="icon-btn" :disabled="locked" :aria-label="t('关闭面板设置')" @click="close"><X :size="19"/></button></header>
      <SettingsVersionPanel :role="role" :csrf="csrf" :blocked="dirty||storageBusy||busy||userBusy||alertBusy||loading" @busy="versionBusy=$event" @reload-ready="allowReload=true"/>
      <nav class="settings-drawer-tabs" role="tablist" :aria-label="t('设置分类')" @keydown="tabKeyboard"><button v-for="item in tabs" :id="'settings-tab-'+item.id" :key="item.id" type="button" role="tab" :tabindex="tab===item.id?0:-1" :aria-selected="tab===item.id" :aria-controls="'settings-section-'+item.id" :class="{selected:tab===item.id}" @click="selectTab(item.id)"><component :is="item.icon" :size="15"/><span>{{item.label}}</span></button></nav>
      <div ref="body" class="settings-drawer-body">
        <p v-if="loading" class="settings-notice" role="status">{{t('正在读取面板设置…')}}</p>
        <p v-if="showFeedback&&error" class="settings-alert" role="alert">{{error}}</p><p v-if="showFeedback&&message" class="settings-notice success" role="status">{{message}}</p>
        <section v-show="tab==='storage'" id="settings-section-storage" class="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-storage"><StorageSettings v-if="storageVisited" :csrf="csrf" :admin="admin" :blocked="busy||userBusy||alertBusy||versionBusy" @busy="storageBusy=$event" @dirty="storageDirty=$event"/></section>
        <section v-show="tab==='appearance'" id="settings-section-appearance" class="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-appearance">
          <div class="settings-section-heading"><h3>{{t('外观与显示')}}</h3><span>{{t('当前浏览器 · 即时生效')}}</span></div>
          <div class="settings-row"><span><strong>{{t('语言 / Language')}}</strong></span><LanguagePicker/></div>
          <div class="settings-group settings-theme-group"><h4>{{t('界面主题')}}</h4><div class="settings-theme-grid"><button v-for="item in themeOptions" :key="item.id" type="button" :class="{selected:theme===item.id}" :aria-pressed="theme===item.id" @click="theme=item.id"><span class="settings-theme-preview" :style="{background:item.base,'--swatch-accent':item.accent}"><i/><b/><em/></span><span class="settings-theme-label">{{item.label}}<small>{{item.hint}}</small></span><span v-if="theme===item.id" class="settings-theme-selected">✓</span></button></div></div>
          <div class="settings-group anime-theme-group"><h4>{{t('二次元典藏')}}<small>{{t('原创人物 · 全景背景')}}</small></h4><div class="anime-theme-grid"><button v-for="item in animeThemes" :key="item.id" type="button" :class="{selected:theme===item.id}" :aria-pressed="theme===item.id" :style="{'--card-accent': item.accent}" @click="theme=item.id"><img :src="'/themes/'+item.id+'-preview.webp'" alt="" width="480" height="270" loading="lazy"/><span class="anime-theme-card-label"><strong>{{t(item.label)}}</strong><small>{{t(item.hint)}}</small></span><span v-if="theme===item.id" class="anime-theme-selected" aria-hidden="true">✓</span></button></div></div>
          <div class="settings-group settings-rows">
            <label class="settings-row"><span><strong>{{t('简洁显示模式')}}</strong><small>{{t('自动隐藏界面中的说明文字，让页面更简洁')}}</small></span><input v-model="simpleDisplay" class="settings-switch" type="checkbox" role="switch"/></label>
            <label class="settings-row"><span><strong>{{t('紧凑布局')}}</strong><small>{{t('减少站点和任务列表的留白')}}</small></span><input v-model="compact" class="settings-switch" type="checkbox" role="switch"/></label>
            <label class="settings-row"><span><strong>{{t('显示流量趋势图')}}</strong><small>{{t('关闭后保留请求数与流量统计')}}</small></span><input v-model="charts" class="settings-switch" type="checkbox" role="switch"/></label>
            <label class="settings-row"><span><strong>{{t('显示文件服务')}}</strong><small>{{t('在侧边栏显示文件服务入口')}}</small></span><input v-model="showFileServices" class="settings-switch" type="checkbox" role="switch"/></label>
            <label class="settings-row"><span><strong>{{t('默认进入页面')}}</strong><small>{{t('未指定页面地址时使用')}}</small></span><select v-model="landing"><option v-for="page in pages" :key="page.id" :value="page.id">{{page.label}}</option></select></label>
          </div>
          <button class="settings-text-button" type="button" @click="emit('resetPreferences')">{{t('恢复浏览器默认偏好')}}</button>
        </section>
        <section v-show="tab==='refresh'" id="settings-section-refresh" class="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-refresh">
          <div class="settings-section-heading"><h3>{{t('自动刷新')}}</h3><span>{{t('当前浏览器 · 即时生效')}}</span></div>
          <div class="settings-group settings-rows">
            <label class="settings-row"><span><strong>{{t('自动刷新页面数据')}}</strong><small>{{t('流量、DDNS 状态及证书进度')}}</small></span><input v-model="autoRefresh" class="settings-switch" type="checkbox" role="switch"/></label>
            <label class="settings-row"><span><strong>{{t('流量与日志刷新间隔')}}</strong></span><select v-model.number="interval" :disabled="!autoRefresh"><option :value="5">{{t('5 秒')}}</option><option :value="15">{{t('15 秒')}}</option><option :value="30">{{t('30 秒')}}</option><option :value="60">{{t('60 秒')}}</option></select></label>
            <label class="settings-row"><span><strong>{{t('页面在后台时暂停刷新')}}</strong><small>{{t('回到页面后立即获取最新数据')}}</small></span><input v-model="pauseHidden" class="settings-switch" type="checkbox" role="switch" :disabled="!autoRefresh"/></label>
            <label class="settings-row"><span><strong>{{t('悬浮日志显示条数')}}</strong><small>{{t('鼠标停留在站点日志图标时预览')}}</small></span><select v-model.number="previewLimit"><option :value="3">{{t('3 条')}}</option><option :value="5">{{t('5 条')}}</option><option :value="10">{{t('10 条')}}</option></select></label>
          </div>
          <p class="drawer-field-help">{{t('这里只控制页面刷新，DDNS 定时更新和证书续期仍由服务器独立运行。')}}</p>
        </section>
        <section v-show="tab==='access'" id="settings-section-access" class="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-access">
          <div class="settings-section-heading"><h3>{{t('访问与安全')}}</h3><span>{{t('服务器设置 · 保存后生效')}}</span></div>
          <div class="settings-group"><h4>{{t('当前访问地址')}}</h4><div class="settings-address"><code>{{accessAddress}}</code><button type="button" class="icon-btn" :aria-label="t('复制面板地址')" @click="copyAddress"><Copy :size="15"/></button></div><p class="drawer-field-help">{{t('浏览器访问端口：')}}{{accessPort}}</p></div>
          <form v-if="admin" class="settings-group" @input.capture="markEdited('port',$event)" @change.capture="markEdited('port',$event)" autocomplete="off" @submit.prevent="submitPort"><h4>{{t('面板监听端口')}}</h4><label class="settings-row"><span><strong>{{t('服务器端口')}}</strong><small>{{t('管理入口与 Web 反代监听分别配置。')}}</small></span><input v-model.number="port" class="settings-port-input" type="number" min="1024" max="65535" required :disabled="locked||loading"/></label><label class="settings-confirm-line"><input v-model="restartConfirmed" type="checkbox" :disabled="locked||!portDirty"/><span>{{t('修改后服务会重启；通过域名访问时，请同步调整反代上游端口。')}}</span></label><button class="primary" :disabled="locked||loading||!portDirty||!restartConfirmed">{{busy?t('正在处理…'):t('保存端口并重启')}}</button></form>
          <form v-if="admin" class="settings-group" @submit.prevent="saveAccess" autocomplete="off"><h4>{{t('面板访问')}}</h4>
            <p v-if="accessError" class="settings-alert" role="alert">{{accessError}}</p>
            <p v-if="accessNote" class="settings-notice success" role="status">{{accessNote}}</p>
            <button v-if="!accessReady&&accessError" class="secondary" type="button" @click="loadAccess">{{t('重试')}}</button>
            <template v-else>
              <label class="settings-row"><span><strong>{{t('禁止从公网用 IP 打开面板')}}</strong><small>{{t('配好 HTTPS 后建议开启；域名访问和局域网 IP 都不受影响，本机连接始终可用。')}}</small></span><input v-model="accessForm.block_public_ip_host" class="settings-switch" type="checkbox" role="switch" :disabled="locked||!accessReady"/></label>
              <p class="drawer-field-help">{{t('当前访问：')}}{{accessCurrent.host||'—'}} · {{accessCurrent.ip||'—'}}</p>
              <div class="settings-form-actions"><button class="primary" :disabled="locked||!accessReady||!accessDirty">{{accessBusy?t('正在处理…'):t('保存')}}</button></div>
              <details class="settings-disclosure"><summary>{{t('高级：指定域名与来源网段')}}<ChevronRight :size="15"/></summary>
                <div>
                  <label class="drawer-input-label">{{t('允许的域名')}}
                    <textarea v-model="accessForm.hosts" rows="2" placeholder="viya.example.com" autocapitalize="none" spellcheck="false" :disabled="locked||!accessReady"></textarea>
                    <small>{{t('留空表示不限制域名。填写后只有列表内的 Host 能打开面板，可防域名被指向本机后绕过限制。')}}</small>
                  </label>
                  <label class="drawer-input-label">{{t('允许的来源网段')}}
                    <textarea v-model="accessForm.ips" rows="2" placeholder="203.0.113.0/24" autocapitalize="none" spellcheck="false" :disabled="locked||!accessReady"></textarea>
                    <small>{{t('留空表示不限制来源。适合只允许固定出口 IP 的场景。')}}</small>
                  </label>
                  <label class="settings-row"><span><strong>{{t('强制 HTTPS')}}</strong><small>{{t('通过 HTTP 打开面板时自动跳转到 HTTPS')}}</small></span><input v-model="accessForm.https_only" class="settings-switch" type="checkbox" role="switch" :disabled="locked||!accessReady"/></label>
                </div>
              </details>
            </template>
          </form>
          <div v-if="admin" class="settings-row settings-policy-entry"><span><strong>{{t('全局默认策略')}}</strong><small>{{t('域名和独立站点可覆盖全局策略')}}</small></span><button type="button" class="secondary" :disabled="locked" @click="openSecurityDefaults()">{{t('配置策略')}}<ChevronRight :size="14"/></button></div>
          <p v-else class="settings-notice">{{t('只读账号不能修改面板端口和全局安全策略。')}}</p>
        </section>
        <section v-show="tab==='account'" id="settings-section-account" class="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-account">
          <div class="settings-section-heading"><h3>{{t('账户')}}</h3><span>{{t('服务器设置 · 单独提交')}}</span></div>
          <div class="settings-account-overview"><span class="settings-avatar"><UserRound :size="22"/></span><div><strong>{{username}}</strong><small>{{admin?t('管理员'):t('只读账户')}}</small></div><span class="settings-session-count">{{sessions===null?'—':sessions}} {{t('个有效会话')}}</span></div>
          <details v-if="admin" class="settings-disclosure"><summary>{{t('修改账户名')}}<ChevronRight :size="15"/></summary><form @input.capture="markEdited('username',$event)" @change.capture="markEdited('username',$event)" autocomplete="off" @submit.prevent="submitUsername"><label class="drawer-input-label">{{t('新账号')}}<input v-model="usernameForm.username" minlength="3" maxlength="64" pattern="[A-Za-z0-9_.-]+" autocomplete="username" required :disabled="locked||loading"/></label><label class="drawer-input-label">{{t('当前密码')}}<input v-model="usernameForm.password" type="password" autocomplete="current-password" required :disabled="locked||loading"/></label><p class="drawer-field-help">{{t('支持字母、数字、点、横线和下划线，3–64 位。修改后当前会话保留，其他会话退出。')}}</p><button class="primary" :disabled="locked||loading||usernameForm.username===username">{{t('修改账号')}}</button></form></details>
          <details class="settings-disclosure"><summary>{{t('修改密码')}}<ChevronRight :size="15"/></summary><form @input.capture="markEdited('password',$event)" @change.capture="markEdited('password',$event)" autocomplete="off" @submit.prevent="submitPassword"><label class="drawer-input-label">{{t('当前密码')}}<input v-model="password.current" type="password" autocomplete="current-password" required :disabled="locked||loading"/></label><label class="drawer-input-label">{{t('新密码')}}<input v-model="password.next" type="password" autocomplete="new-password" minlength="8" maxlength="72" required :disabled="locked||loading"/></label><label class="drawer-input-label">{{t('确认新密码')}}<input v-model="password.confirm" type="password" autocomplete="new-password" minlength="8" maxlength="72" required :disabled="locked||loading"/></label><p class="drawer-field-help">{{t('至少 8 位。修改成功后其他会话退出，当前会话保留。')}}</p><button class="primary" :disabled="locked||loading">{{t('修改密码')}}</button></form></details>
          <div v-if="admin" class="settings-group settings-user-group"><h4>{{t('面板账号')}}</h4><p v-if="userError" class="settings-alert" role="alert">{{userError}}</p><p v-if="userNote" class="settings-notice success" role="status">{{userNote}}</p><button v-if="!usersReady&&userError" type="button" class="secondary" @click="loadUsers">{{t('重试')}}</button><ul class="settings-user-list"><li v-for="item in users" :key="item.id"><div><strong>{{item.username}}</strong><small>{{item.role==='admin'?t('管理员'):t('只读账户')}}</small></div><template v-if="item.username!==username"><select :value="item.role" :disabled="locked" :aria-label="t('账户权限')+' '+item.username" @change="setUserRole(item,$event)"><option value="admin">{{t('管理员')}}</option><option value="viewer">{{t('只读')}}</option></select><button type="button" class="icon-btn" :disabled="locked" :aria-label="t('删除账号')+' '+item.username" @click="removeUser(item)"><Trash2 :size="15"/></button></template><small v-else>{{t('当前登录')}}</small></li></ul><details class="settings-disclosure"><summary>{{t('添加账户')}}<ChevronRight :size="15"/></summary><form @input.capture="markEdited('user',$event)" @change.capture="markEdited('user',$event)" autocomplete="off" @submit.prevent="addUser"><label class="drawer-input-label">{{t('新账号')}}<input v-model="userForm.username" minlength="3" maxlength="64" pattern="[A-Za-z0-9_.-]+" autocomplete="off" required :disabled="locked||!usersReady"/></label><label class="drawer-input-label">{{t('密码')}}<input v-model="userForm.password" type="password" autocomplete="new-password" minlength="8" maxlength="72" required :disabled="locked||!usersReady"/></label><label class="drawer-input-label">{{t('权限')}}<select v-model="userForm.role" :disabled="locked||!usersReady"><option value="viewer">{{t('只读')}}</option><option value="admin">{{t('管理员')}}</option></select></label><button class="primary" :disabled="locked||!usersReady">{{t('添加账号')}}</button></form></details></div>
          <div v-if="admin" class="settings-row"><span><strong>{{t('登录会话')}}</strong><small>{{t('保留当前登录，退出其他设备。')}}</small></span><button type="button" class="secondary" :disabled="locked||loading||sessions===null||sessions<=1" @click="revokeSessions">{{t('退出其他会话')}}</button></div>
        </section>
        <section v-show="tab==='alerts'" id="settings-section-alerts" class="settings-tab-panel" role="tabpanel" aria-labelledby="settings-tab-alerts">
          <div class="settings-section-heading"><h3>{{t('通知')}}</h3><span>{{t('服务器设置 · 保存后生效')}}</span></div>
          <p v-if="!admin" class="settings-notice">{{t('仅管理员可配置通知渠道和发送测试通知。')}}</p>
          <form v-else @submit.prevent="saveAlerts"><p v-if="alertError" class="settings-alert" role="alert">{{alertError}}</p><p v-if="alertNote" class="settings-notice success" role="status">{{alertNote}}</p><button v-if="!alertReady&&alertError" class="secondary" type="button" @click="loadAlerts">{{t('重试')}}</button><div class="settings-group settings-rows"><label class="settings-row"><span><strong>{{t('启用通知')}}</strong><small>{{t('通过 Webhook 接收服务异常')}}</small></span><input v-model="alertForm.enabled" class="settings-switch" type="checkbox" role="switch" :disabled="locked||!alertReady"/></label><label class="drawer-input-label">{{t('Webhook 地址')}}<input v-model="alertForm.webhook_url" type="url" autocomplete="off" autocapitalize="none" spellcheck="false" data-lpignore="true" data-1p-ignore="true" placeholder="https://" :required="alertForm.enabled" :disabled="locked||!alertReady||!alertForm.enabled"/></label><label class="settings-row"><span><strong>{{t('恢复时也通知')}}</strong><small>{{t('故障消除后再发送一条恢复消息')}}</small></span><input v-model="alertForm.notify_recovery" class="settings-switch" type="checkbox" role="switch" :disabled="locked||!alertReady||!alertForm.enabled"/></label></div><div class="settings-form-actions"><button class="primary" :disabled="locked||!alertReady||!alertDirty">{{alertBusy?t('正在处理…'):t('保存通知设置')}}</button><button class="secondary" type="button" :disabled="locked||!alertReady||alertDirty||!alertForm.enabled" @click="testAlerts">{{t('测试已保存配置')}}</button></div><p v-if="alertDirty" class="drawer-field-help">{{t('保存修改后即可测试通知。')}}</p><details class="settings-disclosure"><summary>{{t('通知范围与发送方式')}}<ChevronRight :size="15"/></summary><div><p class="drawer-field-help">{{t('覆盖上游故障、证书申请失败、DDNS 连续失败、数据目录磁盘接近写满、Caddy 进程退出')}}</p><p class="drawer-field-help">{{t('以 JSON 形式 POST 到该地址，内容为 source / event / title / detail / at。同一事件 10 分钟内只推送一次，避免故障抖动造成刷屏。')}}</p></div></details></form>
        </section>
      </div>
      <footer class="settings-drawer-footer"><button type="button" class="primary settings-done" :disabled="locked" @click="close"><Check :size="17"/>{{t('完成')}}</button></footer>
      <div v-if="discardOpen" class="overlay settings-discard-overlay" v-dialog @keydown.stop>
        <section class="modal settings-discard-modal" role="alertdialog" aria-modal="true" aria-labelledby="settings-discard-title"><div class="modal-top"><h2 id="settings-discard-title"><AlertCircle :size="20"/>{{t('放弃未保存的修改？')}}</h2><button type="button" class="icon-btn" data-close :aria-label="t('继续编辑')" @click="discardOpen=false"><X :size="17"/></button></div><p>{{t('端口、账户或通知的未保存修改将被丢弃。已即时保存的外观与刷新偏好会保留。')}}</p><div class="modal-actions"><button type="button" class="secondary" @click="discardOpen=false">{{t('继续编辑')}}</button><button type="button" class="primary" @click="discard">{{t('放弃修改并关闭')}}</button></div></section>
      </div>
    </section>
  </div>
</template>
