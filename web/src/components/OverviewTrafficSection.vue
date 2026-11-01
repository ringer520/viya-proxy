<script setup lang="ts">
import { t } from "../i18n";
import { formatBytes } from "../format";
import TrafficRangeBar from "./TrafficRangeBar.vue";
import { trafficError, trafficRanking, trafficRange, trafficTotals } from "../composables/useTraffic";
import { autoRefresh, refreshInterval } from "../composables/usePreferences";
</script>

<template>
      <section class="traffic-summary">
        <div class="traffic-summary-heading">
          <div>
            <h2>{{ t("站点与转发流量") }}</h2>
            <p>
              {{ t("近 24 小时按整点汇总，")
              }}{{
                autoRefresh
                  ? t("每 ") + refreshInterval + t(" 秒刷新")
                  : t("自动刷新已暂停")
              }}{{
                t(
                  "。合并 Web 站点与端口转发：上行/下行为客户端请求体/响应体字节数，转发按连接字节计数；累计统计从本地保留日志接入开始，重启后保留。",
                )
              }}
            </p>
          </div>
          <span class="badge" :class="{ disabled: !!trafficError }">{{
            trafficError || (autoRefresh ? t("自动更新") : t("手动刷新"))
          }}</span>
        </div>
        <div class="traffic-metrics">
          <article>
            <span>{{ t("请求 / 连接") }}</span
            ><strong
              >{{ trafficTotals.requests.toLocaleString()
              }}<small>{{ t("次") }}</small></strong
            >
          </article>
          <article>
            <span>{{ t("上行流量") }}</span
            ><strong>{{ formatBytes(trafficTotals.upload) }}</strong>
          </article>
          <article>
            <span>{{ t("下行流量") }}</span
            ><strong>{{ formatBytes(trafficTotals.download) }}</strong>
          </article>
          <article>
            <span>{{ t("错误请求（4xx / 5xx）") }}</span
            ><strong
              >{{ trafficTotals.errors.toLocaleString()
              }}<small>{{ t("次") }}</small></strong
            >
          </article>
        </div>
        <TrafficRangeBar v-model:range="trafficRange" :ranking="trafficRanking" />
      </section>
</template>
