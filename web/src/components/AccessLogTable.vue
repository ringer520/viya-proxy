<script setup lang="ts">
import {t} from '../i18n'

defineProps<{events:Record<string,any>[];loading?:boolean;error?:string;compact?:boolean;limit?:number;showHost?:boolean}>()
function bytes(n:number=0){if(n<1024)return `${n} B`;const units=['KiB','MiB','GiB','TiB'];let i=-1;do{n/=1024;i++}while(n>=1024&&i<3);return `${n.toFixed(1)} ${units[i]}`}
</script>
<template>
  <p v-if="loading && !events.length" class="site-log-empty">{{t('正在读取访问日志…')}}</p>
  <p v-else-if="error" class="site-log-empty">{{ error }}</p>
  <p v-else-if="!events.length" class="site-log-empty">{{t('暂无该站点的访问记录。收到请求后约 2 秒更新。')}}</p>
  <div v-else class="site-log-table-scroll"><table class="site-log-table"><thead><tr><th>{{t('时间 / 来源 IP')}}</th><th>{{t('请求')}}</th><th>{{t('状态')}}</th><th>{{t('上行 / 下行')}}</th><th>{{t('耗时')}}</th></tr></thead><tbody><tr v-for="(event,index) in events.slice(0,compact?Math.min(10,Math.max(1,limit||5)):Math.min(200,Math.max(1,limit||50)))" :key="index"><td>{{ new Date(event.ts*1000).toLocaleString() }}<small>{{ event.request?.client_ip || event.request?.remote_ip || '—' }}</small></td><td><b>{{ event.request?.method||'—' }}</b><small v-if="showHost">{{event.request?.host||'—'}}</small><div class="log-path" :title="event.request?.uri">{{ event.request?.uri||'/' }}</div></td><td><em class="http-status" :class="{failed:event.status>=400}">{{ event.status }}</em></td><td>{{ bytes(event.bytes_read) }}<small>{{ bytes(event.size) }}</small></td><td>{{ ((event.duration||0)*1000).toFixed(1) }} ms</td></tr></tbody></table></div>
</template>
