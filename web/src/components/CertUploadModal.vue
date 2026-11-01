<script setup lang="ts">
import { t } from "../i18n";
import {
  certError,
  certFile,
  certForm,
  keyFile,
  saveCert,
  showCert,
} from "../composables/useCertificates";
import { saving } from "../composables/useState";

</script>

<template>
  <div v-if="showCert" class="overlay overlay-top">
    <form class="modal" @submit.prevent="saveCert">
      <div class="modal-top">
        <div>
          <span class="eyebrow">{{ t("自定义证书") }}</span>
          <h2>{{ t("上传 PEM 证书") }}</h2>
        </div>
        <button type="button" class="icon-btn" @click="showCert = false">
          ×
        </button>
      </div>
      <label
        >{{ t("证书名称")
        }}<input
          v-model="certForm.name"
          maxlength="100"
          required
          :placeholder="t('example.com 证书')" /></label
      ><label
        >{{ t("证书链文件")
        }}<input
          type="file"
          accept=".pem,.crt,.cer"
          required
          @change="
            certFile = ($event.target as HTMLInputElement).files?.[0] || null
          " /></label
      ><label
        >{{ t("私钥文件")
        }}<input
          type="file"
          accept=".pem,.key"
          required
          @change="
            keyFile = ($event.target as HTMLInputElement).files?.[0] || null
          "
      /></label>
      <p class="auth-hint">
        {{
          t(
            "只接受未加密 PEM 私钥；服务端会校验证书和私钥匹配、SAN 与有效期。上传后选择站点域名匹配的证书。",
          )
        }}
      </p>
      <p v-if="certError" class="error">{{ certError }}</p>
      <div class="modal-actions">
        <button type="button" class="secondary" @click="showCert = false">
          {{ t("取消") }}</button
        ><button class="primary" :disabled="saving">
          {{ saving ? t("校验并保存…") : t("上传证书") }}
        </button>
      </div>
    </form>
  </div>
</template>
