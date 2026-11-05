<script setup lang="ts">
import { t } from "../i18n";
import {
  ddnsEditingID,
  ddnsError,
  ddnsForm,
  ddnsZoneLoading,
  ddnsZoneMessage,
  ddnsZones,
  interfaceLabel,
  loadDDNSZones,
  manualDDNSZone,
  refreshNetworkInterfaces,
  saveDDNS,
  selectedInterfaceAddresses,
  showDDNS,
  openProvider,
} from "../composables/useNetwork";
import { networkInterfaces, providers, saving } from "../composables/useState";

</script>

<template>
  <div v-if="showDDNS" class="overlay" v-dialog>
    <form class="modal" @submit.prevent="saveDDNS">
      <div class="modal-top">
        <div>
          <span class="eyebrow">DDNS</span>
          <h2>
            {{
              ddnsEditingID === null
                ? t("添加 DDNS 任务")
                : t("编辑 DDNS 任务")
            }}
          </h2>
        </div>
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('关闭 DDNS 表单')"
          @click="showDDNS = false"
        >
          ×
        </button>
      </div>
      <label
        >{{ t("任务名称")
        }}<input
          v-model="ddnsForm.name"
          maxlength="100"
          required
          :placeholder="t('家庭宽带 IPv4')" /></label
      ><label
        >{{ t("DNS 服务商")
        }}<select
          v-model="ddnsForm.dns_provider_id"
          required
          @change="loadDDNSZones(true)"
        >
          <option value="" disabled>{{ t("选择凭据") }}</option>
          <option v-for="p in providers" :key="p.id" :value="String(p.id)">
            {{ p.name }} · {{ p.provider_type }}
          </option>
        </select></label
      >
      <div v-if="providers.length === 0" class="dependency-help">
        <span>{{ t("还没有 DNS 凭据。添加后会保留当前 DDNS 表单。") }}</span
        ><button type="button" class="inline-action" @click="openProvider">
          {{ t("添加 DNS 凭据") }}
        </button>
      </div>
      <label v-if="ddnsZones.length && !manualDDNSZone"
        >DNS Zone<select v-model="ddnsForm.zone" required>
          <option value="" disabled>{{ t("请选择服务商中的 Zone") }}</option>
          <option
            v-for="zone in ddnsZones"
            :key="zone.name"
            :value="zone.name"
          >
            {{ zone.name }}{{ zone.status ? ` · ${zone.status}` : "" }}
          </option>
        </select></label
      ><label v-else
        >DNS Zone<input
          v-model="ddnsForm.zone"
          required
          placeholder="example.com"
          autocapitalize="none" /></label
      ><button
        v-if="ddnsZones.length"
        type="button"
        class="inline-action zone-mode-toggle"
        @click="manualDDNSZone = !manualDDNSZone"
      >
        {{ manualDDNSZone ? t("从 Zone 列表选择") : t("手动输入 Zone") }}
      </button>
      <p class="zone-fetch-hint">
        {{ ddnsZoneLoading ? t("正在读取服务商 Zone…") : ddnsZoneMessage }}
      </p>
      <label
        >{{ t("相对记录名")
        }}<input
          v-model="ddnsForm.record_name"
          required
          :placeholder="t('@ 或 home')" /></label
      ><label
        >{{ t("记录类型")
        }}<select v-model="ddnsForm.record_type">
          <option value="A">A (IPv4)</option>
          <option value="AAAA">AAAA (IPv6)</option>
          <option value="CNAME">{{ t("CNAME（别名）") }}</option>
        </select></label
      ><label v-if="ddnsForm.record_type === 'CNAME'"
        >{{ t("CNAME 目标域名")
        }}<input
          v-model="ddnsForm.cname_target"
          required
          placeholder="target.example.net"
          autocapitalize="none"
        /><small>{{
          t("填写目标主机名，不是 IP 地址。任务会按间隔将别名指向此域名。")
        }}</small></label
      ><label v-if="ddnsForm.record_type !== 'CNAME'"
        >{{ t("公网 IP 获取方式")
        }}<select v-model="ddnsForm.ip_source">
          <option value="interface">{{ t("通过网卡获取") }}</option>
          <option value="http">{{ t("通过公网接口查询") }}</option>
        </select></label
      >
      <div
        v-if="
          ddnsForm.record_type !== 'CNAME' &&
          ddnsForm.ip_source === 'interface'
        "
        class="ddns-interface-fields"
      >
        <label
          >{{ t("网络接口")
          }}<select v-model="ddnsForm.interface_name" required>
            <option value="" disabled>{{ t("选择接口") }}</option>
            <option
              v-for="item in networkInterfaces"
              :key="item.name"
              :value="item.name"
            >
              {{ interfaceLabel(item) }}
            </option>
          </select></label
        ><label
          >{{ t("要发布的") }}
          {{ ddnsForm.record_type === "A" ? "IPv4" : "IPv6" }} {{ t("地址")
          }}<select v-model="ddnsForm.interface_ip">
<option value="">{{t("自动选择稳定地址（跟随网卡变化）")}}</option>
<option v-if="ddnsForm.interface_ip&&!selectedInterfaceAddresses.some(a=>a.ip===ddnsForm.interface_ip)" :value="ddnsForm.interface_ip" disabled>{{ddnsForm.interface_ip}} · {{t('原地址已失效，请重新选择')}}</option>
            <option
              v-for="address in selectedInterfaceAddresses"
              :key="address.ip"
              :value="address.ip" :disabled="address.deprecated||address.tentative"
            >
              {{ address.ip }} · {{ address.scope
              }}{{address.deprecated?t(" · 已弃用"):address.tentative?t(" · 尚未就绪"):address.temporary?t(" · 临时隐私地址"):t(" · 稳定地址")}}
            </option></select
          ><small>{{
            t(
              "有多个 IPv6 时请选公网全局地址，并优先选择长期稳定、不会定期变化的地址。固定地址失效时会提示；选择自动模式可跟随网卡地址变化，并优先使用稳定公网地址。",
            )
          }}</small></label
        >
        <div class="interface-current-ip">
          <span>{{ t("网卡当前可用地址") }}</span
          ><button
            type="button"
            class="inline-action"
            @click="refreshNetworkInterfaces"
          >
            {{ t("刷新网卡") }}
          </button>
          <div v-if="selectedInterfaceAddresses.length">
            <code
              v-for="address in selectedInterfaceAddresses"
              :key="address.ip"
              >{{ address.ip }}
              <small
                >{{ address.scope
                }}{{address.deprecated?t(" · 已弃用"):address.tentative?t(" · 尚未就绪"):address.temporary?t(" · 临时隐私地址"):t(" · 稳定地址")}}</small
              ></code
            >
          </div>
          <small v-else class="interface-warning">{{
            t(
              "此接口暂时没有对应 IP 版本的地址。可换一个接口或改用公网接口查询。",
            )
          }}</small>
        </div>
      </div>
      <p v-else-if="ddnsForm.record_type !== 'CNAME'" class="auth-hint">
        {{
          t(
            "通过多个公网 IP 查询接口轮询获取地址，适用于路由器 NAT 或网卡上没有公网地址的情况。",
          )
        }}
      </p>
      <label
        >{{ t("检查间隔（分钟）")
        }}<input
          v-model.number="ddnsForm.interval_minutes"
          type="number"
          min="5"
          max="1440"
          required /></label
      ><label
        >{{ t("TTL 秒数")
        }}<input
          v-model.number="ddnsForm.ttl_seconds"
          type="number"
          min="60"
          max="86400"
          required /></label
      ><label class="enabled-label"
        ><input v-model="ddnsForm.enabled" type="checkbox" />{{
          t("启用并自动更新")
        }}</label
      >
      <p class="auth-hint">
        {{
          ddnsForm.record_type === "CNAME"
            ? t(
                "CNAME 任务会定时把记录指向上方目标域名；CNAME 记录不能与同名的 A/AAAA 等记录同时存在。",
              )
            : t(
                "自动模式跟随网卡变化，优先选择稳定公网地址；固定地址失效时会报错。网卡仅有内网地址时，公网域名建议使用公网接口查询。",
              )
        }}
      </p>
      <p v-if="ddnsError" class="error">{{ ddnsError }}</p>
      <div class="modal-actions">
        <button type="button" class="secondary" @click="showDDNS = false">
          {{ t("取消") }}</button
        ><button
          class="primary"
          :disabled="
            saving ||
            (ddnsForm.record_type !== 'CNAME' &&
              ddnsForm.ip_source === 'interface' &&
              !selectedInterfaceAddresses.length)
          "
        >
          {{ saving ? t("保存中…") : t("保存任务") }}
        </button>
      </div>
    </form>
  </div>
</template>
