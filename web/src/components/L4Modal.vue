<script setup lang="ts">
import { t } from "../i18n";
import {
  l4EditingID,
  l4Error,
  l4Form,
  saveL4,
  showL4,
} from "../composables/useNetwork";
import { applyRetryVisible, saving } from "../composables/useState";

import { retryApply } from "../composables/useApply";
</script>

<template>
  <div v-if="showL4" class="overlay" v-dialog>
    <form class="modal" @submit.prevent="saveL4">
      <div class="modal-top">
        <div>
          <span class="eyebrow">Layer4</span>
          <h2>
            {{ l4EditingID === null ? t("添加端口转发") : t("编辑端口转发") }}
          </h2>
        </div>
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('关闭')"
          @click="showL4 = false"
        >
          ×
        </button>
      </div>
      <label
        >{{ t("名称")
        }}<input
          v-model="l4Form.name"
          maxlength="100"
          :placeholder="t('SSH 转发')" /></label
      ><label
        >{{ t("监听 IP")
        }}<input
          v-model="l4Form.listen_address"
          required
          :placeholder="t('0.0.0.0 或 ::')" /></label
      ><label
        >{{ t("监听端口")
        }}<input
          v-model.number="l4Form.listen_port"
          type="number"
          min="1"
          max="65535"
          required /></label
      ><label
        >{{ t("上游 host:port")
        }}<input
          v-model="l4Form.upstream_address"
          required
          placeholder="192.168.1.20:22" /></label
      ><label
        >{{ t("协议")
        }}<select v-model="l4Form.protocol">
          <option value="tcp">TCP</option>
          <option value="udp">UDP</option>
          <option value="both">TCP + UDP</option>
        </select></label
      ><label class="enabled-label"
        ><input v-model="l4Form.enabled" type="checkbox" />{{
          t("启用转发")
        }}</label
      >
      <p class="auth-hint">
        {{
          t(
            "监听端口会直接绑定 Debian 主机。低于 1024 的端口需相应系统权限；请勿占用管理接口或 HTTP/HTTPS 端口。",
          )
        }}
      </p>
      <p v-if="l4Error" class="error">{{ l4Error }}</p>
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
        <button type="button" class="secondary" @click="showL4 = false">
          {{ t("取消") }}</button
        ><button class="primary" :disabled="saving">
          {{ saving ? t("应用中…") : t("保存并应用") }}
        </button>
      </div>
    </form>
  </div>
</template>
