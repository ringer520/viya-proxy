<script setup lang="ts">
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
          :placeholder="t('我的 Cloudflare')"
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
        >{{ field
        }}<input
          v-model="providerForm.credentials[field]"
          type="text"
          class="credential-input"
          autocomplete="off"
          autocapitalize="none"
          spellcheck="false"
          data-lpignore="true"
          data-1p-ignore="true"
          required
      /></label>
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
