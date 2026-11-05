import {refreshConfigStatus} from "./useConfigStatus";
import { apiFetch as fetch } from "../api";
// Layer4 port forwards, DDNS tasks, DNS provider credentials, and IP bans.
// Grouped here because they share the same "list + dialog + toggle" shape and
// the same network-service navigation group.
import { computed, ref, watch } from "vue";
import { t, tr } from "../i18n";
import type {
  DDNSRecord,
  DNSZoneOption,
  IPBan,
  Layer4Rule,
  NetworkInterface,
  Provider,
  RuleLogState,
} from "../types";
import { applyRetryVisible, confirmDeletion, csrfToken, ddnsRefreshSequence, ddnsRecords, error, guardWrite, hosts, ipBans, l4Rules, matchesSearch, matchesStatus, networkInterfaces, providers, saving } from "./useState";
import { load, performDelete } from "./useRefresh";

// ---- IP bans ----
export const showBan = ref(false);
export const editingBanID = ref<number | null>(null);
export const banError = ref("");
export const banKind = ref("ip");
export const banForm = ref({ domain: "", ip: "", reason: "",expires_at:0 });
export const banPreview=ref<{domain:string;client_ip:string;client_blocked:boolean;panel_domain:boolean;message:string}|null>(null),banRiskConfirmed=ref(false);
watch([banForm,banKind],()=>{banPreview.value=null;banRiskConfirmed.value=false},{deep:true});
export async function previewBan(){banError.value='';saving.value=true;try{const r=await fetch('/api/v1/ip-bans/preview',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrfToken.value},body:JSON.stringify({...banForm.value,ip:banKind.value==='foreign'?'foreign':banForm.value.ip})});const data=await r.json();if(!r.ok)throw Error(data.error);banPreview.value=data}catch(e){banError.value=e instanceof Error?t(e.message):t('读取失败')}finally{saving.value=false}}

export function openBan(ban?: IPBan) {
  if (!guardWrite()) return;
  banKind.value = ban?.ip === "foreign" ? "foreign" : "ip";
  editingBanID.value = ban?.id ?? null;
  banError.value = "";
  banForm.value = {
    domain: ban?.domain || hosts.value[0]?.domain || "",
    ip: ban?.ip || "",
    reason: ban?.reason || "",expires_at:ban?.expires_at||0,
  };
  showBan.value = true;
}
export async function saveBan() {
  if (!guardWrite()||saving.value) return;
 if(!banPreview.value){await previewBan();return}
 if(!banRiskConfirmed.value){banError.value=t("请核对影响预览并勾选确认");return}
  saving.value = true;
  banError.value = "";
  try {
    const r = await fetch(
      editingBanID.value
        ? `/api/v1/ip-bans/${editingBanID.value}`
        : "/api/v1/ip-bans",
      {
        method: editingBanID.value ? "PUT" : "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken.value,
        },
        body: JSON.stringify({
          ...banForm.value,
          ip: banKind.value === "foreign" ? "foreign" : banForm.value.ip,
          // Bans are permanent until an administrator lifts them manually.
          expires_at: banForm.value.expires_at,confirm_risk:banRiskConfirmed.value,
        }),
      },
    );
    const data = await r.json();
    if (!r.ok) {
      if (r.status === 502) {
        editingBanID.value = data.id;
        applyRetryVisible.value = true;
        error.value = t(data.error);
        await load();
      }
      throw Error(t(data.error) || t("保存封禁失败"));
    }
    showBan.value = false;
    await load();
 void refreshConfigStatus();
  } catch (e) {
    banError.value = e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    saving.value = false;
  }
}
export async function removeBan(id: number) {
  if (!guardWrite()) return;
  if (await confirmDeletion(tr`确认解除这条 IP 封禁？`))
    await performDelete(`/api/v1/ip-bans/${id}`);
}

// ---- Layer4 forwards ----
export const showL4 = ref(false);
export const l4EditingID = ref<number | null>(null);
export const l4Error = ref("");
export const l4PartialSave = ref(false);
export const l4Form = ref({
  name: "",
  listen_address: "0.0.0.0",
  listen_port: 2222,
  upstream_address: "127.0.0.1:22",
  protocol: "tcp",
  enabled: true,
});
export const l4Logs = ref<Record<number, RuleLogState>>({});
export const l4PreviewLayout = ref<Record<string, string>>({});
export async function loadL4Logs(rule: Layer4Rule) {
  const current = l4Logs.value[rule.id];
  if (current?.loading) return;
  if (current && Date.now() - current.loadedAt < 4000) return;
  l4Logs.value = {
    ...l4Logs.value,
    [rule.id]: {
      events: current?.events || [],
      loading: true,
      error: "",
      loadedAt: current?.loadedAt || 0,
    },
  };
  try {
    const r = await fetch(`/api/v1/layer4-rules/${rule.id}/logs`);
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("读取转发日志失败"));
    l4Logs.value = {
      ...l4Logs.value,
      [rule.id]: { events: body, loading: false, error: "", loadedAt: Date.now() },
    };
  } catch (e) {
    l4Logs.value = {
      ...l4Logs.value,
      [rule.id]: {
        events: [],
        loading: false,
        error: e instanceof Error ? t(e.message) : t("读取转发日志失败"),
        loadedAt: Date.now(),
      },
    };
  }
}
export function previewL4(rule: Layer4Rule, event: MouseEvent | FocusEvent) {
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
  const width = Math.min(540, window.innerWidth - 32);
  l4PreviewLayout.value = {
    position: "fixed",
    width: width + "px",
    left:
      Math.max(
        16,
        Math.min(rect.right - width, window.innerWidth - width - 16),
      ) + "px",
    ...(rect.bottom + 300 > window.innerHeight
      ? { bottom: window.innerHeight - rect.top + "px", top: "auto" }
      : { top: rect.bottom + "px", bottom: "auto" }),
  };
  void loadL4Logs(rule);
}
export function openL4(rule?: Layer4Rule) {
  if (!guardWrite()) return;
  l4EditingID.value = rule?.id ?? null;
  l4PartialSave.value = false;
  l4Form.value = rule
    ? { ...rule }
    : {
        name: "",
        listen_address: "0.0.0.0",
        listen_port: 2222,
        upstream_address: "127.0.0.1:22",
        protocol: "tcp",
        enabled: true,
      };
  l4Error.value = "";
  applyRetryVisible.value = false;
  showL4.value = true;
}
export async function saveL4() {
  if (!guardWrite() || saving.value) return;
  if (l4PartialSave.value && l4EditingID.value === null) { l4Error.value=t('规则已保存但暂时无法定位，请关闭后刷新列表再编辑。'); return; }
  l4Error.value = "";
  saving.value = true;
  try {
    const creating = l4EditingID.value === null;
    const url = creating
      ? "/api/v1/layer4-rules"
      : `/api/v1/layer4-rules/${l4EditingID.value}`;
    const r = await fetch(url, {
      method: creating ? "POST" : "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify(l4Form.value),
    });
    const body = r.status === 204 ? {} : await r.json();
    if (!r.ok) {
      if (r.status === 409) {
        await load();
        throw Error(
          t(body.error) || t("该转发已被其他会话修改，请关闭后重新打开"),
        );
      }
      if (r.status === 502) {
        applyRetryVisible.value = true;
        l4PartialSave.value = true;
        await load();
        if (creating) {
          if (Number.isSafeInteger(body.id) && body.id > 0) l4EditingID.value = body.id;
          const saved = l4Rules.value.find(
            (rule) =>
              rule.listen_port === l4Form.value.listen_port &&
              rule.listen_address === l4Form.value.listen_address &&
              rule.protocol === l4Form.value.protocol,
          );
          if (saved) {
            l4EditingID.value = saved.id;
            l4Form.value = { ...saved };
          }
        }
        l4Error.value = tr`${t(body.error) || t("Caddy 未应用配置")}。规则已保存，请勿重复新建；可重新应用或修正后再次保存。`;
        return;
      }
      throw Error(t(body.error) || t("保存失败"));
    }
    applyRetryVisible.value = false;
    l4PartialSave.value = false;
    showL4.value = false;
    await load();
  } catch (e) {
    l4Error.value = e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    saving.value = false;
  }
}
export const l4Toggling = ref<Record<number, boolean>>({});
export async function toggleL4(rule: Layer4Rule) {
  if (!guardWrite()) return;
  if (l4Toggling.value[rule.id]) return;
  l4Toggling.value[rule.id] = true;
  try {
    const r = await fetch(`/api/v1/layer4-rules/${rule.id}/enabled`, {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({ enabled: !rule.enabled, version: rule.version }),
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error));
    await load();
  } catch (e) {
    const message = e instanceof Error ? t(e.message) : t("修改失败");
    await load();
    error.value = message;
  } finally {
    l4Toggling.value[rule.id] = false;
  }
}
export async function removeL4(rule: Layer4Rule) {
  if (!guardWrite()) return;
  if (
    await confirmDeletion(
      tr`删除 ${rule.protocol.toUpperCase()} ${rule.listen_address}:${rule.listen_port} 转发？`,
    )
  )
    await performDelete(`/api/v1/layer4-rules/${rule.id}`);
}

// ---- DDNS ----
export const showDDNS = ref(false);
export const ddnsEditingID = ref<number | null>(null);
export const ddnsError = ref("");
export const ddnsForm = ref({
  name: "",
  comment: "viya",
  dns_provider_id: "",
  zone: "example.com",
  record_name: "@",
  record_type: "A" as "A" | "AAAA" | "CNAME",
  cname_target: "",
  ip_source: "interface" as "interface" | "http" | "",
  interface_name: "",
  interface_ip: "",
  ttl_seconds: 300,
  interval_minutes: 5,
  enabled: true,
});
export const ddnsZones = ref<DNSZoneOption[]>([]);
export const ddnsZoneLoading = ref(false);
export const ddnsZoneMessage = ref("");
export const manualDDNSZone = ref(false);
export const selectedInterfaceAddresses = computed(
  () =>
    networkInterfaces.value
      .find((item) => item.name === ddnsForm.value.interface_name)
      ?.addresses.filter(
        (address) =>
          address.family ===
          (ddnsForm.value.record_type === "A" ? "ipv4" : "ipv6"),
      ) || [],
);
// Empty means follow the interface. Refreshing addresses never silently replaces a pin.
watch(()=>[ddnsForm.value,ddnsForm.value.interface_name,ddnsForm.value.record_type] as const,([form,name,type],[previous,oldName,oldType])=>{if(form===previous&&(name!==oldName||type!==oldType))form.interface_ip=''});
export function formatDDNSTime(value: number) {
  return value ? new Date(value * 1000).toLocaleString() : t("尚未更新");
}
export async function refreshNetworkInterfaces() {
  try {
    const response = await fetch("/api/v1/network-interfaces");
    if (response.ok) networkInterfaces.value = await response.json();
  } catch {}
}
export function interfaceLabel(item: NetworkInterface) {
  const family = ddnsForm.value.record_type === "A" ? "ipv4" : "ipv6";
  const addresses = item.addresses.filter(
    (address) => address.family === family,
  );
  return `${item.name} · ${addresses.length ? tr`${addresses.length} 个${family === "ipv6" ? "IPv6" : "IPv4"} 地址` : t("无对应地址")}`;
}
let ddnsZoneSequence = 0;
export async function loadDDNSZones(clearZone = false) {
  const sequence = ++ddnsZoneSequence,
    providerID = ddnsForm.value.dns_provider_id;
  ddnsZones.value = [];
  ddnsZoneMessage.value = "";
  if (clearZone) ddnsForm.value.zone = "";
  if (!providerID) {
    manualDDNSZone.value = true;
    ddnsZoneMessage.value = t("请先选择 DNS 服务商。");
    ddnsZoneLoading.value = false;
    return;
  }
  ddnsZoneLoading.value = true;
  try {
    const response = await fetch(`/api/v1/dns-providers/${providerID}/zones`);
    const data = await response.json();
    if (sequence !== ddnsZoneSequence) return;
    if (!response.ok) throw Error(t(data.error) || t("读取 DNS Zone 失败"));
    if (data.supported) {
      ddnsZones.value = data.zones || [];
      const exists = ddnsZones.value.some(
        (zone) => zone.name === ddnsForm.value.zone,
      );
      manualDDNSZone.value =
        !ddnsZones.value.length ||
        (!clearZone && !!ddnsForm.value.zone && !exists);
      ddnsZoneMessage.value = ddnsZones.value.length
        ? tr`已读取 ${ddnsZones.value.length} 个可管理 Zone。`
        : t(data.message) || t("此账号下没有可用 Zone，请手动输入。");
    } else {
      manualDDNSZone.value = true;
      ddnsZoneMessage.value =
        t(data.message) || t("此服务商暂不支持自动列出 Zone，请手动输入。");
    }
  } catch (e) {
    if (sequence === ddnsZoneSequence) {
      manualDDNSZone.value = true;
      ddnsZoneMessage.value =
        e instanceof Error ? t(e.message) : t("读取 Zone 失败，可手动输入。");
    }
  } finally {
    if (sequence === ddnsZoneSequence) ddnsZoneLoading.value = false;
  }
}
export async function openDDNS(record?: DDNSRecord) {
  if (!guardWrite()) return;
  await refreshNetworkInterfaces();
  ddnsEditingID.value = record?.id ?? null;
  const firstIPv4 = networkInterfaces.value.find((item) =>
    item.addresses.some((address) => address.family === "ipv4"),
  );
  ddnsForm.value = record
    ? {
        ...record,
        comment: record.comment || "viya",
        cname_target: record.cname_target || "",
        interface_ip: record.interface_ip || "",
        dns_provider_id: String(record.dns_provider_id),
        ip_source: record.ip_source || "http",
        interface_name: record.interface_name || "",
      }
    : {
        name: "",
        comment: "viya",
        dns_provider_id: providers.value[0]
          ? String(providers.value[0].id)
          : "",
        zone: "",
        record_name: "@",
        record_type: "A",
        cname_target: "",
        ip_source: firstIPv4 ? "interface" : "http",
        interface_name: firstIPv4?.name || "",
        interface_ip: "",
        ttl_seconds: 300,
        interval_minutes: 5,
        enabled: true,
      };
  ddnsError.value = "";
  ddnsZones.value = [];
  manualDDNSZone.value = false;
  showDDNS.value = true;
  void loadDDNSZones(false);
}
export async function saveDDNS() {
  if (!guardWrite()) return;
  saving.value = true;
  ddnsError.value = "";
  try {
    const url =
      ddnsEditingID.value === null
        ? "/api/v1/ddns-records"
        : `/api/v1/ddns-records/${ddnsEditingID.value}`;
    const r = await fetch(url, {
      method: ddnsEditingID.value === null ? "POST" : "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({
        ...ddnsForm.value,
        dns_provider_id: Number(ddnsForm.value.dns_provider_id),
      }),
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("保存失败"));
    showDDNS.value = false;
    await load();
  } catch (e) {
    ddnsError.value = e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    saving.value = false;
  }
}
export const ddnsRunning = ref<Record<number, boolean>>({});
export const ddnsToggling = ref<Record<number, boolean>>({});
export const ddnsFeedback = ref<Record<number, string>>({});
let ddnsRefreshPromise: Promise<void> | null = null;
export async function refreshDDNS(force = false) {
  while (ddnsRefreshPromise) {
    await ddnsRefreshPromise;
    if (!force) return;
  }
  const sequence = ++ddnsRefreshSequence.value;
  const task = (async () => {
    try {
      const r = await fetch("/api/v1/ddns-records");
      if (r.ok) {
        const records = await r.json();
        if (sequence === ddnsRefreshSequence.value) ddnsRecords.value = records;
      }
    } catch {}
  })();
  ddnsRefreshPromise = task;
  try {
    await task;
  } finally {
    if (ddnsRefreshPromise === task) ddnsRefreshPromise = null;
  }
}
export function patchDDNS(id: number, patch: Partial<DDNSRecord>) {
  const current = ddnsRecords.value.find((item) => item.id === id);
  if (current) Object.assign(current, patch);
}
export async function toggleDDNS(record: DDNSRecord, enabled: boolean) {
  if (!guardWrite()) return;
  if (ddnsToggling.value[record.id]) return;
  ddnsToggling.value[record.id] = true;
  ddnsFeedback.value[record.id] = "";
  ddnsRefreshSequence.value++;
  try {
    const r = await fetch(`/api/v1/ddns-records/${record.id}/enabled`, {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify({ enabled }),
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("修改失败"));
    ddnsRefreshSequence.value++;
    patchDDNS(record.id, { enabled: body.enabled });
    await refreshDDNS(true);
  } catch (e) {
    ddnsFeedback.value[record.id] =
      e instanceof Error ? t(e.message) : t("修改失败");
    await refreshDDNS(true);
  } finally {
    ddnsToggling.value[record.id] = false;
  }
}
export async function runDDNS(record: DDNSRecord) {
  if (!guardWrite()) return;
  if (ddnsRunning.value[record.id]) return;
  ddnsRunning.value[record.id] = true;
  ddnsFeedback.value[record.id] = "";
  try {
    const r = await fetch(`/api/v1/ddns-records/${record.id}/run`, {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken.value },
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("更新失败"));
    ddnsRefreshSequence.value++;
    patchDDNS(record.id, {
      last_run_at: body.last_run_at,
      last_ip: body.ip,
      last_error: "",
    });
    ddnsFeedback.value[record.id] = t("更新完成");
  } catch (e) {
    ddnsFeedback.value[record.id] =
      e instanceof Error ? t(e.message) : t("更新失败");
  } finally {
    await refreshDDNS(true);
    ddnsRunning.value[record.id] = false;
  }
}
export async function removeDDNS(record: DDNSRecord) {
  if (!guardWrite()) return;
  if (await confirmDeletion(tr`删除 DDNS 任务“${record.name}”？`))
    await performDelete(`/api/v1/ddns-records/${record.id}`);
}

// ---- DNS providers ----
export const showProvider = ref(false);
export const editingProviderID = ref<number | null>(null);
export const providerError = ref("");
export const providerForm = ref({
  name: "",
  provider_type: "cloudflare",
  credentials: {} as Record<string, string>,
});
export const providerFields: Record<string, string[]> = {
  cloudflare: ["api_token"],
  tencentcloud: ["secret_id", "secret_key"],
  alidns: ["access_key_id", "access_key_secret"],
  godaddy: ["api_token"],
  namecheap: ["api_key", "user"],
  digitalocean: ["auth_token"],
  vultr: ["api_token"],
};
export function openProvider() {
  if (!guardWrite()) return;
  editingProviderID.value = null;
  providerForm.value = {
    name: "",
    provider_type: "cloudflare",
    credentials: {},
  };
  providerError.value = "";
  applyRetryVisible.value = false;
  showProvider.value = true;
}
export function editProvider(provider: Provider) {
  editingProviderID.value = provider.id;
  providerForm.value = {
    name: provider.name,
    provider_type: provider.provider_type,
    credentials: {},
  };
  providerError.value = "";
  showProvider.value = true;
}
export async function saveProvider() {
  saving.value = true;
  providerError.value = "";
  try {
    const url =
      editingProviderID.value === null
        ? "/api/v1/dns-providers"
        : `/api/v1/dns-providers/${editingProviderID.value}`;
    const r = await fetch(url, {
      method: editingProviderID.value === null ? "POST" : "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify(providerForm.value),
    });
    const data = await r.json();
    if (!r.ok) {
      if (r.status === 502) {
        applyRetryVisible.value = true;
        await load();
        throw Error(
          tr`${t(data.error) || t("Caddy 未重新载入 DNS 凭据")}。凭据已保存，可重新应用配置。`,
        );
      }
      throw Error(t(data.error) || t("保存 DNS 凭据失败"));
    }
    applyRetryVisible.value = false;
    showProvider.value = false;
    await load();
  } catch (e) {
    providerError.value = e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    saving.value = false;
  }
}
export async function removeProvider(provider: Provider) {
  if (!guardWrite()) return;

  let usage:string[]=[];try{const r=await fetch(`/api/v1/dns-providers/${provider.id}/usage`);const d=await r.json();if(!r.ok)throw Error(d.error);usage=d.map((v:{kind:string;name:string})=>v.kind+': '+v.name)}catch(e){error.value=e instanceof Error?t(e.message):t('读取失败');return}
 if(usage.length){error.value=t('此凭据仍在使用，请先修改关联资源后再删除')+'：'+usage.join('；');return}
  if (await confirmDeletion(tr`删除 DNS 凭据“${provider.name}”？`+(usage.length?"\n"+t("当前关联：")+usage.join("；"):"")))
    await performDelete(`/api/v1/dns-providers/${provider.id}`);
}

// ---- list filtering ----
export const visibleDDNS = computed(() =>
  ddnsRecords.value.filter(
    (record) =>
      matchesSearch(
        record.name,
        record.comment,
        record.zone,
        record.record_name,
        record.record_type,
        record.last_ip,
        record.interface_name,
      ) && matchesStatus(record.enabled),
  ),
);
export const visibleL4 = computed(() =>
  l4Rules.value.filter(
    (rule) =>
      matchesSearch(
        rule.name,
        rule.protocol,
        rule.listen_address,
        rule.listen_port,
        rule.upstream_address,
      ) && matchesStatus(rule.enabled),
  ),
);
export const visibleProviders = computed(() =>
  providers.value.filter((provider) =>
    matchesSearch(provider.name, provider.provider_type),
  ),
);
export const visibleBans = computed(() =>
  ipBans.value,
);
