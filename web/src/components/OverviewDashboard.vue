<script setup lang="ts">
import { computed, ref } from 'vue'
import { Globe2, Cable, RefreshCw, ArrowUpRight, ShieldCheck, CircleAlert, Check, Clock3, Activity } from '@lucide/vue'
import { t, tr, language } from '../i18n'
import ErrorLocations from './ErrorLocations.vue'
import AnimatedMetric from '../AnimatedMetric.vue'
import SystemGauges from './SystemGauges.vue'
import TrafficTrend from './TrafficTrend.vue'
import { hosts, l4Rules, ddnsRecords, certExpiring, revisions, upstreamHealth, online } from '../composables/useState'
import { webHosts } from '../composables/useHosts'
import { traffic, trafficLoadedRange, trafficRange, trafficTrend, trafficTotals, trafficRanking, trafficError } from '../composables/useTraffic'
import { autoRefresh, pauseHidden, documentHidden, trafficCharts } from '../composables/usePreferences'
import { openSiteDetails } from '../composables/useSiteWorkspace'
import { formatBytes } from '../format'
import { reducedMotion } from '../composables/useMotion'
// The English eyebrow labels duplicate the localised headings, so they only
// render for English readers.
const showKicker = language === 'en'
const errorsOpen = ref(false)
const attentionPanel = ref<HTMLElement | null>(null)
const rangeReady = computed(() => trafficRange.value === trafficLoadedRange.value)
function showAttention() {attentionPanel.value?.scrollIntoView({behavior:reducedMotion.value?'instant':'smooth',block:'center'})}
const activeSampling = computed(() => autoRefresh.value && (!pauseHidden.value || !documentHidden.value))
const alerts = computed(() => [
  ...(!online.value ? [{ key:'engine', title:t('引擎未就绪'), detail:t('检查引擎与配置应用状态'), href:'#config-history', host:undefined }] : []),
  ...hosts.value.filter(h => h.enabled && h.type === 'proxy' && upstreamHealth.value[String(h.id)]?.ok === false).map(h => ({ key:'site-'+h.id, title:h.name || h.domain, detail:t('上游无响应'), href:'#proxy-sites', host:h })),
  ...certExpiring.value.filter(c => c.expired || !hosts.value.some(h => (h.domain===c.domain || h.certificate_source?.domain===c.domain) && (h.tls_mode==='internal' || h.certificate_source?.kind==='internal'))).map(c => ({ key:'cert-'+c.domain, title:c.domain, detail:c.expired ? t('证书已过期') : tr`证书将在 ${c.days_left} 天后到期`, href:'#certificates', host:undefined })),
  ...ddnsRecords.value.filter(d => d.enabled && d.last_error).map(d => ({ key:'ddns-'+d.id, title:d.name, detail:t('DDNS 更新失败'), href:'#ddns', host:undefined })),
  ...(revisions.value[0]?.status === 'failed' ? [{ key:'config',title:t('配置应用失败'),detail:revisions.value[0].error || t('查看配置历史'),href:'#config-history',host:undefined }] : []),
])
const counts = computed(() => [
  { label:t('Web 服务'), count:webHosts.value.length, detail:tr`${webHosts.value.filter(h=>h.enabled).length} 个已启用`, href:'#proxy-sites', icon:Globe2, color:'var(--metric-cpu)' },
  { label:t('端口转发'), count:l4Rules.value.length, detail:'TCP / UDP', href:'#layer4', icon:Cable, color:'var(--metric-memory)' },
  { label:t('动态 DNS'), count:ddnsRecords.value.length, detail:tr`${ddnsRecords.value.filter(d=>d.enabled).length} 个自动更新`, href:'#ddns', icon:RefreshCw, color:'var(--success)' },
])
const errorRate = computed(() => { const total=Object.values(traffic.value).reduce((n,s)=>n+s.recent.requests,0); return total ? Object.values(traffic.value).reduce((n,s)=>n+s.recent.errors,0)/total*100 : 0 })
const periods = computed(() => [{id:'24h',label:t('24 小时')},{id:'7d',label:t('7 天')},{id:'30d',label:t('30 天')},{id:'90d',label:t('90 天')} ] as const)
const ranking = computed(() => trafficRanking.value.slice(0,5))
const rankingMax = computed(() => Math.max(1,...ranking.value.map(r=>r.bytes)))
const events = computed(() => [
  ...revisions.value.map(r=>({key:'r'+r.id,title:r.summary?t(r.summary):tr`配置 #${r.id}`,detail:r.status==='applied'?t('已应用'):r.status==='failed'?t('应用失败'):t('已保存'),timestamp:new Date(r.created_at).getTime(),bad:r.status==='failed',href:'#config-history'})),
  ...ddnsRecords.value.filter(d=>d.last_run_at).map(d=>({key:'d'+d.id,title:d.name,detail:d.last_error?t('DDNS 更新失败'):t('DDNS 更新完成'),timestamp:d.last_run_at*1000,bad:!!d.last_error,href:'#ddns'})),
].filter(e=>Number.isFinite(e.timestamp)).sort((a,b)=>b.timestamp-a.timestamp).slice(0,5))
</script>

<template>
  <div class="mission-control">
    <div class="mission-intro"><span><Activity :size="13"/> {{t('实时运行态势')}}</span></div>
    <div class="mission-counts">
      <a v-for="item in counts" :key="item.href" :href="item.href" class="mission-count" :style="{'--signal':item.color}"><span class="count-symbol"><component :is="item.icon" :size="18"/></span><div><span>{{item.label}}</span><strong><AnimatedMetric :value="item.count"/></strong><small>{{item.detail}}</small></div><ArrowUpRight class="count-jump" :size="14"/></a>
      <button type="button" @click="showAttention" class="mission-count" :class="{warning:alerts.length}" :style="{'--signal':alerts.length?'#fb7185':'#34d399'}"><span class="count-symbol"><CircleAlert v-if="alerts.length" :size="18"/><ShieldCheck v-else :size="18"/></span><div><span>{{t('待处理异常')}}</span><strong><AnimatedMetric :value="alerts.length"/></strong><small>{{t('点击查看详情')}}</small></div></button>
    </div>
    <SystemGauges compact :auto-refresh="activeSampling"/>
    <div class="mission-grid">
      <section class="mission-card mission-traffic" :class="{'range-loading':!rangeReady}" :aria-busy="!rangeReady && !trafficError">
        <header class="mission-card-head"><div><span v-if="showKicker" class="mission-kicker">NETWORK ACTIVITY</span><h2>{{t('流量分析')}}</h2></div><div class="mission-periods" role="group" :aria-label="t('统计周期')"><button v-for="period in periods" :key="period.id" :class="{selected:trafficRange===period.id}" :aria-pressed="trafficRange===period.id" @click="trafficRange=period.id">{{period.label}}</button></div></header>
        <p v-if="trafficError" class="mission-data-error" role="alert">{{trafficError}} · {{t('以下为上次采样')}}</p>
        <div class="mission-traffic-kpis"><div><small>{{t('请求 / 连接')}}</small><strong><AnimatedMetric :value="trafficTotals.requests"/></strong></div><div><small>{{t('上行流量')}}</small><strong><AnimatedMetric :value="trafficTotals.upload" format="bytes"/></strong></div><div><small>{{t('下行流量')}}</small><strong><AnimatedMetric :value="trafficTotals.download" format="bytes"/></strong></div><button type="button" class="web-error-trigger" :class="{bad:errorRate>5}" :aria-label="t('查看 Web 错误站点')" @click="errorsOpen=true" :title="t('仅统计 Web 请求的 4xx / 5xx 比例')"><small>{{t('Web 错误率')}}</small><strong><AnimatedMetric :value="errorRate" format="decimal"/><em>%</em></strong></button></div>
        <TrafficTrend v-if="trafficCharts && rangeReady" :points="trafficTrend" :granularity="trafficRange==='24h'?'hour':'day'" :period="trafficRange" embedded/>
        <div v-else-if="trafficCharts" class="mission-chart-loading" role="status"><span v-if="!trafficError"/>{{trafficError?t('当前周期数据不可用'):t('正在加载统计')}}</div>
        <button v-if="!trafficCharts" class="inline-action" type="button" @click="trafficCharts=true">{{t('显示流量趋势图')}}</button>
        <div v-if="trafficCharts" class="mission-chart-note"><span class="mission-legend"/>{{t('上行 + 下行')}}<span>{{t('悬浮查看时段详情')}}</span></div>
      </section>
      <section ref="attentionPanel" id="overview-attention" class="mission-card mission-attention"><header class="mission-card-head"><div><span v-if="showKicker" class="mission-kicker">ACTION CENTER</span><h2>{{t('需要关注')}} <b>{{alerts.length}}</b></h2></div><CircleAlert :size="18"/></header><div v-if="!alerts.length" class="mission-all-clear"><ShieldCheck :size="34"/><strong>{{t('当前无待处理异常')}}</strong></div><div v-else class="mission-alerts"><a v-for="alert in alerts" :key="alert.key" :href="alert.href" @click="alert.host && ($event.preventDefault(),openSiteDetails(alert.host,true))"><span class="alert-marker"/><div><strong>{{alert.title}}</strong><small>{{alert.detail}}</small></div><ArrowUpRight :size="14"/></a></div></section>
      <section class="mission-card mission-ranking"><header class="mission-card-head"><div><span v-if="showKicker" class="mission-kicker">TOP SERVICES</span><h2>{{t('流量排行')}} <small>TOP 5</small></h2></div></header><div v-if="!rangeReady || !ranking.length" class="mission-empty">{{t('所选周期内暂无流量记录')}}</div><a v-for="(row,index) in (rangeReady?ranking:[])" :key="row.key" class="mission-rank-row" :href="row.kind==='forward'?'#layer4':'#proxy-sites'"><span>{{String(index+1).padStart(2,'0')}}</span><div><div><strong>{{row.label}}</strong><small>{{formatBytes(row.bytes)}}</small></div><div class="mission-rank-track"><i :style="{width:row.bytes/rankingMax*100+'%'}"/></div></div></a></section>
      <section class="mission-card mission-events"><header class="mission-card-head"><div><span v-if="showKicker" class="mission-kicker">RECENT ACTIVITY</span><h2>{{t('最近活动')}}</h2></div><Clock3 :size="17"/></header><p class="mission-event-caption">{{t('最近配置与 DDNS 更新')}}</p><div v-if="!events.length" class="mission-empty">{{t('暂无活动记录')}}</div><a v-for="event in events" :key="event.key" :href="event.href" class="mission-event" :class="{bad:event.bad}"><span><CircleAlert v-if="event.bad" :size="12"/><Check v-else :size="12"/></span><div><strong>{{event.title}}</strong><small>{{event.detail}}</small></div><time :title="new Date(event.timestamp).toLocaleString()">{{new Date(event.timestamp).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'})}}</time></a></section>
    </div>
  </div>
  <ErrorLocations v-if="errorsOpen" :range="trafficRange" @close="errorsOpen=false"/>
</template>

<style src="../overview-dashboard.css"></style>
