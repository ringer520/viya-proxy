<script setup lang="ts">
import { ShieldCheck, X } from '@lucide/vue'
import { t } from '../i18n'
import { closeSecurityDialog, saveSecurityDefaults, securityDialog, securityError, securityForm, securityNotice, securitySaving, securityImpact, securityPreviewPending, securityPreviewError, securityEffective, securityEffectiveSource } from '../composables/useSecurity'
import SitePolicyFields from './SitePolicyFields.vue'
</script>
<template>
  <div v-if="securityDialog.open" class="overlay security-policy-overlay" v-dialog>
    <form class="modal security-modal policy-settings" @submit.prevent="saveSecurityDefaults">
      <div class="modal-top"><div><span class="eyebrow">{{securityDialog.scope==='global'?'VIYA / SECURITY':securityDialog.domain}}</span><h2><ShieldCheck :size="21"/>{{securityDialog.scope==='global'?t('全局默认策略'):t('域名安全策略')}}</h2></div><button class="icon-btn" type="button" :disabled="securitySaving" :aria-label="t('关闭')" @click="closeSecurityDialog"><X :size="18"/></button></div>
      <p class="inline-help">{{securityDialog.scope==='global'?t('作为所有跟随默认站点的兜底策略；更具体的域名策略和独立站点优先。'):t('用于当前域名及其子域；更具体的域名策略和独立站点不会被覆盖。')}}</p>
      <p v-if="securityError" class="policy-error" role="alert">{{securityError}}</p>
      <p v-if="securityNotice" class="policy-success" role="status">{{securityNotice}}</p>
      <fieldset :disabled="securitySaving" class="policy-editor-fieldset">
        <label>{{t('配置方式')}}<select v-model="securityForm.mode"><option v-if="securityDialog.scope==='domain'" value="inherit">{{t('继承上级策略')}}</option><option value="custom">{{t('自定义策略')}}</option><option value="none">{{t('不使用默认策略')}}</option></select></label>
      <section class="policy-impact" aria-live="polite"><strong>{{t('本次保存的影响范围')}}</strong><p v-if="securityPreviewPending">{{t('正在计算影响范围…')}}</p><p v-else-if="securityPreviewError" class="policy-error" role="alert">{{securityPreviewError}}</p><template v-else-if="securityImpact"><div class="policy-impact-counts"><span><b>{{securityImpact.updated}}</b>{{t('个跟随站点将同步')}}</span><span><b>{{securityImpact.independent}}</b>{{t('个独立或不继承站点不受影响')}}</span><span><b>{{securityImpact.overridden}}</b>{{t('个使用其他域名策略的站点不受影响')}}</span></div><details v-if="securityImpact.sites.length"><summary>{{t('查看将同步的站点')}}</summary><ul><li v-for="(domain,index) in securityImpact.sites" :key="domain+index">{{domain}}</li></ul></details><small>{{t('保存并应用失败时会恢复原策略；站点认证与上游请求头不会被批量修改。')}}</small></template></section>
        <div v-if="securityForm.mode==='inherit'" class="policy-source-note"><span>{{t('实际来源')}}</span><strong>{{securityEffectiveSource==='global'?t('全局默认策略'):securityEffectiveSource}}</strong><small>{{t('上级策略变更时，跟随的站点会自动同步。')}}</small></div>
        <p v-else-if="securityForm.mode==='none'" class="inline-help">{{t('在此层停止继承默认安全策略。独立配置、访问认证和全局 IP 封禁不受影响。')}}</p>
        <SitePolicyFields v-if="securityForm.mode==='custom'" :key="securityDialog.scope+securityDialog.domain+securityForm.mode" v-model="securityForm" variant="defaults"/>
        <details v-else-if="securityForm.mode==='inherit'" class="policy-effective-details"><summary>{{t('查看生效配置')}}</summary><fieldset disabled><SitePolicyFields :key="JSON.stringify(securityEffective)" :model-value="securityEffective" variant="defaults"/></fieldset></details>
      </fieldset>

      <div class="modal-actions"><button type="button" class="secondary" :disabled="securitySaving" @click="closeSecurityDialog">{{t('关闭')}}</button><button class="primary" :disabled="securitySaving || securityPreviewPending || !securityImpact || !!securityPreviewError">{{securitySaving?t('正在保存并应用…'):t('保存并应用')}}</button></div>
    </form>
  </div>
</template>
