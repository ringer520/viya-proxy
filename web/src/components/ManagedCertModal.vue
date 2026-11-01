<script setup lang="ts">
import { t } from "../i18n";
import {
  editingManagedID,
  managedCertBusy,
  managedCertError,
  managedCertForm,
  requestCertificate,
  showManagedCert,
} from "../composables/useCertificates";
import { openProvider } from "../composables/useNetwork";
import { providers } from "../composables/useState";

</script>

<template>
  <div v-if="showManagedCert" class="overlay" v-dialog>
    <form class="modal" @submit.prevent="requestCertificate">
      <div class="modal-top">
        <div>
          <span class="eyebrow">{{ t("SSL 证书") }}</span>
          <h2>{{ editingManagedID ? t("编辑证书") : t("申请证书") }}</h2>
        </div>
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('关闭证书申请')"
          @click="showManagedCert = false"
        >
          ×
        </button>
      </div>
      <label
        >{{ t("申请名称")
        }}<input
          v-model="managedCertForm.name"
          maxlength="100"
          required
          :placeholder="t('我的站点证书')" /></label
      ><label
        >{{ t("证书域名")
        }}<input
          v-model="managedCertForm.domain"
          :readonly="!!editingManagedID"
          required
          :placeholder="t('app.example.com 或 *.example.com')"
          autocapitalize="none" /></label
      ><label
        >{{ t("验证方式")
        }}<select v-model="managedCertForm.method">
          <option value="dns-01">{{ t("DNS-01（支持泛域名）") }}</option>
          <option value="http-01">HTTP-01</option>
          <option value="internal">{{ t("内部 CA（内网使用）") }}</option>
        </select></label
      >
      <p class="inline-help">
        {{
          managedCertForm.method === "dns-01"
            ? t(
                "通过 DNS API 添加验证记录，不需要开放验证端口；凭据需要对应区域的写入权限。",
              )
            : managedCertForm.method === "http-01"
              ? t("域名须解析至此服务器，公网 80 端口须可达；不支持泛域名。")
              : t(
                  "使用本机 CA 签发。客户端需自行信任 Caddy 根证书，不能作为公网受信任证书。",
                )
        }}
      </p>
      <label v-if="managedCertForm.method !== 'internal'"
        >{{ t("联系邮箱（可选）")
        }}<input
          v-model="managedCertForm.email"
          type="email"
          placeholder="admin@example.com" /></label
      ><label v-if="managedCertForm.method === 'dns-01'"
        >{{ t("DNS 凭据")
        }}<select v-model="managedCertForm.dns_provider_id" required>
          <option value="" disabled>{{ t("选择服务商凭据") }}</option>
          <option
            v-for="provider in providers"
            :key="provider.id"
            :value="String(provider.id)"
          >
            {{ provider.name }} · {{ provider.provider_type }}
          </option>
        </select></label
      >
      <div
        v-if="managedCertForm.method === 'dns-01' && !providers.length"
        class="dependency-help"
      >
        <span>{{ t("请先添加 DNS 凭据。") }}</span
        ><button type="button" class="inline-action" @click="openProvider">
          {{ t("添加 DNS 凭据") }}
        </button>
      </div>
      <p class="auth-hint">
        {{
          t(
            "提交后由 Caddy 在后台申请并自动续期；同域名站点选择“自动 HTTPS”即可使用。已有其他 TLS 配置时请先修改站点。",
          )
        }}
      </p>
      <p v-if="managedCertError" class="error">{{ managedCertError }}</p>
      <div class="modal-actions">
        <button
          type="button"
          class="secondary"
          @click="showManagedCert = false"
        >
          {{ t("取消") }}</button
        ><button class="primary" :disabled="managedCertBusy">
          {{
            managedCertBusy
              ? t("提交中…")
              : editingManagedID
                ? t("保存修改")
                : t("提交申请")
          }}
        </button>
      </div>
    </form>
  </div>
</template>
