<script setup lang="ts">
import { ArrowRight, Copy, ExternalLink, Folder, FolderUp, Globe2, Pencil, Plus, ScrollText, Search, ShieldCheck, Trash2 } from "@lucide/vue";
import {openSiteDetails} from "../composables/useSiteWorkspace";
import { Info, Activity } from "@lucide/vue";
import { t } from "../i18n";
import { formatBytes } from "../format";
import AccessLogTable from "./AccessLogTable.vue";
import { clearListFilters, hasListFilter, hosts, listSearch, readOnly, statusFilter, upstreamHealth } from "../composables/useState";

import { barHeight, hourlyTraffic, siteStats } from "../composables/useTraffic";
import { cloneHost, hostContentStats, hostToggling, openCreate, openEdit, openListenerEditor, openSiteFiles, openSiteLogs, previewLayout, previewSite, remove, selectedListener, selectedPortState, selectListener, siteGroups, siteIconColors, siteIconStyle, siteLogs, siteURL, toggleHost, toggleWebSearch, visibleHosts, webListeners, webSearchInput, webSearchOpen } from "../composables/useHosts";
import { openSecurityDefaults, securityDefaults } from "../composables/useSecurity";
import { upstreamLabel } from "../composables/useOverview";
import { previewLimit, trafficCharts } from "../composables/usePreferences";
</script>

<template>
      <section class="panel site-panel">
        <div class="panel-head">
          <div class="site-list-heading">
            <h2>{{ t("站点列表") }}</h2>
            <button
              class="icon-btn"
              :aria-expanded="webSearchOpen"
              aria-controls="web-site-search"
              :aria-label="t('搜索 Web 服务')"
              :title="t('搜索 Web 服务')"
              @click="toggleWebSearch"
            >
              <Search :size="17" /></button
            ><input
              v-if="webSearchOpen"
              id="web-site-search"
              ref="webSearchInput"
              v-model="listSearch"
              type="search"
              :placeholder="t('搜索域名、上游或路径')"
              :aria-label="t('搜索 Web 站点')"
              @keydown.esc="
                webSearchOpen = false;
                listSearch = '';
              "
            />
            <label class="inline-list-status">
              <span>{{ t("状态") }}</span>
              <select v-model="statusFilter">
                <option value="all">{{ t("全部状态") }}</option>
                <option value="enabled">{{ t("已启用") }}</option>
                <option value="disabled">{{ t("已停用") }}</option>
              </select>
            </label>
          </div>
        </div>

        <div class="listener-workspace">
          <nav
            class="site-exposure-filters"
            :aria-label="t('Web 端口监听选项卡')"
            role="tablist"
          >
            <button
              v-for="(listener, index) in webListeners"
              :key="listener.id"
              role="tab"
              :aria-selected="selectedListener === index"
              :class="{ selected: selectedListener === index }"
              @click="selectListener(index)"
            >
              {{ listener.protocol.toUpperCase() }} · {{ listener.port }}
              <b>{{
                hosts.filter((host) => host.listener_id === listener.id).length
              }}</b></button
            ><button :disabled="readOnly" @click="openListenerEditor(null)">
              <Plus :size="14" />{{ t("添加端口监听") }}
            </button>
          </nav>
          <div class="listener-selected">
            <span
              >{{ webListeners[selectedListener]?.protocol.toUpperCase() }}
              {{ t("监听端口") }}
              <strong>:{{ webListeners[selectedListener]?.port }}</strong></span
            ><button
              class="secondary"
              :disabled="readOnly"
              @click="openListenerEditor(selectedListener)"
            >
              {{ t("修改端口 / 检测") }}</button
            ><span
              role="status"
              :class="
                selectedPortState.available === false ? 'error' : 'probe-ok'
              "
              >{{ selectedPortState.message }}</span
            >
          </div>
        </div>
        <div class="site-list-tools">
          <span>{{ visibleHosts.length }} {{ t("个匹配站点") }}</span
          ><button
            v-if="hasListFilter"
            class="inline-action"
            @click="clearListFilters"
          >
            {{ t("清除筛选") }}
          </button>
        </div>
        <div v-if="visibleHosts.length" class="site-groups">
          <section
            v-for="group in siteGroups"
            :key="group.domain"
            class="site-group"
          >
            <div class="site-group-heading">
              <div>
                <strong>{{ group.domain }}</strong
                ><span>{{ group.hosts.length }} {{ t("个代理服务") }}</span>
              </div>
              <div class="site-group-actions">
                <button
                  class="icon-btn group-security-btn"
                  :class="{ enabled: securityDefaults.domains[group.domain]?.enabled }"
                  type="button"
                  :disabled="readOnly"
                  :aria-label="t('安全响应头默认配置')"
                  :title="t('为这个域名设置默认安全响应头')"
                  @click="openSecurityDefaults(group.domain)"
                >
                  <ShieldCheck :size="16" />
                  <span class="security-default-label">{{t("默认安全配置")}}</span>
                  <span
                    v-if="securityDefaults.domains[group.domain]?.enabled"
                    class="security-dot"
                  ></span>
                </button>
                <button
                  class="secondary add-subdomain-button"
                  :disabled="readOnly"
                  @click="openCreate(group.domain)"
                >
                  <Plus :size="14" />{{ t("添加子域名") }}
                </button>
              </div>
            </div>
            <div class="site-group-sites">
              <div
                v-for="host in group.hosts"
                :key="host.id"
                role="group"
                :aria-label="host.domain"
                class="host-row"
              >
                <div
                  class="host-icon site-icon"
                  :style="siteIconStyle(host.icon_color)"
                >
                  <Folder v-if="host.type === 'static'" :size="18" />
                  <ArrowRight v-else-if="host.type === 'redirect'" :size="18" />
                  <Globe2 v-else :size="18" />
                </div>
                <div class="host-main site-main">
                  <div class="site-title"><button class="site-diagnose-icon" :aria-label="t('一键诊断')" :title="t('一键诊断')" @click="openSiteDetails(host,true)"><Activity :size="15"/></button>
                    <button class="site-name-link" :title="t('站点详情')" @click="openSiteDetails(host)"><strong class="entity-name" :style="{color:siteIconColors.find(color=>color.key===host.icon_color)?.value||'var(--accent)'}">{{ host.name||host.domain }}</strong></button
                    ><small
                      class="proto-tag"
                      :class="{ plain: host.tls_mode === 'none' }"
                      >{{ host.tls_mode === "none" ? "HTTP" : "HTTPS" }}</small
                    ><small v-if="host.type === 'static'" class="proto-tag kind">
                      {{ t("静态") }}</small
                    ><small
                      v-else-if="host.type === 'redirect'"
                      class="proto-tag kind"
                      >{{ t("重定向") }}</small
                    ><small v-if="host.name" class="site-alias">{{
                      host.domain
                    }}</small>
                  </div>
                  <div v-if="host.tags?.length" class="site-tags"><span v-for="tag in host.tags" :key="tag">{{tag}}</span></div>
                  <div v-if="host.type === 'static'" class="static-path-row">
                    <button
                      class="static-upload-btn"
                      :class="{
                        empty: hostContentStats[host.id]?.empty ?? true,
                      }"
                      :title="
                        (hostContentStats[host.id]?.empty ?? true)
                          ? t('上传网站文件')
                          : t('管理网站文件')
                      "
                      @click="openSiteFiles(host)"
                    >
                      <FolderUp :size="15" />
                      <template v-if="hostContentStats[host.id]">
                        <template v-if="hostContentStats[host.id].empty">{{
                          t("空站点 · 点此上传")
                        }}</template>
                        <template v-else>{{
                          hostContentStats[host.id].files +
                          t(" 个文件") +
                          " · " +
                          formatBytes(hostContentStats[host.id].total_size)
                        }}</template>
                      </template>
                      <template v-else>{{ t("空站点 · 点此上传") }}</template>
                    </button>
                    <span class="static-root-path">{{
                      host.root_path || t("托管目录")
                    }}</span>
                  </div>
                  <span v-else class="site-upstream">{{
                    host.type === "redirect"
                      ? t("跳转至 ") + (host.redirect_target || "")
                      : host.upstream
                  }}</span>
                  <div class="site-badges">
                    <span
                      v-if="upstreamHealth[String(host.id)]"
                      class="upstream-state"
                      :class="
                        upstreamHealth[String(host.id)].ok ? 'ok' : 'down'
                      "
                      :title="upstreamHealth[String(host.id)].error || ''"
                      ><i class="dot"></i>{{ upstreamLabel(host.id) }}</span
                    >
                    <span
                      v-if="host.certificate_source"
                      class="cert-line"
                      :class="host.certificate_source.kind"
                      :title="
                        [
                          t(host.certificate_source.label),
                          host.certificate_source.domain,
                          host.certificate_source.not_after
                            ? t('到期 ') +
                              new Date(
                                host.certificate_source.not_after,
                              ).toLocaleDateString()
                            : '',
                        ]
                          .filter(Boolean)
                          .join(' · ')
                      "
                      ><ShieldCheck :size="14" />{{
                        t(host.certificate_source.label)
                      }}<small
                        v-if="host.certificate_source.kind === 'wildcard'"
                        >{{ host.certificate_source.domain }}</small
                      ></span
                    >
                    <div class="site-log-preview-wrap">
                      <button
                        class="site-log-trigger"
                        :aria-label="
                          t('预览 ') + host.domain + t(' 的访问日志')
                        "
                        :aria-describedby="'site-log-' + host.id"
                        :title="t('悬浮预览访问日志；点击查看完整记录')"
                        @mouseenter="previewSite(host, $event)"
                        @click="openSiteLogs(host)"
                      >
                        <ScrollText :size="15" />
                      </button>
                      <div
                        :id="'site-log-' + host.id"
                        role="tooltip"
                        class="site-log-tooltip"
                        :style="previewLayout"
                      >
                        <div class="site-log-heading">
                          <b>{{ host.domain }} {{ t("· 最近访问") }}</b
                          ><span
                            >{{ t("最近") }} {{ previewLimit }}
                            {{ t("条") }}</span
                          >
                        </div>
                        <AccessLogTable
                          :events="
                            (siteLogs[host.id]?.events || []).slice(
                              0,
                              previewLimit,
                            )
                          "
                          :loading="siteLogs[host.id]?.loading"
                          :error="siteLogs[host.id]?.error"
                          :limit="previewLimit"
                          compact
                        /><button
                          class="inline-action"
                          @click="openSiteLogs(host)"
                        >
                          {{ t("查看最近 50 条记录 →") }}
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="site-traffic">
                  <div>
                    <span>{{ t("近24小时") }}</span
                    ><strong
                      >{{ siteStats(host).recent.requests.toLocaleString() }}
                      {{ t("次") }}</strong
                    ><span
                      >↑ {{ formatBytes(siteStats(host).recent.upload) }} · ↓
                      {{ formatBytes(siteStats(host).recent.download) }}</span
                    >
                  </div>
                  <div
                    v-if="trafficCharts"
                    class="traffic-spark"
                    :aria-label="t('最近24小时流量趋势')"
                  >
                    <i
                      v-for="point in hourlyTraffic(host)"
                      :key="point.hour"
                      :style="{
                        height:
                          barHeight(host, point.upload + point.download) + 'px',
                      }"
                      :title="
                        new Date(point.hour * 1000).toLocaleString() +
                        ' · ' +
                        point.requests +
                        t(' 次 · ') +
                        formatBytes(point.upload + point.download)
                      "
                    ></i>
                  </div>
                  <small
                    >{{ t("累计") }}
                    {{ siteStats(host).total.requests.toLocaleString() }}
                    {{ t("次 ·") }}
                    {{
                      formatBytes(
                        siteStats(host).total.upload +
                          siteStats(host).total.download,
                      )
                    }}</small
                  >
                </div>
                <button
                  class="site-switch"
                  type="button"
                  role="switch"
                  :aria-checked="host.enabled"
                  :aria-label="
                    (host.enabled ? t('停用站点 ') : t('启用站点 ')) +
                    host.domain
                  "
                  :class="{ on: host.enabled }"
                  :disabled="!!hostToggling[host.id]"
                  :title="
                    host.enabled ? t('已启用，点击停用') : t('已停用，点击启用')
                  "
                  @click="toggleHost(host)"
                >
                  <span /></button
                ><a
                  class="icon-btn"
                  :href="siteURL(host)"
                  target="_blank"
                  rel="noopener noreferrer"
                  :title="
                    host.tls_mode === 'none'
                      ? t('通过 HTTP 打开 ') + host.domain
                      : t('通过 HTTPS 打开 ') + host.domain
                  "
                  ><ExternalLink :size="16" /></a
                ><button
                  class="icon-btn"
                  :aria-label="t('克隆站点')"
                  :title="t('克隆站点')"
                  @click="cloneHost(host)"
                >
                  <Copy :size="15" /></button
                ><button
                  class="icon-btn"
                  :aria-label="t('编辑')"
                  :title="t('编辑')"
                  @click="openEdit(host)"
                >
                  <Pencil :size="15" /></button
                ><button
                  class="icon-btn danger"
                  :aria-label="t('删除')"
                  :title="t('删除')"
                  @click="remove(host)"
                >
                  <Trash2 :size="16" />
                </button>
              </div>
            </div>
          </section>
        </div>
        <div v-else class="empty">
          <div class="empty-icon"><Globe2 :size="23" /></div>
          <strong>{{
            hasListFilter
              ? t("未找到匹配站点")
              : hosts.length
                ? t("此监听下没有站点")
                : t("还没有代理站点")
          }}</strong
          ><span>{{ t("点击添加站点，将流量转发到你的服务。") }}</span>
        </div>
      </section>
</template>
