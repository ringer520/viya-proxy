<script setup lang="ts">
import { apiFetch as fetch } from "../api";
import AnimatedMetric from '../AnimatedMetric.vue'

import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { t } from '../i18n'
import { Cpu, MemoryStick, HardDrive, RefreshCw, Activity } from '@lucide/vue'

const props = defineProps<{ autoRefresh: boolean; compact?: boolean }>()
type Usage = { percent: number; used: number; total: number; path?: string }
type HostInfo = { hostname?: string; platform?: string; platform_version?: string; kernel?: string; arch?: string }
type Metrics = { cpu_percent?: number; cores: number; platform: string; host?: HostInfo; uptime?: number; memory?: Usage; disk?: Usage; sampled_at: number; errors: string[] }

const data = ref<Metrics | null>(null)
const error = ref('')
const busy = ref(false)
// Keep a short local history so each gauge can draw its own trend line.
const history = ref<Record<string, number[]>>({ cpu: [], memory: [], disk: [] })

function push(key: string, value: number | undefined) {
  if (value === undefined) return
  const list = history.value[key] || []
  history.value[key] = [...list, Math.max(0, Math.min(100, value))].slice(-24)
}
function bytes(v: number) {
  return (v / 1024 ** 3).toFixed(1) + ' GiB'
}
const gauges = computed(() => [
  {
    key: 'cpu',
    label: 'CPU',
    icon: Cpu,
    value: data.value?.cpu_percent,
    detail: data.value ? data.value.cores + ' ' + t('逻辑核心') : t('等待采样'),
    color: 'var(--metric-cpu)',
    glow: 'rgba(56,189,248,.45)',
  },
  {
    key: 'memory',
    label: t('内存'),
    icon: MemoryStick,
    value: data.value?.memory?.percent,
    detail: data.value?.memory ? bytes(data.value.memory.used) + ' / ' + bytes(data.value.memory.total) : t('暂不可用'),
    color: 'var(--metric-memory)',
    glow: 'rgba(167,139,250,.45)',
  },
  {
    key: 'disk',
    label: t('磁盘'),
    icon: HardDrive,
    value: data.value?.disk?.percent,
    detail: data.value?.disk ? bytes(data.value.disk.used) + ' / ' + bytes(data.value.disk.total) : t('暂不可用'),
    color: 'var(--success)',
    glow: 'rgba(52,211,153,.45)',
  },
])
const uptime = computed(() => {
  const s = data.value?.uptime
  if (s === undefined) return '—'
  return Math.floor(s / 86400) + 'd ' + Math.floor((s % 86400) / 3600) + 'h ' + Math.floor((s % 3600) / 60) + 'm'
})
// gopsutil reports distro ids in lower case ("debian", "rockylinux"); map the
// common ones to their real spelling and capitalise anything else.
const PLATFORM_LABELS: Record<string, string> = {
  debian: 'Debian',
  ubuntu: 'Ubuntu',
  centos: 'CentOS',
  almalinux: 'AlmaLinux',
  rockylinux: 'Rocky Linux',
  fedora: 'Fedora',
  arch: 'Arch Linux',
  alpine: 'Alpine',
  opensuse: 'openSUSE',
  openwrt: 'OpenWrt',
  raspbian: 'Raspbian',
  linuxmint: 'Linux Mint',
}
function prettyPlatform(name?: string) {
  if (!name) return ''
  const key = name.toLowerCase().replace(/\s+/g, '')
  return PLATFORM_LABELS[key] || name.charAt(0).toUpperCase() + name.slice(1)
}
const osLabel = computed(() => {
  const host = data.value?.host
  if (!host) return '—'
  const name = prettyPlatform(host.platform) || data.value?.platform || ''
  return [name, host.platform_version].filter(Boolean).join(' ') || '—'
})
const osDetail = computed(() => {
  const host = data.value?.host
  if (!host) return ''
  return [
    osLabel.value,
    host.kernel,
    host.arch,
    host.hostname,
  ].filter(Boolean).join(' · ')
})
const R = 52
const CIRC = 2 * Math.PI * R

function dash(value: number | undefined) {
  const pct = Math.max(0, Math.min(100, value ?? 0))
  return `${(pct / 100) * CIRC} ${CIRC}`
}
function trend(key: string) {
  const list = history.value[key] || []
  if (list.length < 2) return ''
  const step = 100 / (list.length - 1)
  return list.map((v, i) => `${(i * step).toFixed(1)},${(26 - (v / 100) * 24).toFixed(1)}`).join(' ')
}

async function load() {
  if (busy.value) return
  busy.value = true
  try {
    const r = await fetch('/api/v1/system-metrics')
    if (!r.ok) throw Error(t('读取服务器指标失败'))
    const next: Metrics = await r.json()
    data.value = next
    error.value = ''
    push('cpu', next.cpu_percent)
    push('memory', next.memory?.percent)
    push('disk', next.disk?.percent)
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t('读取失败')
  } finally {
    busy.value = false
  }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  timer = setInterval(() => {
    if (props.autoRefresh) void load()
  }, 5000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <section class="system-observatory" :class="{compact}" aria-labelledby="system-title">
    <header class="telemetry-head">
      <div class="telemetry-title">
        <span v-if="!compact" class="eyebrow"><Activity :size="13" /> HOST TELEMETRY</span>
        <h2 id="system-title">{{ t('服务器监控') }}</h2>
        <p v-if="!compact">{{ t('服务器实时资源占用') }} · {{ data?.platform || '—' }}</p>
      </div>
      <div class="telemetry-meta">
        <span class="live-pill"><i />{{ autoRefresh ? t('实时采样') : t('已暂停') }}</span>
        <span class="meta-item meta-os" :title="osDetail || undefined"><small>{{ t('系统') }}</small><strong>{{ osLabel }}</strong></span>
        <span class="meta-item"><small>{{ t('运行时长') }}</small><strong>{{ uptime }}</strong></span>
        <span v-if="!compact" class="meta-item meta-disk"><small>{{ t('数据目录所在磁盘') }}</small><strong>{{ data?.disk?.path || '—' }}</strong></span>
        <button class="icon-btn telemetry-refresh" :disabled="busy" :title="t('刷新')" :aria-label="t('刷新')" @click="load">
          <RefreshCw :size="16" :class="{ spinning: busy }" />
        </button>
      </div>
    </header>

    <p v-if="error" class="error" role="alert">{{ error }} · {{ t('以下为上次采样') }}</p>

    <div class="gauge-grid">
      <article
        v-for="g in gauges"
        :key="g.key"
        class="gauge-card"
        :class="{ alert: (g.value ?? 0) >= 90 }"
        :style="{ '--gauge': g.value !== undefined && g.value >= 90 ? 'var(--danger)' : g.color, '--gauge-glow': g.value !== undefined && g.value >= 90 ? 'rgba(251,113,133,.5)' : g.glow }"
      >
        <div class="gauge-top">
          <span class="gauge-icon"><component :is="g.icon" :size="15" /></span>
          <strong>{{ g.label }}</strong>
          <span class="gauge-state">{{ g.value === undefined ? t('等待采样') : g.value >= 90 ? t('高占用') : compact ? '' : t('实时') }}</span>
        </div>
        <div class="gauge-body">
          <svg class="gauge-ring" viewBox="0 0 132 132" role="img" :aria-label="g.label + ': ' + (g.value === undefined ? t('暂不可用') : g.value.toFixed(1) + '%')">
            <defs>
              <linearGradient :id="'ring-' + g.key" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0%" :stop-color="g.value !== undefined && g.value >= 90 ? 'var(--danger)' : g.color" stop-opacity=".55" />
                <stop offset="100%" :stop-color="g.value !== undefined && g.value >= 90 ? 'var(--danger)' : g.color" />
              </linearGradient>
            </defs>
            <circle cx="66" cy="66" :r="R" class="ring-track" />
            <circle
              cx="66"
              cy="66"
              :r="R"
              class="ring-value"
              :stroke="'url(#ring-' + g.key + ')'"
              :stroke-dasharray="dash(g.value)"
              transform="rotate(-90 66 66)"
            />
          </svg>
          <div class="gauge-center">
            <b><AnimatedMetric v-if="g.value !== undefined" :value="g.value" format="decimal"/><template v-else>—</template></b><i>%</i>
          </div>
        </div>
        <svg v-if="trend(g.key)" class="gauge-spark" viewBox="0 0 100 26" preserveAspectRatio="none" aria-hidden="true">
          <polyline :points="trend(g.key)" fill="none" :stroke="g.value !== undefined && g.value >= 90 ? 'var(--danger)' : g.color" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        <p class="gauge-detail">{{ g.detail }}</p>
      </article>
    </div>

    <footer v-if="!compact" class="telemetry-foot">
      <span class="system-pulse" />{{ t('真实服务器数据') }} · {{ t('每 5 秒刷新') }}<span v-if="!autoRefresh"> · {{ t('自动刷新已暂停') }}</span>
      <time v-if="data">{{ new Date(data.sampled_at * 1000).toLocaleTimeString() }}</time>
      <span v-if="data?.errors.length" class="error">{{ data.errors.join(' · ') }}</span>
    </footer>
    <p v-if="compact && data?.errors.length" class="compact-sample-error" role="status">{{data.errors.join(' · ')}}</p>
  </section>
</template>

<style scoped>
.system-observatory {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background:
    radial-gradient(560px 200px at 8% -20%, color-mix(in srgb, var(--accent) 16%, transparent), transparent 70%),
    linear-gradient(165deg, var(--surface) 0%, var(--surface-2) 100%);
  box-shadow: var(--shadow-card);
  padding: 20px 22px 16px;
  color: var(--text);
}
.telemetry-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; flex-wrap: wrap; }
.telemetry-title .eyebrow { display: inline-flex; align-items: center; gap: 6px; color: var(--accent); font-size: 10.5px; }
.telemetry-title h2 { margin: 6px 0 2px; font-size: 19px; letter-spacing: -0.02em; color: var(--text); }
.telemetry-title p { margin: 0; color: var(--muted); font-size: 12px; }
.telemetry-meta { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; }
.meta-item { display: flex; flex-direction: column; gap: 2px; }
.meta-item small { color: var(--muted); font-size: 10px; }
.meta-item strong { font-size: 13px; font-variant-numeric: tabular-nums; color: var(--text); }
.meta-os strong { max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 550; }
.meta-disk strong { max-width: 190px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 550; }
.live-pill {
  display: inline-flex; align-items: center; gap: 7px;
  padding: 4px 10px; border-radius: 999px;
  border: 1px solid color-mix(in srgb, var(--accent) 32%, transparent);
  background: var(--accent-soft); color: var(--accent);
  font-size: 11px; font-weight: 650;
}
.live-pill i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.gauge-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(148px, 1fr)); gap: 14px; margin-top: 18px; }
.gauge-card {
  position: relative; overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 15px;
  padding: 14px 16px 12px;
  background: linear-gradient(160deg, color-mix(in srgb, var(--surface-2) 88%, transparent), var(--surface));
  transition: border-color .2s var(--ease), transform .2s var(--ease), box-shadow .2s var(--ease);
}
.gauge-card::after {
  content: ""; position: absolute; right: -40px; top: -60px; width: 140px; height: 140px;
  background: radial-gradient(circle, color-mix(in srgb, var(--gauge) 24%, transparent), transparent 68%);
  pointer-events: none;
}
.gauge-card:hover { transform: translateY(-2px); border-color: color-mix(in srgb, var(--gauge) 45%, var(--line)); box-shadow: 0 18px 34px -26px var(--gauge-glow); }
.gauge-card.alert { border-color: color-mix(in srgb, #fb7185 46%, var(--line)); }
.gauge-top { display: flex; align-items: center; gap: 8px; }
.gauge-icon {
  display: grid; place-items: center; width: 26px; height: 26px; border-radius: 8px;
  background: color-mix(in srgb, var(--gauge) 16%, transparent); color: var(--gauge);
}
.gauge-top strong { font-size: 12.5px; letter-spacing: .02em; color: var(--text); }
.gauge-state { margin-left: auto; font-size: 10.5px; color: var(--muted); }
.gauge-card.alert .gauge-state { color: #fb7185; font-weight: 650; }
.gauge-body { position: relative; display: grid; place-items: center; margin: 2px 0 -6px; }
.gauge-ring { width: min(100%, 134px); height: auto; overflow: visible; }
.ring-track { fill: none; stroke: color-mix(in srgb, var(--line) 90%, transparent); stroke-width: 9; }
.ring-value {
  fill: none; stroke-width: 9; stroke-linecap: round;
  filter: drop-shadow(0 0 6px var(--gauge-glow));
  transition: stroke-dasharray .7s var(--ease);
}
.gauge-center { position: absolute; display: flex; align-items: baseline; gap: 2px; }
.gauge-center b { font-size: 30px; font-weight: 700; letter-spacing: -0.03em; font-variant-numeric: tabular-nums; color: var(--text); }
.gauge-center i { font-style: normal; font-size: 12px; color: var(--muted); }
.gauge-spark { display: block; width: 100%; height: 24px; opacity: .85; }
.gauge-detail { margin: 4px 0 0; font-size: 11.5px; color: var(--muted); font-variant-numeric: tabular-nums; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.telemetry-foot { display: flex; align-items: center; gap: 8px; margin-top: 12px; color: var(--muted); font-size: 10.5px; flex-wrap: wrap; }
.telemetry-foot time { margin-left: auto; font-variant-numeric: tabular-nums; }
.system-pulse { width: 7px; height: 7px; border-radius: 50%; background: var(--accent); box-shadow: 0 0 0 4px var(--accent-soft); }
.telemetry-refresh { width: 32px; height: 32px; }
.spinning { animation: viya-spin .9s linear infinite; }
@media (max-width: 900px) {
  .meta-disk { display: none; }
}
@media (prefers-reduced-motion: reduce) {
  .gauge-card { transition: none; }
  .ring-value { transition: none; }
}
</style>

<style scoped>
.compact.system-observatory{padding:16px 20px;border-radius:18px;box-shadow:none;background:linear-gradient(130deg,color-mix(in srgb,var(--accent) 4%,var(--surface)),var(--surface));overflow:visible}
.compact .telemetry-head{align-items:center;gap:8px}
.compact .telemetry-title h2{font-size:13px;margin:0}
.compact .telemetry-meta{gap:12px}
.compact .meta-item{flex-direction:row;align-items:center;gap:7px}
.compact .meta-item strong{font-size:11px}
.compact .live-pill{font-size:10px;padding:3px 8px}
.compact .telemetry-refresh{min-height:26px;min-width:26px;width:26px;height:26px}
.compact .gauge-grid{grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin-top:12px}
.compact .gauge-card{display:grid;grid-template-columns:86px minmax(0,1fr);grid-template-rows:22px 22px 22px;align-items:center;gap:0 12px;padding:10px 12px;min-height:86px;border-radius:13px;box-shadow:none;overflow:hidden}
.compact .gauge-body{grid-column:1;grid-row:1 / 4;width:86px;height:86px;margin:0}
.compact .gauge-ring{width:86px;height:86px}
.compact .gauge-center b{font-size:21px}
.compact .gauge-center i{font-size:10px}
.compact .gauge-top{grid-column:2;grid-row:1;gap:5px;min-width:0}
.compact .gauge-top strong{font-size:12px}
.compact .gauge-icon{width:22px;height:22px;border-radius:6px}
.compact .gauge-state{font-size:9px}
.compact .gauge-detail{grid-column:2;grid-row:2;font-size:10px;text-align:left;margin:0;white-space:normal;line-height:1.5}
.compact .gauge-spark{grid-column:2;grid-row:3;position:static;width:100%;height:20px;margin:0;opacity:.8}
.compact-sample-error{font-size:11px;color:#e05264;margin:8px 0 0}
@media(max-width:1200px){.compact .gauge-card{grid-template-columns:70px minmax(0,1fr);padding:8px;gap:0 6px}.compact .gauge-body,.compact .gauge-ring{width:70px;height:70px}.compact .gauge-center b{font-size:18px}}
@media(max-width:700px){.compact .gauge-grid{grid-template-columns:1fr}.compact .gauge-card{grid-template-columns:74px minmax(0,1fr);padding:8px 14px;gap:0 16px}.compact .gauge-state{margin-left:auto}.compact.system-observatory{padding:14px}.compact .meta-item{display:none}}
</style>
