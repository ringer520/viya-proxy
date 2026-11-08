<script setup lang="ts">
import ResourceUsage from "./ResourceUsage.vue";
import { Copy, Pencil, Plus, ShieldCheck, Trash2 } from "@lucide/vue";
import { t } from "../i18n";
import { deleteBusy, hasListFilter } from "../composables/useState";

import { automaticCertificates, certError, certForm, openManagedCert, removeAutomaticCert, removeCert, removeManagedCert, showCert, visibleCerts, visibleManagedCerts } from "../composables/useCertificates";
</script>

<template>
      <section

        class="panel managed-cert-panel"
      >
        <div class="panel-head">
          <div>
            <h2>{{ t("申请与自动续期") }}</h2>
            <p>
              {{
                t("申请记录独立管理；相同域名的站点选择“自动 HTTPS”即可使用。")
              }}
            </p>
          </div>
        </div>
        <div v-if="visibleManagedCerts.length" class="host-list">
          <div
            v-for="cert in visibleManagedCerts"
            :key="cert.id"
            class="host-row cert-host-row"
          >
            <div class="host-icon"><ShieldCheck :size="18" /></div>
            <div class="host-main">
              <strong>{{ cert.name }} · {{ cert.domain }}</strong
              ><span
                >{{
                  cert.method === "internal"
                    ? t("内部 CA")
                    : cert.method.toUpperCase()
                }}
                · {{ cert.issuer || t("等待验证和签发") }} ·
                {{
                  cert.not_after
                    ? t("到期 ") + new Date(cert.not_after).toLocaleString()
                    : t("签发完成后显示有效期")
                }}</span
              >
              <div
                v-if="cert.status !== 'issued' && cert.status !== 'expired'"
                class="cert-progress"
              >
                <div class="cert-progress-heading">
                  <span>{{
                    cert.progress_message || t("等待 Caddy 状态")
                  }}</span
                  ><strong>{{ cert.progress || 0 }}%</strong>
                </div>
                <div
                  class="cert-progress-track"
                  role="progressbar"
                  :aria-valuenow="cert.progress || 0"
                  aria-valuemin="0"
                  aria-valuemax="100"
                >
                  <i
                    :class="{ retrying: cert.status === 'retrying' }"
                    :style="{ width: Math.max(4, cert.progress || 0) + '%' }"
                  />
                </div>
                <small v-if="cert.last_error">{{ cert.last_error }}</small>
              </div>
              <ResourceUsage kind="certificate" :domain="cert.domain"/>
            </div>
            <span
              class="badge"
              :class="{ disabled: cert.status !== 'issued' }"
              >{{
                cert.status === "issued"
                  ? t("已签发 · 自动续期")
                  : cert.status === "expired"
                    ? t("已到期")
                    : cert.status === "validating"
                      ? t("域名验证中")
                      : cert.status === "obtaining"
                        ? t("正在申请")
                        : cert.status === "retrying"
                          ? t("自动重试中")
                          : cert.status === "config-error"
                            ? t("待重新应用")
                            : t("准备签发")
              }}</span
            ><button
              class="icon-btn"
              :title="t('编辑证书')"
              :aria-label="t('编辑证书')"
              @click="openManagedCert(cert)"
            >
              <Pencil :size="16" /></button
            ><button
              class="icon-btn"
              :title="t('克隆证书')"
              :aria-label="t('克隆证书')"
              @click="openManagedCert(cert, true)"
            >
              <Copy :size="16" /></button
            ><button
              class="icon-btn danger"
              :title="t('删除证书')"
              :aria-label="t('删除证书')"
              @click="removeManagedCert(cert)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="empty-state">
          {{
            hasListFilter
              ? t("未找到匹配的托管证书。")
              : t("尚未申请证书，可从右上角申请证书。")
          }}
        </div>
        <p class="certificate-note">
          {{
            t(
              "申请期间实时显示 Caddy 的验证与签发进度；签发后自动读取有效期并显示续期状态。DNS 传播或验证失败时，Caddy 会在后台重试。",
            )
          }}
        </p>
      </section>
      <section

        class="panel automatic-certificates"
      >
        <div class="panel-head">
          <div>
            <h2>{{ t("站点自动证书") }}</h2>
            <p>
              {{
                t(
                  "未匹配泛域名证书的 HTTPS 站点由 Caddy 单独申请并续期，自动同步到此列表。",
                )
              }}
            </p>
          </div>
          <span class="badge"
            >{{ automaticCertificates.length }} {{ t("张") }}</span
          >
        </div>
        <div v-if="automaticCertificates.length" class="host-list">
          <div
            v-for="cert in automaticCertificates"
            :key="cert.domain"
            class="host-row"
          >
            <div class="host-icon"><ShieldCheck :size="18" /></div>
            <div class="host-main">
              <strong>{{ cert.domain }}</strong
              ><span
                >{{ t(cert.source.label) }} ·
                {{
                  cert.source.not_after
                    ? t("到期 ") +
                      new Date(cert.source.not_after).toLocaleString()
                    : cert.source.status === "disabled"
                      ? t("站点停用，启用后自动申请")
                      : t("等待引擎签发，查看申请进度或引擎日志")
                }}</span
              >
              <ResourceUsage kind="certificate" :domain="cert.domain"/>
            </div>
            <span class="certificate-origin individual">{{
              cert.source.kind === "internal"
                ? t("内部证书")
                : t("单独自动申请")
            }}</span
            ><span
              class="badge"
              :class="{ disabled: cert.source.status !== 'issued' }"
              >{{
                cert.source.status === "issued"
                  ? t("已签发")
                  : cert.source.status === "expired"
                    ? t("已过期")
                    : cert.source.status === "disabled"
                      ? t("站点已停用")
                      : cert.source.status === "pending"
                        ? t("等待签发")
                        : t("申请处理中")
              }}</span
            ><a class="secondary" href="#proxy-sites">{{
              t("管理关联站点")
            }}</a
            ><button
              class="icon-btn danger"
              :title="t('删除证书和申请')"
              :aria-label="t('删除证书和申请')"
              :disabled="deleteBusy"
              @click="removeAutomaticCert(cert)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="empty-state">
          {{
            t(
              "暂无独立申请的站点证书；匹配到的泛域名证书可在 Web 站点查看复用标记。",
            )
          }}
        </div>
      </section>
      <section

        id="certificates"
        class="panel provider-panel"
      >
        <div class="panel-head">
          <div>
            <h2>{{ t("自定义 TLS 证书") }}</h2>
            <p>
              {{
                t("PEM 证书和匹配的私钥保存在受限权限目录，可绑定到代理站点。")
              }}
            </p>
          </div>
          <button
            class="secondary"
            @click="
              certError = '';
              certForm.name = '';
              showCert = true;
            "
          >
            <Plus :size="15" />{{ t("上传证书") }}
          </button>
        </div>
        <div v-if="visibleCerts.length" class="host-list">
          <div v-for="cert in visibleCerts" :key="cert.id" class="host-row">
            <div class="host-icon"><ShieldCheck :size="17" /></div>
            <div class="host-main">
              <strong>{{ cert.name }}</strong
              ><span
                >{{ cert.dns_names.join(", ") }} {{ t("· 到期") }}
                {{ new Date(cert.not_after).toLocaleDateString() }}</span
              >
              <ResourceUsage kind="certificate" :id="cert.id"/>
            </div>
            <button
              class="icon-btn danger"
              :title="t('删除证书')"
              @click="removeCert(cert)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div v-else class="empty-state">
          {{
            hasListFilter
              ? t("未找到匹配项目，可清除筛选后重试。")
              : t("尚未上传自定义证书")
          }}
        </div>
      </section>
</template>
