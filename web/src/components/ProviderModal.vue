<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Eye, EyeOff } from "@lucide/vue";
import { t } from "../i18n";
import {
  editingProviderID,
  providerError,
  providerFields,
  providerForm,
  saveProvider,
  showProvider,
} from "../composables/useNetwork";
import { applyRetryVisible, saving } from "../composables/useState";

import { retryApply } from "../composables/useApply";

const revealedFields = ref<Record<string, boolean>>({});
watch([showProvider, () => providerForm.value.provider_type], () => {
  revealedFields.value = {};
});

const fieldLabels: Record<string, string> = {
  api_token: "API Token",
  secret_id: "SecretId",
  secret_key: "SecretKey",
  access_key_id: "AccessKey ID",
  access_key_secret: "AccessKey Secret",
  api_key: "API Key",
  user: "用户名",
  auth_token: "API Token",
};
const publicFields = new Set(["user", "secret_id", "access_key_id"]);

function fieldLabel(field: string) {
  const raw = fieldLabels[field];
  return raw ? t(raw) : field;
}

const namePlaceholder = computed(() => {
  switch (providerForm.value.provider_type) {
    case "cloudflare":
      return t("我的 Cloudflare");
    case "tencentcloud":
      return t("我的腾讯云 DNSPod");
    case "alidns":
      return t("我的阿里云 DNS");
    default:
      return t("我的 DNS 凭据");
  }
});
</script>

<template>
  <div v-if="showProvider" class="overlay overlay-top" v-dialog>
    <form class="modal" @submit.prevent="saveProvider">
      <div class="modal-top">
        <div>
          <span class="eyebrow">{{ t("DNS 提供商") }}</span>
          <h2>
            {{
              editingProviderID === null
                ? t("添加 DNS 凭据")
                : t("替换 DNS 凭据")
            }}
          </h2>
        </div>
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('关闭')"
          @click="showProvider = false"
        >
          ×
        </button>
      </div>
      <label
        >{{ t("名称")
        }}<input
          v-model="providerForm.name"
          :placeholder="namePlaceholder"
          maxlength="100"
          required /></label
      ><label
        >{{ t("服务商")
        }}<select
          v-model="providerForm.provider_type"
          @change="providerForm.credentials = {}"
        >
          <option value="cloudflare">Cloudflare</option>
          <option value="tencentcloud">{{ t("腾讯云 DNSPod") }}</option>
          <option value="alidns">{{ t("阿里云 DNS") }}</option>
          <option value="godaddy">GoDaddy</option>
          <option value="namecheap">Namecheap</option>
          <option value="digitalocean">DigitalOcean</option>
          <option value="vultr">Vultr</option>
        </select></label
      ><label
        v-for="field in providerFields[providerForm.provider_type]"
        :key="field"
        >{{ fieldLabel(field) }}
        <div class="credential-input-wrap">
          <input
            v-model="providerForm.credentials[field]"
            :type="publicFields.has(field) || revealedFields[field] ? 'text' : 'password'"
            autocomplete="off"
            autocapitalize="none"
            spellcheck="false"
            data-lpignore="true"
            data-1p-ignore="true"
            required
          />
          <button
            v-if="!publicFields.has(field)"
            type="button"
            class="icon-btn credential-toggle"
            :aria-label="revealedFields[field] ? t('隐藏密钥') : t('显示密钥')"
            :title="revealedFields[field] ? t('隐藏密钥') : t('显示密钥')"
            @click="revealedFields[field] = !revealedFields[field]"
          >
            <EyeOff v-if="revealedFields[field]" :size="15" />
            <Eye v-else :size="15" />
          </button>
        </div>
      </label>
      <p class="auth-hint">
        {{
          t(
            "使用仅具备所需 DNS 区域写入权限的 API 凭据。密钥不会再次回显。替换凭据时请重新输入全部必要字段。",
          )
        }}
      </p>
      <p v-if="providerError" class="error">{{ providerError }}</p>
      <button
        v-if="applyRetryVisible"
        type="button"
        class="secondary retry-button"
        :disabled="saving"
        @click="retryApply"
      >
        {{ t("重新应用到 Caddy") }}
      </button>
      <div class="modal-actions">
        <button type="button" class="secondary" @click="showProvider = false">
          {{ t("取消") }}</button
        ><button class="primary" :disabled="saving">
          {{ saving ? t("加密保存中…") : t("加密并保存") }}
        </button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.credential-input-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 7px;
}
.credential-input-wrap input {
  margin-top: 0;
  flex: 1;
  min-width: 0;
}
.credential-toggle {
  flex: none;
  width: 38px;
  height: 38px;
}
</style>
