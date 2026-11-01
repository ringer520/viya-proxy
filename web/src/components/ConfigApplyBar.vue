<script setup lang="ts">
import {t} from '../i18n'
import {readOnly} from '../composables/useState'
import {configStatus,applying,useConfigStatusPolling,confirmPanelAccess} from '../composables/useConfigStatus'
useConfigStatusPolling()
</script>
<template><div v-if="configStatus.stage!=='applied'||configStatus.port_confirmation" class="config-status-bar" :class="configStatus.stage" role="status"><template v-if="configStatus.stage!=='applied'"><span :class="{spinning:applying}">{{applying?'◌':configStatus.stage==='failed'?'!':'✓'}}</span><div><strong>{{t(configStatus.message||'正在读取配置状态…')}}</strong><small v-if="configStatus.updated">{{new Date(configStatus.updated).toLocaleString()}}</small></div></template><div v-if="configStatus.port_confirmation" class="port-confirmation"><strong>{{t('请确认新端口可以访问')}}</strong><p>{{configStatus.port_confirmation.old_port}} → {{configStatus.port_confirmation.new_port}} · {{t('截止时间')}} {{new Date(configStatus.port_confirmation.deadline*1000).toLocaleTimeString()}} · {{t('超时将自动恢复旧端口')}}</p><button class="primary" :disabled="readOnly" @click="confirmPanelAccess">{{t('确认当前访问正常')}}</button></div></div></template>
