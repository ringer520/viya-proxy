<script setup lang="ts">
import { computed } from 'vue'
import AnimatedMetric from '../AnimatedMetric.vue'
import { trafficCharts } from '../composables/usePreferences'
import { t } from '../i18n'
import type { SiteTraffic } from '../types'
const props=defineProps<{stats:SiteTraffic;kind:'web'|'forward';protocol?:string}>()
// A flat zero line reads as a stray divider, so only draw the spark when the
// 30-day window actually carries traffic.
const hasTraffic=computed(()=>(props.stats.daily||[]).some(p=>p.upload+p.download>0))
const unit=computed(()=>props.kind==='web'?t('次请求'):props.protocol==='udp'?t('会话'):props.protocol==='tcp'?t('连接'):t('连接 / 会话'))
const line=computed(()=>{
 const days=new Map((props.stats.daily||[]).map(p=>[p.day,p.upload+p.download])); const now=Math.floor(Date.now()/86400000)*86400;
 const values=Array.from({length:30},(_,i)=>days.get(now-(29-i)*86400)||0),max=Math.max(1,...values);
 return values.map((v,i)=>`${i*120/29},${22-v/max*18}`).join(' ')
})
</script>
<template><div class="service-traffic-cell" :aria-label="t('近30天')"><div class="service-traffic-main"><strong><AnimatedMetric :value="stats.recent.requests"/></strong><small>{{unit}}</small><span>30d</span></div><div class="service-traffic-bytes"><span><small>{{t('上行')}}</small><b><AnimatedMetric :value="stats.recent.upload" format="bytes"/></b></span><span><small>{{t('下行')}}</small><b><AnimatedMetric :value="stats.recent.download" format="bytes"/></b></span></div><svg v-if="trafficCharts && hasTraffic" class="service-traffic-spark" viewBox="0 0 120 26" aria-hidden="true" preserveAspectRatio="none"><polyline pathLength="1" :points="line" fill="none" stroke="currentColor" stroke-width="1.5" vector-effect="non-scaling-stroke"/></svg></div></template>
