<script setup lang="ts">
import { Cable, Pencil, ScrollText, Server, Trash2, Search } from "@lucide/vue";
import { t } from "../i18n";
import { formatBytes } from "../format";
import { clearListFilters, hasListFilter, listSearch, statusFilter } from "../composables/useState";
import { nextTick, ref } from "vue";

import { l4BarHeight, hourlyL4Traffic, l4Stats } from "../composables/useTraffic";
import { l4Logs, l4PreviewLayout, l4Toggling, openL4, previewL4, removeL4, toggleL4, visibleL4 } from "../composables/useNetwork";
import { trafficCharts } from "../composables/usePreferences";
const searchOpen = ref(false);
const searchInput = ref<HTMLInputElement | null>(null);
async function toggleSearch() {
  searchOpen.value = !searchOpen.value;
  if (searchOpen.value) { await nextTick(); searchInput.value?.focus(); }
  else listSearch.value = "";
}
</script>

<template>
      <section class="panel provider-panel">
        <div class="panel-head">
          <div class="list-page-heading">
            <div class="list-page-title">
            <h2>{{ t("TCP / UDP 端口转发") }}</h2>
            <p>
              {{ t("由 Caddy Layer4 直接监听主机端口，请确认防火墙已放行。") }}
            </p>
            </div>
            <div class="inline-list-controls">
              <button class="icon-btn" type="button" :aria-expanded="searchOpen" :aria-label="t('搜索当前列表')" :title="t('搜索当前列表')" @click="toggleSearch"><Search :size="17" /></button>
              <input v-if="searchOpen" ref="searchInput" v-model="listSearch" type="search" :placeholder="t('输入域名、名称或地址，支持多个关键词')" @keydown.esc="toggleSearch" />
              <label class="inline-list-status"><span>{{ t("状态") }}</span><select v-model="statusFilter"><option value="all">{{ t("全部状态") }}</option><option value="enabled">{{ t("已启用") }}</option><option value="disabled">{{ t("已停用") }}</option></select></label>
              <span class="inline-list-count" role="status">{{ visibleL4.length }} {{ t("项") }}</span>
              <button v-if="hasListFilter" class="secondary" @click="clearListFilters">{{ t("清除筛选") }}</button>
            </div>
          </div>
        </div>
        <div v-if="visibleL4.length" class="host-list">
          <div
            v-for="rule in visibleL4"
            :key="rule.id"
            role="group"
            :aria-label="rule.name || String(rule.listen_port)"
            class="host-row l4-row"
          >
            <div class="host-icon l4-icon"><Server :size="18" /></div>
            <div class="host-main site-main">
              <div class="site-title">
                <strong class="entity-name">{{
                  rule.name || t("未命名转发")
                }}</strong
                ><small class="l4-proto">{{
                  rule.protocol.toUpperCase()
                }}</small>
              </div>
              <div class="l4-detail">
                <span class="l4-port"
                  ><small>{{ t("监听端口") }}</small
                  ><b>{{ rule.listen_port }}</b></span
                ><span class="l4-target"
                  ><small>{{ t("监听目标") }}</small
                  ><code>{{ rule.upstream_address }}</code></span
                ><span class="l4-state" :class="{ off: !rule.enabled }"
                  ><i />{{ rule.enabled ? t("已启用") : t("已停用") }}</span
                ><div class="site-log-preview-wrap">
                  <button
                    class="site-log-trigger"
                    :aria-label="t('预览转发日志')"
                    :title="t('悬浮预览转发日志')"
                    @mouseenter="previewL4(rule, $event)"
                    @focus="previewL4(rule, $event)"
                  >
                    <ScrollText :size="15" />
                  </button>
                  <div role="tooltip" class="site-log-tooltip" :style="l4PreviewLayout">
                    <div class="site-log-heading">
                      <b
                        >{{ rule.name || t("端口转发") }} ·
                        {{ rule.listen_port }}</b
                      ><span>{{ t("最近") }} 50 {{ t("条") }}</span>
                    </div>
                    <p v-if="l4Logs[rule.id]?.loading" class="site-log-empty">
                      {{ t("正在读取转发日志…") }}
                    </p>
                    <p
                      v-else-if="l4Logs[rule.id]?.error"
                      class="site-log-empty"
                    >
                      {{ l4Logs[rule.id]?.error }}
                    </p>
                    <p
                      v-else-if="!l4Logs[rule.id]?.events?.length"
                      class="site-log-empty"
                    >
                      {{ t("暂无转发日志；连接失败时会记录到这里。") }}
                    </p>
                    <ul v-else class="l4-log-list">
                      <li
                        v-for="(log, index) in l4Logs[rule.id].events"
                        :key="index"
                      >
                        <time>{{
                          new Date((log.ts || 0) * 1000).toLocaleString()
                        }}</time>
                        <b
                          class="log-level"
                          :class="String(log.level).toLowerCase()"
                          >{{ log.level }}</b
                        ><span
                          >{{ log.msg
                          }}<small v-if="log.error"> · {{ log.error }}</small></span
                        ><small v-if="log.remote"
                          >{{ t("来源") }} {{ log.remote }}</small
                        >
                      </li>
                    </ul>
                  </div>
                </div>
              </div>
            </div>
            <div class="site-traffic">
              <div>
                <span>{{ t("近24小时") }}</span
                ><strong
                  >{{ l4Stats(rule).recent.requests.toLocaleString() }}
                  {{ t("连接") }}</strong
                ><span
                  >↑ {{ formatBytes(l4Stats(rule).recent.upload) }} · ↓
                  {{ formatBytes(l4Stats(rule).recent.download) }}</span
                >
              </div>
              <div
                v-if="trafficCharts"
                class="traffic-spark"
                :aria-label="t('最近24小时流量趋势')"
              >
                <i
                  v-for="point in hourlyL4Traffic(rule)"
                  :key="point.hour"
                  :style="{
                    height:
                      l4BarHeight(rule, point.upload + point.download) + 'px',
                  }"
                  :title="
                    new Date(point.hour * 1000).toLocaleString() +
                    ' · ' +
                    point.requests +
                    t(' 连接 · ') +
                    formatBytes(point.upload + point.download)
                  "
                ></i>
              </div>
              <small
                >{{ t("累计") }}
                {{ l4Stats(rule).total.requests.toLocaleString() }}
                {{ t("连接 ·") }}
                {{
                  formatBytes(
                    l4Stats(rule).total.upload +
                      l4Stats(rule).total.download,
                  )
                }}</small
              >
            </div>
            <button
              class="site-switch"
              type="button"
              role="switch"
              :aria-checked="rule.enabled"
              :aria-label="
                t('启用转发') + ' ' + (rule.name || rule.listen_port)
              "
              :class="{ on: rule.enabled }"
              :disabled="!!l4Toggling[rule.id]"
              @click="toggleL4(rule)"
            >
              <span /></button
            ><button
              class="icon-btn"
              :aria-label="t('编辑')"
              :title="t('编辑')"
              @click="openL4(rule)"
            >
              <Pencil :size="15" /></button
            ><button
              class="icon-btn danger"
              :aria-label="t('删除')"
              :title="t('删除')"
              @click="removeL4(rule)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="empty">
          <div class="empty-icon"><Cable :size="23" /></div>
          <strong>{{
            hasListFilter
              ? t("未找到匹配项目")
              : t("尚未配置四层端口转发")
          }}</strong
          ><span>{{ t("添加转发后，由 Caddy Layer4 直接监听主机端口。") }}</span>
        </div>
      </section>
</template>
