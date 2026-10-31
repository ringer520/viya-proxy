<script setup lang="ts">
import { t } from "../i18n";
import { formatBytes } from "../format";

// Period selector plus the per-site ranking for the selected window. The parent
// owns the fetch, this only renders and reports the chosen range.
const range = defineModel<"24h" | "7d" | "30d">("range", { required: true });
defineProps<{
  ranking: { id: number; domain: string; bytes: number; requests: number }[];
}>();
const options = [
  { value: "24h", label: t("24 小时") },
  { value: "7d", label: t("7 天") },
  { value: "30d", label: t("30 天") },
] as const;
</script>

<template>
  <div class="traffic-range-bar">
    <div class="range-switch" role="group" :aria-label="t('统计周期')">
      <button
        v-for="option in options"
        :key="option.value"
        type="button"
        class="chip"
        :class="{ selected: range === option.value }"
        :aria-pressed="range === option.value"
        @click="range = option.value"
      >
        {{ option.label }}
      </button>
    </div>
    <ol v-if="ranking.length" class="traffic-ranking">
      <li v-for="row in ranking" :key="row.id">
        <span>{{ row.domain }}</span
        ><strong>{{ formatBytes(row.bytes) }}</strong
        ><small>{{ row.requests.toLocaleString() }} {{ t("次") }}</small>
      </li>
    </ol>
    <p v-else class="traffic-ranking-empty">
      {{ t("所选周期内暂无流量记录") }}
    </p>
  </div>
</template>
