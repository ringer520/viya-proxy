<script setup lang="ts">
import { computed } from 'vue'
import { themeChoice } from '../composables/usePreferences'
import { animeThemes } from '../animeThemes'
import { t } from '../i18n'
const active = computed(() => animeThemes.find(theme => theme.id === themeChoice.value))
</script>

<template>
  <div v-if="active" :key="active.id" class="anime-backdrop" aria-hidden="true">
    <div class="anime-wallpaper"/>
    <div class="anime-atmosphere"/>
    <div class="anime-particles"><i v-for="n in 12" :key="n" :style="{'--particle':n}"/></div>
    <div class="anime-signature"><i/><strong>{{ active.subtitle }}</strong><small>{{ t(active.label) }}</small></div>
  </div>
</template>
