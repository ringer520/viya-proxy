<script setup lang="ts">
import { themeChoice } from '../composables/usePreferences'
</script>

<template>
  <svg class="theme-emblem" viewBox="0 0 32 32" fill="none" aria-hidden="true">
    <g v-if="themeChoice==='sakura'" fill="currentColor">
      <ellipse v-for="n in 5" :key="n" cx="16" cy="9" rx="4.5" ry="7" :transform="`rotate(${(n-1)*72} 16 16)`" opacity=".85"/>
      <circle cx="16" cy="16" r="3" fill="var(--nav-bg)"/>
    </g>
    <g v-else-if="themeChoice==='neon'" stroke="currentColor" stroke-width="1.5">
      <path d="M3 7h9l5 5h12M3 25h9l5-5h12M8 7v18M23 12v8"/>
      <circle cx="8" cy="7" r="2.5" fill="var(--nav-bg)"/><circle cx="23" cy="20" r="2.5" fill="var(--nav-bg)"/>
      <path d="m14 16 3-3 3 3-3 3z" fill="currentColor"/>
    </g>
    <g v-else-if="themeChoice==='lunar'" fill="currentColor">
      <path d="M21 3a13 13 0 1 0 8 20A12 12 0 0 1 21 3Z"/>
      <path d="m24 4 1.2 3.8L29 9l-3.8 1.2L24 14l-1.2-3.8L19 9l3.8-1.2Z" opacity=".85"/>
    </g>
  </svg>
</template>
