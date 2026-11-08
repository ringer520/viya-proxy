<script setup lang="ts">
import {onMounted,ref} from "vue";
import {apiFetch} from "../api";
import {readOnly, csrfToken} from "../composables/useState";
import {recoverConfig,recoveryBusy,recoveryError} from "../composables/useConfigStatus";
import { t } from "../i18n";
import ConfigTransfer from "./ConfigTransfer.vue";
import { configurationRestored } from "../composables/useConfig";

const recoveryBackups=ref<{name:string;updated:string;size:number}[]>([]);
const recoveryListError=ref("");
onMounted(async()=>{try{const r=await apiFetch("/api/v1/config/backups");if(!r.ok)throw Error(t("无法读取恢复点列表"));recoveryBackups.value=await r.json()}catch(e){recoveryListError.value=e instanceof Error?e.message:t("读取失败")}});
</script>

<template>
      <ConfigTransfer
        :csrf="csrfToken"
        @restored="configurationRestored"
      />
      <section class="panel recovery-backups"><div class="panel-head"><div><h2>{{t("自动恢复点")}}</h2><p>{{t("本机加密保存，保留最近 5 个日期的可用配置；账号不包含在内。")}}</p></div></div><p v-if="recoveryListError" class="error">{{recoveryListError}}</p><div v-for="backup in recoveryBackups" :key="backup.name" class="recovery-backup-row"><strong>{{backup.name.replace("auto-","")}}</strong><time>{{new Date(backup.updated).toLocaleString()}}</time><button class="secondary" :disabled="readOnly||recoveryBusy" @click="recoverConfig(backup.name)">{{t("恢复到此配置")}}</button></div><p v-if="!recoveryBackups.length" class="empty-state">{{t("首次成功应用配置后生成恢复点")}}</p><p v-if="recoveryError" class="error">{{recoveryError}}</p></section>
</template>
