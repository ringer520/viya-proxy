<script setup lang="ts">
import {onMounted,ref} from "vue";
import {apiFetch} from "../api";
import {readOnly} from "../composables/useState";
import {recoverConfig,recoveryBusy,recoveryError} from "../composables/useConfigStatus";
import { History } from "@lucide/vue";
import { t, tr } from "../i18n";
import ConfigTransfer from "./ConfigTransfer.vue";
import { csrfToken, revisions } from "../composables/useState";

import { configurationRestored, diffValue, loadRevisionDiff, revisionDiff, revisionDiffBusy, revisionDiffError } from "../composables/useConfig";
const recoveryBackups=ref<{name:string;updated:string;size:number}[]>([]);
const recoveryListError=ref("");
onMounted(async()=>{try{const r=await apiFetch("/api/v1/config/backups");if(!r.ok)throw Error(t("无法读取恢复点列表"));recoveryBackups.value=await r.json()}catch(e){recoveryListError.value=e instanceof Error?e.message:t("读取失败")}});
</script>

<template>
      <ConfigTransfer

        :csrf="csrfToken"
        @restored="configurationRestored"
      />
      <section class="panel recovery-backups"><div class="panel-head"><div><h2>{{t("自动恢复点")}}</h2><p>{{t("本机加密保存，保留最近 7 个日期的可用配置；账号不包含在内。")}}</p></div></div><p v-if="recoveryListError" class="error">{{recoveryListError}}</p><div v-for="backup in recoveryBackups" :key="backup.name" class="recovery-backup-row"><strong>{{backup.name.replace("auto-","")}}</strong><time>{{new Date(backup.updated).toLocaleString()}}</time><button class="secondary" :disabled="readOnly||recoveryBusy" @click="recoverConfig(backup.name)">{{t("恢复到此配置")}}</button></div><p v-if="!recoveryBackups.length" class="revision-empty">{{t("首次成功应用配置后生成恢复点")}}</p><p v-if="recoveryError" class="error">{{recoveryError}}</p></section>
      <section

        class="panel revisions"
      >
        <div class="panel-head">
          <div>
            <h2><History :size="16" />{{ t("配置历史") }}</h2>
            <p>
              {{ t("最近 10 条配置应用记录；业务配置可从上方保存或恢复。") }}
            </p>
          </div>
        </div>
        <div v-if="revisions.length" class="revision-list">
          <div
            v-for="revision in revisions.slice(0, 10)"
            :key="revision.id"
            class="revision-row"
          >
            <span class="revision-id">#{{ revision.id }}</span
            ><span class="revision-state" :class="revision.status">{{
              revision.status === "applied" ? t("已应用") : t("失败")
            }}</span
            ><span class="revision-error" v-if="revision.error">{{
              revision.error
            }}</span
            ><time>{{ revision.created_at }}</time
            ><button
              class="secondary revision-diff-btn"
              type="button"
              :disabled="revisionDiffBusy"
              @click="loadRevisionDiff(revision.id)"
            >
              {{ t("与当前对比") }}
            </button>
          </div>
        </div>
        <div v-else class="revision-empty">{{ t("尚无配置变更记录") }}</div>
        <p v-if="revisionDiffError" class="error" role="alert">
          {{ revisionDiffError }}
        </p>
        <div v-if="revisionDiff" class="revision-diff">
          <div class="revision-diff-head">
            <strong>{{
              tr`版本 #${revisionDiff.from} → #${revisionDiff.to}`
            }}</strong
            ><button
              type="button"
              class="secondary"
              @click="revisionDiff = null"
            >
              {{ t("关闭对比") }}
            </button>
          </div>
          <p class="drawer-field-help">
            {{
              t(
                "历史快照已脱敏（密码与凭据被替换），因此只能用于对比，不能直接回滚。需要恢复到旧配置请使用上方的配置恢复。",
              )
            }}
          </p>
          <p v-if="revisionDiff.truncated" class="drawer-field-help">
            {{ t("差异较多，每组仅显示前 200 项。") }}
          </p>
          <p
            v-if="
              !revisionDiff.added.length &&
              !revisionDiff.removed.length &&
              !revisionDiff.changed.length
            "
            class="revision-diff-empty"
          >
            {{ t("两个版本内容相同。") }}
          </p>
          <div v-if="revisionDiff.added.length" class="diff-group added">
            <h5>{{ t("新增") }} · {{ revisionDiff.added.length }}</h5>
            <ul>
              <li v-for="item in revisionDiff.added" :key="'a' + item.path">
                <code>{{ item.path }}</code
                ><span>{{ diffValue(item.to) }}</span>
              </li>
            </ul>
          </div>
          <div v-if="revisionDiff.removed.length" class="diff-group removed">
            <h5>{{ t("移除") }} · {{ revisionDiff.removed.length }}</h5>
            <ul>
              <li v-for="item in revisionDiff.removed" :key="'r' + item.path">
                <code>{{ item.path }}</code
                ><span>{{ diffValue(item.from) }}</span>
              </li>
            </ul>
          </div>
          <div v-if="revisionDiff.changed.length" class="diff-group changed">
            <h5>{{ t("变更") }} · {{ revisionDiff.changed.length }}</h5>
            <ul>
              <li v-for="item in revisionDiff.changed" :key="'c' + item.path">
                <code>{{ item.path }}</code
                ><span>{{ diffValue(item.from) }}</span
                ><span class="diff-arrow">→</span
                ><span>{{ diffValue(item.to) }}</span>
              </li>
            </ul>
          </div>
        </div>
      </section>
</template>
