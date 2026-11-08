import {registerRefreshTask} from "./useRefresh";
import { apiFetch as fetch } from "../api";
// Traffic aggregation for web sites and layer4 forwards. Both sources are
// merged on the overview; the helpers here precompute per-host/per-rule
// indices so the template never re-derives them on every render.
import { computed, ref, watch } from "vue";
import { t } from "../i18n";
import type {
  Counters,
  Host,
  Layer4Rule,
  SiteTraffic,
  TrafficRankRow,
} from "../types";
import { authenticated, csrfToken, hosts, l4Rules } from "./useState";


export const traffic = ref<Record<string, SiteTraffic>>({});
export const l4Traffic = ref<Record<string, SiteTraffic>>({});
export const trafficError = ref("");
// List statistics always describe 30 days, regardless of the overview's selected range.
const siteTrafficMonth = ref<Record<string, SiteTraffic>>({});
const forwardTrafficMonth = ref<Record<string, SiteTraffic>>({});
export const webListTotals = computed(() => sumRecent(siteTrafficMonth.value));
export const forwardListTotals = computed(() => sumRecent(forwardTrafficMonth.value));
function sumRecent(data: Record<string, SiteTraffic>) {
 const sum=emptyCounters(); for(const item of Object.values(data)) addCounter(sum,item.recent); return sum;
}

const emptyCounters = (): Counters => ({
  requests: 0,
  errors: 0,
  upload: 0,
  download: 0,
});
// The overview metrics combine web sites and port forwards. For layer4 rules
// "requests" carries the connection count, so the sum reads naturally as
// "requests / connections" once both sources are merged.
export const trafficTotals = computed(() => {
  const sum = emptyCounters();
  for (const item of Object.values(traffic.value)) addCounter(sum, item.recent);
  for (const item of Object.values(l4Traffic.value))
    addCounter(sum, item.recent);
  return sum;
});
function addCounter(sum: Counters, from: Counters) {
  sum.requests += from.requests;
  sum.errors += from.errors;
  sum.upload += from.upload;
  sum.download += from.download;
}
// Stable zero-value buckets: the template reads stats many times per render,
// so allocating a fresh object for sites without traffic wasted both memory and
// reference equality (each call looked like a new value to Vue).
const ZERO_COUNTERS = Object.freeze(emptyCounters()) as Counters;
const EMPTY_SITE_TRAFFIC: SiteTraffic = {
  total: ZERO_COUNTERS,
  recent: ZERO_COUNTERS,
  hourly: [],
};
export const siteTrafficIndex = computed(() => {
  const index: Record<number, SiteTraffic> = {};
  for (const host of hosts.value)
    index[host.id] = siteTrafficMonth.value[host.id] || EMPTY_SITE_TRAFFIC;
  return index;
});
export function siteStats(host: Host): SiteTraffic {
  return siteTrafficIndex.value[host.id] || EMPTY_SITE_TRAFFIC;
}
export const l4TrafficIndex = computed(() => {
  const index: Record<number, SiteTraffic> = {};
  for (const rule of l4Rules.value)
    index[rule.id] = forwardTrafficMonth.value[rule.id] || EMPTY_SITE_TRAFFIC;
  return index;
});
export function l4Stats(rule: Layer4Rule): SiteTraffic {
  return l4TrafficIndex.value[rule.id] || EMPTY_SITE_TRAFFIC;
}
// Precomputed once per traffic refresh: 24 aligned buckets plus the peak
// volume per site. The template previously recomputed both for every bar.
function hourIndexFor(summaries: Record<string, SiteTraffic>) {
  const current = Math.floor(Date.now() / 3600000) * 3600;
  const index: Record<
    number,
    { points: ({ hour: number } & Counters)[]; max: number }
  > = {};
  for (const [id, summary] of Object.entries(summaries)) {
    const byHour = new Map<number, { hour: number } & Counters>();
    for (const point of summary.hourly) byHour.set(point.hour, point);
    const points: ({ hour: number } & Counters)[] = [];
    let max = 0;
    for (let offset = 23; offset >= 0; offset--) {
      const hour = current - offset * 3600;
      const point = byHour.get(hour) || { hour, ...ZERO_COUNTERS };
      points.push(point);
      const volume = point.upload + point.download;
      if (volume > max) max = volume;
    }
    index[Number(id)] = { points, max };
  }
  return index;
}
export const trafficHourIndex = computed(() => hourIndexFor(traffic.value));
export const l4HourIndex = computed(() => hourIndexFor(l4Traffic.value));
// Longer windows reuse the same card but aggregate by day. traffic_hours keeps
// 90 days of hourly rows, so 90d is the widest honest window.
export const trafficRange = ref<"24h" | "7d" | "30d" | "90d">("24h");
export const trafficLoadedRange = ref<"24h" | "7d" | "30d" | "90d" | null>(null);
// The window is chosen by the backend, so the series has to be re-fetched.
watch(trafficRange, () => {
  void refreshTraffic();
});
export const trafficDays = computed(() =>
  trafficRange.value === "90d"
    ? 90
    : trafficRange.value === "30d"
      ? 30
      : trafficRange.value === "7d"
        ? 7
        : 0,
);
export const trafficDayStart = computed(() =>
  Math.floor(Date.now() / 86400000) * 86400,
);
// Whole-network series for the overview trend card: web sites plus forwards.
export const trafficTrend = computed(() => {
  const days = trafficDays.value;
  if (!days) {
    const totals = Array.from({ length: 24 }, () => 0);
    for (const current of [
      ...Object.values(trafficHourIndex.value),
      ...Object.values(l4HourIndex.value),
    ])
      current.points.forEach((point, i) => {
        totals[i] = (totals[i] || 0) + point.upload + point.download;
      });
    const hourStart = Math.floor(Date.now() / 3600000) * 3600;
    return totals.map((bytes, i) => ({
      hour: hourStart - (23 - i) * 3600,
      bytes,
    }));
  }
  const totals = Array.from({ length: days }, () => 0);
  for (const summary of [
    ...Object.values(traffic.value),
    ...Object.values(l4Traffic.value),
  ])
    for (const point of summary.daily || []) {
      const index =
        days - 1 - Math.round((trafficDayStart.value - point.day) / 86400);
      if (index >= 0 && index < days)
        totals[index] += point.upload + point.download;
    }
  return totals.map((bytes, i) => ({
    hour: trafficDayStart.value - (days - 1 - i) * 86400,
    bytes,
  }));
});
// Sites and forwards ranked by the volume inside the selected window. The
// kind field lets the range bar tag forwards so they read apart from domains.
export const trafficRanking = computed<TrafficRankRow[]>(() => {
  const rows: TrafficRankRow[] = [];
  for (const host of hosts.value) {
    const recent = traffic.value[host.id]?.recent;
    const bytes = recent ? recent.upload + recent.download : 0;
    if (bytes > 0)
      rows.push({
        key: `site-${host.id}`,
        kind: "site",
        label: host.domain,
        bytes,
        requests: recent ? recent.requests : 0,
      });
  }
  for (const rule of l4Rules.value) {
    const recent = l4Traffic.value[rule.id]?.recent;
    const bytes = recent ? recent.upload + recent.download : 0;
    if (bytes > 0)
      rows.push({
        key: `fwd-${rule.id}`,
        kind: "forward",
        label:
          rule.name || `${rule.protocol.toUpperCase()} ${rule.listen_port}`,
        bytes,
        requests: recent ? recent.requests : 0,
      });
  }
  rows.sort((a, b) => b.bytes - a.bytes);
  return rows.slice(0, 6);
});
let trafficSequence = 0;
export async function refreshTraffic() {
  if (!authenticated.value) return;
  const seq = ++trafficSequence;
  const range = trafficRange.value;
  try {
    const responses = await Promise.all([
      fetch(`/api/v1/proxy-hosts/traffic?range=${range}`),
      fetch(`/api/v1/layer4-rules/traffic?range=${range}`),
      ...(range === "30d" ? [] : [fetch("/api/v1/proxy-hosts/traffic?range=30d"),fetch("/api/v1/layer4-rules/traffic?range=30d")]),
    ]);
    const [siteRes,l4Res] = responses;
    if (siteRes.status === 401 || l4Res.status === 401) {
      authenticated.value = false;
      csrfToken.value = "";
      return;
    }
    if (responses.some(response=>!response.ok)) throw Error(t("流量统计暂不可用"));
    const [siteData, l4Data] = await Promise.all([
      siteRes.json(),
      l4Res.json(),
    ]);
    if (seq !== trafficSequence) return;
    traffic.value = siteData;
    l4Traffic.value = l4Data;
    trafficLoadedRange.value = range;
    if (range === '30d') { siteTrafficMonth.value = siteData; forwardTrafficMonth.value = l4Data; }
    else { const [webMonth,forwardMonth]=await Promise.all([responses[2]!.json(),responses[3]!.json()]); if(seq!==trafficSequence)return;siteTrafficMonth.value=webMonth;forwardTrafficMonth.value=forwardMonth; }
    trafficError.value = "";
  } catch (e) {
    if (seq !== trafficSequence) return;
    trafficError.value = e instanceof Error ? t(e.message) : t("统计读取失败");
  }
}

registerRefreshTask("traffic",refreshTraffic,"sites");
