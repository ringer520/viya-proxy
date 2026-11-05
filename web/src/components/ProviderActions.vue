<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue'
import { ShieldCheck, LoaderCircle } from '@lucide/vue'
import { apiFetch } from '../api'
import { csrfToken, readOnly } from '../composables/useState'
import { t } from '../i18n'
import type { Provider } from '../types'

const props = defineProps<{ provider: Provider }>()
const busy = ref(false), note = ref(''), failed = ref(false), zone = ref('')
const reads = new AbortController()
watch(() => [props.provider.id, props.provider.checked_at, props.provider.check_message, props.provider.check_status], () => {
  note.value = props.provider.check_message ? t(props.provider.check_message) : ''
  failed.value = ['auth-failed','permission-denied','network-error','provider-error'].includes(props.provider.check_status || '')
}, { immediate: true })
async function check() {
  if (busy.value || readOnly.value) return
  busy.value = true; note.value = ''; failed.value = false
  try {
    const response = await apiFetch(`/api/v1/dns-providers/${props.provider.id}/check`, {
      method: 'POST', signal: reads.signal,
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken.value },
      body: JSON.stringify({ zone: zone.value }),
    })
    const data = await response.json()
    if (!response.ok) throw Error(t(data.error) || t('检查失败'))
    failed.value = ['auth-failed','permission-denied','network-error','provider-error'].includes(data.status)
    note.value = t(data.message)
  } catch (error) {
    if (!reads.signal.aborted) { failed.value = true; note.value = error instanceof Error ? t(error.message) : t('检查失败') }
  } finally { busy.value = false }
}
onBeforeUnmount(() => reads.abort())
</script>

<template>
  <div class="provider-verification">
    <div class="provider-actions-toolbar">
      <input v-if="provider.provider_type !== 'cloudflare'" v-model="zone" :aria-label="t('用于验证的 DNS Zone')" :placeholder="t('验证 Zone（可选）')" maxlength="253" :disabled="busy || readOnly"/>
      <button type="button" class="secondary" :disabled="busy || readOnly" :aria-busy="busy" @click="check"><LoaderCircle v-if="busy" :size="15" class="spinning"/><ShieldCheck v-else :size="15"/>{{busy ? t('处理中…') : t('验证凭据')}}</button>
      <slot/>
    </div>
    <p v-if="note" class="provider-check-note" :class="{failed}" :role="failed ? 'alert' : 'status'">{{note}}</p>
  </div>
</template>
