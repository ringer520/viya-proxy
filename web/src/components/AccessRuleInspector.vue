<script setup lang="ts">
import {ref} from 'vue'
import {apiFetch} from '../api'
import {hosts,readOnly} from '../composables/useState'
import {openBan} from '../composables/useNetwork'
import {t} from '../i18n'
import type{IPBan}from '../types'
const domain=ref(''),ip=ref(''),busy=ref(false),error=ref(''),result=ref<{ip:string;domain:string;matches:IPBan[];message:string}|null>(null)
async function inspect(){busy.value=true;error.value='';result.value=null;try{const r=await apiFetch('/api/v1/ip-bans/explain?'+new URLSearchParams({domain:domain.value,ip:ip.value}));const d=await r.json();if(!r.ok)throw Error(t(d.error));result.value=d}catch(e){error.value=e instanceof Error?t(e.message):t('检查失败')}finally{busy.value=false}}
</script>
<template><details class="op-disclosure"><summary>{{t('检查拦截原因')}}</summary><form class="op-toolbar" @submit.prevent="inspect"><label>{{t('站点')}}<select v-model="domain" required><option value="" disabled>{{t('选择站点')}}</option><option v-for="host in hosts" :key="host.id" :value="host.domain">{{host.domain}}</option></select></label><label>{{t('访问来源 IP')}}<input v-model="ip" :placeholder="t('留空检查当前来源')" maxlength="64"/></label><button class="secondary" :disabled="busy">{{busy?t('正在检查…'):t('检查规则')}}</button></form><p v-if="error" class="error" role="alert">{{error}}</p><section v-if="result" class="op-callout"><strong>{{result.domain}} · {{result.ip}}</strong><p>{{result.matches.length?t('命中以下规则'):t('未命中 IP 封禁或地区规则')}}</p><div v-for="rule in result.matches" :key="rule.id" class="op-match"><span>#{{rule.id}} · {{rule.ip==='foreign'?t('地区限制'):rule.ip}} · {{rule.reason||t('未填写原因')}}</span><button class="secondary" type="button" :disabled="readOnly" @click="openBan(rule)">{{t('查看规则')}}</button></div><small>{{t(result.message)}}</small></section></details></template>
