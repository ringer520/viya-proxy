<script setup lang="ts">
import { AlertTriangle, FolderUp, ChevronDown, Globe2 } from "@lucide/vue";
import { t } from "../i18n";
import {
  advancedOpen,
  advancedSummary,
  composedDomain,
  domainMode,
  domainParent,
  domainPrefix,
  editingID,
  form,
  openSiteFilesForForm,
  partialSave,
  save,
  setDomainMode,
  show,
  siteFormPolicy,
  siteIconColors,
  siteIconStyle,
  siteRootDomains,
  siteTypeHint,
  siteTypeOptions,
  testUpstream,
  upstreamCheck,
  upstreamTesting,
} from "../composables/useHosts";
import { openProvider } from "../composables/useNetwork";
import { certError, certForm, showCert } from "../composables/useCertificates";
import { applyRetryVisible, certificates, error, providers, saving } from "../composables/useState";

import { retryApply } from "../composables/useApply";
import SitePolicyFields from "./SitePolicyFields.vue";
import SiteSecurityPolicy from "./SiteSecurityPolicy.vue";
import { ref, watch } from "vue";
const iconPickerOpen = ref(false);
watch(show, (open) => {
  if (!open) {
    error.value = "";
    iconPickerOpen.value = false;
  }
});

function closeIconPickerOnOutsideClick(event: MouseEvent) {
  if (iconPickerOpen.value && !(event.target as Element).closest(".site-icon-picker")) {
    iconPickerOpen.value = false;
  }
}

</script>

<template>
  <div v-if="show" class="overlay" v-dialog>
    <form class="modal proxy-modal" @submit.prevent="save" @click="closeIconPickerOnOutsideClick">
      <div class="modal-top">
        <div>
          <span class="eyebrow">{{
            editingID === null ? t("新建反代") : t("修改反代")
          }}</span>
          <h2>
            {{ editingID === null ? t("添加代理站点") : t("编辑代理站点") }}
          </h2>
        </div>
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('关闭')"
          :disabled="saving"
          @click="show = false"
        >
          ×
        </button>
      </div>
      <div v-if="error" class="form-error-banner" role="alert">
        <AlertTriangle :size="18" />
        <span>{{ error }}</span>
      </div>
      <p class="form-intro">
        {{
          t(
            "填写名称、域名与类型，再按需配置上游和证书；IP 规则、限速与安全响应头位于“访问控制与安全”。",
          )
        }}
      </p>
      <h3 class="form-section-title">{{ t("基本信息") }}</h3>
      <div class="site-name-entry">
        <div class="site-name-label-row">
          <div class="site-icon-picker" @keydown.esc.stop="iconPickerOpen = false">
            <button
              type="button"
              class="site-icon-trigger"
              :style="siteIconStyle(form.icon_color)"
              :aria-label="t('选择站点图标颜色')"
              :aria-expanded="iconPickerOpen"
              aria-haspopup="listbox"
              :title="t('选择站点图标颜色')"
              @click="iconPickerOpen = !iconPickerOpen"
            >
              <Globe2 :size="19" aria-hidden="true" />
              <ChevronDown :size="12" aria-hidden="true" />
            </button>
            <div v-if="iconPickerOpen" class="site-icon-menu" role="listbox" :aria-label="t('站点图标颜色')">
              <button
                v-for="color in siteIconColors"
                :key="color.key"
                type="button"
                role="option"
                class="site-icon-option"
                :style="siteIconStyle(color.key)"
                :aria-label="t(color.label)"
                :aria-selected="form.icon_color === color.key"
                :title="t(color.label)"
                @click="form.icon_color = color.key; iconPickerOpen = false"
              ><Globe2 :size="19" aria-hidden="true" /></button>
            </div>
          </div>
          <label for="site-display-name">{{ t("站点名称（选填）") }}</label>
        </div>
        <div class="site-name-control">
          <input
            id="site-display-name"
            v-model="form.name"
            maxlength="100"
            :placeholder="t('为站点起一个便于识别的名称')"
          />
        </div>
        <small>{{ t("仅用于面板中识别，不影响访问域名。") }}</small>
      </div>
      <h3 class="form-section-title">{{ t("域名与类型") }}</h3>
      <div v-if="editingID === null" class="domain-entry-mode">
        <button
          type="button"
          :class="{ selected: domainMode === 'subdomain' }"
          :disabled="!siteRootDomains.length"
          @click="setDomainMode('subdomain')"
        >
          {{ t("添加到已有主域名") }}</button
        ><button
          type="button"
          :class="{ selected: domainMode === 'full' }"
          @click="setDomainMode('full')"
        >
          {{ t("填写完整域名") }}
        </button>
      </div>
      <template v-if="editingID === null && domainMode === 'subdomain'"
        ><label
          >{{ t("主域名")
          }}<select v-model="domainParent" required>
            <option value="" disabled>{{ t("选择已有主域名") }}</option>
            <option
              v-for="domain in siteRootDomains"
              :key="domain"
              :value="domain"
            >
              {{ domain }}
            </option>
          </select></label
        ><label
          >{{ t("子域名前缀")
          }}<input
            v-model="domainPrefix"
            placeholder="qb"
            autocomplete="off"
            autocapitalize="none"
            spellcheck="false"
            required
          /><small
            >{{ t("只需填写前缀，例如 qb；最终访问域名为") }}
            <code>{{
              composedDomain || "qb." + (domainParent || t("主域名"))
            }}</code
            >。</small
          ></label
        ></template
      >
      <label v-else
        >{{ t("前端域名")
        }}<input
          v-model="form.domain"
          placeholder="app.example.com"
          autocomplete="off"
          autocapitalize="none"
          spellcheck="false"
          required
        /><small>{{
          t("填写完整域名，不包含 http:// 或路径。")
        }}</small></label
      >
      <div class="site-type-setting">
        <span class="field-label">{{ t("站点类型") }}</span>
        <div
          class="site-type-picker"
          role="radiogroup"
          :aria-label="t('站点类型')"
        >
          <button
            v-for="opt in siteTypeOptions"
            :key="opt.value"
            type="button"
            role="radio"
            :aria-checked="form.type === opt.value"
            :class="{ selected: form.type === opt.value }"
            :style="{ '--tcolor': opt.color }"
            @click="form.type = opt.value"
          >
            <span class="site-type-icon"
              ><component :is="opt.icon" :size="17"
            /></span>
            <span class="site-type-text">
              <strong>{{ opt.label }}</strong>
              <small>{{ opt.desc }}</small>
            </span>
          </button>
        </div>
        <small class="site-type-hint">{{ siteTypeHint }}</small>
      </div>
      <h3 class="form-section-title">{{ t("路由与上游") }}</h3>
      <template v-if="form.type === 'proxy'">
        <label
          >{{ t("上游地址")
          }}<input
            v-model="form.upstream"
            type="url"
            placeholder="http://192.168.1.20:8080"
            autocomplete="url"
            required
          /><small>{{
            t(
              "填写 http(s):// 地址。端口可省略，HTTP 默认 80，HTTPS 默认 443。",
            )
          }}</small></label
        >
        <div class="probe-row">
          <button
            type="button"
            class="secondary"
            :disabled="upstreamTesting || !form.upstream"
            @click="testUpstream"
          >
            {{ upstreamTesting ? t("正在测试…") : t("测试上游连通性") }}</button
          ><span
            v-if="upstreamCheck"
            :class="upstreamCheck.ok ? 'probe-ok' : 'probe-error'"
            >{{ upstreamCheck.message }}</span
          >
        </div>
        <details class="site-upstream-options"><summary>{{t('上游请求头')}}</summary><SitePolicyFields v-model="siteFormPolicy" variant="upstream"/></details>
      </template>
      <template v-else-if="form.type === 'static'">
        <button
          v-if="editingID !== null"
          type="button"
          class="static-manage"
          @click="openSiteFilesForForm()"
        >
            <span class="static-manage-icon"><FolderUp :size="20" /></span>
            <span class="static-manage-text"><strong>{{ t("管理站点文件") }}</strong><small>{{ t("上传、删除、管理网站文件") }}</small></span>
            <span class="static-manage-cta">{{ t("打开") }} →</span>
        </button>
        <p v-else class="inline-help">{{ t("保存后可在站点卡片里上传网站文件。") }}</p>
        <label
          >{{ t("网站根目录（选填）")
          }}<input
            v-model="form.root_path"
            :placeholder="t('留空自动使用托管目录')"
            autocapitalize="none"
            spellcheck="false"
          /><small>{{
            t(
              "留空自动使用数据目录下的 sites/域名；填写绝对路径可指向服务器上已有的目录（只读服务）。",
            )
          }}</small></label
        >
      </template>
      <template v-else>
        <label
          >{{ t("跳转目标")
          }}<input
            v-model="form.redirect_target"
            type="url"
            placeholder="https://new.example.com"
            required
          /><small>{{
            t("填写 http(s):// 地址，不带查询参数和片段。")
          }}</small></label
        >
        <label
          >{{ t("跳转状态码")
          }}<select v-model.number="form.redirect_status">
            <option :value="301">301 {{ t("永久重定向") }}</option>
            <option :value="302">302 {{ t("临时重定向") }}</option>
            <option :value="307">307 {{ t("临时（保留方法）") }}</option>
            <option :value="308">308 {{ t("永久（保留方法）") }}</option>
          </select></label
        >
        <label class="drawer-check-line"
          ><input v-model="form.redirect_keep_path" type="checkbox" />{{
            t("保留访问路径与查询参数")
          }}</label
        >
      </template>
      <section>
      <label
        >{{ t("访问路径（选填）")
        }}<input
          v-model="form.path"
          :placeholder="t('留空匹配整个域名，例如 /api')"
          autocapitalize="none"
          spellcheck="false"
        /><small>{{
          t("留空时整个域名都转发；填写路径后仅匹配该路径和它下面的子路径。")
        }}</small></label
      >
      <h3 class="form-section-title">{{ t("HTTPS 与证书") }}</h3>
      <label
        >TLS / HTTPS<select v-model="form.tls_mode">
          <option value="auto">{{ t("自动 HTTPS（推荐）") }}</option>
          <option value="http-01">ACME HTTP-01</option>
          <option value="dns-01">{{ t("DNS-01（支持泛域名）") }}</option>
          <option value="internal">{{ t("Caddy 内部 CA") }}</option>
          <option value="custom">{{ t("自定义证书") }}</option>
          <option value="none">{{ t("仅 HTTP") }}</option>
        </select></label
      >
      <p class="inline-help" v-if="form.tls_mode === 'auto'">
        {{
          t(
            "Caddy 自动申请和续期证书。公网域名需解析到此服务器，并确保 HTTP/HTTPS 验证端口可达。",
          )
        }}
      </p>
      <p class="inline-help" v-else-if="form.tls_mode === 'http-01'">
        {{
          t("HTTP-01 需要公网域名和可从互联网访问的 80 端口；不支持泛域名。")
        }}
      </p>
      <p class="inline-help" v-else-if="form.tls_mode === 'dns-01'">
        {{
          t(
            "DNS-01 可签发泛域名证书。先选择已保存的 DNS 凭据，首次使用可在这里添加。",
          )
        }}
      </p>
      <p class="inline-help" v-else-if="form.tls_mode === 'internal'">
        {{ t("使用 Caddy 本地 CA。客户端需手动信任该 CA 根证书。") }}
      </p>
      <p class="inline-help" v-else-if="form.tls_mode === 'custom'">
        {{ t("使用已上传且 SAN 覆盖此域名的证书。") }}
      </p>
      <p class="inline-help" v-else>
        {{ t("此站点只通过 HTTP 提供服务，不会申请 TLS 证书。") }}
      </p>
      <label v-if="form.tls_mode === 'dns-01'"
        >{{ t("DNS 服务商")
        }}<select v-model="form.dns_provider_id" required>
          <option value="" disabled>{{ t("选择已保存的服务商凭据") }}</option>
          <option
            v-for="provider in providers"
            :key="provider.id"
            :value="String(provider.id)"
          >
            {{ provider.name }} · {{ provider.provider_type }}
          </option>
        </select></label
      >
      <div
        v-if="form.tls_mode === 'dns-01' && providers.length === 0"
        class="dependency-help"
      >
        <span>{{ t("还没有 DNS 凭据。添加后会保留当前站点表单。") }}</span
        ><button type="button" class="inline-action" @click="openProvider">
          {{ t("添加 DNS 凭据") }}
        </button>
      </div>
      <label v-if="form.tls_mode === 'custom'"
        >{{ t("自定义证书")
        }}<select v-model="form.custom_certificate_id" required>
          <option value="" disabled>{{ t("选择覆盖此域名的证书") }}</option>
          <option
            v-for="cert in certificates"
            :key="cert.id"
            :value="String(cert.id)"
          >
            {{ cert.name }} · {{ cert.dns_names.join(", ") }}
          </option>
        </select></label
      >
      <div
        v-if="form.tls_mode === 'custom' && certificates.length === 0"
        class="dependency-help"
      >
        <span>{{ t("还没有上传证书。上传后会保留当前站点表单。") }}</span
        ><button
          type="button"
          class="inline-action"
          @click="
            certError = '';
            certForm.name = '';
            showCert = true;
          "
        >
          {{ t("上传自定义证书") }}
        </button>
      </div>
      </section>
      <label class="enabled-label"
        ><input v-model="form.enabled" type="checkbox" />{{
          t("启用此站点")
        }}</label
      >
      <button
        type="button"
        class="advanced-toggle"
        :aria-expanded="advancedOpen"
        @click="advancedOpen = !advancedOpen"
      >
        <span class="advanced-toggle-copy"><strong>{{ t("访问控制与安全") }}</strong><small>{{ advancedSummary }}</small></span>
        <ChevronDown class="advanced-chevron" :class="{ expanded: advancedOpen }" :size="18" aria-hidden="true" />
      </button>
      <div v-if="advancedOpen" class="advanced-fields">
        <SiteSecurityPolicy v-model="siteFormPolicy" :domain="composedDomain"/>
        <SitePolicyFields
          v-model="siteFormPolicy"
          v-model:password="form.basic_auth_password"
          variant="local"
          :editing="editingID !== null"
        />
      </div>
      <button
        v-if="applyRetryVisible"
        type="button"
        class="secondary retry-button"
        :disabled="saving"
        @click="retryApply"
      >
        {{ saving ? t("正在应用…") : t("重新应用到 Caddy") }}
      </button>
      <div class="site-save-summary"><strong>{{t("将发布")}} {{composedDomain||t("请填写域名")}}</strong><small>{{form.type==='static'?t("静态站点"):form.type==='redirect'?form.redirect_target:form.upstream}}</small></div>
      <div class="modal-actions">
        <button type="button" class="secondary" @click="show = false">
          {{ t("取消") }}</button
        ><button
          class="primary"
          :disabled="saving || (partialSave && editingID === null)"
        >
          {{
            saving
              ? t("应用中…")
              : partialSave && editingID !== null
                ? t("修正后重新应用")
                : t("保存并应用")
          }}
        </button>
      </div>
    </form>
  </div>
</template>
