import { apiFetch as fetch } from "../api";
// Re-apply the whole Caddy config after a partial save (502). This lives in its
// own module (not useCore) so useCore never has to import useNetwork — breaking
// the useCore → useNetwork → useCore cycle that would otherwise trip a TDZ on
// useNetwork's module-level `watch` calls during native-ESM dev loading.
import { t } from "../i18n";
import { applyRetryVisible, csrfToken, error, hosts, l4Rules, saving } from "./useState";
import { load } from "./useRefresh";
import {
  advancedOpen,
  editingID,
  form,
  partialSave,
} from "./useHosts";
import {
  l4EditingID,
  l4Error,
  l4Form,
  l4PartialSave,
  providerError,
} from "./useNetwork";

export async function retryApply() {
  saving.value = true;
  try {
    const r = await fetch("/api/v1/config/apply", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken.value },
    });
    const body = r.status === 204 ? {} : await r.json();
    if (!r.ok) throw Error(t(body.error) || t("重新应用失败"));
    applyRetryVisible.value = false;
    advancedOpen.value = false;
    form.value.basic_auth_password = "";
    providerError.value = "";
    l4Error.value = "";
    await load();

    if (partialSave.value && editingID.value === null) {
      const saved = hosts.value.find(
        (host) =>
          host.domain === form.value.domain.trim().toLowerCase() &&
          host.listener_id === form.value.listener_id,
      );
      if (saved) editingID.value = saved.id;
    }
    if (l4PartialSave.value && l4EditingID.value === null) {
      const saved = l4Rules.value.find(
        (rule) =>
          rule.listen_port === l4Form.value.listen_port &&
          rule.listen_address === l4Form.value.listen_address &&
          rule.protocol === l4Form.value.protocol,
      );
      if (saved) l4EditingID.value = saved.id;
    }
    if (editingID.value !== null) partialSave.value = false;
    if (l4EditingID.value !== null) l4PartialSave.value = false;
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t("重新应用失败");
  } finally {
    saving.value = false;
  }
}
