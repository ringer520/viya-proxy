<script setup lang="ts">
import { Network, Pencil, RefreshCw, Trash2, Search } from "@lucide/vue";
import { t } from "../i18n";
import type { DDNSRecord } from "../types";
import { clearListFilters, hasListFilter, listSearch, readOnly, statusFilter } from "../composables/useState";
import { nextTick, ref } from "vue";

import { ddnsFeedback, ddnsRunning, ddnsToggling, formatDDNSTime, openDDNS, removeDDNS, runDDNS, toggleDDNS, visibleDDNS } from "../composables/useNetwork";
const searchOpen = ref(false);
const searchInput = ref<HTMLInputElement | null>(null);
async function toggleSearch() {
  searchOpen.value = !searchOpen.value;
  if (searchOpen.value) { await nextTick(); searchInput.value?.focus(); }
  else listSearch.value = "";
}

const STAGE_TEXT: Record<string, string> = {
  address: "获取地址",
  credentials: "读取凭据",
  compare: "比较 DNS",
  update: "提交修改",
  verify: "验证服务商结果",
};
const RESULT_TEXT: Record<string, string> = {
  updated: "已更新并验证",
  unchanged: "地址未变化",
  unverified: "已提交 · 验证待完成",
  failed: "更新失败",
};
function recordHost(record: DDNSRecord) {
  return record.record_name === "@"
    ? record.zone
    : `${record.record_name}.${record.zone}`;
}
function sourceText(record: DDNSRecord) {
  if (record.record_type === "CNAME")
    return t("别名 → ") + (record.cname_target || t("未设置目标"));
  return record.ip_source === "interface"
    ? t("网卡 · ") + record.interface_name
    : t("公网 IP 查询");
}
function stageText(record: DDNSRecord) {
  return t(STAGE_TEXT[record.last_stage || ""] || "未知");
}
function resultText(record: DDNSRecord) {
  if (record.running) return t("正在更新") + " · " + stageText(record);
  return t(RESULT_TEXT[record.last_status || ""] || "等待首次执行");
}
</script>

<template>
      <section class="panel provider-panel">
        <div class="panel-head">
          <div class="list-page-heading">
            <div class="list-page-title">
            <h2>{{ t("动态 DNS") }}</h2>
            <p>
              {{
                t("每个任务独立按设定间隔探测公网 IPv4 或 IPv6 并更新记录。")
              }}
            </p>
            </div>
            <div class="inline-list-controls">
              <button class="icon-btn" type="button" :aria-expanded="searchOpen" :aria-label="t('搜索当前列表')" :title="t('搜索当前列表')" @click="toggleSearch"><Search :size="17" /></button>
              <input v-if="searchOpen" ref="searchInput" v-model="listSearch" type="search" :placeholder="t('输入域名、名称或地址，支持多个关键词')" @keydown.esc="toggleSearch" />
              <label class="inline-list-status"><span>{{ t("状态") }}</span><select v-model="statusFilter"><option value="all">{{ t("全部状态") }}</option><option value="enabled">{{ t("自动更新开启") }}</option><option value="disabled">{{ t("自动更新关闭") }}</option></select></label>
              <span class="inline-list-count" role="status">{{ visibleDDNS.length }} {{ t("项") }}</span>
              <button v-if="hasListFilter" class="secondary" @click="clearListFilters">{{ t("清除筛选") }}</button>
            </div>
          </div>
        </div>
        <div v-if="visibleDDNS.length" class="host-list">
          <div
            v-for="record in visibleDDNS"
            :key="record.id"
            role="group"
            :aria-label="record.name"
            class="host-row ddns-record-row"
          >
            <div class="host-icon ddns-record-icon"><Network :size="19" /></div>
            <div class="host-main">
              <strong>{{ recordHost(record) }} · {{ record.record_type }}</strong>
              <span
                >{{ record.name }} · {{ sourceText(record) }} ·
                {{ t("每") }} {{ record.interval_minutes }}
                {{ t("分钟") }}</span
              ><span
                v-if="record.record_type !== 'CNAME'"
                class="ddns-address"
                ><b>{{ t("当前解析") }}</b
                ><code>{{ record.last_ip || t("尚未更新") }}</code></span
              ><small v-if="record.last_error" class="ddns-error"
                >{{ t("更新失败") }}：{{ record.last_error
                }}<template v-if="record.last_stage">
                  · {{ t("阶段") }} {{ stageText(record) }}</template
                >
                · {{ t("上次检查") }}
                {{ formatDDNSTime(record.last_run_at) }}</small
              ><small v-else class="op-status ddns-last-run"
                >{{ resultText(record) }} · {{ t("上次检查") }}
                {{ formatDDNSTime(record.last_run_at)
                }}<template v-if="record.last_changed_at">
                  · {{ t("上次修改") }}
                  {{ formatDDNSTime(record.last_changed_at) }}</template
                ></small
              >
            </div>
            <div class="switch-with-label">
              <span>{{ t("自动更新") }}</span
              ><button
                class="site-switch"
                type="button"
                role="switch"
                :aria-checked="record.enabled"
                :aria-label="t('自动更新') + ' ' + record.name"
                :class="{ on: record.enabled }"
                :disabled="readOnly || !!ddnsToggling[record.id]"
                @click="toggleDDNS(record, !record.enabled)"
              >
                <span />
              </button>
            </div>
            <span
              v-if="ddnsFeedback[record.id]"
              class="ddns-feedback"
              role="status"
              >{{ ddnsFeedback[record.id] }}</span
            ><button
              class="secondary ddns-run-button"
              :disabled="readOnly || ddnsRunning[record.id]"
              :aria-busy="ddnsRunning[record.id]"
              :title="t('立即手动更新')"
              @click="runDDNS(record)"
            >
              <RefreshCw
                :size="14"
                :class="{ spinning: ddnsRunning[record.id] }"
              />{{
                ddnsRunning[record.id] ? t("更新中…") : t("手动更新")
              }}</button
            ><button
              class="icon-btn"
              :disabled="readOnly"
              :aria-label="t('编辑')"
              :title="t('编辑')"
              @click="openDDNS(record)"
            >
              <Pencil :size="15" /></button
            ><button
              class="icon-btn danger"
              :disabled="readOnly"
              :aria-label="t('删除')"
              :title="t('删除')"
              @click="removeDDNS(record)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="empty">
          <div class="empty-icon"><RefreshCw :size="23" /></div>
          <strong>{{
            hasListFilter
              ? t("未找到匹配项目")
              : t("尚未配置 DDNS 任务")
          }}</strong
          ><span>{{ t("添加 DDNS 后，公网地址变化会自动更新 DNS 记录。") }}</span>
        </div>
      </section>
</template>
