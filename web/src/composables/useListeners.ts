// HTTP(S) listener editor: the ports the web services bind to. Split out of
// useHosts so the site form and the listener modal stop sharing one module.
import { ref } from "vue";
import { apiFetch as fetch } from "../api";
import { t, tr } from "../i18n";
import type { WebListener } from "../types";
import { confirmDeletion, csrfToken, guardWrite, hosts } from "./useState";
import { load } from "./useRefresh";

/** Which protocol the site list is filtered to; follows the selected listener. */
export const siteExposureFilter = ref<"all" | "https" | "http">("https");

export const webListeners = ref<WebListener[]>([
    { id: "https-main", protocol: "https", port: 443 },
    { id: "http-main", protocol: "http", port: 80 },
  ]);
export const selectedListener = ref(0);
export const listenerLoaded = ref(false);
export const showListenerEditor = ref(false);
export const listenerEditingIndex = ref<number | null>(null);
export const listenerDraft = ref<WebListener>({ id: "", protocol: "https", port: 0 });
export const listenerChecking = ref(false);
export const listenerAvailable = ref<boolean | null>(null);
export const listenerMessage = ref("");
export const listenerSaving = ref(false);
export const listenerCreationMode = ref<"empty" | "copy">("empty");
export const listenerCopyFrom = ref("");
export const selectedPortState = ref<{
  checking: boolean;
  available: boolean | null;
  message: string;
}>({ checking: false, available: null, message: "" });
let selectedPortSequence = 0;
async function checkSelectedPort() {
  const sequence = ++selectedPortSequence,
    listener = webListeners.value[selectedListener.value];
  if (!listener) return;
  selectedPortState.value = {
    checking: true,
    available: null,
    message: t("正在检测端口…"),
  };
  try {
    const r = await fetch(
      `/api/v1/web-listeners/check?port=${listener.port}&protocol=${listener.protocol}`,
    );
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("检测失败"));
    if (sequence === selectedPortSequence)
      selectedPortState.value = {
        checking: false,
        available: body.available,
        message: t(body.message),
      };
  } catch (e) {
    if (sequence === selectedPortSequence)
      selectedPortState.value = {
        checking: false,
        available: false,
        message: e instanceof Error ? t(e.message) : t("检测失败"),
      };
  }
}
export async function loadWebListeners() {
  if (listenerLoaded.value) return;
  try {
    const r = await fetch("/api/v1/web-listeners");
    if (r.ok) {
      webListeners.value = await r.json();
      listenerLoaded.value = true;
      selectListener(0);
    }
  } catch {}
}
export function selectListener(index: number) {
  selectedListener.value = index;
  siteExposureFilter.value = webListeners.value[index]?.protocol || "https";
  void checkSelectedPort();
}
// crypto.randomUUID only exists in secure contexts, so a plain-HTTP panel
// access would throw here and the listener dialog would never open.
function randomListenerID() {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function")
    return crypto.randomUUID();
  return (
    "listener-" +
    Date.now().toString(36) +
    "-" +
    Math.random().toString(36).slice(2, 10)
  );
}
export function openListenerEditor(index: number | null) {
  if (!guardWrite()) return;
  listenerEditingIndex.value = index;
  listenerDraft.value =
    index === null
      ? { id: randomListenerID(), protocol: "https", port: 0 }
      : { ...webListeners.value[index]! };
  listenerCreationMode.value = "empty";
  listenerCopyFrom.value = "";
  listenerAvailable.value = null;
  listenerMessage.value = "";
  showListenerEditor.value = true;
  if (index !== null) void checkListenerPort();
}
let listenerCheckSequence = 0;
export async function checkListenerPort() {
  const sequence = ++listenerCheckSequence,
    port = listenerDraft.value.port;
  listenerAvailable.value = null;
  listenerChecking.value = true;
  try {
    if (!Number.isInteger(port) || port < 1 || port > 65535)
      throw Error(t("端口范围为 1–65535"));
    if (
      webListeners.value.some(
        (item, index) =>
          index !== listenerEditingIndex.value && item.port === port,
      )
    )
      throw Error(t("此端口已存在于其他监听"));
    const r = await fetch(
      `/api/v1/web-listeners/check?port=${port}&protocol=${listenerDraft.value.protocol}`,
    );
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("检测失败"));
    if (sequence === listenerCheckSequence) {
      listenerAvailable.value = body.available;
      listenerMessage.value = t(body.message);
    }
  } catch (e) {
    if (sequence === listenerCheckSequence) {
      listenerAvailable.value = false;
      listenerMessage.value = e instanceof Error ? t(e.message) : t("检测失败");
    }
  } finally {
    if (sequence === listenerCheckSequence) listenerChecking.value = false;
  }
}
async function persistListeners(
  next: WebListener[],
  target = selectedListener.value,
) {
  listenerSaving.value = true;
  try {
    const r = await fetch("/api/v1/web-listeners", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken.value,
      },
      body: JSON.stringify(next),
    });
    const body = await r.json();
    if (!r.ok) throw Error(t(body.error) || t("监听保存失败"));
    webListeners.value = body;
    await load();
    selectListener(Math.min(target, body.length - 1));
    showListenerEditor.value = false;
    listenerMessage.value = "";
  } catch (e) {
    listenerAvailable.value = false;
    listenerMessage.value = e instanceof Error ? t(e.message) : t("保存失败");
  } finally {
    listenerSaving.value = false;
  }
}
export async function saveListener() {
  if (
    listenerSaving.value ||
    listenerChecking.value ||
    listenerAvailable.value !== true
  )
    return;
  const next = webListeners.value.map((item) => ({ ...item }));
  if (listenerEditingIndex.value === null) {
    if (listenerCreationMode.value === "copy" && !listenerCopyFrom.value) {
      listenerMessage.value = t("请选择导入来源监听");
      return;
    }
    next.push({
      ...listenerDraft.value,
      copy_from:
        listenerCreationMode.value === "copy"
          ? listenerCopyFrom.value
          : undefined,
    });
  } else next[listenerEditingIndex.value] = { ...listenerDraft.value };
  await persistListeners(
    next,
    listenerEditingIndex.value === null
      ? next.length - 1
      : selectedListener.value,
  );
}
export async function deleteListener() {
  if (listenerSaving.value) return;
  const index = listenerEditingIndex.value;
  if (
    index === null ||
    !(await confirmDeletion(
      tr`删除 ${webListeners.value[index]?.protocol.toUpperCase()} :${webListeners.value[index]?.port} 监听及其 ${hosts.value.filter((host) => host.listener_id === webListeners.value[index]?.id).length} 个站点？`,
    ))
  )
    return;
  await persistListeners(webListeners.value.filter((_, i) => i !== index));
}
/** Hosts whose WebDAV address was just copied, so the row can confirm inline. */