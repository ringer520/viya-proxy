import {registerRefreshTask} from "./useRefresh";
import { apiFetch as fetch } from "../api";
// Managed (ACME) certificates, the automatic certificate inventory, and
// custom PEM uploads. Managed + inventory are refreshed inside `load()`, so
// this module only exposes the refs and the request/delete entry points.
import { computed, ref } from "vue";
import { t, tr } from "../i18n";
import type {
  AutomaticCertificate,
  Certificate,
  ManagedCertificate,
} from "../types";
import { applyRetryVisible, hosts, certificates, confirmDeletion, csrfToken, error, guardWrite, matchesSearch, saving } from "./useState";
import { load, performDelete } from "./useRefresh";

import {managedCertificates} from "./useState";
export {managedCertificates} from "./useState";
export const showManagedCert = ref(false);
export const managedCertError = ref("");
export const managedCertBusy = ref(false);
export const managedCertForm = ref({
  name: "",
  domain: "",
  method: "dns-01",
  email: "",
  dns_provider_id: "",
});
export const editingManagedID = ref<number | null>(null);

// 证书域名多数情况下就是某个 Zone 的泛域名，而 DNS 凭据本身已经能列出 Zone。
// 选完凭据后直接读出来，省掉一次手打，但最终值仍由用户决定。
export const managedCertZones = ref<{ name: string }[]>([]);
export const managedCertZoneLoading = ref(false);
export const managedCertZoneMessage = ref("");
export const managedCertZone = ref("");
export const managedCertManualZone = ref(false);
// 上一次自动填入的值。只有域名仍等于它（或为空）时才允许覆盖，
// 避免把用户手改过的域名冲掉。
const managedCertAutoDomain = ref("");
let certZoneSequence = 0;

function certZoneForDomain(domain: string): string {
  const plain = domain.replace(/^\*\./, "").toLowerCase();
  if (!plain) return "";
  const hit = managedCertZones.value.find(
    (zone) => plain === zone.name || plain.endsWith("." + zone.name),
  );
  return hit?.name || "";
}

function fillManagedCertDomain(zone: string) {
  if (editingManagedID.value !== null) return;
  const domain = managedCertForm.value.domain.trim();
  if (domain && domain !== managedCertAutoDomain.value) return;
  const wildcard = `*.${zone}`;
  managedCertForm.value.domain = wildcard;
  managedCertAutoDomain.value = wildcard;
  if (!managedCertForm.value.name.trim()) managedCertForm.value.name = zone;
}

export async function loadManagedCertZones(reset = false) {
  const sequence = ++certZoneSequence;
  managedCertZones.value = [];
  managedCertZoneMessage.value = "";
  managedCertManualZone.value = false;
  if (reset) {
    managedCertZone.value = "";
    if (managedCertForm.value.domain === managedCertAutoDomain.value) {
      managedCertForm.value.domain = "";
    }
    managedCertAutoDomain.value = "";
  }
  const providerID =
    managedCertForm.value.method === "dns-01"
      ? managedCertForm.value.dns_provider_id
      : "";
  if (!providerID) {
    managedCertManualZone.value = true;
    managedCertZoneLoading.value = false;
    return;
  }
  managedCertZoneLoading.value = true;
  try {
    const response = await fetch(`/api/v1/dns-providers/${providerID}/zones`);
    const data = await response.json();
    if (sequence !== certZoneSequence) return;
    if (!response.ok) throw Error(t(data.error) || t("读取 Zone 失败，可手动输入。"));
    if (!data.supported) {
      managedCertManualZone.value = true;
      managedCertZoneMessage.value =
        t(data.message) || t("此服务商暂不支持自动列出 Zone，请手动输入。");
      return;
    }
    managedCertZones.value = data.zones || [];
    if (!managedCertZones.value.length) {
      managedCertManualZone.value = true;
      managedCertZoneMessage.value =
        t(data.message) || t("此服务商暂不支持自动列出 Zone，请手动输入。");
      return;
    }
    const existing = certZoneForDomain(managedCertForm.value.domain);
    if (existing) {
      managedCertZone.value = existing;
      managedCertZoneMessage.value = tr`已读取 ${managedCertZones.value.length} 个可管理 Zone。`;
    } else if (managedCertZones.value.length === 1) {
      managedCertZone.value = managedCertZones.value[0].name;
      managedCertZoneMessage.value = tr`已读取 ${managedCertZones.value.length} 个可管理 Zone。`;
      fillManagedCertDomain(managedCertZone.value);
    } else {
      managedCertZoneMessage.value = tr`已读取 ${managedCertZones.value.length} 个可管理 Zone。`;
    }
  } catch (e) {
    if (sequence === certZoneSequence) {
      managedCertManualZone.value = true;
      managedCertZoneMessage.value =
        e instanceof Error ? t(e.message) : t("读取 Zone 失败，可手动输入。");
    }
  } finally {
    if (sequence === certZoneSequence) managedCertZoneLoading.value = false;
  }
}

// 用户从下拉里明确选了一个 Zone，此时覆盖域名是预期的。
export function pickManagedCertZone(zone: string) {
  managedCertZone.value = zone;
  if (!zone) return;
  const domain = managedCertForm.value.domain.trim();
  if (editingManagedID.value === null && (!domain || domain === managedCertAutoDomain.value)) {
    managedCertForm.value.domain = `*.${zone}`;
    managedCertAutoDomain.value = `*.${zone}`;
  }
  if (!managedCertForm.value.name.trim()) managedCertForm.value.name = zone;
}

export function openManagedCert(cert?: ManagedCertificate, clone = false) {
  if (!guardWrite()) return;
  editingManagedID.value = cert && !clone ? cert.id : null;
  managedCertForm.value = cert
    ? {
        name: cert.name + (clone ? t(" 副本") : ""),
        domain: clone ? "" : cert.domain,
        method: cert.method,
        email: cert.email || "",
        dns_provider_id: cert.dns_provider_id
          ? String(cert.dns_provider_id)
          : "",
      }
    : {
        name: "",
        domain: "",
        method: "dns-01",
        email: "",
        dns_provider_id: "",
      };
  managedCertError.value = "";
  managedCertAutoDomain.value = "";
  managedCertZones.value = [];
  managedCertZone.value = "";
  managedCertZoneMessage.value = "";
  managedCertManualZone.value = false;
  showManagedCert.value = true;
  if (managedCertForm.value.method === "dns-01" && managedCertForm.value.dns_provider_id) {
    void loadManagedCertZones();
  }
}
export async function refreshManagedCertificates() {
  try {
    const r = await fetch("/api/v1/managed-certificates");
    if (r.ok) managedCertificates.value = await r.json();
  } catch {}
}
export async function requestCertificate() {
  managedCertBusy.value = true;
  managedCertError.value = "";
  try {
    const r = await fetch(
      editingManagedID.value
        ? `/api/v1/managed-certificates/${editingManagedID.value}`
        : "/api/v1/managed-certificates",
      {
        method: editingManagedID.value ? "PUT" : "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken.value,
        },
        body: JSON.stringify({
          ...managedCertForm.value,
          dns_provider_id:
            managedCertForm.value.method === "dns-01"
              ? Number(managedCertForm.value.dns_provider_id)
              : null,
        }),
      },
    );
    const data = await r.json();
    if (!r.ok) {
      if (r.status === 502) {
        showManagedCert.value = false;
        applyRetryVisible.value = true;
        await load();
        error.value = t(data.error);
        return;
      }
      throw Error(t(data.error) || t("证书申请失败"));
    }
    showManagedCert.value = false;
    error.value = "";
    await load();
  } catch (e) {
    managedCertError.value = e instanceof Error ? t(e.message) : t("申请失败");
  } finally {
    managedCertBusy.value = false;
  }
}
export async function removeManagedCert(cert: ManagedCertificate) {
  const usage=hosts.value.filter(h=>h.certificate_source?.domain===cert.domain).map(h=>h.domain);
  if (await confirmDeletion(tr`停止“${cert.domain}”的证书托管和自动续期？`+(usage.length?"\n"+t("当前关联：")+usage.join("；"):"")))
    await performDelete(`/api/v1/managed-certificates/${cert.id}`);
}
export const automaticCertificates = ref<AutomaticCertificate[]>([]);
export async function refreshCertificateInventory() {
  try {
    const r = await fetch("/api/v1/certificate-inventory");
    if (r.ok) automaticCertificates.value = await r.json();
  } catch {}
}
export async function removeAutomaticCert(cert: AutomaticCertificate) {
  if (
    await confirmDeletion(
      tr`将彻底删除“${cert.domain}”的证书文件和申请记录，并由引擎重新应用配置。之后若站点仍选择自动 HTTPS，引擎会重新申请。确定继续吗？`,
    )
  )
    await performDelete(
      `/api/v1/certificate-inventory/${encodeURIComponent(cert.domain)}`,
    );
}

// Custom PEM upload.
export const showCert = ref(false);
export const certForm = ref({ name: "" });
export const certFile = ref<File | null>(null);
export const keyFile = ref<File | null>(null);
export const certError = ref("");

export async function saveCert() {
  certError.value = "";
  saving.value = true;
  try {
    if (!certFile.value || !keyFile.value)
      throw Error(t("请选择证书和私钥 PEM 文件"));
    const data = new FormData();
    data.set("name", certForm.value.name);
    data.set("certificate", certFile.value);
    data.set("private_key", keyFile.value);
    const r = await fetch("/api/v1/custom-certificates", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken.value },
      body: data,
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("上传失败"));
    showCert.value = false;
    certFile.value = null;
    keyFile.value = null;
    certForm.value.name = "";
    await load();
  } catch (e) {
    certError.value = e instanceof Error ? t(e.message) : t("上传失败");
  } finally {
    saving.value = false;
  }
}
export async function removeCert(cert: Certificate) {
  if (await confirmDeletion(tr`删除证书“${cert.name}”？`))
    await performDelete(`/api/v1/custom-certificates/${cert.id}`);
}

// ---- list filtering ----
export const visibleCerts = computed(() =>
  certificates.value.filter((cert) =>
    matchesSearch(cert.name, ...cert.dns_names),
  ),
);
export const visibleManagedCerts = computed(() =>
  managedCertificates.value.filter((cert) =>
    matchesSearch(cert.name, cert.domain, cert.status, cert.method),
  ),
);

registerRefreshTask("certificates",async()=>Promise.all([refreshManagedCertificates(),refreshCertificateInventory()]));
