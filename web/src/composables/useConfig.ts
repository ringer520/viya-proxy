import { apiFetch as fetch } from "../api";
// Config revision diff. Stored revisions are redacted, so the panel can only
// compare them, not replay them; real rollback goes through the encrypted
// archive above this list.
import { ref } from "vue";
import { t } from "../i18n";

export const revisionDiff = ref<{
  id: number;
  from: number;
  to: number;
  added: { path: string; to: string }[];
  removed: { path: string; from: string }[];
  changed: { path: string; from: string; to: string }[];
  truncated: boolean;
} | null>(null);
export const revisionDiffBusy = ref(false);
export const revisionDiffError = ref("");

export async function loadRevisionDiff(id: number, against = "current") {
  revisionDiffBusy.value = true;
  revisionDiffError.value = "";
  try {
    const r = await fetch(
      `/api/v1/config/revisions/${id}/diff?against=${encodeURIComponent(
        String(against),
      )}`,
    );
    const data = await r.json();
    if (!r.ok) throw Error(t(data.error) || t("对比失败"));
    revisionDiff.value = {
      id,
      from: data.from,
      to: data.to,
      added: data.added || [],
      removed: data.removed || [],
      changed: data.changed || [],
      truncated: !!data.truncated,
    };
  } catch (e) {
    revisionDiffError.value =
      e instanceof Error ? t(e.message) : t("对比失败");
  } finally {
    revisionDiffBusy.value = false;
  }
}
export function diffValue(value: string) {
  return value.length > 160 ? value.slice(0, 160) + "…" : value;
}
// Replays a restored preferences bundle (from ConfigTransfer) and reloads.
export function configurationRestored(
  preferences: Record<string, string>,
  message: string,
) {
  const keys = Object.keys(localStorage).filter((key) =>
    key.startsWith("viya-"),
  );
  for (const key of keys) localStorage.removeItem(key);
  for (const [key, value] of Object.entries(preferences)) {
    if (
      key.startsWith("viya-") &&
      key.length < 100 &&
      typeof value === "string" &&
      value.length < 32768
    )
      localStorage.setItem(key, value);
  }
  sessionStorage.setItem("viya-restored-message", message);
  window.location.reload();
}
