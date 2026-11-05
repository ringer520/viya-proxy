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
  showManagedCert.value = true;
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

registerRefreshTask("certificates",async()=>Promise.all([refreshManagedCertificates(),refreshCertificateInventory()]),"certs");
