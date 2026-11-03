<script setup lang="ts">
import { computed } from 'vue'
import { themeChoice } from '../composables/usePreferences'
import { animeThemes } from '../animeThemes'
const active = computed(() => animeThemes.find(theme => theme.id === themeChoice.value))
</script>

<template>
  <div v-if="active" :key="active.id" class="anime-backdrop" aria-hidden="true">
    <div class="anime-wallpaper"/>
    <div class="anime-atmosphere"/>
    <div class="anime-particles"><i v-for="n in 10" :key="n" :style="{'--particle':n}"/></div>
    <div class="anime-signature"><span>VIYA COLLECTION / 0{{animeThemes.indexOf(active)+1}}</span><strong>{{active.subtitle}}</strong></div>
  </div>
</template>
