<script setup lang="ts">
import { ExternalLink } from "@lucide/vue";
import { t } from "../i18n";
import { online, revisions } from "../composables/useState";

import { overviewItems } from "../composables/useOverview";
</script>

<template>
      <section class="overview-grid">
        <a
          v-for="item in overviewItems"
          :key="item.id"
          :href="'#' + item.id"
          class="overview-card"
          ><div class="overview-card-top">
            <span class="host-icon"
              ><component :is="item.icon" :size="21" /></span
            ><ExternalLink :size="15" />
          </div>
          <strong>{{ item.count }}</strong>
          <h2>{{ item.label }}</h2>
          <p>{{ item.detail }}</p>
          <span class="overview-link">{{ t("进入管理 →") }}</span></a
        >
      </section>
      <section

        class="panel overview-status"
      >
        <div class="panel-head">
          <div>
            <h2>{{ t("最近配置状态") }}</h2>
            <p>{{ t("配置应用结果可在配置历史中查看。") }}</p>
          </div>
          <a href="#config-history" class="secondary">{{
            t("查看配置历史")
          }}</a>
        </div>
        <div class="overview-health">
          <span class="dot" :class="{ ok: online }" /><span>{{
            online
              ? t("Caddy 已连接，服务正在运行")
              : t("Caddy 尚未就绪，请检查配置历史和服务状态")
          }}</span
          ><span
            v-if="revisions[0]"
            class="badge"
            :class="{ disabled: revisions[0].status !== 'applied' }"
            >{{ t("最近提交 #") }}{{ revisions[0].id }} ·
            {{
              revisions[0].status === "applied" ? t("已应用") : t("失败")
            }}</span
          >
        </div>
      </section>
</template>
