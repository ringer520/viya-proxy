<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { X, CircleAlert, RefreshCw } from '@lucide/vue'
import { apiFetch } from '../api'
import { t } from '../i18n'
type Group={site_id:number;name:string;domain:string;status:number;count:number}
const props=defineProps<{range:string}>(),emit=defineEmits<{close:[]}>()
const groups=ref<Group[]>([]),requests=ref(0),busy=ref(false),error=ref('')
let request:AbortController|undefined
function label(status:number){if([502,503,504].includes(status))return t('上游异常');if(status>=500)return t('应用错误');if(status===401)return t('需要身份验证');if(status===403)return t('访问被拒绝');if(status===404)return t('页面不存在');if(status===429)return t('请求过于频繁');return t('请求错误')}
const total=computed(()=>groups.value.reduce((n,g)=>n+g.count,0))
const share=computed(()=>requests.value?(total.value/requests.value*100).toFixed(1):'0.0')
function percent(count:number){return requests.value?(count/requests.value*100).toFixed(1):'0.0'}
async function load(){request?.abort();const controller=new AbortController();request=controller;busy.value=true;error.value='';groups.value=[]
 try{const r=await apiFetch('/api/v1/proxy-hosts/error-locations?range='+props.range,{signal:controller.signal});const data=await r.json();if(!r.ok)throw Error(t(data.error)||t('读取失败'));if(controller.signal.aborted)return;groups.value=data.groups||[];requests.value=Number(data.requests)||0}
 catch(e){if(!controller.signal.aborted)error.value=e instanceof Error?t(e.message):t('读取失败')}
 finally{if(request===controller)busy.value=false}
}
watch(()=>props.range,load,{immediate:true});onBeforeUnmount(()=>request?.abort())
</script>
<template><div class="overlay" v-dialog><section class="modal error-locations"><div class="modal-top"><div><span class="eyebrow">{{range}}</span><h2><CircleAlert :size="20"/>{{t('异常定位')}}</h2></div><button class="icon-btn" :aria-label="t('关闭异常定位')" @click="emit('close')"><X :size="18"/></button></div><p class="auth-hint">{{t('按站点与状态码汇总计数，只保留统计，不保存具体请求记录。')}}</p><p class="auth-hint">{{t('按 HTTP 状态码分类，不直接判定攻击或故障。')}}</p><p v-if="error" class="error" role="alert">{{error}}</p><p v-else-if="busy" role="status">{{t('正在加载…')}}</p><template v-else><div class="error-log-caption"><strong>{{t('错误合计')}} {{total.toLocaleString()}}</strong><span>{{t('该周期 Web 请求')}} {{requests.toLocaleString()}} · {{t('占比')}} {{share}}%</span><small>{{t('统计来自聚合表，重启或升级不会丢失。')}}</small></div><div v-if="groups.length" class="error-location-list"><div class="error-location-head"><span>{{t('站点 / 服务')}}</span><span>{{t('异常类型')}}</span><span>{{t('次数')}}</span></div><div v-for="group in groups" :key="group.site_id+'-'+group.status" class="error-location-row"><span><strong>{{group.name||group.domain}}</strong><small>{{group.domain}}</small></span><span>{{label(group.status)}}<small>HTTP {{group.status}}</small></span><b>{{group.count.toLocaleString()}}<small>{{percent(group.count)}}%</small></b></div></div><div v-else class="empty">{{t('所选周期内没有错误记录。')}}</div></template><div class="modal-actions"><button class="secondary" :disabled="busy" @click="load"><RefreshCw :size="15"/>{{t('刷新')}}</button><button class="primary" @click="emit('close')">{{t('关闭')}}</button></div></section></div></template>
