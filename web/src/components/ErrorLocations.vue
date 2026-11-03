<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue'
import { X, ChevronLeft, ChevronRight, CircleAlert, RefreshCw } from '@lucide/vue'
import AccessLogTable from './AccessLogTable.vue'
import { apiFetch } from '../api'
import { t } from '../i18n'
type Group={site_id:number;name:string;domain:string;status:number;count:number;events:Record<string,any>[]}
const props=defineProps<{range:string}>(),emit=defineEmits<{close:[]}>()
const groups=ref<Group[]>([]),selected=ref<Group|null>(null),busy=ref(false),error=ref(''),limited=ref(false)
let request:AbortController|undefined
function label(status:number){if([502,503,504].includes(status))return t('上游异常');if(status>=500)return t('应用错误');if(status===401)return t('需要身份验证');if(status===403)return t('访问被拒绝');if(status===404)return t('页面不存在');if(status===429)return t('请求过于频繁');return t('请求错误')}
async function load(){request?.abort();const controller=new AbortController();request=controller;busy.value=true;error.value='';groups.value=[];selected.value=null
 try{const r=await apiFetch('/api/v1/proxy-hosts/error-locations?range='+props.range,{signal:controller.signal});const data=await r.json();if(!r.ok)throw Error(data.error||t('读取失败'));if(controller.signal.aborted)return;groups.value=data.groups;limited.value=data.limited}
 catch(e){if(!controller.signal.aborted)error.value=e instanceof Error?t(e.message):t('读取失败')}
 finally{if(request===controller)busy.value=false}
}
watch(()=>props.range,load,{immediate:true});onBeforeUnmount(()=>request?.abort())
</script>
<template><div class="overlay" v-dialog><section class="modal error-locations"><div class="modal-top"><div><span class="eyebrow">{{range}}</span><h2><CircleAlert :size="20"/>{{t('异常定位')}}</h2></div><button class="icon-btn" :aria-label="t('关闭异常定位')" @click="emit('close')"><X :size="18"/></button></div><p class="auth-hint">{{t('仅近期记录：按所选时间范围筛选保留的访问日志，次数可能少于完整流量统计。')}}</p><p class="auth-hint">{{t('按 HTTP 状态码分类，不直接判定攻击或故障。')}}</p><p v-if="limited" class="auth-hint">{{t('已达到分析上限，仅展示最近 20000 条错误记录。')}}</p><p v-if="error" class="error" role="alert">{{error}}</p><p v-else-if="busy" role="status">{{t('正在加载…')}}</p><template v-else-if="selected"><button class="secondary" @click="selected=null"><ChevronLeft :size="15"/>{{t('返回列表')}}</button><div class="error-log-caption"><strong>{{selected.name||selected.domain}}</strong><span>{{selected.domain}} · {{label(selected.status)}} · {{selected.status}}</span><small>{{t('最多展示该类型最近 30 条记录。')}}</small></div><AccessLogTable :events="selected.events" :limit="30"/></template><div v-else-if="groups.length" class="error-location-list"><div class="error-location-head"><span>{{t('站点 / 服务')}}</span><span>{{t('异常类型')}}</span><span>{{t('次数')}}</span></div><button v-for="group in groups" :key="group.site_id+'-'+group.status" class="error-location-row" @click="selected=group"><span><strong>{{group.name||group.domain}}</strong><small>{{group.domain}}</small></span><span>{{label(group.status)}}<small>HTTP {{group.status}}</small></span><b>{{group.count}}<ChevronRight :size="14"/></b></button></div><div v-else class="empty">{{t('近期保留的日志中没有错误记录。')}}</div><div class="modal-actions"><button class="secondary" :disabled="busy" @click="load"><RefreshCw :size="15"/>{{t('刷新')}}</button><button class="primary" @click="emit('close')">{{t('关闭')}}</button></div></section></div></template>
