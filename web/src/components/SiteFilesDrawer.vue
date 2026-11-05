<script setup lang="ts">
import { apiFetch as fetch } from "../api";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { t, tr } from "../i18n";
import { formatBytes } from "../format";
import { FolderUp, X, FolderPlus, Upload, ArrowUp, Download, Trash2, File, Folder, FolderOpen, AlertTriangle, Loader2, ExternalLink } from "@lucide/vue";
import FilePreviewPane from "./site-files/FilePreviewPane.vue";
import { fileApi, fileHeaders } from "./site-files/client";
import type { Entry } from "./site-files/types";
import { useFileUpload } from "../composables/useFileUpload";

// Static-site file drawer: browse, upload (files, folders or a zip that
// auto-expands, with progress and overwrite confirmation), mkdir and delete.
// Preview/editing lives in FilePreviewPane, the upload pipeline in useFileUpload.

const props = defineProps<{
  host: {
    id: number;
    type?: string;
    domain: string;
    root_path?: string;
    tls_mode?: string;
  };
  csrf: string;
}>();
const emit = defineEmits<{ (e: "close"): void }>();

// Handle exposed by FilePreviewPane so this drawer keeps owning Escape,
// unmount and beforeunload behaviour.
type PreviewPaneHandle = {
  open: (entry: Entry) => Promise<void>;
  requestClose: () => boolean;
  isOpen: () => boolean;
  isDirty: () => boolean;
};

const entries = ref<Entry[]>([]);
const path = ref("");
const writable = ref(false);
const truncated = ref(false);
const loading = ref(false);
const busy = ref(false);
const error = ref("");
const note = ref("");
const dragOver = ref(false);
const pendingDelete = ref<Entry | null>(null);
const mkdirOpen = ref(false);
const mkdirName = ref("");
const fileInput = ref<HTMLInputElement | null>(null);
const dirInput = ref<HTMLInputElement | null>(null);
const drawer = ref<HTMLElement | null>(null);
const mkdirInput = ref<HTMLInputElement | null>(null);
const previewPane = ref<PreviewPaneHandle | null>(null);
const previewLocked = ref(false);
const search = ref("");

const visibleEntries = computed(() => entries.value.filter(e => e.name.toLowerCase().includes(search.value.toLowerCase())).sort((a,b) => Number(b.is_dir)-Number(a.is_dir) || a.name.localeCompare(b.name)));

const crumbs = computed(() => {
  const parts = path.value ? path.value.split("/").filter(Boolean) : [];
  return parts.map((name, index) => ({ name, path: parts.slice(0, index + 1).join("/") }));
});

const api = (query = "") => fileApi(props.host.id, query);
const headers = (json = false) => fileHeaders(props.csrf, json);
const siteURL = computed(() => {
  const scheme = props.host.tls_mode === "none" ? "http" : "https";
  return `${scheme}://${props.host.domain}`;
});

const upload = useFileUpload({
  api: (query) => api(query),
  getCsrf: () => props.csrf,
  getPath: () => path.value,
  getEntryPaths: () => entries.value.map((e) => e.path),
  reload: () => load(),
  setBusy: (value) => (busy.value = value),
  setError: (value) => (error.value = value),
  setNote: (value) => (note.value = value),
  resetInputs: () => {
    if (fileInput.value) fileInput.value.value = "";
    if (dirInput.value) dirInput.value.value = "";
  },
});
const { uploadProgress, conflictPrompt, handleFiles, cancelConflict, confirmConflict, dropTransfer } = upload;

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const r = await fetch(
      api(`?path=${encodeURIComponent(path.value)}`),
      { credentials: "same-origin" },
    );
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("读取目录失败"));
    entries.value = body.items || [];
    writable.value = !!body.writable;
    truncated.value = !!body.truncated;
    path.value = body.path || "";
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t("读取目录失败");
  } finally {
    loading.value = false;
  }
}

function enter(entry: Entry) {
  if (!entry.is_dir) { void previewPane.value?.open(entry); return; }
  path.value = entry.path;
  void load();
}
function goUp() {
  const parts = path.value.split("/").filter(Boolean);
  parts.pop();
  path.value = parts.join("/");
  void load();
}
function goTo(crumb: { path: string }) {
  path.value = crumb.path;
  void load();
}

function closeDrawer() { if (previewPane.value?.requestClose() ?? true) emit("close"); }

async function onDrop(event: DragEvent) {
  dragOver.value = false;
  await dropTransfer(event.dataTransfer);
}

// --- mkdir / delete / download ---------------------------------------------

function openMkdir() {
  mkdirName.value = "";
  mkdirOpen.value = true;
  requestAnimationFrame(() => mkdirInput.value?.focus());
}
function cancelMkdir() {
  mkdirOpen.value = false;
  mkdirName.value = "";
}
async function confirmMkdir() {
  const name = mkdirName.value.trim();
  if (!name) return;
  busy.value = true; error.value = ""; note.value = "";
  try {
    const target = path.value ? `${path.value}/${name}` : name;
    const r = await fetch(api("/mkdir"), { method: "POST", headers: headers(true), body: JSON.stringify({ path: target }), credentials: "same-origin" });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("创建文件夹失败"));
    mkdirOpen.value = false; mkdirName.value = "";
    await load();
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t("创建文件夹失败");
  } finally { busy.value = false; }
}

async function remove(entry: Entry) {
  busy.value = true; error.value = ""; note.value = "";
  try {
    const recursive = entry.is_dir ? "&recursive=1" : "";
    const r = await fetch(api(`?path=${encodeURIComponent(entry.path)}${recursive}`), { method: "DELETE", headers: headers(), credentials: "same-origin" });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("删除失败"));
    pendingDelete.value = null;
    await load();
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t("删除失败");
  } finally { busy.value = false; }
}

function download(entry: Entry) {
  window.open(api(`/download?path=${encodeURIComponent(entry.path)}`));
}

function modifiedLabel(value: string) {
  return value ? value.replace("T", " ").replace("Z", "") : "";
}

function onKeydown(event: KeyboardEvent) {
  if (event.key !== "Escape") return;
  const pane = previewPane.value;
  if (pane?.isOpen()) { event.stopImmediatePropagation(); pane.requestClose(); return; }
  if (conflictPrompt.value) { conflictPrompt.value = null; return; }
  if (pendingDelete.value) { pendingDelete.value = null; return; }
  if (mkdirOpen.value) { cancelMkdir(); return; }
  closeDrawer();
}

function protectUnload(event: BeforeUnloadEvent) { if (previewPane.value?.isDirty()) { event.preventDefault(); event.returnValue = ""; } }
onMounted(() => {
  window.addEventListener("beforeunload", protectUnload);
  document.addEventListener("keydown", onKeydown);
  requestAnimationFrame(() => drawer.value?.focus());
  void load();
});
onBeforeUnmount(() => {
  document.removeEventListener("keydown", onKeydown);
  window.removeEventListener("beforeunload", protectUnload);
});
</script>

<template>
  <div class="sfd-backdrop">
    <section
      ref="drawer"
      class="sfd-drawer"
      role="dialog"
      aria-modal="true"
      aria-labelledby="sfd-title"
      tabindex="-1"
    >
      <header class="sfd-header">
        <span class="sfd-title-icon"><FolderUp :size="22" /></span>
        <div class="sfd-heading">
          <span class="eyebrow">{{ t("文件浏览器") }}</span>
          <h2 id="sfd-title">{{ host.domain }}</h2>
        </div>
        <a
          class="sfd-visit"
          :href="siteURL"
          target="_blank"
          rel="noopener noreferrer"
          :title="t('在新标签页打开站点')"
        >
          <ExternalLink :size="15" />{{ t("访问站点") }}
        </a>
        <button
          type="button"
          class="icon-btn"
          :disabled="busy || previewLocked"
          :aria-label="t('关闭')"
          @click="closeDrawer"
        >
          <X :size="20" />
        </button>
      </header>

      <p v-if="!writable" class="sfd-banner readonly">
        <AlertTriangle :size="16" />
        {{ props.host.type === "files" ? t("外部共享目录：支持预览和管理员文本编辑；上传和删除请使用已授权的 WebDAV 或服务器工具。") : t("该站点目录由面板外部管理，这里只读。上传请放到服务器上的对应目录。") }}
      </p>

      <div class="sfd-toolbar">
        <nav class="sfd-crumbs" :aria-label="t('当前目录')">
          <button
            type="button"
            class="sfd-crumb root"
            :class="{ current: !path }"
            @click="goTo({ path: '' })"
          >
            {{ t("根目录") }}
          </button>
          <template v-for="(crumb, i) in crumbs" :key="crumb.path">
            <span class="sfd-sep">/</span>
            <button
              type="button"
              class="sfd-crumb"
              :class="{ current: i === crumbs.length - 1 }"
              @click="goTo(crumb)"
            >
              {{ crumb.name }}
            </button>
          </template>
        </nav>
        <input v-model="search" class="sfd-search" :placeholder="t('搜索当前目录')" :aria-label="t('搜索当前目录')" />
        <div class="sfd-actions">
          <button
            type="button"
            class="secondary"
            :disabled="busy || !writable"
            @click="openMkdir"
          >
            <FolderPlus :size="15" />{{ t("新建文件夹") }}
          </button>
          <button
            type="button"
            class="secondary"
            :disabled="busy || !writable"
            :title="t('上传整个文件夹，保留目录结构')"
            @click="dirInput?.click()"
          >
            <FolderOpen :size="15" />{{ t("上传文件夹") }}
          </button>
          <button
            type="button"
            class="primary"
            :disabled="busy || !writable"
            @click="fileInput?.click()"
          >
            <Loader2 v-if="busy" :size="15" class="spin" />
            <Upload v-else :size="15" />
            {{ busy ? t("正在处理…") : t("上传文件 / ZIP") }}
          </button>
          <input
            ref="fileInput"
            type="file"
            multiple
            hidden
            @change="handleFiles(($event.target as HTMLInputElement).files)"
          />
          <input
            ref="dirInput"
            type="file"
            multiple
            webkitdirectory
            hidden
            @change="handleFiles(($event.target as HTMLInputElement).files)"
          />
        </div>
      </div>

      <div v-if="mkdirOpen" class="sfd-mkdir">
        <input
          ref="mkdirInput"
          v-model="mkdirName"
          :placeholder="t('新文件夹名称')"
          autocomplete="off"
          @keyup.enter="confirmMkdir"
        />
        <button type="button" class="primary" :disabled="busy" @click="confirmMkdir">
          {{ t("创建") }}
        </button>
        <button type="button" class="secondary" @click="cancelMkdir">
          {{ t("取消") }}
        </button>
      </div>

      <div v-if="uploadProgress !== null" class="sfd-progress" role="status">
        <div class="sfd-progress-bar">
          <div class="sfd-progress-fill" :style="{ width: Math.round((uploadProgress || 0) * 100) + '%' }"></div>
        </div>
        <span>{{ Math.round((uploadProgress || 0) * 100) }}%</span>
      </div>

      <div
        class="sfd-body"
        :class="{ dragging: dragOver }"
        @dragover.prevent="dragOver = true"
        @dragleave.prevent="dragOver = false"
        @drop.prevent="onDrop"
      >
        <table class="sfd-table">
          <thead>
            <tr>
              <th>{{ t("名称") }}</th>
              <th class="num">{{ t("大小") }}</th>
              <th class="mtime">{{ t("修改时间") }}</th>
              <th class="ops"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="path">
              <td colspan="4">
                <button type="button" class="sfd-up" @click="goUp">
                  <ArrowUp :size="15" /> {{ t("返回上级") }}
                </button>
              </td>
            </tr>
            <tr v-if="loading">
              <td colspan="4" class="sfd-empty">
                <Loader2 :size="16" class="spin" /> {{ t("加载中…") }}
              </td>
            </tr>
            <template v-else>
              <tr v-for="entry in visibleEntries" :key="entry.path">
                <td>
                  <button
                    type="button"
                    class="sfd-name"
                    @click="enter(entry)"
                  >
                    <Folder v-if="entry.is_dir" :size="16" class="dir" />
                    <File v-else :size="16" />
                    <span>{{ entry.name }}</span>
                  </button>
                </td>
                <td class="num">{{ entry.is_dir ? "—" : formatBytes(entry.size) }}</td>
                <td class="mtime">{{ modifiedLabel(entry.modified) }}</td>
                <td class="ops">
                  <button
                    v-if="!entry.is_dir"
                    type="button"
                    class="sfd-op"
                    :aria-label="t('下载')"
                    :title="t('下载')"
                    @click="download(entry)"
                  >
                    <Download :size="16" />
                  </button>
                  <button
                    v-if="writable"
                    type="button"
                    class="sfd-op danger"
                    :aria-label="t('删除')"
                    :title="t('删除')"
                    @click="pendingDelete = entry"
                  >
                    <Trash2 :size="16" />
                  </button>
                </td>
              </tr>
              <tr v-if="!visibleEntries.length">
                <td colspan="4" class="sfd-empty">{{ t("目录为空") }}</td>
              </tr>
            </template>
          </tbody>
        </table>
        <p v-if="truncated" class="sfd-note-inline">
          {{ t("文件较多，仅显示前 2000 项。") }}
        </p>
        <div v-if="dragOver" class="sfd-drop-hint">
          <Upload :size="20" />
          {{ t("释放以拖拽上传文件或文件夹") }}
        </div>
      </div>

      <FilePreviewPane
        ref="previewPane"
        :host="host"
        :csrf="csrf"
        :reload="load"
        @note="note = $event"
        @busy="previewLocked = $event"
      />
      <footer class="sfd-footer">
        <p v-if="error" class="sfd-error" role="alert">
          <AlertTriangle :size="14" />{{ error }}
        </p>
        <p v-else-if="note" class="sfd-note" role="status">{{ note }}</p>
        <p v-else class="sfd-hint">
          {{ t("支持多选、整个文件夹与 ZIP（自动解压）；同名文件会先确认。") }}
        </p>
      </footer>

      <div
        v-if="pendingDelete"
        class="sfd-confirm-backdrop"
        @click.self="pendingDelete = null"
      >
        <div class="sfd-confirm" role="alertdialog" aria-modal="true">
          <span class="sfd-confirm-icon"><AlertTriangle :size="22" /></span>
          <p>
            {{
              pendingDelete.is_dir
                ? t("确认删除该文件夹及其中全部内容？")
                : t("确认删除该文件？")
            }}
            <code>{{ pendingDelete.name }}</code>
          </p>
          <div class="sfd-confirm-actions">
            <button type="button" class="secondary" @click="pendingDelete = null">
              {{ t("取消") }}
            </button>
            <button
              type="button"
              class="danger-button"
              :disabled="busy"
              @click="remove(pendingDelete)"
            >
              {{ t("删除") }}
            </button>
          </div>
        </div>
      </div>

      <div
        v-if="conflictPrompt"
        class="sfd-confirm-backdrop"
        @click.self="cancelConflict"
      >
        <div class="sfd-confirm" role="alertdialog" aria-modal="true">
          <span class="sfd-confirm-icon"><AlertTriangle :size="22" /></span>
          <p>
            {{ tr`有 ${conflictPrompt.conflicts.length} 个文件已存在，如何处理？` }}
            <code>{{ conflictPrompt.conflicts.slice(0, 4).join("、") }}{{ conflictPrompt.conflicts.length > 4 ? " …" : "" }}</code>
          </p>
          <div class="sfd-confirm-actions">
            <button type="button" class="secondary" @click="cancelConflict">
              {{ t("取消") }}
            </button>
            <button
              type="button"
              class="secondary"
              @click="confirmConflict(false)"
            >
              {{ t("跳过已存在") }}
            </button>
            <button
              type="button"
              class="danger-button"
              @click="confirmConflict(true)"
            >
              {{ t("覆盖") }}
            </button>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.sfd-backdrop {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(11, 20, 43, 0.42);
  backdrop-filter: blur(3px);
  display: flex;
  justify-content: flex-end;
}
.sfd-drawer {
  display: flex;
  flex-direction: column;
  width: min(760px, 100%);
  height: 100dvh;
  background: var(--surface);
  color: var(--text);
  box-shadow: -20px 0 70px rgba(0, 0, 0, 0.18);
  outline: none;
  animation: sfd-enter 0.22s ease-out;
}
.sfd-header {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 24px 28px 20px;
  border-bottom: 1px solid var(--line);
  flex-shrink: 0;
}
.sfd-title-icon {
  display: grid;
  place-items: center;
  width: 45px;
  height: 45px;
  border-radius: 14px;
  background: color-mix(in srgb, var(--accent) 12%, var(--surface));
  color: var(--accent);
  flex-shrink: 0;
}
.sfd-heading {
  flex: 1;
  min-width: 0;
}
.sfd-heading .eyebrow {
  display: block;
  font-size: 10px;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--muted);
}
.sfd-heading h2 {
  font-size: 22px;
  margin: 4px 0 0;
  line-height: 1.3;
  overflow-wrap: anywhere;
}
.sfd-header > .icon-btn {
  margin-left: auto;
}
.sfd-visit {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  flex-shrink: 0;
  padding: 8px 14px;
  border: 1px solid var(--line-strong);
  border-radius: 9px;
  background: var(--surface);
  color: var(--accent);
  font-weight: 500;
  font-size: 13px;
  text-decoration: none;
  white-space: nowrap;
}
.sfd-visit:hover {
  border-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 8%, var(--surface));
}

.sfd-banner {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  margin: 16px 28px 0;
  padding: 11px 14px;
  border-radius: 10px;
  font-size: 12px;
  line-height: 1.6;
}
.sfd-banner.readonly {
  background: color-mix(in srgb, var(--accent-2, #0891b2) 10%, var(--surface));
  color: var(--muted);
  border: 1px solid color-mix(in srgb, var(--accent-2, #0891b2) 30%, var(--line));
}
.sfd-banner svg {
  flex-shrink: 0;
  margin-top: 1px;
}

.sfd-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding: 16px 28px 12px;
  flex-shrink: 0;
}
.sfd-crumbs {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 3px;
  min-width: 0;
  font-size: 13px;
  color: var(--muted);
}
.sfd-crumb {
  border: 0;
  background: none;
  color: var(--muted);
  font: inherit;
  cursor: pointer;
  padding: 3px 7px;
  border-radius: 7px;
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sfd-crumb:hover {
  background: var(--surface-2);
  color: var(--text);
}
.sfd-crumb.root {
  font-weight: 600;
}
.sfd-crumb.current {
  color: var(--text);
  font-weight: 600;
}
.sfd-sep {
  color: var(--muted);
  opacity: 0.6;
  user-select: none;
}
.sfd-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-shrink: 0;
  flex-wrap: wrap;
}
.sfd-actions .primary,
.sfd-actions .secondary {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  white-space: nowrap;
}

.sfd-mkdir {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 0 28px 12px;
  flex-shrink: 0;
}
.sfd-mkdir input {
  flex: 1;
  min-width: 0;
  border: 1px solid var(--line-strong);
  border-radius: 9px;
  background: var(--surface-2);
  color: var(--text);
  padding: 9px 12px;
  font: 13px inherit;
  outline: none;
}
.sfd-mkdir input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.sfd-progress {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 28px 12px;
  flex-shrink: 0;
  font-size: 12px;
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}
.sfd-progress-bar {
  flex: 1;
  height: 6px;
  border-radius: 3px;
  background: var(--surface-2);
  overflow: hidden;
}
.sfd-progress-fill {
  height: 100%;
  background: var(--accent);
  border-radius: 3px;
  transition: width 0.15s ease;
}

.sfd-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0 28px 20px;
  position: relative;
  overscroll-behavior: contain;
}
.sfd-body.dragging {
  background: color-mix(in srgb, var(--accent) 4%, var(--surface));
}
.sfd-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
  border: 1px solid var(--line);
  border-radius: 12px;
  overflow: hidden;
}
.sfd-table th {
  text-align: left;
  font-weight: 600;
  color: var(--muted);
  padding: 10px 12px;
  background: var(--surface-2);
  border-bottom: 1px solid var(--line);
  font-size: 11px;
  letter-spacing: 0.02em;
}
.sfd-table td {
  padding: 7px 12px;
  border-bottom: 1px solid var(--line);
  vertical-align: middle;
}
.sfd-table tbody tr:last-child td {
  border-bottom: 0;
}
.sfd-table th.num,
.sfd-table td.num {
  text-align: right;
  width: 90px;
  font-variant-numeric: tabular-nums;
}
.sfd-table th.mtime,
.sfd-table td.mtime {
  color: var(--muted);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
  width: 150px;
}
.sfd-table th.ops,
.sfd-table td.ops {
  text-align: right;
  width: 76px;
  white-space: nowrap;
}
.sfd-name {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  max-width: 100%;
  border: 0;
  background: none;
  color: var(--text);
  font: inherit;
  cursor: pointer;
  padding: 3px 6px;
  border-radius: 7px;
  text-align: left;
}
.sfd-name:hover {
  background: var(--surface-2);
}
.sfd-name svg {
  flex-shrink: 0;
  color: var(--muted);
}
.sfd-name svg.dir {
  color: var(--accent-2, #0891b2);
}
.sfd-name span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sfd-up {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  border: 0;
  background: none;
  color: var(--muted);
  font: 12px inherit;
  cursor: pointer;
  padding: 4px 6px;
  border-radius: 7px;
}
.sfd-up:hover {
  color: var(--accent);
  background: var(--surface-2);
}
.sfd-op {
  display: inline-grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--muted);
  cursor: pointer;
}
.sfd-op:hover {
  background: var(--surface-2);
  color: var(--accent);
}
.sfd-op.danger:hover {
  color: #dc2626;
  background: #dc262610;
}
.sfd-empty {
  text-align: center;
  color: var(--muted);
  padding: 30px 8px;
  font-size: 13px;
}
.sfd-empty svg {
  vertical-align: -3px;
}
.sfd-note-inline {
  margin: 10px 0 0;
  font-size: 11px;
  color: var(--muted);
}
.sfd-drop-hint {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: color-mix(in srgb, var(--accent) 8%, var(--surface));
  border: 2px dashed var(--accent);
  border-radius: 12px;
  color: var(--accent);
  font-size: 13px;
  pointer-events: none;
}

.sfd-footer {
  flex-shrink: 0;
  padding: 14px 28px 18px;
  border-top: 1px solid var(--line);
  min-height: 46px;
}
.sfd-footer p {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
}
.sfd-hint {
  color: var(--muted);
}
.sfd-note {
  color: #15803d;
  background: #16a34a12;
  border-radius: 8px;
  padding: 8px 12px;
}
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

.sfd-confirm-backdrop {
  position: absolute;
  inset: 0;
  z-index: 10;
  background: rgba(11, 20, 43, 0.4);
  display: grid;
  place-items: center;
  padding: 24px;
}
.sfd-confirm {
  width: min(400px, 100%);
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 16px;
  padding: 22px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.25);
  text-align: center;
}
.sfd-confirm-icon {
  display: inline-grid;
  place-items: center;
  width: 46px;
  height: 46px;
  border-radius: 50%;
  background: #dc262612;
  color: #dc2626;
  margin-bottom: 12px;
}
.sfd-confirm p {
  margin: 0 0 18px;
  font-size: 14px;
  line-height: 1.6;
  color: var(--text);
}
.sfd-confirm code {
  display: block;
  margin-top: 6px;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--muted);
  background: var(--surface-2);
  padding: 4px 8px;
  border-radius: 6px;
  overflow-wrap: anywhere;
  max-height: 90px;
  overflow-y: auto;
}
.sfd-confirm-actions {
  display: flex;
  gap: 10px;
  justify-content: center;
  flex-wrap: wrap;
}

.spin {
  animation: sfd-spin 0.8s linear infinite;
}
@keyframes sfd-spin {
  to {
    transform: rotate(360deg);
  }
}
@keyframes sfd-enter {
  from {
    transform: translateX(25px);
    opacity: 0;
  }
  to {
    transform: translateX(0);
    opacity: 1;
  }
}

@media (max-width: 560px) {
  .sfd-header {
    padding: 20px;
  }
  .sfd-toolbar {
    padding: 14px 20px 10px;
    flex-direction: column;
    align-items: stretch;
  }
  .sfd-actions {
    justify-content: flex-end;
  }
  .sfd-actions .primary,
  .sfd-actions .secondary {
    flex: 1;
    justify-content: center;
  }
  .sfd-mkdir {
    padding: 0 20px 10px;
    flex-wrap: wrap;
  }
  .sfd-mkdir input {
    flex-basis: 100%;
  }
  .sfd-progress {
    padding: 0 20px 10px;
  }
  .sfd-body {
    padding: 0 20px 16px;
  }
  .sfd-table th.mtime,
  .sfd-table td.mtime {
    display: none;
  }
  .sfd-footer {
    padding: 12px 20px 16px;
  }
  .sfd-banner {
    margin: 12px 20px 0;
  }
}
@media (prefers-reduced-motion: reduce) {
  .sfd-drawer {
    animation: none;
  }
  .spin {
    animation: none;
  }
}
.sfd-search { min-width:140px; flex:1; }
</style>
