<script setup lang="ts">
import ServiceTrafficCell from "./ServiceTrafficCell.vue";
import { ArrowLeftRight, ArrowRight, CornerUpRight, Copy, ExternalLink, Folder, FolderUp, Globe2, MoreHorizontal, Pencil, Plus, ScrollText, Search, ShieldCheck, Trash2 } from "@lucide/vue";
import {openSiteDetails} from "../composables/useSiteWorkspace";
import { Activity } from "@lucide/vue";
import { t } from "../i18n";
import { onMounted, onUnmounted, ref } from "vue";
import type { Host } from "../types";
import { formatBytes } from "../format";
import AccessLogTable from "./AccessLogTable.vue";
import { clearListFilters, hasListFilter, hosts, listSearch, readOnly, statusFilter, upstreamHealth } from "../composables/useState";

import { siteStats } from "../composables/useTraffic";
import { cloneHost, hostContentStats, hostToggling, openCreate, openEdit, openListenerEditor, openSiteFiles, openSiteLogs, previewLayout, previewSite, remove, selectedListener, selectedPortState, selectListener, siteGroups, siteIconStyle, siteLogs, siteURL, toggleHost, toggleWebSearch, visibleHosts, webListeners, webSearchInput, webSearchOpen } from "../composables/useHosts";
import { openSecurityDefaults, resolveSecurityPolicy } from "../composables/useSecurity";
import { previewLimit } from "../composables/usePreferences";
function hasSiteName(host: Host) { return !!host.name?.trim() && host.name.trim() !== host.domain; }
function certificateDetails(host: Host) {
  const source = host.certificate_source;
  if (!source) return t("暂无证书");
  return [source.kind === "wildcard" ? t("泛域名证书") : t(source.label), source.domain,
    source.not_after ? t("到期 ") + new Date(source.not_after).toLocaleString() : ""].filter(Boolean).join(" · ");
}
function closeSiteMenu(event: Event) {
  (event.currentTarget as HTMLElement).closest("details")?.removeAttribute("open");
}
const sitePanel = ref<HTMLElement | null>(null);
function closeOutsideMenus(event: PointerEvent) {
  sitePanel.value?.querySelectorAll<HTMLDetailsElement>(".site-more-menu[open]").forEach(menu => {
    if (!menu.contains(event.target as Node)) menu.open = false;
  });
}
function closeMenusOnEscape(event: KeyboardEvent) {
  if (event.key !== "Escape") return;
  sitePanel.value?.querySelectorAll<HTMLDetailsElement>(".site-more-menu[open]").forEach(menu => {
    menu.open = false;
    menu.querySelector<HTMLElement>("summary")?.focus();
    event.stopPropagation();
  });
}
onMounted(() => { document.addEventListener("pointerdown", closeOutsideMenus); document.addEventListener("keydown", closeMenusOnEscape); });
onUnmounted(() => { document.removeEventListener("pointerdown", closeOutsideMenus); document.removeEventListener("keydown", closeMenusOnEscape); });
function certificateIssue(host: Host) {
  const source = host.certificate_source;
  if (!host.enabled || !source || source.kind === "http") return "";
  if (["failed", "error"].includes(source.status)) return t("证书申请失败");
  if (source.not_after) {
    const remaining = new Date(source.not_after).getTime() - Date.now();
    if (remaining <= 0) return t("证书已过期");
    // Caddy internal certificates are deliberately short-lived and renew automatically.
    if (source.kind !== "internal" && host.tls_mode !== "internal" && remaining <= 14 * 86400000) return t("证书即将到期");
  }
  return "";
}
</script>

<template>
      <section ref="sitePanel" class="panel site-panel">
        <div class="panel-head">
          <div class="site-list-heading">
            <h2>{{ t("站点列表") }}</h2>
            <div class="inline-list-controls site-list-controls">
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
            <span class="inline-list-count" role="status">{{ visibleHosts.length }} {{ t("项") }}</span>
            <button v-if="hasListFilter" class="secondary" @click="clearListFilters">{{ t("清除筛选") }}</button>
            </div>
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
                  :class="{ enabled: resolveSecurityPolicy(group.domain).policy.mode !== 'none' }"
                  type="button"
                  :disabled="readOnly"
                  :aria-label="t('域名安全策略')"
                  :title="t('管理此域名的安全策略与继承方式')"
                  @click="openSecurityDefaults(group.domain)"
                >
                  <ShieldCheck :size="16" />
                  <span class="security-default-label">{{t("域名安全策略")}}</span>
                  <span
                    v-if="resolveSecurityPolicy(group.domain).policy.mode !== 'none'"
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
            <div class="site-columns" aria-hidden="true"><span>{{t('访问入口')}}</span><span>{{t('转发目标')}}</span><span>{{t('近30天')}}</span><span>{{t('操作')}}</span></div>
            <div class="site-group-sites">
              <div
                v-for="host in group.hosts"
                :key="host.id"
                role="group"
                :aria-label="host.domain"
                class="host-row site-list-row"
                :class="{ 'site-is-disabled': !host.enabled, 'site-has-name': hasSiteName(host), 'site-route-down': host.enabled && host.type === 'proxy' && upstreamHealth[String(host.id)]?.ok === false }"
              >
                <div class="site-identity">
                  <div class="host-icon site-icon" :style="siteIconStyle(host.icon_color)">
                    <Folder v-if="host.type === 'static'" :size="17" />
                    <ArrowRight v-else-if="host.type === 'redirect'" :size="17" />
                    <Globe2 v-else :size="17" />
                  </div>
                  <div class="site-identity-text">
                    <button v-if="hasSiteName(host)" class="site-name-link" :title="t('站点详情')" @click="openSiteDetails(host)"><span class="site-display-name">{{ host.name }}</span></button>
                    <div class="site-domain-row">
                      <a class="site-domain-link" :href="siteURL(host)" target="_blank" rel="noopener noreferrer" :title="siteURL(host)">{{ host.domain }}<span class="site-domain-path">{{ host.path === '/' ? '' : host.path }}</span></a>
<span class="site-entry-security">
                      <small class="site-protocol">{{ host.tls_mode === 'none' ? 'HTTP' : 'HTTPS' }}</small>
                      <span v-if="host.certificate_source && host.certificate_source.kind !== 'http'" class="site-certificate-hint" tabindex="0" :aria-label="certificateDetails(host)" :class="{ 'certificate-warning': certificateIssue(host) }">
                        <ShieldCheck :size="13" />
                        <span class="site-certificate-tooltip" role="tooltip">{{certificateDetails(host)}}</span>
                      </span>
                      <span v-if="certificateIssue(host)" class="site-certificate-error">{{certificateIssue(host)}}</span>
                      <span v-if="!host.enabled" class="site-disabled-label">{{t('已停用')}}</span>
                    </span>
                    </div>
                    <div v-if="host.tags?.length" class="site-tags"><span v-for="tag in host.tags" :key="tag">{{tag}}</span></div>
                  </div>
                </div>
                <div class="site-route">
                  <span class="site-connection" aria-hidden="true"><Folder v-if="host.type === 'static'" :size="16" /><CornerUpRight v-else-if="host.type === 'redirect'" :size="16" /><ArrowLeftRight v-else :size="16" /></span>
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
                    <span class="static-root-path" tabindex="0" :title="host.root_path || t('托管目录')">{{t('托管目录')}}</span>
                  </div>
                  <span v-else class="site-upstream">{{
                    host.type === "redirect"
                      ? t("跳转至 ") + (host.redirect_target || "")
                      : host.upstream
                  }}</span>
                  <div class="site-badges">
                    <span
                      v-if="host.enabled && host.type === 'proxy' && upstreamHealth[String(host.id)]"
                      class="upstream-state"
                      :class="
                        upstreamHealth[String(host.id)].ok ? 'ok' : 'down'
                      "
                      :title="upstreamHealth[String(host.id)].error || ''"
                      ><i class="dot"></i>{{ upstreamHealth[String(host.id)].ok ? Math.round(upstreamHealth[String(host.id)].latency_ms) + ' ms' : t('上游无响应') }}</span
                    >
                  </div>
                </div>
                <ServiceTrafficCell class="site-metrics" :stats="siteStats(host)" kind="web"/>
                <div class="site-actions">
                  <div class="site-actions-top">
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
                  :disabled="readOnly || !!hostToggling[host.id]"
                  :title="
                    host.enabled ? t('已启用，点击停用') : t('已停用，点击启用')
                  "
                  @click="toggleHost(host)"
                >
                  <span /></button
                >
                  </div>
                  <div class="site-actions-buttons">
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

                    <button class="icon-btn site-diagnostic-action" :aria-label="t('一键诊断')" :title="t('一键诊断')" @click="openSiteDetails(host,true)"><Activity :size="16" /></button>
                    <button class="icon-btn" :disabled="readOnly" :aria-label="t('编辑')" :title="t('编辑')" @click="openEdit(host)"><Pencil :size="16" /></button>
                    <details class="site-more-menu">
                      <summary class="icon-btn" :aria-label="t('更多操作')" :title="t('更多操作')"><MoreHorizontal :size="17" /></summary>
                      <div class="site-more-options">
                        <a :href="siteURL(host)" target="_blank" rel="noopener noreferrer" @click="closeSiteMenu"><ExternalLink :size="15" />{{t('打开站点')}}</a>
                        <button :disabled="readOnly" @click="cloneHost(host); closeSiteMenu($event)"><Copy :size="15" />{{t('克隆站点')}}</button>
                        <button class="danger" :disabled="readOnly" @click="remove(host); closeSiteMenu($event)"><Trash2 :size="15" />{{t('删除')}}</button>
                      </div>
                    </details>
                  </div>
                </div>
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
