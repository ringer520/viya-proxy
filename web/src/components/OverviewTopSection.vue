<script setup lang="ts">
import { Activity, Globe2, History } from "@lucide/vue";
import { t } from "../i18n";
import SystemGauges from "./SystemGauges.vue";
import TrafficTrend from "./TrafficTrend.vue";
import AttentionPanel from "./AttentionPanel.vue";
import { certExpiring, hosts, online, revisions } from "../composables/useState";

import { trafficRange, trafficTrend } from "../composables/useTraffic";
import { attentionSites } from "../composables/useOverview";
import { autoRefresh, documentHidden, pauseHidden } from "../composables/usePreferences";
</script>

<template>
      <div class="overview-telemetry">
        <SystemGauges
          :auto-refresh="autoRefresh && (!pauseHidden || !documentHidden)"
        />
        <TrafficTrend
          :points="trafficTrend"
          :granularity="trafficRange === '24h' ? 'hour' : 'day'"
        />
      </div>
      <AttentionPanel

        :certificates="certExpiring"
        :sites="attentionSites"
      />
      <section class="stats kpi-row">
        <article class="kpi">
          <span class="kpi-icon kpi-blue"><Globe2 :size="20" /></span>
          <div class="kpi-body">
            <span>{{ t("代理站点") }}</span
            ><strong>{{ hosts.length }}</strong
            ><small>{{ t("已配置规则") }}</small>
          </div>
        </article>
        <article class="kpi">
          <span class="kpi-icon kpi-green"><Activity :size="20" /></span>
          <div class="kpi-body">
            <span>{{ t("运行状态") }}</span
            ><strong class="status-text"
              ><i class="dot" :class="{ ok: online }"></i
              >{{ online ? t("正常") : t("未连接") }}</strong
            ><small>Caddy Admin API</small>
          </div>
        </article>
        <article class="kpi">
          <span class="kpi-icon kpi-purple"><History :size="20" /></span>
          <div class="kpi-body">
            <span>{{ t("最近应用") }}</span
            ><strong>{{
              revisions.find((r) => r.status === "applied")?.id ?? "—"
            }}</strong
            ><small>{{ t("配置版本") }}</small>
          </div>
        </article>
      </section>
</template>
