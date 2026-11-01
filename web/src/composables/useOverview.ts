// Overview-only display computations (attention list, upstream label, feature
// cards). Kept apart from useCore so the overview page can import them without
// dragging the whole store into every consumer.
import { computed } from "vue";
import { Cable, Globe2, RefreshCw, ShieldCheck } from "@lucide/vue";
import { t, tr } from "../i18n";
import { hosts, l4Rules, ddnsRecords, certificates, upstreamHealth } from "./useState";

import { managedCertificates, automaticCertificates } from "./useCertificates";

// Grouping and filtering used to run inside the template on every render.
export function upstreamLabel(id: number) {
  const state = upstreamHealth.value[String(id)];
  if (!state) return "";
  return state.ok
    ? tr`上游 ${Math.round(state.latency_ms)} ms`
    : t("上游无响应");
}
// Sites whose upstream refused a TCP connection. Showing this on the overview
// is the whole point: the proxy itself is healthy, the thing behind it is not.
export const attentionSites = computed(() =>
  hosts.value
    .map((host) => ({
      domain: host.domain,
      target: upstreamHealth.value[String(host.id)]?.target || host.upstream,
      error: upstreamHealth.value[String(host.id)]?.error || "",
      health: upstreamHealth.value[String(host.id)],
    }))
    .filter((entry) => entry.health && !entry.health.ok)
    .map(({ domain, target, error }) => ({ domain, target, error })),
);
export const overviewItems = computed(() => [
  {
    id: "proxy-sites",
    label: t("Web 服务"),
    count: hosts.value.length,
    detail: tr`${hosts.value.filter((h) => h.enabled).length} 个站点已启用`,
    icon: Globe2,
  },
  {
    id: "layer4",
    label: t("端口转发"),
    count: l4Rules.value.length,
    detail: t("TCP / UDP 转发规则"),
    icon: Cable,
  },
  {
    id: "ddns",
    label: t("动态 DNS"),
    count: ddnsRecords.value.length,
    detail: t("公网地址更新任务"),
    icon: RefreshCw,
  },
  {
    id: "certificates",
    label: t("SSL 证书"),
    count:
      certificates.value.length +
      managedCertificates.value.length +
      automaticCertificates.value.length,
    detail: t("托管申请与自定义证书"),
    icon: ShieldCheck,
  },
]);
