<script setup lang="ts">
import {language,setLanguage} from '../i18n'
</script>
<template><div class="language-picker" role="group" aria-label="Language / 语言"><button type="button" lang="zh-CN" :aria-pressed="language==='zh-CN'" :class="{selected:language==='zh-CN'}" @click="setLanguage('zh-CN')">简体中文</button><button type="button" lang="en" :aria-pressed="language==='en'" :class="{selected:language==='en'}" @click="setLanguage('en')">🌐 English</button></div></template>
