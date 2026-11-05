<script setup lang="ts">
import {computed,ref} from "vue";
import AccessRuleInspector from "./AccessRuleInspector.vue";
import { Ban, Pencil, ShieldCheck } from "@lucide/vue";
import { t } from "../i18n";

import { openBan, removeBan, visibleBans } from "../composables/useNetwork";
const kind=ref('manual');const counts=computed(()=>({manual:visibleBans.value.filter(b=>b.ip!=='foreign'&&b.source==='manual').length,region:visibleBans.value.filter(b=>b.ip==='foreign').length,auto:visibleBans.value.filter(b=>b.ip!=='foreign'&&b.source!=='manual').length}));const displayBans=computed(()=>visibleBans.value.filter(b=>kind.value==='region'?b.ip==='foreign':kind.value==='manual'?b.ip!=='foreign'&&b.source==='manual':b.ip!=='foreign'&&b.source!=='manual'));
</script>

<template>
      <section class="panel provider-panel">
        <div class="panel-head">
          <div>
            <h2>{{ t("IP 封禁") }}</h2>
            <p>
              {{
                t(
                  "手动封禁 IP 或 CIDR 网段，长期有效，需手动解除。自动防护仍会封禁重复触发 400/401/403 的来源。",
                )
              }}
            </p>
          </div>
        </div>
        <nav class="op-tabs" role="tablist" :aria-label="t('访问规则分类')"><button v-for="item in [{id:'manual',name:'手动 IP 封禁'},{id:'region',name:'地区访问限制'},{id:'auto',name:'自动封禁'}]" :key="item.id" class="secondary" role="tab" :aria-selected="kind===item.id" @click="kind=item.id">{{t(item.name)}}<b>{{counts[item.id as keyof typeof counts]}}</b></button></nav>
<AccessRuleInspector/>
<div v-if="displayBans.length" class="host-list">
          <div
            v-for="ban in displayBans"
            :key="ban.id"
            role="group"
            :aria-label="ban.ip"
            class="host-row"
          >
            <div class="host-icon"><ShieldCheck :size="17" /></div>
            <div class="host-main">
              <strong
                >{{ ban.ip === "foreign" ? t("禁止境外 IP") : ban.ip }} ·
                {{ ban.domain }}</strong
              ><span
                >{{ ban.source === "manual" ? t("手动") : t("自动") }} ·
                {{ ban.reason || t("未填写原因") }}
                {{
                  ban.expires_at
                    ? t("· 到期 ") +
                      new Date(ban.expires_at * 1000).toLocaleString()
                    : t("· 永久封禁")
                }}</span
              >
            </div>
            <button class="secondary" @click="openBan(ban)">
              <Pencil :size="14" />{{ t("编辑") }}</button
            ><button class="secondary" @click="removeBan(ban.id)">
              {{ t("解除封禁") }}
            </button>
          </div>
        </div>
        <div v-else class="empty">
          <div class="empty-icon"><Ban :size="23" /></div>
          <strong>{{
            t("此分类没有规则")
          }}</strong
          ><span>{{ t("添加封禁规则，阻止恶意来源访问站点。") }}</span>
        </div>
      </section>
</template>
