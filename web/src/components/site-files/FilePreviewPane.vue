<script setup lang="ts">
import { apiFetch as fetch } from "../../api";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { t } from "../../i18n";
import { fileApi, fileHeaders } from "./client";
import type { Entry } from "./types";

// Text/image preview and inline editing for the static-site file drawer.
// Owns the preview cursor plus its own dirty/loading flags; the drawer asks for
// them through the exposed handle so its Escape/unload guards stay central.

type PreviewState = {
  entry: Entry;
  content: string;
  original: string;
  version: string;
  editable: boolean;
  image?: string;
};

const props = defineProps<{
  host: { id: number };
  csrf: string;
  reload: () => Promise<void>;
}>();
const emit = defineEmits<{
  (e: "note", text: string): void;
  (e: "busy", value: boolean): void;
}>();

const preview = ref<PreviewState | null>(null);
const previewError = ref("");
const textBusy = ref(false);
const textLoading = ref(false);
const textDirty = computed(
  () => !!preview.value && preview.value.content !== preview.value.original,
);

function api(query = "") {
  return fileApi(props.host.id, query);
}

// The drawer's close button stays disabled while a preview is loading/saving.
watch(
  () => textBusy.value || textLoading.value,
  (value) => emit("busy", value),
);

function closePreview() {
  if (
    textBusy.value ||
    textLoading.value ||
    (textDirty.value && !window.confirm(t("放弃文件的未保存修改？")))
  )
    return false;
  if (preview.value?.image) URL.revokeObjectURL(preview.value.image);
  preview.value = null;
  return true;
}

async function previewFile(entry: Entry) {
  if (!closePreview()) return;
  preview.value = {
    entry,
    content: "",
    original: "",
    version: "",
    editable: false,
  };
  textLoading.value = true;
  previewError.value = "";
  try {
    if (/\.(png|jpe?g|gif|webp|avif|bmp)$/i.test(entry.name)) {
      if (entry.size > 10 * 1024 * 1024)
        throw Error(t("图片预览最大支持 10 MiB"));
      const r = await fetch(
        api(`/download?path=${encodeURIComponent(entry.path)}`),
      );
      if (!r.ok) throw Error(t("读取文件失败"));
      const blob = await r.blob();
      if (
        blob.size > 10 * 1024 * 1024 ||
        !/^image\/(png|jpeg|gif|webp|avif|bmp)$/i.test(blob.type)
      )
        throw Error(t("不支持的图片格式，请下载后查看"));
      preview.value.image = URL.createObjectURL(blob);
      return;
    }
    const r = await fetch(api(`/text?path=${encodeURIComponent(entry.path)}`));
    const b = await r.json();
    if (!r.ok) throw Error(t(b.error) || t("读取文件失败"));
    preview.value = {
      entry,
      content: b.content,
      original: b.content,
      version: b.version,
      editable: b.editable,
    };
  } catch (e) {
    previewError.value = e instanceof Error ? e.message : t("读取文件失败");
  } finally {
    textLoading.value = false;
  }
}

async function saveText() {
  const d = preview.value;
  if (!d?.editable || textBusy.value) return;
  textBusy.value = true;
  previewError.value = "";
  try {
    const r = await fetch(api(`/text?path=${encodeURIComponent(d.entry.path)}`), {
      method: "PUT",
      headers: fileHeaders(props.csrf, true),
      body: JSON.stringify({ content: d.content, version: d.version }),
    });
    const b = await r.json();
    if (!r.ok) throw Error(t(b.error) || t("保存失败"));
    d.original = d.content;
    d.version = b.version;
    emit("note", t("文件已保存"));
    await props.reload();
  } catch (e) {
    previewError.value = e instanceof Error ? e.message : t("保存失败");
  } finally {
    textBusy.value = false;
  }
}

onBeforeUnmount(() => {
  if (preview.value?.image) URL.revokeObjectURL(preview.value.image);
});

defineExpose({
  open: previewFile,
  requestClose: closePreview,
  isOpen: () => !!preview.value,
  isDirty: () => textDirty.value,
});
</script>

<template>
  <section v-if="preview" class="sfd-preview" :aria-label="t('文件预览与编辑')">
    <header><strong>{{preview.entry.name}}</strong><small>{{preview.image?t('图片预览'):t('UTF-8 文本 · 最大 1 MiB')}}</small>
      <button v-if="preview.editable" class="primary" :disabled="!textDirty || textBusy" @click="saveText">{{textBusy?t('正在保存…'):t('保存文件')}}</button>
      <button class="secondary" :disabled="textBusy || textLoading" @click="closePreview">{{t('关闭预览')}}</button></header>
    <p v-if="previewError" class="sfd-error" role="alert">{{previewError}}</p>
    <p v-else-if="textLoading">{{t('加载中…')}}</p>
    <img v-else-if="preview.image" :src="preview.image" :alt="preview.entry.name" class="sfd-image-preview" />
    <textarea v-else v-model="preview.content" :readonly="!preview.editable || textBusy" spellcheck="false" :aria-label="t('文件内容')" />
  </section>
</template>

<style scoped>
.sfd-preview { padding:16px; border-top:1px solid var(--line); flex:0 1 55vh; min-height:150px; overflow:auto; }
.sfd-preview header { display:flex; flex-wrap:wrap; align-items:center; gap:10px; margin-bottom:12px; }
.sfd-preview header strong { flex:1; overflow-wrap:anywhere; }
.sfd-preview textarea { width:100%; min-height:320px; max-height:55vh; resize:vertical; font:13px/1.7 ui-monospace,monospace; tab-size:2; white-space:pre; }
.sfd-image-preview {display:block; max-width:100%; max-height:50vh; object-fit:contain; margin:auto;}

.sfd-error {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  color: #dc2626;
  background: #dc262610;
  border: 1px solid #dc262633;
  border-radius: 8px;
  padding: 8px 12px;
  overflow-wrap: anywhere;
}
.sfd-error svg {
  flex-shrink: 0;
  margin-top: 2px;
}
</style>
