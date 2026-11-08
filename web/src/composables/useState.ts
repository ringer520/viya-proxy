
// useState is the shared store + hub of the console. Every domain composable
// imports the data refs and feedback channels it needs from here; the fetch
// fan-out lives in useRefresh so a single refresh keeps all lists consistent.
import { computed, ref } from "vue";
import type {
  Certificate,
  ManagedCertificate,
  DDNSRecord,
  ExpiringCertificate,
  Host,
  IPBan,
  Layer4Rule,
  NetworkInterface,
  Provider,
  UpstreamHealth,
} from "../types";
// ---- session state (owned here so every domain can gate on it) ----
export const authenticated = ref(false);
export const csrfToken = ref("");
export const authChecked = ref(false);
export const setupRequired = ref(false);
export const panelUsername = ref("");
export const panelRole = ref<"admin" | "viewer">("admin");

// Read-only accounts cannot save anything, so the panel starts in a sealed mode
// instead of letting every button fail one request later.
export const readOnly = computed(() => panelRole.value === "viewer");
// Every write entry point short-circuits here so a viewer never opens a form
// they cannot submit; the backend independently rejects writes with 403.
export function guardWrite(): boolean {
  return !readOnly.value;
}

// ---- shared write feedback ----
export const error = ref("");
export const saving = ref(false);
export const applyRetryVisible = ref(false);

// ---- data lists populated by load() ----
export const hosts = ref<Host[]>([]);
export const providers = ref<Provider[]>([]);
export const l4Rules = ref<Layer4Rule[]>([]);
export const ddnsRecords = ref<DDNSRecord[]>([]);
export const certificates = ref<Certificate[]>([]);
export const certExpiring = ref<ExpiringCertificate[]>([]);
export const ipBans = ref<IPBan[]>([]);
export const upstreamHealth = ref<Record<string, UpstreamHealth>>({});
export const networkInterfaces = ref<NetworkInterface[]>([]);
export const refreshErrors = ref<Record<string,string>>({});
export const online = ref(false);
export const refreshing = ref(false);
export const lastRefreshedAt = ref(0);

// ---- delete confirmation dialog ----
export const deletePrompt = ref("");
export const confirmationTitle=ref("确认删除");
export const deleteConfirmOption = ref<{ label: string; checked: boolean } | null>(null);
export const deleteConfirmOptionResult = ref(false);
export const deleteBusy = ref(false);
export let deleteResolver: ((result: boolean) => void) | null = null;
export function confirmDeletion(message: string,title="确认删除",option?: { label: string; checked?: boolean }): Promise<boolean> {
  if (deleteResolver || deleteBusy.value) return Promise.resolve(false);
  confirmationTitle.value=title;
  deleteConfirmOption.value=option ? { label: option.label, checked: option.checked ?? false } : null;
  deleteConfirmOptionResult.value=false;
  deletePrompt.value = message;
  return new Promise((resolve) => {
    deleteResolver = resolve;
  });
}
export function resolveDelete(result: boolean) {
  const resolve = deleteResolver;
  deleteResolver = null;
  deleteConfirmOptionResult.value=result && !!deleteConfirmOption.value?.checked;
  deleteConfirmOption.value=null;
  deletePrompt.value = "";
  resolve?.(result);
}
// ---- list search + status filter (shared across every list page) ----
export const listSearch = ref("");
export const statusFilter = ref<"all" | "enabled" | "disabled">("all");
export function matchesSearch(...values: unknown[]) {
  const words = listSearch.value
      .trim()
      .toLowerCase()
      .split(/\s+/)
      .filter(Boolean),
    text = values
      .filter((value) => value != null)
      .join(" ")
      .toLowerCase();
  return words.every((word) => text.includes(word));
}
export function matchesStatus(enabled: boolean) {
  return (
    statusFilter.value === "all" ||
    enabled === (statusFilter.value === "enabled")
  );
}
export const hasListFilter = computed(
  () => !!listSearch.value.trim() || statusFilter.value !== "all",
);
export function clearListFilters() {
  listSearch.value = "";
  statusFilter.value = "all";
}


export const ddnsRefreshSequence = ref(0);

export const managedCertificates=ref<ManagedCertificate[]>([]);
