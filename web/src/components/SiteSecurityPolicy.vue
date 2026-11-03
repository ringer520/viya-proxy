<script setup lang="ts">
import { computed, watch } from 'vue'
import { ShieldCheck } from '@lucide/vue'
import { t } from '../i18n'
import { emptySitePolicy, type SitePolicy } from '../policy'
import { resolveSecurityPolicy } from '../composables/useSecurity'
import SitePolicyFields from './SitePolicyFields.vue'
const props=defineProps<{modelValue:SitePolicy;domain:string}>()
const emit=defineEmits<{(event:'update:modelValue',value:SitePolicy):void}>()
const mode=computed(()=>props.modelValue.mode || (props.modelValue.inherit?'follow':'custom'))
const resolved=computed(()=>resolveSecurityPolicy(props.domain))
const effective=computed(()=>mode.value==='follow'?resolved.value.policy:mode.value==='none'?{...emptySitePolicy(),mode:'none' as const}:props.modelValue)
function withPolicy(policy:SitePolicy,nextMode:SitePolicy['mode']):SitePolicy {
  return {...policy,mode:nextMode,inherit:nextMode==='follow',basic_auth_enabled:props.modelValue.basic_auth_enabled,basic_auth_user:props.modelValue.basic_auth_user,custom_headers:props.modelValue.custom_headers}
}
function setMode(event:Event){
  const next=(event.target as HTMLSelectElement).value as SitePolicy['mode']
  const policy=next==='follow'?resolved.value.policy:next==='none'?emptySitePolicy():effective.value
  emit('update:modelValue',withPolicy(JSON.parse(JSON.stringify(policy)),next))
}
watch(()=>props.domain,()=>{if(mode.value==='follow')emit('update:modelValue',withPolicy(resolved.value.policy,'follow'))})
</script>
<template>
  <section class="site-policy-choice">
    <div class="policy-choice-title"><ShieldCheck :size="18"/><strong>{{t('站点安全策略')}}</strong></div>
    <label>{{t('配置方式')}}<select :value="mode" @change="setMode"><option value="follow">{{t('跟随域名策略')}}</option><option value="custom">{{t('独立配置')}}</option><option value="none">{{t('不使用默认策略')}}</option></select></label>
    <div v-if="mode==='follow'" class="policy-source-note"><span>{{t('实际来源')}}</span><strong>{{resolved.source==='global'?t('全局默认策略'):resolved.source}}</strong><small>{{t('上级策略变更时自动同步；认证和上游请求头仍由本站点单独配置。')}}</small></div>
    <p v-else-if="mode==='none'" class="inline-help">{{t('不应用默认响应头、IP 规则和请求限制；全局 IP 封禁与本站点访问认证仍然有效。')}}</p>
    <p v-else class="inline-help">{{t('此站点独立管理，修改域名或全局策略不会覆盖这里的配置。')}}</p>
    <div v-if="mode!=='none'" class="policy-summary-grid"><span>{{t('浏览器防护')}}<b>{{effective.enabled?t('已启用'):t('未启用')}}</b></span><span>{{t('IP 访问控制')}}<b>{{(effective.ip_allowlist?.length||0)+(effective.ip_denylist?.length||0)}} {{t('条规则')}}</b></span><span>{{t('请求限速')}}<b>{{effective.rate_limit_requests?effective.rate_limit_requests+' / '+effective.rate_limit_window:t('未限制')}}</b></span></div>
    <SitePolicyFields v-if="mode==='custom'" :key="mode" :model-value="modelValue" @update:model-value="emit('update:modelValue',$event)" variant="site"/>
    <details v-else-if="mode==='follow'" class="policy-effective-details"><summary>{{t('查看生效配置')}}</summary><fieldset disabled><SitePolicyFields :key="domain+resolved.source+JSON.stringify(effective)" :model-value="effective" variant="defaults"/></fieldset></details>
  </section>
</template>
