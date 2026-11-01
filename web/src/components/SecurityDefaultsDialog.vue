<script setup lang="ts">
import { t } from "../i18n";
import {
  applySecurityToExisting,
  closeSecurityDialog,
  saveSecurityDefaults,
  securityApplyPrompt,
  securityDialog,
  securityError,
  securityForm,
  securityNotice,
  securitySaving,
  switchSecurityScope,
} from "../composables/useSecurity";
import SitePolicyFields from "./SitePolicyFields.vue";
</script>

<template>
  <div v-if="securityDialog.open" class="overlay" v-dialog>
    <form class="modal security-modal" @submit.prevent="saveSecurityDefaults">
      <div class="modal-top">
        <div>
          <span class="eyebrow">{{ t("安全响应头") }}</span>
          <h2>{{ t("默认安全配置") }}</h2>
        </div>
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('关闭')"
          @click="closeSecurityDialog"
        >
          ×
        </button>
      </div>
      <div class="security-scope-tabs">
        <button
          type="button"
          :class="{ selected: securityDialog.scope === 'global' }"
          @click="switchSecurityScope('global')"
        >
          {{ t("全局默认") }}
        </button>
        <button
          v-if="securityDialog.domain"
          type="button"
          :class="{ selected: securityDialog.scope === 'domain' }"
          @click="switchSecurityScope('domain')"
        >
          {{ securityDialog.domain }}
        </button>
      </div>
      <p class="inline-help">
        {{
          securityDialog.scope === "global"
            ? t("所有新建站点的兜底配置，根域名可以覆盖它。")
            : t("该域名及其所有子域新建时自动继承这份配置。")
        }}
      </p>
      <SitePolicyFields v-model="securityForm" variant="defaults" />
      <p v-if="securityError" class="error">{{ securityError }}</p>
      <p v-if="securityNotice" class="inline-help">{{ securityNotice }}</p>
      <div v-if="securityApplyPrompt" class="security-apply">
        <p class="inline-help">
          {{
            t("将这份配置应用到该域名下 ") +
            securityApplyPrompt.count +
            t(" 个已有子域？只更新仍跟随默认的站点。")
          }}
        </p>
        <div class="security-apply-actions">
          <button
            type="button"
            class="secondary"
            @click="securityApplyPrompt = null"
          >
            {{ t("暂不") }}
          </button>
          <button
            type="button"
            class="primary"
            @click="applySecurityToExisting"
          >
            {{ t("应用") }}
          </button>
        </div>
      </div>
      <div class="modal-actions">
        <button type="button" class="secondary" @click="closeSecurityDialog">
          {{ t("取消") }}
        </button>
        <button class="primary" :disabled="securitySaving">
          {{ securitySaving ? t("保存中…") : t("保存") }}
        </button>
      </div>
    </form>
  </div>
</template>
