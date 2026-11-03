<script setup lang="ts">
import { ref } from "vue";
import { t } from "../i18n";
import { httpMethods, type SitePolicy } from "../policy";

// Policy editors expose browser, IP and request controls. Authentication and
// upstream headers use separate site-owned variants. Raw textarea input stays
// local while typing, so partially entered lines are not discarded.
const props = withDefaults(
  defineProps<{
    modelValue: SitePolicy;
    variant?: "defaults" | "site" | "local" | "upstream";
    password?: string;
    editing?: boolean;
  }>(),
  { variant: "site", password: "", editing: false },
);

const emit = defineEmits<{
  (e: "update:modelValue", v: SitePolicy): void;
  (e: "update:password", v: string): void;
}>();

function serialize(headers: Record<string, string>): string {
  return Object.entries(headers || {})
    .map(([key, value]) => `${key}: ${value}`)
    .join("\n");
}
function parseHeaders(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const index = line.indexOf(":");
    if (index <= 0) continue;
    const name = line.slice(0, index).trim();
    const value = line.slice(index + 1).trim();
    if (name) out[name] = value;
  }
  return out;
}
const toLines = (text: string) =>
  text
    .split("\n")
    .map((item) => item.trim())
    .filter(Boolean);

const headersText = ref(serialize(props.modelValue.custom_headers));
const allowText = ref((props.modelValue.ip_allowlist || []).join("\n"));
const denyText = ref((props.modelValue.ip_denylist || []).join("\n"));

function patch(partial: Partial<SitePolicy>) {
  emit("update:modelValue", { ...props.modelValue, ...partial });
}
function emitLists() {
  if (props.variant === "upstream") patch({custom_headers: parseHeaders(headersText.value)});
  else patch({ip_allowlist: toLines(allowText.value), ip_denylist: toLines(denyText.value)});
}
function toggleMethod(method: string, checked: boolean) {
  const set = new Set(props.modelValue.allowed_methods || []);
  if (checked) set.add(method);
  else set.delete(method);
  patch({ allowed_methods: [...set] });
}
</script>

<template>
  <div v-if="variant === 'defaults' || variant === 'site'" class="policy-group">
    <div class="policy-group-head">
      <strong>{{ t("浏览器防护") }}</strong>
    </div>
    <label class="enabled-label"
      ><input
        type="checkbox"
        :checked="modelValue.enabled"
        @change="patch({ enabled: ($event.target as HTMLInputElement).checked })"
      />{{ t("启用安全响应头") }}</label
    >
    <template v-if="modelValue.enabled">
      <label
        >X-Frame-Options<select
          :value="modelValue.frame_options"
          @change="
            patch({
              frame_options: ($event.target as HTMLSelectElement).value,
            })
          "
        >
          <option value="">{{ t("不设置") }}</option>
          <option value="SAMEORIGIN">SAMEORIGIN</option>
          <option value="DENY">DENY</option>
        </select></label
      >
      <label
        >Referrer-Policy<select
          :value="modelValue.referrer_policy"
          @change="
            patch({
              referrer_policy: ($event.target as HTMLSelectElement).value,
            })
          "
        >
          <option value="">{{ t("不设置") }}</option>
          <option value="strict-origin-when-cross-origin">
            strict-origin-when-cross-origin
          </option>
          <option value="same-origin">same-origin</option>
          <option value="no-referrer">no-referrer</option>
        </select></label
      >
      <div class="static-switches">
        <label class="static-switch" :title="t('阻止浏览器猜测文件类型')"
          ><input
            type="checkbox"
            :checked="modelValue.nosniff"
            @change="patch({ nosniff: ($event.target as HTMLInputElement).checked })"
          /><span>nosniff</span></label
        ><label class="static-switch" :title="t('强制浏览器后续只用 HTTPS 访问')"
          ><input
            type="checkbox"
            :checked="modelValue.hsts"
            @change="patch({ hsts: ($event.target as HTMLInputElement).checked })"
          /><span>HSTS</span></label
        >
      </div>
      <label
        >{{ t("Content-Security-Policy（选填）")
        }}<input
          :value="modelValue.csp"
          placeholder="default-src 'self'"
          autocapitalize="none"
          spellcheck="false"
          @input="patch({ csp: ($event.target as HTMLInputElement).value })"
      /></label>
    </template>
    <label class="enabled-label"
      ><input
        type="checkbox"
        :checked="modelValue.hide_server"
        @change="
          patch({ hide_server: ($event.target as HTMLInputElement).checked })
        "
      />{{ t("隐藏 Server / X-Powered-By 响应头") }}</label
    >
  </div>

  <div v-if="variant === 'local'" class="policy-group">
    <strong class="policy-group-title">{{ t("访问认证") }}</strong>
    <label class="enabled-label"
      ><input
        type="checkbox"
        :checked="modelValue.basic_auth_enabled"
        @change="
          patch({
            basic_auth_enabled: ($event.target as HTMLInputElement).checked,
          })
        "
      />{{ t("启用 Basic Auth") }}</label
    ><template v-if="modelValue.basic_auth_enabled"
      ><label
        >{{ t("Basic Auth 用户名")
        }}<input
          :value="modelValue.basic_auth_user"
          autocomplete="off"
          required
          @input="
            patch({ basic_auth_user: ($event.target as HTMLInputElement).value })
          " /></label
      ><label
        >{{
          editing
            ? t("替换密码（留空保留原密码）")
            : t("Basic Auth 密码（至少 8 位）")
        }}<input
          :value="password"
          type="text"
          class="credential-input"
          autocomplete="off"
          autocapitalize="none"
          spellcheck="false"
          data-lpignore="true"
          data-1p-ignore="true"
          :required="!editing"
          minlength="8"
          @input="
            emit('update:password', ($event.target as HTMLInputElement).value)
          " /></label
      ></template
    >
  </div>

  <div v-if="variant === 'upstream'" class="policy-group">
    <label
      >{{ t("上游请求头")
      }}<textarea
        v-model="headersText"
        rows="3"
        placeholder="X-Environment: production"
        @input="emitLists"
      /><small>{{ t("每行一条，格式为“名称: 值”。") }}</small></label
    >  </div>
  <div v-if="variant === 'defaults' || variant === 'site'" class="policy-group">
    <strong class="policy-group-title">{{ t("访问限制") }}</strong>
    <label      >{{ t("IP 白名单（每行一个 IP 或 CIDR）")
      }}<textarea
        v-model="allowText"
        rows="2"
        placeholder="192.168.1.0/24"
        @input="emitLists"
      /></label
    ><label
      >{{ t("IP 黑名单（每行一个 IP 或 CIDR）")
      }}<textarea
        v-model="denyText"
        rows="2"
        placeholder="203.0.113.10"
        @input="emitLists"
      />
    </label>
  </div>
  <div v-if="variant === 'defaults' || variant === 'site'" class="policy-group">
    <strong class="policy-group-title">{{t('请求限制')}}</strong>
    <div class="rate-limit-fields">
      <label
        >{{ t("速率限制请求数")
        }}<input
          type="number"
          min="0"
          max="1000000"
          :value="modelValue.rate_limit_requests"
          @input="
            patch({
              rate_limit_requests:
                Number(($event.target as HTMLInputElement).value) || 0,
            })
          "
        /><small>{{ t("设为 0 表示关闭。") }}</small></label
      ><label
        >{{ t("时间窗口")
        }}<select
          :value="modelValue.rate_limit_window"
          @change="
            patch({
              rate_limit_window: ($event.target as HTMLSelectElement).value,
            })
          "
        >
          <option value="1s">{{ t("1 秒") }}</option>
          <option value="10s">{{ t("10 秒") }}</option>
          <option value="1m">{{ t("1 分钟") }}</option>
          <option value="1h">{{ t("1 小时") }}</option>
        </select></label
      >
    </div>
    <label
      >{{ t("请求体上限（字节，0 为不限）")
      }}<input
        type="number"
        min="0"
        step="1024"
        :value="modelValue.max_body_bytes"
        @input="
          patch({
            max_body_bytes: Number(($event.target as HTMLInputElement).value) || 0,
          })
        "
      /><small>{{ t("超过限制的请求会被拒绝，用于防止大包攻击。") }}</small></label
    >
    <div class="method-chips">
      <span>{{ t("允许的 HTTP 方法") }}</span>
      <label
        v-for="method in httpMethods"
        :key="method"
        class="method-chip"
        :class="{ on: modelValue.allowed_methods.includes(method) }"
        ><input
          type="checkbox"
          :value="method"
          :checked="modelValue.allowed_methods.includes(method)"
          @change="
            toggleMethod(method, ($event.target as HTMLInputElement).checked)
          "
        />{{ method }}</label
      >
    </div>
    <small class="method-hint">{{
      t("不勾选表示不限制；勾选后其余方法返回 405。")
    }}</small>

  </div>
</template>
