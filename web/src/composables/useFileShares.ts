import { ref } from "vue";
import { apiFetch } from "../api";
import type { FileShare } from "../components/file-shares/types";

// Module singleton: the hub is the only consumer, and keeping the list in one
// place means the WebDAV settings dialog can refresh it after a save.
export const fileShares = ref<FileShare[]>([]);
export const fileSharesLoading = ref(false);
export const fileSharesError = ref("");

export async function refreshFileShares(): Promise<void> {
  fileSharesLoading.value = true;
  fileSharesError.value = "";
  try {
    const r = await apiFetch("/api/v1/file-shares", { credentials: "same-origin" });
    if (!r.ok) throw new Error("load-failed");
    const body = (await r.json()) as { shares?: FileShare[] };
    fileShares.value = Array.isArray(body.shares) ? body.shares : [];
  } catch {
    fileShares.value = [];
    fileSharesError.value = "load-failed";
  } finally {
    fileSharesLoading.value = false;
  }
}
