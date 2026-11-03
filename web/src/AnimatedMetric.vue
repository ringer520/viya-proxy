<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue'
import { formatBytes } from './format'
import { reducedMotion } from './composables/useMotion'
const props = withDefaults(defineProps<{ value: number; format?: 'number' | 'bytes' | 'decimal' }>(), { format: 'number' })
// Show the real first sample immediately; animate subsequent changes only.
const displayed = ref(Number.isFinite(props.value) ? props.value : 0)
let frame = 0
watch([() => props.value, reducedMotion], ([rawValue, reduced]) => {
  cancelAnimationFrame(frame)
  const value = Number.isFinite(rawValue) ? rawValue : 0
  if (reduced || document.hidden) { displayed.value = value; return }
  if (displayed.value === value) return
  const from = displayed.value, start = performance.now()
  const tick = (now: number) => {
    const progress = Math.min(1, (now - start) / 480)
    displayed.value = from + (value - from) * (1 - Math.pow(1 - progress, 3))
    if (progress < 1) frame = requestAnimationFrame(tick)
  }
  frame = requestAnimationFrame(tick)
}, { immediate: true })
onBeforeUnmount(() => cancelAnimationFrame(frame))
</script>
<template><span class="animated-metric">{{ format === 'bytes' ? formatBytes(displayed) : format === 'decimal' ? displayed.toFixed(1) : Math.round(displayed).toLocaleString() }}</span></template>
