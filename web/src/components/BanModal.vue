<script setup lang="ts">
import { t } from "../i18n";
import {
  banError,banPreview,banRiskConfirmed,
  banForm,
  banKind,
  editingBanID,
  saveBan,
  showBan,
} from "../composables/useNetwork";
import { applyRetryVisible, hosts, saving } from "../composables/useState";

import { retryApply } from "../composables/useApply";
</script>

<template>
  <div v-if="showBan" class="overlay" v-dialog>
    <form class="modal" @submit.prevent="saveBan">
      <div class="modal-top">
        <div>
          <span class="eyebrow">{{ t("IP 访问控制") }}</span>
          <h2>{{ editingBanID ? t("编辑封禁") : t("添加封禁") }}</h2>
        </div>
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('关闭封禁表单')"
          @click="showBan = false"
        >
          ×
        </button>
      </div>
      <label
        >{{ t("站点域名")
        }}<select v-model="banForm.domain" required>
          <option value="" disabled>{{ t("选择站点") }}</option>
          <option
            v-for="domain in [...new Set(hosts.map((host) => host.domain))]"
            :key="domain"
            :value="domain"
          >
            {{ domain }}
          </option>
        </select></label
      ><label
        >{{ t("封禁类型")
        }}<select v-model="banKind">
          <option value="ip">{{ t("指定 IP / 网段") }}</option>
          <option value="foreign">
            {{ t("禁止境外 IP（仅允许中国大陆及内网）") }}
          </option>
        </select></label
      ><label v-if="banKind === 'ip'"
        >{{ t("IP 或网段")
        }}<input
          v-model="banForm.ip"
          required
          :placeholder="t('192.0.2.10 或 192.0.2.0/24')"
      /></label>
      <p v-else class="auth-hint">
        {{
          t(
            "同时覆盖 IPv4 / IPv6，保留内网及回环访问。依据 APNIC 的 CN 分配数据近似判断归属地，不含港澳台。按直接连接 IP 判断；位于 CDN / 代理后时会判断代理地址，请先确认访问链路。此规则不替代身份验证。",
          )
        }}
      </p>
      <label
        >{{ t("封禁原因")
        }}<textarea v-model="banForm.reason" rows="2" maxlength="500"
      /></label>
      <p class="auth-hint">
        {{
          t(
            "默认为永久封禁，需手动解除。保存后立即应用；手动规则不会被自动防护覆盖。",
          )
        }}
      </p>
      <section v-if="banPreview" class="op-callout" :class="{warning:banPreview.panel_domain||banPreview.client_blocked}"><strong>{{t('影响预览')}} · {{banPreview.domain}}</strong><p>{{t('当前访问来源')}}：<code>{{banPreview.client_ip}}</code> · {{banPreview.client_blocked?t('会命中此规则'):t('未命中此规则')}}</p><p>{{t(banPreview.message)}}</p><p v-if="banPreview.panel_domain">{{t('此站点指向面板自身。保存后需在 120 秒内从该域名确认可访问，否则自动恢复原规则。可从局域网入口取消修改。')}}</p><label class="enabled-label"><input v-model="banRiskConfirmed" type="checkbox"/>{{t('我已核对访问来源和影响范围')}}</label></section>
      <p v-if="banError" class="error" role="alert">{{ banError }}</p>
      <button
        v-if="applyRetryVisible"
        type="button"
        class="secondary"
        @click="retryApply"
      >
        {{ t("重新应用到 Caddy") }}
      </button>
      <div class="modal-actions">
        <button type="button" class="secondary" @click="showBan = false">
          {{ t("取消") }}</button
        ><button class="primary" :disabled="saving">
          {{saving?t("处理中…"):!banPreview?t("预览影响"):t("保存并应用")}}
        </button>
      </div>
    </form>
  </div>
</template>
