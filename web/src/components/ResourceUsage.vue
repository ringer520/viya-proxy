<script setup lang="ts">
import {computed} from 'vue'
import {t} from '../i18n'
import {hosts} from '../composables/useState'
import {openSiteDetails,providerUsage,certificateUsage} from '../composables/useSiteWorkspace'
const props=defineProps<{kind:'provider'|'certificate';id?:number;domain?:string}>()
const usages=computed(()=>props.kind==='provider'?providerUsage(props.id!):certificateUsage(props.domain||'',props.id))
</script>
<template><details class="resource-usage"><summary>{{t('关联使用')}} · {{usages.length}}</summary><p v-if="!usages.length">{{t('暂无关联对象')}}</p><ul><li v-for="usage in usages" :key="usage"><button v-if="hosts.some(h=>h.domain===(usage.startsWith(t('站点')+': ')?usage.slice((t('站点')+': ').length):usage))" class="inline-action" @click="openSiteDetails(hosts.find(h=>h.domain===(usage.startsWith(t('站点')+': ')?usage.slice((t('站点')+': ').length):usage))!)">{{usage}}</button><span v-else>{{usage}}</span></li></ul></details></template>
