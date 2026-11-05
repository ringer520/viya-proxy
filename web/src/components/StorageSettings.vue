<script setup lang="ts">
import {computed,onMounted,ref,watch} from 'vue'
import {apiFetch} from '../api'
import {confirmDeletion} from '../composables/useState'
import {t} from '../i18n'
import {refreshTraffic} from '../composables/useTraffic'
const props=defineProps<{csrf:string;admin:boolean;blocked:boolean}>()
const emit=defineEmits<{busy:[value:boolean];dirty:[value:boolean]}>()
type Usage={total:number;skipped:number;schema_version:number;categories:{name:string;bytes:number;files:number}[];retention:{traffic_days:number;access_events:number}}
const usage=ref<Usage|null>(null),error=ref(''),note=ref(''),busy=ref(false),loading=ref(false)
const days=ref(90),events=ref(100)
const dirty=computed(()=>!!usage.value&&(days.value!==usage.value.retention.traffic_days||events.value!==usage.value.retention.access_events))
watch(dirty,value=>emit('dirty',value))
const names:Record<string,string>={database:'数据库',sites:'网站文件',certificates:'证书',logs:'日志',recovery:'恢复点',other:'其他'}
const bytes=(n:number)=>n<1024?`${n} B`:n<1048576?`${(n/1024).toFixed(1)} KiB`:`${(n/1048576).toFixed(1)} MiB`
async function load(){loading.value=true;error.value='';try{const r=await apiFetch('/api/v1/storage');if(!r.ok)throw Error(t('容量读取失败'));usage.value=await r.json();days.value=usage.value!.retention.traffic_days;events.value=usage.value!.retention.access_events}catch(e){error.value=e instanceof Error?e.message:t('读取失败')}finally{loading.value=false}}
async function write(cleanup=false){
 if(busy.value||props.blocked||!props.admin)return
 if(cleanup&&!await confirmDeletion(t('按已保存的保留策略清理过期统计和近期访问记录。网站文件、证书和恢复点会保留。确认继续？'),t('确认清理')))return
 busy.value=true;emit('busy',true);error.value='';note.value=''
 try{const r=await apiFetch(cleanup?'/api/v1/storage/cleanup':'/api/v1/storage/retention',{method:cleanup?'POST':'PUT',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify(cleanup?{confirmed:true}:{traffic_days:days.value,access_events:events.value})});const data=await r.json();if(!r.ok)throw Error(t(data.error)||t('保存失败'));note.value=cleanup?t('已清理过期记录；数据库空闲页会自动复用'):t('已保存保留策略');await load()}catch(e){error.value=e instanceof Error?e.message:t('操作失败')}finally{busy.value=false;emit('busy',false)}
}
async function clearStatistics(){
 if(busy.value||props.blocked||!props.admin)return
 if(!await confirmDeletion(t('清空所有 Web 服务及端口转发的累计、月度统计和近期访问样例，清空后重新计数。此操作无法撤销，原始日志文件及业务配置会保留。'),t('清空所有统计')))return
 busy.value=true;emit('busy',true);error.value='';note.value=''
 try{const response=await apiFetch('/api/v1/traffic/reset',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrf},body:JSON.stringify({confirmed:true})});const data=await response.json();if(!response.ok)throw Error(t(data.error)||t('操作失败'));await refreshTraffic();await load();note.value=t('所有统计已清空，新的访问将重新计数')}
 catch(e){error.value=e instanceof Error?t(e.message):t('操作失败')}
 finally{busy.value=false;emit('busy',false)}
}
onMounted(load)
</script>
<template>
 <div class="settings-section-heading"><h3>{{t('存储与保留')}}</h3><button class="secondary" type="button" :disabled="busy||loading||dirty||blocked" @click="load">{{t('刷新')}}</button></div>
 <p v-if="error" class="settings-alert" role="alert">{{error}}</p><p v-if="note" class="settings-notice" role="status">{{note}}</p>
 <p v-if="loading" role="status">{{t('正在统计容量…')}}</p>
 <template v-if="usage">
 <div class="settings-group"><h4>{{t('数据目录')}} · {{bytes(usage.total)}}</h4><div class="storage-breakdown"><div v-for="item in usage.categories" :key="item.name" class="storage-cell"><span>{{t(names[item.name]||item.name)}}<small>{{item.files}} {{t('个文件')}}</small></span><strong>{{bytes(item.bytes)}}</strong></div></div><p class="drawer-field-help">{{t('仅统计数据目录，不包含外部备份与外部网站目录；不跟随符号链接。')}}<span v-if="usage.skipped">{{t('部分文件无法读取')}}：{{usage.skipped}}</span></p></div>
 <form v-if="admin" class="settings-group" @submit.prevent="write()"><h4>{{t('保留策略')}}</h4><label class="settings-row"><span>{{t('流量明细保留天数')}}<small>30–365</small></span><input v-model.number="days" type="number" min="30" max="365" required :disabled="busy||blocked"/></label><label class="settings-row"><span>{{t('每站点近期访问记录')}}<small>{{t('0 表示不保留预览，流量统计不受影响')}}</small></span><input v-model.number="events" type="number" min="0" max="100" required :disabled="busy||blocked"/></label><p class="drawer-field-help">{{t('清理按已保存策略执行，删除的记录无法恢复。缩短流量保留天数也会影响累计统计范围。日志文件的大小与保留期限在日志页面配置。')}}</p><div class="settings-form-actions"><button class="primary" :disabled="busy||blocked">{{t('保存保留策略')}}</button><button class="secondary" type="button" :disabled="busy||blocked||dirty" @click="write(true)">{{t('清理过期记录')}}</button></div></form>
 <div v-if="admin" class="settings-group statistics-reset"><h4>{{t('流量统计')}}</h4><p class="drawer-field-help">{{t('Web 服务和端口转发按近 30 天展示，可清空历史统计后重新计数。')}}</p><button class="secondary danger" type="button" :disabled="busy||blocked||loading||dirty" @click="clearStatistics">{{t('清空所有统计')}}</button></div>
 <details class="settings-disclosure"><summary>{{t('备份与恢复范围')}}</summary><p>{{t('配置历史中的加密导出不含账户和网站文件。完整迁移请使用备份脚本，同时保存主密钥，并执行校验或恢复演练。')}}</p><p>{{t('完整备份包含账户、DNS 凭据和证书私钥，应存放在受保护的位置。')}}</p><code>VIYA_BACKUP_INCLUDE_KEY=1 VIYA_BACKUP_STOP_SERVICE=1 sh /opt/viya/scripts/backup-linux.sh</code><p>{{t('数据库版本')}} {{usage.schema_version}}</p></details>
 </template>
</template>

<style scoped>
.storage-breakdown{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;margin:12px 0}
.storage-cell{padding:12px;border:1px solid var(--line);border-radius:9px;background:var(--surface-2);min-width:0}
.storage-cell span{font-size:12px;color:var(--muted)}.storage-cell small{display:block;font-size:10px;margin:4px 0 9px}.storage-cell strong{font-size:15px;display:block;overflow-wrap:anywhere}
@media(max-width:540px){.storage-breakdown{grid-template-columns:repeat(2,minmax(0,1fr))}}
</style>
