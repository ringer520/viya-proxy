<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { t } from '../i18n'
import { TrendingUp } from '@lucide/vue'
import { formatBytes } from '../format'

const props = defineProps<{ points: { hour: number; bytes: number }[]; granularity?: 'hour' | 'day'; period?: '24h' | '7d' | '30d'; embedded?: boolean }>()

const W = 720
const H = 190
const PAD_X = 8
const PAD_TOP = 14
const PAD_BOTTOM = 26
const drawn = ref(false)
onMounted(() => {
  requestAnimationFrame(() => (drawn.value = true))
})

const max = computed(() => Math.max(1, ...props.points.map((p) => p.bytes)))
const total = computed(() => props.points.reduce((sum, p) => sum + p.bytes, 0))
const peak = computed(() => props.points.reduce((best, p) => (p.bytes > best.bytes ? p : best), props.points[0] || { hour: 0, bytes: 0 }))
const hasData = computed(() => props.points.some((p) => p.bytes > 0))

// The same card draws 24 hours or N days; only the axis label differs.
function label(unix: number) {
  const at = new Date(unix * 1000)
  return props.granularity === 'day'
    ? `${at.getMonth() + 1}/${at.getDate()}`
    : at.getHours().toString().padStart(2, '0') + ':00'
}
const peakLabel = computed(() =>
  props.granularity === 'day' ? t('峰值日期') : t('峰值时段'),
)
function x(i: number) {
  const last = Math.max(1, props.points.length - 1)
  return PAD_X + (i / last) * (W - PAD_X * 2)
}
function y(bytes: number) {
  const usable = H - PAD_TOP - PAD_BOTTOM
  return PAD_TOP + (1 - bytes / max.value) * usable
}

// Catmull-Rom → cubic bezier keeps the line smooth without overshooting the
// zero baseline.
const linePath = computed(() => {
  const pts = props.points.map((p, i) => ({ x: x(i), y: y(p.bytes) }))
  if (pts.length < 2) return ''
  let d = `M ${pts[0].x.toFixed(1)} ${pts[0].y.toFixed(1)}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i - 1] || pts[i]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[i + 2] || p2
    const c1x = p1.x + (p2.x - p0.x) / 6
    const c1y = Math.max(Math.min(p1.y,p2.y),Math.min(Math.max(p1.y,p2.y),p1.y + (p2.y - p0.y) / 6))
    const c2x = p2.x - (p3.x - p1.x) / 6
    const c2y = Math.max(Math.min(p1.y,p2.y),Math.min(Math.max(p1.y,p2.y),p2.y - (p3.y - p1.y) / 6))
    d += ` C ${c1x.toFixed(1)} ${c1y.toFixed(1)}, ${c2x.toFixed(1)} ${c2y.toFixed(1)}, ${p2.x.toFixed(1)} ${p2.y.toFixed(1)}`
  }
  return d
})
const areaPath = computed(() => {
  if (!linePath.value) return ''
  const base = H - PAD_BOTTOM
  return `${linePath.value} L ${x(props.points.length - 1).toFixed(1)} ${base} L ${x(0).toFixed(1)} ${base} Z`
})
const peakPoint = computed(() => {
  const i = props.points.findIndex((p) => p === peak.value)
  return i < 0 || !hasData.value ? null : { x: x(i), y: y(peak.value.bytes), hour: peak.value.hour }
})
const labels = computed(() => Array.from(new Set([0, .25, .5, .75, 1].map(r=>Math.round(r*Math.max(0,props.points.length-1))))).map(i=>({i,text:props.points[i]?label(props.points[i].hour):''})))
const hovered = ref<number | null>(null)
const hoverPoint = computed(() => hovered.value === null ? null : props.points[hovered.value] || null)
const hoverX = computed(() => hovered.value === null ? 0 : x(hovered.value))
const title = computed(() => props.period === '7d' ? t('7 天流量趋势') : props.period === '30d' ? t('30 天流量趋势') : t('24 小时流量趋势'))
function inspect(event: PointerEvent) {
  const bounds=(event.currentTarget as SVGSVGElement).getBoundingClientRect()
  const position=((event.clientX-bounds.left)/bounds.width*W-PAD_X)/(W-PAD_X*2)
  hovered.value=Math.max(0,Math.min(props.points.length-1,Math.round(position*(props.points.length-1))))
}
function inspectKey(event: KeyboardEvent) {
  if (event.key==='Escape') {hovered.value=null;return}
  if (!['ArrowLeft','ArrowRight'].includes(event.key)) return
  event.preventDefault()
  hovered.value=Math.max(0,Math.min(props.points.length-1,(hovered.value ?? 0)+(event.key==='ArrowRight'?1:-1)))
}
watch(()=>props.period,()=>{hovered.value=null})
</script>
<template>
  <section class="traffic-trend" :class="{embedded}" :aria-label="title">
    <header v-if="!embedded"><h2>{{title}}</h2><strong>{{formatBytes(total)}}</strong></header>
    <div class="trend-chart">
      <span class="chart-ceiling">{{formatBytes(max)}}</span>
      <svg :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" role="img" tabindex="0" :aria-label="title" @pointermove="inspect" @pointerleave="hovered=null" @keydown="inspectKey" @blur="hovered=null">
        <defs><linearGradient id="mission-trend-area" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="var(--accent)" stop-opacity=".32"/><stop offset="100%" stop-color="var(--accent)" stop-opacity="0"/></linearGradient><linearGradient id="mission-trend-line" x1="0" y1="0" x2="1" y2="0"><stop offset="0%" stop-color="var(--accent-2)"/><stop offset="100%" stop-color="var(--accent)"/></linearGradient></defs>
        <g class="chart-grid"><line v-for="n in 5" :key="n" :x1="PAD_X" :x2="W-PAD_X" :y1="PAD_TOP+(H-PAD_TOP-PAD_BOTTOM)/4*(n-1)" :y2="PAD_TOP+(H-PAD_TOP-PAD_BOTTOM)/4*(n-1)"/></g>
        <path :key="'area-'+period" :d="areaPath" fill="url(#mission-trend-area)" class="chart-area"/>
        <path :key="'line-'+period" :d="linePath" fill="none" stroke="url(#mission-trend-line)" stroke-width="2.5" vector-effect="non-scaling-stroke" stroke-linecap="round" pathLength="1" class="chart-line"/>
        <g v-if="peakPoint && !hoverPoint"><circle :cx="peakPoint.x" :cy="peakPoint.y" r="8" class="chart-halo"/><circle :cx="peakPoint.x" :cy="peakPoint.y" r="3" class="chart-dot"/></g>
        <g v-if="hoverPoint"><line :x1="hoverX" :x2="hoverX" :y1="PAD_TOP" :y2="H-PAD_BOTTOM" class="chart-crosshair"/><circle :cx="hoverX" :cy="y(hoverPoint.bytes)" r="8" class="chart-halo"/><circle :cx="hoverX" :cy="y(hoverPoint.bytes)" r="4" class="chart-dot"/></g>
      </svg>
      <div class="trend-axis"><span v-for="axis in labels" :key="axis.i" :style="{left:x(axis.i)/W*100+'%'}">{{axis.text}}</span></div>
      <div v-if="hoverPoint" class="chart-tooltip" role="tooltip" :style="hoverX/W<.5?{left:hoverX/W*100+'%'}:{right:(1-hoverX/W)*100+'%'}"><span>{{new Date(hoverPoint.hour*1000).toLocaleString([], {month:'short',day:'numeric',hour:granularity==='day'?undefined:'2-digit',minute:granularity==='day'?undefined:'2-digit'})}}</span><strong>{{formatBytes(hoverPoint.bytes)}}</strong></div>
      <div v-if="!hasData" class="trend-empty">{{t('所选周期内暂无流量记录')}}</div>
    </div>
  </section>
</template>
<style scoped>
.traffic-trend{min-width:0;padding:16px;background:var(--surface);border:1px solid var(--line);border-radius:16px}.traffic-trend.embedded{border:0;border-radius:0;padding:0;background:transparent}header{display:flex;justify-content:space-between;align-items:center}h2{font-size:15px}.trend-chart{position:relative;height:200px;padding-top:8px}.trend-chart>svg{width:100%;height:100%;display:block;overflow:visible;outline-offset:3px}.chart-ceiling{position:absolute;top:0;left:7px;color:var(--muted);font-size:9px}.chart-grid line{stroke:var(--line);stroke-dasharray:2 6;opacity:.6}.chart-line{stroke-dasharray:1;stroke-dashoffset:0;animation:mission-line-reveal 1.3s cubic-bezier(.22,1,.36,1) both;filter:drop-shadow(0 3px 5px var(--accent-soft))}.chart-area{animation:mission-area-reveal 1.2s ease both}.chart-halo{fill:var(--accent);opacity:.15}.chart-dot{fill:var(--accent);stroke:var(--surface);stroke-width:2}.chart-crosshair{stroke:var(--accent);stroke-width:1;stroke-dasharray:3 4;opacity:.6}.trend-axis{position:relative;height:15px;margin-top:-15px}.trend-axis span{position:absolute;transform:translateX(-50%);font-size:9px;color:var(--muted)}.trend-axis span:first-child{transform:none}.trend-axis span:last-child{transform:translateX(-100%)}.chart-tooltip{pointer-events:none;position:absolute;top:28px;min-width:112px;padding:10px 12px;display:grid;gap:5px;z-index:2;background:var(--surface);border:1px solid color-mix(in srgb,var(--accent) 35%,var(--line));border-radius:10px;box-shadow:0 12px 35px #0002;animation:mission-tooltip-in .15s ease}.chart-tooltip span{font-size:10px;color:var(--muted)}.chart-tooltip strong{font-size:16px;color:var(--text)}.trend-empty{position:absolute;top:45%;left:0;right:0;text-align:center;font-size:12px;color:var(--muted);pointer-events:none}@keyframes mission-line-reveal{from{stroke-dashoffset:1}to{stroke-dashoffset:0}}@keyframes mission-area-reveal{from{opacity:0}to{opacity:1}}@keyframes mission-tooltip-in{from{opacity:0;transform:translateY(3px)}to{opacity:1;transform:translateY(0)}}@media(prefers-reduced-motion:reduce){.chart-line,.chart-area,.chart-tooltip{animation:none}}
</style>
