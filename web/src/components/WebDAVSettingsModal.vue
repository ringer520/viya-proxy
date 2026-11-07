<script setup lang="ts">
import { ref, onMounted } from "vue";
import { Settings, X } from "@lucide/vue";
import { t } from "../i18n";
import { apiFetch } from "../api";

const emit = defineEmits<{ (e: "close"): void }>();

const configured = ref(false);
const domain = ref("");
const rootPath = ref("");
const account = ref("");
const password = ref("");
const writeable = ref(false);
const saving = ref(false);
const errorMsg = ref("");
const savedOnce = ref(false);

interface SettingsBody {
  domain: string;
  root_path: string;
  basic_auth_user: string;
  basic_auth_password?: string;
  webdav_write: boolean;
}

async function loadSettings() {
  const r = await apiFetch("/api/v1/webdav-settings");
  const body = await r.json().catch(() => null);
  if (r.ok && body) {
    configured.value = !!body.configured;
    domain.value = body.domain || "";
    rootPath.value = body.root_path || "";
    account.value = body.basic_auth_user || "";
    writeable.value = !!body.webdav_write;
  }
}

function save() {
  saving.value = true;
  errorMsg.value = "";
  const body: SettingsBody = {
    domain: domain.value.trim(),
    root_path: rootPath.value.trim(),
    basic_auth_user: account.value.trim(),
    basic_auth_password: password.value,
    webdav_write: writeable.value,
  };
  apiFetch("/api/v1/webdav-settings", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
    .then(async (r) => {
      const resp = await r.json().catch(() => null);
      if (!r.ok) throw new Error((resp && t(resp.error)) || t("保存失败"));
      configured.value = true;
      savedOnce.value = true;
      password.value = "";
      emit("close");
    })
    .catch((e) => (errorMsg.value = e.message || t("保存失败")))
    .finally(() => (saving.value = false));
}

onMounted(loadSettings);
</script>

<template>
  <div class="fm-modal-mask" @click.self="emit('close')">
    <div class="fm-modal webdav-modal">
      <header class="fm-modal-head">
        <strong><Settings :size="16" /> {{ t("WebDAV 设置") }}</strong>
        <span class="fm-spacer" />
        <button class="icon-btn" :title="t('关闭')" @click="emit('close')"><X :size="16" /></button>
      </header>

      <div class="webdav-form">
        <p class="webdav-intro">
          {{
            t("把一个服务器目录发布为 WebDAV 共享：客户端用同一地址、同一套账号挂载，可读写文件。")
          }}
        </p>

        <label class="form-row">
          <span class="form-label">{{ t("共享目录（绝对路径）") }}</span>
          <input v-model="rootPath" type="text" :placeholder="t('例如 /srv/share')" />
        </label>

        <label class="form-row">
          <span class="form-label">{{ t("访问域名") }}</span>
          <input v-model="domain" type="text" :placeholder="t('例如 share.example.com')" />
        </label>

        <label class="form-row">
          <span class="form-label">{{ t("账号") }}</span>
          <input v-model="account" type="text" :placeholder="t('WebDAV 登录用户名')" />
        </label>

        <label class="form-row">
          <span class="form-label">{{ configured ? t("密码（留空则不修改）") : t("密码") }}</span>
          <input v-model="password" type="password" :placeholder="t('至少 8 位')" />
        </label>

        <label class="form-check">
          <input v-model="writeable" type="checkbox" />
          <span>{{ t("允许 WebDAV 写入（上传、修改、删除）") }}</span>
        </label>

        <p v-if="writeable" class="webdav-warning">
          {{
            t("已开启写入：WebDAV 客户端可修改该目录下的任何文件，请务必设置强密码。")
          }}
        </p>

        <div v-if="errorMsg" class="fm-error">{{ errorMsg }}</div>
        <p v-else-if="savedOnce" class="webdav-ok">{{ t("已保存并应用。") }}</p>
      </div>

      <footer class="fm-modal-foot">
        <button class="primary" :disabled="saving" @click="save()">
          {{ saving ? t("保存中…") : t("保存") }}
        </button>
        <button class="secondary" @click="emit('close')">{{ t("取消") }}</button>
      </footer>
    </div>
  </div>
</template>

<style src="./webdav-modal.css"></style>
