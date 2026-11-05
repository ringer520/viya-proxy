<script setup lang="ts">
import { t } from "../i18n";
</script>

<template>
  <div class="page-loading" role="status" aria-live="polite">
    <span class="page-loading-dot" aria-hidden="true"></span>
    <span class="page-loading-dot" aria-hidden="true"></span>
    <span class="page-loading-dot" aria-hidden="true"></span>
    <small>{{ t("读取中…") }}</small>
  </div>
</template>

<style scoped>
.page-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 220px;
  color: var(--muted);
}
.page-loading small {
  margin-left: 6px;
  font-size: 12px;
}
.page-loading-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--accent);
  opacity: 0.35;
  animation: page-loading-pulse 1.1s ease-in-out infinite;
}
.page-loading-dot:nth-child(2) {
  animation-delay: 0.15s;
}
.page-loading-dot:nth-child(3) {
  animation-delay: 0.3s;
}
@keyframes page-loading-pulse {
  0%,
  100% {
    opacity: 0.25;
    transform: translateY(0);
  }
  50% {
    opacity: 1;
    transform: translateY(-3px);
  }
}
@media (prefers-reduced-motion: reduce) {
  .page-loading-dot {
    animation: none;
  }
}
</style>
