<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import {
  Folder,
  FolderOpen,
  File as FileIcon,
  ArrowUp,
  RefreshCw,
  Upload,
  FolderPlus,
  FilePlus,
  Download,
  Pencil,
  Trash2,
  Settings,
  ChevronRight,
  Home,
  Lock,
} from "@lucide/vue";
import { t } from "../i18n";
import { apiFetch } from "../api";
import { formatBytes } from "../format";
import WebDAVSettingsModal from "./WebDAVSettingsModal.vue";

const STORAGE_KEY = "viya-fm-path";

interface FileItem {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  modified: string;
}
interface DirNode {
  name: string;
  path: string;
  has_children: boolean;
  readable: boolean;
}

const currentPath = ref<string>("");
const parentPath = ref<string>("");
const items = ref<FileItem[]>([]);
const loading = ref(false);
const errorMsg = ref<string>("");
const pathInput = ref<string>("");
const writable = ref(true);
const truncated = ref(false);

// Tree state
const treeCache = ref<Record<string, DirNode[]>>({});
const expanded = ref<Set<string>>(new Set());
const treeLoading = ref<string>("");

// Editor state
const editorOpen = ref(false);
const editorName = ref("");
const editorPath = ref("");
const editorContent = ref("");
const editorVersion = ref("");
const editorSaving = ref(false);

const webdavOpen = ref(false);

async function apiJSON(url: string, init?: RequestInit): Promise<any> {
  const r = await apiFetch(url, init);
  let body: any = null;
  try {
    body = await r.json();
  } catch {
    body = null;
  }
  if (!r.ok) {
    throw new Error((body && t(body.error)) || `请求失败（${r.status}）`);
  }
  return body;
}

function guardWrite(): boolean {
  if (!writable.value) {
    errorMsg.value = t("文件服务为只读模式，不能修改。");
    return false;
  }
  return true;
}

async function loadList(path: string) {
  loading.value = true;
  errorMsg.value = "";
  try {
    const data = await apiJSON(`/api/v1/files/browse?path=${encodeURIComponent(path)}`);
    items.value = (data.items || []) as FileItem[];
    // Sort: directories first, then by name.
    items.value.sort((a: FileItem, b: FileItem) => {
      if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    currentPath.value = data.path || "";
    parentPath.value = data.parent || "";
    pathInput.value = currentPath.value;
    writable.value = data.writable !== false;
    truncated.value = !!data.truncated;
    localStorage.setItem(STORAGE_KEY, currentPath.value);
  } catch (e: any) {
    errorMsg.value = e.message || t("无法读取目录");
  } finally {
    loading.value = false;
  }
}

function navigateTo(path: string) {
  loadList(path);
}

function goUp() {
  if (parentPath.value !== "") navigateTo(parentPath.value);
  else navigateTo("");
}

function goRoot() {
  navigateTo("");
}

function submitPath() {
  let p = pathInput.value.trim();
  if (p === "/") p = "";
  navigateTo(p);
}

function breadcrumbs(): { name: string; path: string }[] {
  const p = currentPath.value;
  if (p === "" || p === "/") return [{ name: t("根目录"), path: "" }];
  const parts = p.split("/").filter(Boolean);
  const crumbs: { name: string; path: string }[] = [{ name: t("根目录"), path: "" }];
  let acc = "";
  for (const part of parts) {
    acc = acc ? acc + "/" + part : part;
    crumbs.push({ name: part, path: acc });
  }
  return crumbs;
}

async function uploadFiles(files: FileList | null) {
  if (!guardWrite()) return;
  if (!files || files.length === 0) return;
  loading.value = true;
  errorMsg.value = "";
  try {
    const form = new FormData();
    for (let i = 0; i < files.length; i++) form.append("files", files[i]);
    const r = await apiFetch(`/api/v1/files/upload?path=${encodeURIComponent(currentPath.value)}`, {
      method: "POST",
      body: form,
    });
    const body = await r.json().catch(() => null);
    if (!r.ok) throw new Error((body && t(body.error)) || t("上传失败"));
    await loadList(currentPath.value);
  } catch (e: any) {
    errorMsg.value = e.message || t("上传失败");
  } finally {
    loading.value = false;
  }
}

function newFolder() {
  if (!guardWrite()) return;
  const name = window.prompt(t("新文件夹名称"), "");
  if (name == null) return;
  const trimmed = name.trim();
  if (!trimmed) return;
  const target = currentPath.value ? currentPath.value + "/" + trimmed : trimmed;
  apiJSON("/api/v1/files/mkdir", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path: target }),
  })
    .then(() => loadList(currentPath.value))
    .catch((e) => (errorMsg.value = e.message));
}

function newFile() {
  if (!guardWrite()) return;
  const name = window.prompt(t("新建文件名称（含扩展名）"), "");
  if (name == null) return;
  const trimmed = name.trim();
  if (!trimmed) return;
  const target = currentPath.value ? currentPath.value + "/" + trimmed : trimmed;
  apiJSON("/api/v1/files/create", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path: target }),
  })
    .then(() => loadList(currentPath.value))
    .catch((e) => (errorMsg.value = e.message));
}

function download(item: FileItem) {
  const a = document.createElement("a");
  a.href = `/api/v1/files/download?path=${encodeURIComponent(item.path)}`;
  a.download = item.name;
  document.body.appendChild(a);
  a.click();
  a.remove();
}

function rename(item: FileItem) {
  if (!guardWrite()) return;
  const next = window.prompt(t("重命名为"), item.name);
  if (next == null) return;
  const trimmed = next.trim();
  if (!trimmed || trimmed === item.name) return;
  apiJSON("/api/v1/files/rename", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path: item.path, name: trimmed }),
  })
    .then(() => loadList(currentPath.value))
    .catch((e) => (errorMsg.value = e.message));
}

function remove(item: FileItem) {
  if (!guardWrite()) return;
  const msg = item.is_dir
    ? t("确认删除文件夹“{0}”及其下所有内容？此操作不可恢复。").replace("{0}", item.name)
    : t("确认删除文件“{0}”？此操作不可恢复。").replace("{0}", item.name);
  if (!window.confirm(msg)) return;
  const qs = item.is_dir ? `?path=${encodeURIComponent(item.path)}&recursive=1` : `?path=${encodeURIComponent(item.path)}`;
  apiJSON(`/api/v1/files${qs}`, { method: "DELETE" })
    .then(() => loadList(currentPath.value))
    .catch((e) => (errorMsg.value = e.message));
}

async function openItem(item: FileItem) {
  if (item.is_dir) {
    navigateTo(item.path);
    // Expand the node in the tree.
    if (!expanded.value.has(item.path)) {
      expanded.value.add(item.path);
      await loadTree(item.path);
    }
    return;
  }
  // Try to open as text for preview/edit.
  try {
    loading.value = true;
    const data = await apiJSON(`/api/v1/files/content?path=${encodeURIComponent(item.path)}`);
    editorName.value = item.name;
    editorPath.value = item.path;
    editorContent.value = data.content || "";
    editorVersion.value = data.version || "";
    editorOpen.value = true;
  } catch (e: any) {
    // Not a text file: offer download instead.
    errorMsg.value = e.message;
    window.alert(t("该文件不是可预览的文本，已触发下载。"));
    download(item);
  } finally {
    loading.value = false;
  }
}

function saveEditor() {
  if (!guardWrite()) return;
  editorSaving.value = true;
  apiJSON("/api/v1/files/content", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path: editorPath.value, content: editorContent.value, version: editorVersion.value }),
  })
    .then((d) => {
      editorVersion.value = d.version || editorVersion.value;
      editorOpen.value = false;
      loadList(currentPath.value);
    })
    .catch((e) => (errorMsg.value = e.message))
    .finally(() => (editorSaving.value = false));
}

// ---- Tree ----
async function loadTree(path: string) {
  treeLoading.value = path;
  try {
    const data = await apiJSON(`/api/v1/files/tree?path=${encodeURIComponent(path)}`);
    treeCache.value[path] = (data.dirs || []) as DirNode[];
  } catch {
    treeCache.value[path] = [];
  } finally {
    treeLoading.value = "";
  }
}

function toggleExpand(path: string) {
  if (expanded.value.has(path)) {
    expanded.value.delete(path);
  } else {
    expanded.value.add(path);
    if (!treeCache.value[path]) loadTree(path);
  }
  // trigger reactivity for Set
  expanded.value = new Set(expanded.value);
}

function treeChildren(path: string): DirNode[] {
  return treeCache.value[path] || [];
}

// Build the flattened visible tree with depth.
const visibleTree = computed(() => {
  const out: { node: DirNode; depth: number }[] = [];
  const walk = (path: string, depth: number) => {
    for (const node of treeChildren(path)) {
      out.push({ node, depth });
      if (expanded.value.has(node.path)) walk(node.path, depth + 1);
    }
  };
  walk("", 0);
  return out;
});

function isCurrent(path: string) {
  return currentPath.value === path;
}

function selectTree(path: string) {
  navigateTo(path);
}

function refreshTree() {
  treeCache.value = {};
  expanded.value = new Set();
  loadTree("");
}

function fileTypeLabel(item: FileItem) {
  if (item.is_dir) return t("文件夹");
  const ext = item.name.includes(".") ? item.name.split(".").pop()!.toLowerCase() : "";
  return ext ? ext.toUpperCase() + " " + t("文件") : t("文件");
}

function formatDate(iso: string) {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

onMounted(async () => {
  const last = localStorage.getItem(STORAGE_KEY) || "";
  await loadTree("");
  // Expand the chain toward the last visited path so it is visible.
  if (last) {
    const parts = last.split("/").filter(Boolean);
    let acc = "";
    for (const part of parts) {
      acc = acc ? acc + "/" + part : part;
      expanded.value.add(acc);
      await loadTree(acc);
    }
    expanded.value = new Set(expanded.value);
  }
  navigateTo(last);
});

const fileInput = ref<HTMLInputElement | null>(null);
function triggerUpload() {
  if (!guardWrite()) return;
  fileInput.value?.click();
}
</script>

<template>
  <section class="panel file-manager">
    <header class="fm-topbar">
      <div class="fm-title">
        <h2>{{ t("文件管理器") }}</h2>
        <span class="fm-badge" :class="writable ? 'rw' : 'ro'">
          <Lock v-if="!writable" :size="12" />
          {{ writable ? t("可写") : t("只读") }}
        </span>
        <span class="fm-root-hint">/</span>
      </div>
      <button class="secondary" @click="webdavOpen = true">
        <Settings :size="15" /> {{ t("WebDAV 设置") }}
      </button>
    </header>

    <div class="fm-body">
      <!-- Left: directory tree -->
      <aside class="fm-tree">
        <div class="fm-tree-head">
          <span>{{ t("目录树") }}</span>
          <button class="icon-btn" :title="t('刷新目录树')" @click="refreshTree()">
            <RefreshCw :size="13" />
          </button>
        </div>
        <div class="fm-tree-scroll">
          <div class="fm-tree-root">
            <button class="fm-tree-item fm-root-btn" :class="{ active: isCurrent('') }" @click="goRoot()">
              <Home :size="14" /> <span>{{ t("根目录") }}</span>
            </button>
            <template v-for="entry in visibleTree" :key="entry.node.path">
              <div
                class="fm-tree-item"
                :class="{
                  active: isCurrent(entry.node.path),
                  expanded: expanded.has(entry.node.path),
                  unreadable: entry.node.readable === false,
                }"
                :style="{ paddingLeft: 8 + entry.depth * 16 + 'px' }"
              >
                <button
                  v-if="entry.node.has_children"
                  class="fm-tree-toggle"
                  :disabled="treeLoading === entry.node.path"
                  @click.stop="toggleExpand(entry.node.path)"
                >
                  <ChevronRight :size="13" />
                </button>
                <span v-else class="fm-tree-toggle-spacer" />
                <button
                  class="fm-tree-label"
                  :class="{ active: isCurrent(entry.node.path) }"
                  :title="entry.node.readable === false ? t('该目录不可读') : entry.node.name"
                  @click="selectTree(entry.node.path)"
                >
                  <Lock v-if="entry.node.readable === false" :size="13" />
                  <component v-else :is="expanded.has(entry.node.path) ? FolderOpen : Folder" :size="14" />
                  <span class="fm-tree-name">{{ entry.node.name }}</span>
                </button>
              </div>
            </template>
          </div>
        </div>
      </aside>

      <!-- Right: file list -->
      <div class="fm-main">
        <div class="fm-toolbar">
          <div class="fm-nav-group">
            <button
              class="icon-btn"
              :title="t('返回上级')"
              :disabled="currentPath === '' && parentPath === ''"
              @click="goUp()"
            >
              <ArrowUp :size="16" />
            </button>
            <button class="icon-btn" :title="t('刷新')" :disabled="loading" @click="navigateTo(currentPath)">
              <RefreshCw :size="15" :class="{ spin: loading }" />
            </button>
            <nav class="fm-breadcrumbs" aria-label="breadcrumb">
              <button
                v-for="(c, i) in breadcrumbs()"
                :key="c.path + i"
                class="fm-crumb"
                :class="{ current: i === breadcrumbs().length - 1 }"
                @click="selectTree(c.path)"
              >
                {{ c.name }}
              </button>
            </nav>
          </div>

          <input
            class="fm-path-input"
            v-model="pathInput"
            :placeholder="t('输入路径后回车')"
            @keyup.enter="submitPath"
          />

          <div v-if="writable" class="fm-action-group">
            <button class="primary" :disabled="loading" @click="triggerUpload()">
              <Upload :size="15" /> {{ t("上传") }}
            </button>
            <button class="secondary" :disabled="loading" @click="newFolder()">
              <FolderPlus :size="15" /> {{ t("新建文件夹") }}
            </button>
            <button class="secondary" :disabled="loading" @click="newFile()">
              <FilePlus :size="15" /> {{ t("新建文件") }}
            </button>
          </div>
          <span v-else class="fm-readonly-note">
            <Lock :size="13" /> {{ t("只读模式") }}
          </span>

          <input
            ref="fileInput"
            type="file"
            multiple
            hidden
            @change="uploadFiles(($event.target as HTMLInputElement).files)"
          />
        </div>

        <div v-if="errorMsg" class="fm-error">
          <span>{{ errorMsg }}</span>
          <button class="fm-error-close" :title="t('关闭')" @click="errorMsg = ''">✕</button>
        </div>
        <div v-if="truncated" class="fm-truncated">
          {{ t("目录内容过多，仅显示前一部分。") }}
        </div>

        <div class="fm-list-wrap">
          <table class="fm-table">
            <thead>
              <tr>
                <th class="fm-col-name">{{ t("名称") }}</th>
                <th class="fm-col-type">{{ t("类型") }}</th>
                <th class="fm-col-size">{{ t("大小") }}</th>
                <th class="fm-col-mod">{{ t("修改时间") }}</th>
                <th class="fm-col-actions">{{ t("操作") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="loading && items.length === 0">
                <td colspan="5" class="fm-empty">{{ t("读取中…") }}</td>
              </tr>
              <tr v-else-if="items.length === 0">
                <td colspan="5" class="fm-empty">
                  <div class="fm-empty-state">
                    <Folder :size="26" />
                    <span>{{ t("此目录为空") }}</span>
                  </div>
                </td>
              </tr>
              <tr v-for="item in items" :key="item.path" class="fm-row" @dblclick="openItem(item)">
                <td class="fm-col-name">
                  <button class="fm-name-btn" @click="openItem(item)">
                    <component :is="item.is_dir ? Folder : FileIcon" :size="15" />
                    <span>{{ item.name }}</span>
                  </button>
                </td>
                <td class="fm-col-type">{{ fileTypeLabel(item) }}</td>
                <td class="fm-col-size">{{ item.is_dir ? "—" : formatBytes(item.size) }}</td>
                <td class="fm-col-mod">{{ formatDate(item.modified) }}</td>
                <td class="fm-col-actions">
                  <button v-if="!item.is_dir" class="icon-btn" :title="t('预览')" @click="openItem(item)">
                    <Pencil :size="15" />
                  </button>
                  <button v-if="!item.is_dir" class="icon-btn" :title="t('下载')" @click="download(item)">
                    <Download :size="15" />
                  </button>
                  <template v-if="writable">
                    <button class="icon-btn" :title="t('重命名')" @click="rename(item)">
                      <FilePlus :size="15" />
                    </button>
                    <button class="icon-btn danger" :title="t('删除')" @click="remove(item)">
                      <Trash2 :size="15" />
                    </button>
                  </template>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- Text editor / preview -->
    <div v-if="editorOpen" class="fm-modal-mask" @click.self="editorOpen = false">
      <div class="fm-modal">
        <header class="fm-modal-head">
          <strong>{{ editorName }}</strong>
          <span class="fm-modal-path">{{ editorPath }}</span>
          <span class="fm-spacer" />
          <span v-if="!writable" class="fm-badge ro"><Lock :size="12" /> {{ t("只读") }}</span>
          <button class="icon-btn" :title="t('关闭')" @click="editorOpen = false">✕</button>
        </header>
        <textarea class="fm-editor" v-model="editorContent" spellcheck="false" :readonly="!writable"></textarea>
        <footer class="fm-modal-foot">
          <button v-if="writable" class="primary" :disabled="editorSaving" @click="saveEditor()">
            {{ editorSaving ? t("保存中…") : t("保存") }}
          </button>
          <button class="secondary" @click="editorOpen = false">{{ writable ? t("取消") : t("关闭") }}</button>
        </footer>
      </div>
    </div>

    <WebDAVSettingsModal v-if="webdavOpen" @close="webdavOpen = false" />
  </section>
</template>

<style src="../file-manager.css"></style>
