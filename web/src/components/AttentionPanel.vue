<script setup lang="ts">
import { t, tr } from "../i18n";
import { computed } from "vue";

// The overview's "what is broken right now" card. It is deliberately fed plain
// data shapes rather than the Host type so it can stay independent of App.vue.
const props = defineProps<{
  certificates: {
    domain: string;
    days_left: number;
    expired: boolean;
    sites: string[];
  }[];
  sites: { domain: string; target: string; error?: string }[];
}>();
// Reactive: the panel mounts on the first shell render, before the async load()
// has populated the lists, so a snapshot taken here would freeze at zero.
const total = computed(() => props.certificates.length + props.sites.length);
</script>

<template>
  <section
    v-if="total"
    class="panel attention-panel"
    :aria-label="t('需要关注')"
  >
    <div class="panel-head">
      <h3>{{ t("需要关注") }}</h3>
      <span class="attention-count">{{ total }}</span>
    </div>
    <ul class="attention-list">
      <li
        v-for="entry in sites"
        :key="'upstream-' + entry.domain"
        class="attention-item danger"
      >
        <strong>{{ entry.domain }}</strong>
        <span>{{ t("上游无响应") }} · {{ entry.target }}</span>
        <small v-if="entry.error">{{ entry.error }}</small>
      </li>
      <li
        v-for="item in certificates"
        :key="'cert-' + item.domain"
        class="attention-item"
        :class="item.expired ? 'danger' : 'warn'"
      >
        <strong>{{ item.domain }}</strong>
        <span>{{
          item.expired
            ? t("证书已过期")
            : tr`证书将在 ${item.days_left} 天后到期`
        }}</span>
        <small v-if="item.sites.length > 1">{{ item.sites.join("、") }}</small>
      </li>
    </ul>
  </section>
</template>
