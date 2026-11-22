<script setup lang="ts">
import { RefreshCw } from "@lucide/vue";
import { t } from "../i18n";
import {
  loadSiteLogs,
  siteLogs,
  viewingLogSite,
} from "../composables/useHosts";
import { l4Logs, loadL4Logs, viewingLogL4 } from "../composables/useNetwork";
import type { Layer4Rule } from "../types";
import AccessLogTable from "./AccessLogTable.vue";

defineProps<{ autoRefresh: boolean; refreshInterval: number }>();

function l4Endpoint(rule: Layer4Rule) {
  const host = rule.listen_address.includes(":")
    ? `[${rule.listen_address}]`
    : rule.listen_address;
  return `${host}:${rule.listen_port}`;
}
</script>

<template>
  <div
    v-if="viewingLogSite"
    class="overlay"
    v-dialog
    @click.self="viewingLogSite = null"
    @keydown.esc="viewingLogSite = null"
  >
    <section
      class="modal site-log-modal"
      role="dialog"
      aria-modal="true"
      :aria-label="t('站点访问日志')"
    >
      <div class="modal-top">
        <div>
          <span class="eyebrow">{{ t("站点访问日志") }}</span>
          <h2>{{ viewingLogSite.domain }}</h2>
          <p class="form-intro">
            {{
              viewingLogSite.type === "redirect"
                ? viewingLogSite.redirect_target
                : viewingLogSite.type === "static" ||
                    viewingLogSite.type === "files"
                  ? viewingLogSite.root_path || t("托管目录")
                  : viewingLogSite.upstream
            }}
          </p>
        </div>
        <button
          class="icon-btn"
          :aria-label="t('关闭访问日志')"
          @click="viewingLogSite = null"
        >
          ×
        </button>
      </div>
      <div class="site-log-toolbar">
        <span
          >{{ t("最近 50 条 ·") }}
          {{
            autoRefresh
              ? t("每 ") + refreshInterval + t(" 秒刷新")
              : t("自动刷新已暂停")
          }}
          {{ t("· 查询参数和凭据已隐藏") }}</span
        ><button
          class="secondary"
          @click="loadSiteLogs(viewingLogSite, true)"
        >
          <RefreshCw :size="14" />{{ t("刷新") }}
        </button>
      </div>
      <AccessLogTable
        :events="siteLogs[viewingLogSite.id]?.events || []"
        :loading="siteLogs[viewingLogSite.id]?.loading"
        :error="siteLogs[viewingLogSite.id]?.error"
      />
      <div class="modal-actions">
        <button class="secondary" @click="viewingLogSite = null">
          {{ t("关闭") }}
        </button>
      </div>
    </section>
  </div>

  <div
    v-if="viewingLogL4"
    class="overlay"
    v-dialog
    @click.self="viewingLogL4 = null"
    @keydown.esc="viewingLogL4 = null"
  >
    <section
      class="modal site-log-modal"
      role="dialog"
      aria-modal="true"
      :aria-label="t('转发日志')"
    >
      <div class="modal-top">
        <div>
          <span class="eyebrow">{{ t("转发日志") }}</span>
          <h2>{{ viewingLogL4.name || t("端口转发") }}</h2>
          <p class="form-intro">
            {{ l4Endpoint(viewingLogL4) }} → {{ viewingLogL4.upstream_address }}
          </p>
        </div>
        <button
          class="icon-btn"
          :aria-label="t('关闭转发日志')"
          @click="viewingLogL4 = null"
        >
          ×
        </button>
      </div>
      <div class="site-log-toolbar">
        <span
          >{{ t("最近 50 条 ·") }}
          {{
            autoRefresh
              ? t("每 ") + refreshInterval + t(" 秒刷新")
              : t("自动刷新已暂停")
          }}</span
        ><button
          class="secondary"
          @click="loadL4Logs(viewingLogL4, true)"
        >
          <RefreshCw :size="14" />{{ t("刷新") }}
        </button>
      </div>
      <p
        v-if="
          l4Logs[viewingLogL4.id]?.loading &&
          !l4Logs[viewingLogL4.id]?.events?.length
        "
        class="site-log-empty"
      >
        {{ t("正在读取转发日志…") }}
      </p>
      <p v-else-if="l4Logs[viewingLogL4.id]?.error" class="site-log-empty">
        {{ l4Logs[viewingLogL4.id]?.error }}
      </p>
      <p
        v-else-if="!l4Logs[viewingLogL4.id]?.events?.length"
        class="site-log-empty"
      >
        {{ t("暂无转发日志；连接失败时会记录到这里。") }}
      </p>
      <ul v-else class="l4-log-list">
        <li
          v-for="(log, index) in l4Logs[viewingLogL4.id]?.events || []"
          :key="index"
        >
          <time>{{ new Date((log.ts || 0) * 1000).toLocaleString() }}</time>
          <b class="log-level" :class="String(log.level).toLowerCase()">{{
            log.level
          }}</b>
          <span
            >{{ log.msg }}<small v-if="log.error"> · {{ log.error }}</small></span
          >
          <small v-if="log.remote">{{ t("来源") }} {{ log.remote }}</small>
        </li>
      </ul>
      <div class="modal-actions">
        <button class="secondary" @click="viewingLogL4 = null">
          {{ t("关闭") }}
        </button>
      </div>
    </section>
  </div>
</template>
