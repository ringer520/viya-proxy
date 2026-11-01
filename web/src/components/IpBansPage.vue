<script setup lang="ts">
import { Ban, Pencil, ShieldCheck } from "@lucide/vue";
import { t } from "../i18n";
import { hasListFilter } from "../composables/useState";

import { openBan, removeBan, visibleBans } from "../composables/useNetwork";
</script>

<template>
      <section class="panel provider-panel">
        <div class="panel-head">
          <div>
            <h2>{{ t("IP 封禁") }}</h2>
            <p>
              {{
                t(
                  "手动封禁 IP 或 CIDR 网段，长期有效，需手动解除。自动防护仍会封禁重复触发 400/401/403 的来源。",
                )
              }}
            </p>
          </div>
        </div>
        <div v-if="visibleBans.length" class="host-list">
          <div
            v-for="ban in visibleBans"
            :key="ban.id"
            role="group"
            :aria-label="ban.ip"
            class="host-row"
          >
            <div class="host-icon"><ShieldCheck :size="17" /></div>
            <div class="host-main">
              <strong
                >{{ ban.ip === "foreign" ? t("禁止境外 IP") : ban.ip }} ·
                {{ ban.domain }}</strong
              ><span
                >{{ ban.source === "manual" ? t("手动") : t("自动") }} ·
                {{ ban.reason || t("未填写原因") }}
                {{
                  ban.expires_at
                    ? t("· 到期 ") +
                      new Date(ban.expires_at * 1000).toLocaleString()
                    : t("· 永久封禁")
                }}</span
              >
            </div>
            <button class="secondary" @click="openBan(ban)">
              <Pencil :size="14" />{{ t("编辑") }}</button
            ><button class="secondary" @click="removeBan(ban.id)">
              {{ t("解除封禁") }}
            </button>
          </div>
        </div>
        <div v-else class="empty">
          <div class="empty-icon"><Ban :size="23" /></div>
          <strong>{{
            hasListFilter
              ? t("未找到匹配项目")
              : t("当前没有封禁记录")
          }}</strong
          ><span>{{ t("添加封禁规则，阻止恶意来源访问站点。") }}</span>
        </div>
      </section>
</template>
