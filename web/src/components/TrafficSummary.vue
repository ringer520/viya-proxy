<script setup lang="ts">
import { computed } from 'vue'
import { Activity, Cable, ArrowUp, ArrowDown } from '@lucide/vue'
import AnimatedMetric from '../AnimatedMetric.vue'
import { webListTotals, forwardListTotals } from '../composables/useTraffic'
import { t } from '../i18n'
const props=defineProps<{kind:'web'|'forward'}>()
const total=computed(()=>props.kind==='web'?webListTotals.value:forwardListTotals.value)
</script>
<template><div class="traffic-summary-strip" :aria-label="t(kind==='web'?'Web 服务近 30 天统计':'端口转发近 30 天统计')"><span class="traffic-summary-scope"><Activity v-if="kind==='web'" :size="16"/><Cable v-else :size="16"/><span>{{t(kind==='web'?'Web 服务':'端口转发')}}<small>30d</small></span></span><span><small>{{t(kind==='web'?'请求数':'连接 / 会话')}}</small><strong><AnimatedMetric :value="total.requests"/></strong></span><span><small><ArrowUp :size="11"/>{{t('上行')}}</small><strong><AnimatedMetric :value="total.upload" format="bytes"/></strong></span><span><small><ArrowDown :size="11"/>{{t('下行')}}</small><strong><AnimatedMetric :value="total.download" format="bytes"/></strong></span></div></template>
