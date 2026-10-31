<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { t } from '../i18n'
import { TrendingUp } from '@lucide/vue'
import { formatBytes } from '../format'

const props = defineProps<{ points: { hour: number; bytes: number }[] }>()

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
    const c1y = p1.y + (p2.y - p0.y) / 6
    const c2x = p2.x - (p3.x - p1.x) / 6
    const c2y = p2.y - (p3.y - p1.y) / 6
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
const labels = computed(() =>
  [0, 6, 12, 18, 23].map((i) => {
    const p = props.points[i]
    return { i, text: p ? new Date(p.hour * 1000).getHours().toString().padStart(2, '0') + ':00' : '' }
  }),
)
</script>

<template>
  <section class="traffic-trend" aria-labelledby="trend-title">
    <header>
      <div>
        <span class="eyebrow"><TrendingUp :size="13" /> TRAFFIC PULSE</span>
        <h2 id="trend-title">{{ t('24 小时流量趋势') }}</h2>
        <p>{{ t('全部站点的上行与下行合计，按整点汇总') }}</p>
      </div>
      <div class="trend-meta">
        <span><small>{{ t('近 24 小时') }}</small><strong>{{ formatBytes(total) }}</strong></span>
        <span><small>{{ t('峰值时段') }}</small><strong>{{ hasData ? new Date(peak.hour * 1000).getHours().toString().padStart(2, '0') + ':00' : '—' }}</strong></span>
      </div>
    </header>
    <div class="trend-chart">
      <svg :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" role="img" :aria-label="t('24 小时流量趋势')">
        <defs>
          <linearGradient id="trend-area" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="var(--accent)" stop-opacity=".32" />
            <stop offset="100%" stop-color="var(--accent)" stop-opacity="0" />
          </linearGradient>
          <linearGradient id="trend-line" x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stop-color="var(--accent-2)" />
            <stop offset="100%" stop-color="var(--accent)" />
          </linearGradient>
        </defs>
        <g class="grid">
          <line v-for="n in 4" :key="n" :x1="PAD_X" :x2="W - PAD_X" :y1="PAD_TOP + ((H - PAD_TOP - PAD_BOTTOM) / 4) * (n - 1)" :y2="PAD_TOP + ((H - PAD_TOP - PAD_BOTTOM) / 4) * (n - 1)" />
        </g>
        <path :d="areaPath" fill="url(#trend-area)" class="area" :class="{ drawn }" />
        <path :d="linePath" fill="none" stroke="url(#trend-line)" stroke-width="2.4" stroke-linecap="round" pathLength="1" class="line" :class="{ drawn }" />
        <g v-if="peakPoint" class="peak">
          <circle :cx="peakPoint.x" :cy="peakPoint.y" r="9" class="peak-halo" />
          <circle :cx="peakPoint.x" :cy="peakPoint.y" r="3.6" class="peak-dot" />
        </g>
      </svg>
      <div class="trend-axis">
        <span v-for="label in labels" :key="label.i" :style="{ left: (x(label.i) / W) * 100 + '%' }">{{ label.text }}</span>
      </div>
      <p v-if="!hasData" class="trend-empty">{{ t('近 24 小时暂无流量，站点收到请求后会在这里出现曲线。') }}</p>
      <p v-else class="trend-peak">{{ t('峰值') }} {{ formatBytes(peak.bytes) }}</p>
    </div>
  </section>
</template>

<style scoped>
.traffic-trend {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background:
    radial-gradient(520px 200px at 92% -10%, color-mix(in srgb, var(--accent-2) 14%, transparent), transparent 70%),
    linear-gradient(165deg, var(--surface) 0%, var(--surface-2) 100%);
  box-shadow: var(--shadow-card);
  padding: 20px 22px 14px;
  display: flex;
  flex-direction: column;
}
header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.eyebrow { display: inline-flex; align-items: center; gap: 6px; color: var(--accent-2); font-size: 10.5px; }
h2 { margin: 6px 0 2px; font-size: 19px; letter-spacing: -0.02em; }
header p { margin: 0; color: var(--muted); font-size: 12px; }
.trend-meta { display: flex; gap: 18px; text-align: right; }
.trend-meta span { display: flex; flex-direction: column; gap: 2px; }
.trend-meta small { color: var(--muted); font-size: 10px; }
.trend-meta strong { font-size: 14px; font-variant-numeric: tabular-nums; }
.eyebrow svg { width: 14px; height: 14px; flex: none; }
.trend-chart { position: relative; margin-top: 10px; flex: 1; min-height: 170px; }
.trend-chart > svg { width: 100%; height: 100%; min-height: 170px; display: block; overflow: visible; }
.grid line { stroke: color-mix(in srgb, var(--line) 60%, transparent); stroke-dasharray: 3 6; }
.line { filter: drop-shadow(0 6px 14px var(--glow)); }
.line.drawn { stroke-dasharray: 1; stroke-dashoffset: 0; animation: trend-draw 1.1s var(--ease) both; }
@keyframes trend-draw { from { stroke-dashoffset: 1; } to { stroke-dashoffset: 0; } }
.area { opacity: 0; transition: opacity .7s ease .25s; }
.area.drawn { opacity: 1; }
.peak-halo { fill: color-mix(in srgb, var(--accent) 20%, transparent); }
.peak-dot { fill: var(--accent); }
.trend-axis { position: relative; height: 16px; margin-top: -14px; }
.trend-axis span { position: absolute; transform: translateX(-50%); font-size: 10px; color: var(--muted); font-variant-numeric: tabular-nums; }
.trend-axis span:first-child { transform: none; }
.trend-axis span:last-child { transform: translateX(-100%); }
.trend-empty, .trend-peak { position: absolute; margin: 0; font-size: 11px; color: var(--muted); }
.trend-empty { inset: 42% 0 auto; text-align: center; }
.trend-peak { right: 2px; top: 48%; color: color-mix(in srgb, var(--accent) 80%, var(--text)); }
@media (max-width: 640px) {
  .trend-meta { display: none; }
}
@media (prefers-reduced-motion: reduce) {
  .line.drawn, .peak-halo { animation: none; }
  .area { transition: none; opacity: 1; }
}
</style>
