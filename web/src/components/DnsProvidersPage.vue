<script setup lang="ts">
import ProviderActions from "./ProviderActions.vue";
import ResourceUsage from "./ResourceUsage.vue";
import { KeyRound, Pencil, Trash2 } from "@lucide/vue";
import { t } from "../i18n";
import { hasListFilter, readOnly } from "../composables/useState";

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
            class="host-row provider-row"
          >
            <div class="provider-identity"><div class="host-icon"><KeyRound :size="18" /></div>
            <div class="provider-details">
              <strong>{{ provider.name }}</strong
              ><span
                >{{ provider.provider_type }}
                {{ t("· 凭据已保存，不会在列表中显示") }}</span
              >
              <ResourceUsage kind="provider" :id="provider.id"/>
            </div></div>
            <ProviderActions :provider="provider">
            <button
              class="secondary"
              :disabled="readOnly"
              :title="t('替换凭据')"
              @click="editProvider(provider)"
            >
              <Pencil :size="15" />{{t('替换凭据')}}</button
            ><button
              class="icon-btn danger"
              :disabled="readOnly"
              :aria-label="t('删除凭据')"
              :title="t('删除凭据')"
              @click="removeProvider(provider)"
            >
              <Trash2 :size="16" />
            </button>
            </ProviderActions>
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
