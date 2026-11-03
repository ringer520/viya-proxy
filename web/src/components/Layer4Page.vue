<script setup lang="ts">
import ServiceTrafficCell from "./ServiceTrafficCell.vue";
import { ArrowLeftRight, Cable, Pencil, ScrollText, Trash2, Search } from "@lucide/vue";
import { t } from "../i18n";
import { clearListFilters, hasListFilter, listSearch, readOnly, statusFilter } from "../composables/useState";
import { nextTick, ref } from "vue";

import { l4Stats } from "../composables/useTraffic";
import { l4Logs, l4PreviewLayout, l4Toggling, openL4, previewL4, removeL4, toggleL4, visibleL4 } from "../composables/useNetwork";
import type { Layer4Rule } from "../types";
function listenEndpoint(rule:Layer4Rule){return `${rule.listen_address.includes(":")?"["+rule.listen_address+"]":rule.listen_address}:${rule.listen_port}`}
const searchOpen = ref(false);
const searchInput = ref<HTMLInputElement | null>(null);
async function toggleSearch() {
  searchOpen.value = !searchOpen.value;
  if (searchOpen.value) { await nextTick(); searchInput.value?.focus(); }
  else listSearch.value = "";
}
</script>

<template>
      <section class="panel provider-panel forward-panel">
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
        <template v-if="visibleL4.length">
          <div class="forward-columns" aria-hidden="true"><span>{{t('转发规则 / 监听入口')}}</span><span>{{t('上游目标')}}</span><span>{{t('近30天')}}</span><span>{{t('操作')}}</span></div>
          <div class="forward-list">
            <div v-for="rule in visibleL4" :key="rule.id" role="group" :aria-label="rule.name || String(rule.listen_port)" class="forward-row" :class="{disabled:!rule.enabled}">
              <div class="forward-entry"><span class="forward-icon"><Cable :size="18"/></span><div><div class="forward-title"><strong>{{rule.name || t('未命名转发')}}</strong><span v-for="protocol in (rule.protocol==='both'?['tcp','udp']:[rule.protocol])" :key="protocol" class="forward-protocol" :class="protocol">{{protocol.toUpperCase()}}</span></div><code class="forward-address">{{listenEndpoint(rule)}}</code><small>{{rule.listen_address==='0.0.0.0'?t('所有 IPv4 网卡'):rule.listen_address==='::'?t('所有 IPv6 网卡'):t('指定本机地址')}}</small></div></div>
              <div class="forward-target"><ArrowLeftRight class="forward-connector" :size="16" aria-hidden="true"/><code>{{rule.upstream_address}}</code><span class="forward-state" :class="{on:rule.runtime?.state==='listening'}" :title="t(rule.runtime?.message||'状态尚未核实')"><i/>{{t(({listening:'正在监听',loaded:'已加载 · 待核实',pending:'待生效',unbound:'监听未核实','still-active':'旧监听未停用',disabled:'已停用',unknown:'状态未知'} as Record<string,string>)[rule.runtime?.state||'unknown'])}}</span></div>
              <ServiceTrafficCell class="forward-metrics" :stats="l4Stats(rule)" kind="forward" :protocol="rule.protocol"/>
              <div class="forward-actions">
                <button class="site-switch" type="button" role="switch" :aria-checked="rule.enabled" :aria-label="(rule.enabled?t('停用转发'):t('启用转发'))+' '+(rule.name||rule.listen_port)" :title="rule.enabled?t('停用转发'):t('启用转发')" :class="{on:rule.enabled}" :disabled="readOnly || !!l4Toggling[rule.id]" @click="toggleL4(rule)"><span/></button>
<div class="site-log-preview-wrap">
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
                <button class="icon-btn" :disabled="readOnly" :aria-label="t('编辑')" :title="t('编辑')" @click="openL4(rule)"><Pencil :size="15"/></button>
                <button class="icon-btn danger" :disabled="readOnly" :aria-label="t('删除')" :title="t('删除')" @click="removeL4(rule)"><Trash2 :size="15"/></button>
              </div>
            </div>
          </div>
        </template>
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
