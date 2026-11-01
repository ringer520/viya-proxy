<script setup lang="ts">
import ResourceUsage from "./ResourceUsage.vue";
import { KeyRound, Pencil, ShieldCheck, Trash2 } from "@lucide/vue";
import { t } from "../i18n";
import { hasListFilter } from "../composables/useState";

import { editProvider, removeProvider, visibleProviders } from "../composables/useNetwork";
</script>

<template>
      <section

        id="dns-providers"
        class="panel provider-panel"
      >
        <div class="panel-head">
          <div>
            <h2>{{ t("DNS 凭据") }}</h2>
            <p>{{ t("密钥以 AES-256-GCM 加密后保存在本机数据库。") }}</p>
          </div>
        </div>
        <div v-if="visibleProviders.length" class="host-list">
          <div
            v-for="provider in visibleProviders"
            :key="provider.id"
            role="group"
            :aria-label="provider.name"
            class="host-row"
          >
            <div class="host-icon"><ShieldCheck :size="17" /></div>
            <div class="host-main">
              <ResourceUsage kind="provider" :id="provider.id"/>
              <strong>{{ provider.name }}</strong
              ><span
                >{{ provider.provider_type }}
                {{ t("· 凭据已保存，不会在列表中显示") }}</span
              >
            </div>
            <button
              class="icon-btn"
              :title="t('替换凭据')"
              @click="editProvider(provider)"
            >
              <Pencil :size="15" /></button
            ><button
              class="icon-btn danger"
              :title="t('删除凭据')"
              @click="removeProvider(provider)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="empty">
          <div class="empty-icon"><KeyRound :size="23" /></div>
          <strong>{{
            hasListFilter
              ? t("未找到匹配项目")
              : t("尚未添加 DNS 服务商凭据")
          }}</strong
          ><span>{{ t("添加凭据后，即可用于证书申请和 DDNS。") }}</span>
        </div>
      </section>
</template>
