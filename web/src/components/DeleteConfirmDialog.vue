<script setup lang="ts">
import { X } from "@lucide/vue";
import { t } from "../i18n";
import { confirmationTitle, deleteConfirmOption, deletePrompt, resolveDelete } from "../composables/useState";

</script>

<template>
  <div v-if="deletePrompt" class="overlay" v-dialog>
    <section
      class="modal confirm-delete-modal"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="delete-title"
    >
      <div class="modal-top">
        <h2 id="delete-title">{{ t(confirmationTitle) }}</h2>
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('关闭删除确认')"
          @click="resolveDelete(false)"
        >
          <X :size="17" />
        </button>
      </div>
      <p>{{ deletePrompt }}</p>
      <label v-if="deleteConfirmOption" class="delete-confirm-option">
        <input v-model="deleteConfirmOption.checked" type="checkbox" />
        {{ t(deleteConfirmOption.label) }}
      </label>
      <p class="auth-hint">{{ t("确认后立即执行，请核对要操作的项目。") }}</p>
      <div class="modal-actions">
        <button type="button" class="secondary" @click="resolveDelete(false)">
          {{ t("取消") }}</button
        ><button
          type="button"
          class="danger-button"
          @click="resolveDelete(true)"
        >
          {{ t("确认") }}
        </button>
      </div>
    </section>
  </div>
</template>
