<script setup lang="ts">
import { RefreshCw } from "@lucide/vue";
import { t } from "../i18n";
import {
  loadSiteLogs,
  siteLogs,
  viewingLogSite,
} from "../composables/useHosts";
import AccessLogTable from "./AccessLogTable.vue";

defineProps<{ autoRefresh: boolean; refreshInterval: number }>();
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
          <p class="form-intro">{{ viewingLogSite.upstream }}</p>
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
</template>
