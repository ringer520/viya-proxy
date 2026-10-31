<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { t } from "../i18n";
import { formatBytes } from "../format";

// Static-site file manager. Deliberately minimal: browse, upload (including
// zip expansion), create folders and delete. No editing — content is meant to
// be dropped in as a build artifact.

type Entry = {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  modified: string;
};

const props = defineProps<{
  host: { id: number; domain: string; root_path?: string };
  csrf: string;
}>();
const emit = defineEmits<{ (e: "close"): void }>();

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
const fileInput = ref<HTMLInputElement | null>(null);

const crumbs = computed(() => {
  const parts = path.value ? path.value.split("/").filter(Boolean) : [];
  return parts.map((name, index) => ({
    name,
    path: parts.slice(0, index + 1).join("/"),
  }));
});

function api(query = "") {
  return `/api/v1/proxy-hosts/${props.host.id}/files${query}`;
}
function headers(json = false) {
  const h: Record<string, string> = { "X-CSRF-Token": props.csrf };
  if (json) h["Content-Type"] = "application/json";
  return h;
}

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
  if (!entry.is_dir) return;
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

async function uploadFiles(files: FileList | File[]) {
  if (!files.length) return;
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    const data = new FormData();
    for (const file of Array.from(files)) data.append("files", file, file.name);
    const r = await fetch(api(`/upload?path=${encodeURIComponent(path.value)}`), {
      method: "POST",
      headers: headers(),
      body: data,
      credentials: "same-origin",
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("上传失败"));
    note.value =
      body.extracted > 0
        ? t("已上传 ") +
          body.stored +
          t(" 个文件，解压 ") +
          body.extracted +
          t(" 个文件")
        : t("已上传 ") + body.stored + t(" 个文件");
    await load();
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t("上传失败");
  } finally {
    busy.value = false;
    if (fileInput.value) fileInput.value.value = "";
  }
}

function onDrop(event: DragEvent) {
  dragOver.value = false;
  const files = event.dataTransfer?.files;
  if (files && files.length) void uploadFiles(files);
}

async function mkdir() {
  const name = window.prompt(t("新文件夹名称"));
  if (!name || !name.trim()) return;
  busy.value = true;
  error.value = "";
  try {
    const target = path.value ? `${path.value}/${name.trim()}` : name.trim();
    const r = await fetch(api("/mkdir"), {
      method: "POST",
      headers: headers(true),
      body: JSON.stringify({ path: target }),
      credentials: "same-origin",
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("创建文件夹失败"));
    await load();
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t("创建文件夹失败");
  } finally {
    busy.value = false;
  }
}

async function remove(entry: Entry) {
  busy.value = true;
  error.value = "";
  try {
    const recursive = entry.is_dir ? "&recursive=1" : "";
    const r = await fetch(
      api(`?path=${encodeURIComponent(entry.path)}${recursive}`),
      {
        method: "DELETE",
        headers: headers(),
        credentials: "same-origin",
      },
    );
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("删除失败"));
    pendingDelete.value = null;
    await load();
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t("删除失败");
  } finally {
    busy.value = false;
  }
}

function download(entry: Entry) {
  window.open(
    `/api/v1/proxy-hosts/${props.host.id}/files/download?path=${encodeURIComponent(entry.path)}`,
  );
}

function modifiedLabel(value: string) {
  return value ? value.replace("T", " ").replace("Z", "") : "";
}

onMounted(load);
</script>

<template>
  <div class="overlay" @click.self="emit('close')">
    <div class="modal site-files-modal">
      <div class="modal-top">
        <div>
          <span class="eyebrow">{{ t("静态站点文件") }}</span>
          <h2>{{ host.domain }}</h2>
        </div>
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('关闭')"
          @click="emit('close')"
        >
          ×
        </button>
      </div>

      <div class="files-toolbar">
        <nav class="files-crumbs" :aria-label="t('当前目录')">
          <button type="button" class="crumb" @click="goTo({ path: '' })">
            {{ t("根目录") }}
          </button>
          <template v-for="crumb in crumbs" :key="crumb.path">
            <span>/</span>
            <button type="button" class="crumb" @click="goTo(crumb)">
              {{ crumb.name }}
            </button>
          </template>
        </nav>
        <div class="files-actions">
          <button
            type="button"
            class="secondary"
            :disabled="busy || !writable"
            @click="mkdir"
          >
            {{ t("新建文件夹") }}
          </button>
          <button
            type="button"
            class="primary"
            :disabled="busy || !writable"
            @click="fileInput?.click()"
          >
            {{ busy ? t("正在处理…") : t("上传文件 / ZIP") }}
          </button>
          <input
            ref="fileInput"
            type="file"
            multiple
            hidden
            @change="uploadFiles(($event.target as HTMLInputElement).files || [])"
          />
        </div>
      </div>

      <p v-if="!writable" class="inline-help">
        {{
          t("该站点目录由面板外部管理，这里只读。上传请放到服务器上的对应目录。")
        }}
      </p>
      <p v-else class="inline-help">
        {{ t("支持多选上传；ZIP 会自动解压到当前目录。") }}
      </p>

      <div
        class="files-dropzone"
        :class="{ over: dragOver }"
        @dragover.prevent="dragOver = true"
        @dragleave.prevent="dragOver = false"
        @drop.prevent="onDrop"
      >
        <table class="files-table">
          <thead>
            <tr>
              <th>{{ t("名称") }}</th>
              <th>{{ t("大小") }}</th>
              <th>{{ t("修改时间") }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="path">
              <td colspan="4">
                <button type="button" class="crumb" @click="goUp">
                  … {{ t("返回上级") }}
                </button>
              </td>
            </tr>
            <tr v-for="entry in entries" :key="entry.path">
              <td>
                <button
                  type="button"
                  class="crumb file-name"
                  @click="enter(entry)"
                >
                  {{ entry.is_dir ? "▸ " : "" }}{{ entry.name }}
                </button>
              </td>
              <td>{{ entry.is_dir ? "—" : formatBytes(entry.size) }}</td>
              <td class="file-mtime">{{ modifiedLabel(entry.modified) }}</td>
              <td class="file-ops">
                <button
                  v-if="!entry.is_dir"
                  type="button"
                  class="inline-action"
                  @click="download(entry)"
                >
                  {{ t("下载") }}
                </button>
                <button
                  v-if="writable"
                  type="button"
                  class="inline-action danger"
                  @click="pendingDelete = entry"
                >
                  {{ t("删除") }}
                </button>
              </td>
            </tr>
            <tr v-if="!loading && !entries.length">
              <td colspan="4" class="files-empty">{{ t("目录为空") }}</td>
            </tr>
          </tbody>
        </table>
        <p v-if="truncated" class="inline-help">
          {{ t("文件较多，仅显示前 2000 项。") }}
        </p>
      </div>

      <p v-if="note" class="auth-notice" role="status">{{ note }}</p>
      <p v-if="error" class="error">{{ error }}</p>

      <div v-if="pendingDelete" class="delete-prompt">
        <p>
          {{
            pendingDelete.is_dir
              ? t("确认删除该文件夹及其中全部内容？")
              : t("确认删除该文件？")
          }}
          <code>{{ pendingDelete.name }}</code>
        </p>
        <div class="prompt-actions">
          <button type="button" class="secondary" @click="pendingDelete = null">
            {{ t("取消") }}
          </button>
          <button
            type="button"
            class="danger-btn"
            :disabled="busy"
            @click="remove(pendingDelete)"
          >
            {{ t("删除") }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.site-files-modal {
  width: min(760px, 94vw);
  max-height: 88vh;
  overflow: auto;
}
.files-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
  margin: 12px 0 6px;
}
.files-crumbs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
  font-size: 13px;
  color: var(--muted);
}
.crumb {
  border: 0;
  background: none;
  color: inherit;
  font: inherit;
  cursor: pointer;
  padding: 2px 4px;
  border-radius: 6px;
}
.crumb:hover {
  background: var(--fill-soft, rgba(127, 127, 127, 0.12));
  color: var(--text);
}
.files-actions {
  display: flex;
  gap: 8px;
}
.files-dropzone {
  border: 1px dashed var(--line);
  border-radius: 12px;
  padding: 6px;
  margin-top: 8px;
}
.files-dropzone.over {
  border-color: var(--accent, #378add);
  background: rgba(55, 138, 221, 0.08);
}
.files-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.files-table th {
  text-align: left;
  font-weight: 500;
  color: var(--muted);
  padding: 6px 8px;
  border-bottom: 1px solid var(--line);
}
.files-table td {
  padding: 6px 8px;
  border-bottom: 1px solid var(--line);
  vertical-align: middle;
}
.files-table tr:last-child td {
  border-bottom: 0;
}
.file-name {
  text-align: left;
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.file-mtime {
  color: var(--muted);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.file-ops {
  white-space: nowrap;
  text-align: right;
}
.file-ops .danger {
  color: var(--danger, #c0392b);
}
.files-empty {
  text-align: center;
  color: var(--muted);
  padding: 22px 8px;
}
.delete-prompt {
  margin-top: 12px;
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 12px;
}
.prompt-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 8px;
}
.danger-btn {
  background: var(--danger, #c0392b);
  color: #fff;
  border: 0;
  border-radius: 8px;
  padding: 8px 14px;
  cursor: pointer;
}
@media (max-width: 560px) {
  .files-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .file-mtime {
    display: none;
  }
}
</style>
