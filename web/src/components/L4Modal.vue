<script setup lang="ts">
import {ref,watch} from "vue";
import {apiFetch} from "../api";
import {csrfToken} from "../composables/useState";
import { t } from "../i18n";
import {
  l4EditingID,
  l4Error,
  l4Form,
  l4PartialSave,
  saveL4,
  showL4,
} from "../composables/useNetwork";
import { applyRetryVisible, saving } from "../composables/useState";

import { retryApply } from "../composables/useApply";
const checkBusy=ref(false),checkNote=ref('');watch(l4Form,()=>checkNote.value='',{deep:true});
async function checkPort(){checkBusy.value=true;checkNote.value='';try{const r=await apiFetch('/api/v1/layer4-rules/check',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrfToken.value},body:JSON.stringify({...l4Form.value,id:l4EditingID.value||0})});const d=await r.json();if(!r.ok)throw Error(t(d.error));checkNote.value=t(d.message)}catch(e){checkNote.value=e instanceof Error?t(e.message):t('检查失败')}finally{checkBusy.value=false}}
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
          :disabled="saving"
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
      <button type="button" class="secondary" :disabled="checkBusy||saving" @click="checkPort">{{checkBusy?t("正在检查…"):t("检查监听端口")}}</button><p v-if="checkNote" class="op-callout" role="status">{{checkNote}}</p>
<p v-if="l4Error" class="error" role="alert">{{ l4Error }}</p>
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
        <button type="button" class="secondary" :disabled="saving" @click="showL4 = false">
          {{ t("取消") }}</button
        ><button class="primary" :disabled="saving || (l4PartialSave && l4EditingID === null)">
          {{ saving ? t("应用中…") : t("保存并应用") }}
        </button>
      </div>
    </form>
  </div>
</template>
