<script setup lang="ts">
import { t } from "../i18n";
import {
  checkListenerPort,
  deleteListener,
  listenerAvailable,
  listenerChecking,
  listenerCopyFrom,
  listenerCreationMode,
  listenerDraft,
  listenerEditingIndex,
  listenerMessage,
  listenerSaving,
  saveListener,
  showListenerEditor,
  webListeners,
} from "../composables/useHosts";
import { hosts } from "../composables/useState";

</script>

<template>
  <div v-if="showListenerEditor" class="overlay" v-dialog>
    <form class="modal" @submit.prevent="saveListener">
      <div class="modal-top">
        <h2>
          {{
            listenerEditingIndex === null
              ? t("添加端口监听")
              : t("修改端口监听")
          }}
        </h2>
        <button
          type="button"
          class="icon-btn"
          :disabled="listenerSaving"
          :aria-label="t('关闭监听设置')"
          @click="showListenerEditor = false"
        >
          ×
        </button>
      </div>
      <div v-if="listenerEditingIndex === null" class="listener-create-options">
        <label>{{ t("配置来源") }}<select
            v-model="listenerCreationMode"
            @change="listenerCopyFrom=''"
          >
            <option value="empty">{{ t("完全新建") }}</option>
            <option value="copy">{{ t("从已有监听完全导入") }}</option>
          </select></label>
        <label v-if="listenerCreationMode === 'copy'"
          >{{ t("导入来源")
          }}<select
            v-model="listenerCopyFrom"
            required
            @change="
              listenerDraft.protocol =
                webListeners.find((item) => item.id === listenerCopyFrom)
                  ?.protocol || 'https';
              if (listenerDraft.port) checkListenerPort();
            "
          >
            <option value="">{{ t("请选择已有监听") }}</option>
            <option
              v-for="listener in webListeners"
              :key="listener.id"
              :value="listener.id"
            >
              {{ listener.protocol.toUpperCase() }} :{{ listener.port }} ·
              {{
                hosts.filter((host) => host.listener_id === listener.id).length
              }}
              个站点
            </option>
          </select></label
        >
      </div>
      <label
        >{{ t("协议")
        }}<select
          v-model="listenerDraft.protocol"
          :disabled="listenerEditingIndex !== null || listenerSaving"
          @change="checkListenerPort"
        >
          <option value="https">HTTPS</option>
          <option value="http">HTTP</option>
        </select></label
      ><label
        >{{ t("监听端口")
        }}<input
          :value="listenerDraft.port || ''"
          @input="
            listenerDraft.port = Number(
              ($event.target as HTMLInputElement).value,
            );
            checkListenerPort();
          "
          :disabled="listenerSaving"
          type="number"
          min="1"
          max="65535"
          required
      /></label>
      <p
        role="status"
        :class="listenerAvailable === false ? 'error' : 'probe-ok'"
      >
        {{ listenerChecking ? t("正在检测端口…") : listenerMessage }}
      </p>
      <button
        type="button"
        class="secondary"
        :disabled="listenerChecking"
        @click="checkListenerPort"
      >
        {{ t("重新检测") }}
      </button>
      <p class="auth-hint">
        {{
          t(
            "端口检测针对服务器本地绑定，不代表公网防火墙已放行。HTTP-01 验证仍要求公网 80 端口可达。",
          )
        }}
      </p>
      <div class="modal-actions">
        <button
          v-if="
            listenerEditingIndex !== null &&
            webListeners.filter(
              (item) => item.protocol === listenerDraft.protocol,
            ).length > 1
          "
          type="button"
          class="danger-button"
          :disabled="listenerSaving"
          @click="deleteListener"
        >
          {{ t("删除监听") }}</button
        ><button
          type="button"
          class="secondary"
          :disabled="listenerSaving"
          @click="showListenerEditor = false"
        >
          {{ t("取消") }}</button
        ><button
          class="primary"
          :disabled="
            listenerChecking || listenerAvailable !== true || listenerSaving
          "
        >
          {{ listenerSaving ? t("保存中…") : t("保存监听") }}
        </button>
      </div>
    </form>
  </div>
</template>
