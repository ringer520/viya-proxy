<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  FolderOpen, RefreshCw, Settings, Copy, Check, FolderUp, ShieldCheck,
  ShieldAlert, Lock, Unlock, PenLine, Eye, ExternalLink, Terminal, HardDrive,
} from "@lucide/vue";
import { t } from "../i18n";
import { openSiteFiles } from "../composables/useHosts";
import {
  fileShares, fileSharesLoading, fileSharesError, refreshFileShares,
} from "../composables/useFileShares";
import {
  shareAddress, mountCommands, shareWarnings,
} from "./file-shares/types";
import type { FileShare } from "./file-shares/types";
import WebDAVSettingsModal from "./WebDAVSettingsModal.vue";

const webdavOpen = ref(false);
const copied = ref("");
const expanded = ref<number | null>(null);

async function copy(text: string, key: string) {
  try {
    await navigator.clipboard.writeText(text);
    copied.value = key;
    window.setTimeout(() => {
      if (copied.value === key) copied.value = "";
    }, 1600);
  } catch {
    copied.value = "";
  }
}

function manage(share: FileShare) {
  const host = {
    id: share.id,
    domain: share.domain,
    type: share.type,
    root_path: share.root_path,
    tls_mode: share.tls_mode,
  };
  openSiteFiles(host);
}

function toggleCommands(id: number) {
  expanded.value = expanded.value === id ? null : id;
}

function closeWebDAV() {
  webdavOpen.value = false;
  void refreshFileShares();
}

onMounted(() => void refreshFileShares());
</script>

<template>
  <section class="file-hub">
    <header class="hub-head">
      <div class="hub-head-title">
        <FolderOpen :size="20" />
        <div>
          <h2>{{ t("文件与共享") }}</h2>
          <p>{{ t("集中查看站点文件与 WebDAV 共享目录，显示每项的访问范围和认证状态。") }}</p>
        </div>
      </div>
      <div class="hub-head-actions">
        <button class="secondary" :disabled="fileSharesLoading" @click="refreshFileShares()">
          <RefreshCw :size="15" /> {{ t("刷新") }}
        </button>
        <button class="primary" @click="webdavOpen = true">
          <Settings :size="15" /> {{ t("WebDAV 设置") }}
        </button>
      </div>
    </header>

    <p v-if="fileSharesError" class="hub-alert">{{ t("共享列表加载失败，请刷新重试。") }}</p>

    <div v-if="fileSharesLoading && !fileShares.length" class="hub-empty">
      <RefreshCw :size="22" class="hub-spin" /> {{ t("正在加载共享列表…") }}
    </div>

    <div v-else-if="!fileShares.length" class="hub-empty">
      <HardDrive :size="26" />
      <strong>{{ t("还没有共享目录") }}</strong>
      <span>{{ t("创建静态站点或文件服务站点后，它们会出现在这里。") }}</span>
    </div>

    <div v-else class="hub-grid">
      <article v-for="share in fileShares" :key="share.id" class="share-card" :class="{ 'is-disabled': !share.enabled }">
        <div class="share-card-head">
          <div class="share-domain">
            <FolderOpen :size="17" />
            <strong>{{ share.domain || share.name }}</strong>
          </div>
          <span class="share-type" :class="share.type">{{ share.type === "files" ? t("共享目录") : t("静态站点") }}</span>
        </div>

        <div class="share-path">
          <span>{{ t("目录") }}</span>
          <code :title="share.root_path">{{ share.root_path || t("未设置") }}</code>
        </div>

        <div class="share-badges">
          <span class="share-badge" :class="share.enabled ? 'ok' : 'off'">
            {{ share.enabled ? t("已启用") : t("已停用") }}
          </span>
          <span v-if="share.auth_enabled" class="share-badge ok">
            <Lock :size="12" /> {{ t("需认证") }}
          </span>
          <span v-else class="share-badge warn">
            <Unlock :size="12" /> {{ t("匿名可访问") }}
          </span>
          <span v-if="share.webdav" class="share-badge" :class="share.webdav_write ? 'warn' : 'ok'">
            <PenLine :size="12" /> {{ share.webdav_write ? t("可读可写") : t("只读") }}
          </span>
          <span v-if="share.browse" class="share-badge">
            <Eye :size="12" /> {{ t("目录浏览") }}
          </span>
        </div>

        <p v-if="shareWarnings(share).includes('write-no-auth')" class="share-warning">
          <ShieldAlert :size="14" /> {{ t("已允许写入但未开启认证，请立即配置账号密码。") }}
        </p>
        <p v-else-if="shareWarnings(share).includes('no-auth')" class="share-warning">
          <ShieldAlert :size="14" /> {{ t("此共享未开启访问认证，任何知道地址的人都能访问。") }}
        </p>
        <p v-else-if="share.auth_enabled" class="share-ok">
          <ShieldCheck :size="14" /> {{ t("访问受账号保护：") }}{{ share.auth_user }}
        </p>

        <div v-if="share.webdav && share.url" class="share-address">
          <code :title="shareAddress(share)">{{ shareAddress(share) }}</code>
          <button class="icon-btn" :title="t('复制地址')" @click="copy(shareAddress(share), 'addr-' + share.id)">
            <Check v-if="copied === 'addr-' + share.id" :size="14" />
            <Copy v-else :size="14" />
          </button>
        </div>
        <p v-else-if="share.webdav" class="share-muted">{{ t("监听信息不可用，暂时无法生成访问地址。") }}</p>

        <div class="share-actions">
          <button class="secondary" @click="manage(share)">
            <FolderUp :size="14" /> {{ t("管理文件") }}
          </button>
          <button v-if="share.webdav" class="secondary" @click="toggleCommands(share.id)">
            <Terminal :size="14" /> {{ expanded === share.id ? t("收起挂载命令") : t("挂载命令") }}
          </button>
          <a v-if="share.url" class="secondary" :href="share.url" target="_blank" rel="noopener noreferrer">
            <ExternalLink :size="14" /> {{ t("打开") }}
          </a>
        </div>

        <div v-if="expanded === share.id && share.webdav" class="mount-list">
          <div v-for="cmd in mountCommands(share)" :key="cmd.id" class="mount-row">
            <span class="mount-label">{{ cmd.label }}</span>
            <code>{{ cmd.command }}</code>
            <button class="icon-btn" :title="t('复制命令')" @click="copy(cmd.command, cmd.id + '-' + share.id)">
              <Check v-if="copied === cmd.id + '-' + share.id" :size="14" />
              <Copy v-else :size="14" />
            </button>
          </div>
          <p class="mount-note">{{ t("命令中的账号为示例，请按实际 WebDAV 账号替换。") }}</p>
        </div>
      </article>
    </div>

    <section class="hub-guide">
      <h3>{{ t("管理整台服务器的文件") }}</h3>
      <p>{{ t("面板不再提供服务器文件管理器。共享目录之外的文件，请用更合适的工具访问：") }}</p>
      <div class="guide-list">
        <div class="guide-item">
          <strong>SFTP / WinSCP / VS Code Remote</strong>
          <span>{{ t("通过 SSH 管理整机文件，权限与审计都由系统控制。") }}</span>
        </div>
        <div class="guide-item">
          <strong>WebDAV</strong>
          <span>{{ t("把需要的目录发布成共享，用系统自带的网络驱动器挂载。") }}</span>
        </div>
        <div class="guide-item">
          <strong>filebrowser</strong>
          <span>{{ t("确需网页版文件管理器时，用独立容器限定挂载范围，避免扩大面板权限。") }}</span>
        </div>
      </div>
      <code class="guide-command">docker run -d --name files -v /srv/data:/srv -p 8182:80 filebrowser/filebrowser</code>
    </section>

    <WebDAVSettingsModal v-if="webdavOpen" @close="closeWebDAV" />
  </section>
</template>

<style src="./file-hub.css"></style>
