<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { ScrollText, Terminal, Settings, RefreshCw, ShieldCheck } from '@lucide/vue'
import AccessLogTable from './AccessLogTable.vue'
import { t } from '../i18n'

interface AuditEntry {
  id: number
  username: string
  ip: string
  user_agent: string
  result: string
  reason: string
  created_at: number
}

const props = defineProps<{ csrf: string; autoRefresh: boolean }>()
const tab = ref('access'),
  events = ref<Record<string, any>[]>([]),
  program = ref<Record<string, any>[]>([]),
  audit = ref<AuditEntry[]>([]),
  error = ref(''),
  message = ref(''),
  busy = ref(false),
  loading = ref(false),
  filter = ref('ALL'),
  auditFilter = ref('ALL')
const settings = ref({ level: 'INFO', access_mb: 100, program_mb: 20, backups: 5, days: 30 })
const visibleProgram = computed(() =>
  program.value.filter((e) => filter.value === 'ALL' || String(e.level).toUpperCase() === filter.value),
)
const visibleAudit = computed(() =>
  audit.value.filter((e) => auditFilter.value === 'ALL' || e.result === auditFilter.value),
)
const tabs = computed(() => [
  { id: 'access', name: t('访问日志'), icon: ScrollText },
  { id: 'program', name: t('程序日志'), icon: Terminal },
  { id: 'audit', name: t('登录审计'), icon: ShieldCheck },
  { id: 'settings', name: t('日志配置'), icon: Settings },
])
let sequence = 0

function endpoint(which: string) {
  if (which === 'program') return '/api/v1/logs/program'
  if (which === 'audit') return '/api/v1/auth/audit?limit=200'
  return '/api/v1/logs'
}

async function load() {
  if (loading.value || tab.value === 'settings') return
  loading.value = true
  const selected = tab.value,
    current = ++sequence
  try {
    const r = await fetch(endpoint(selected))
    if (!r.ok) throw Error(t('读取失败'))
    const body = await r.json()
    if (current !== sequence || selected !== tab.value) return
    if (selected === 'program') program.value = body
    else if (selected === 'audit') audit.value = Array.isArray(body) ? body : []
    else events.value = body
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t('读取失败')
  } finally {
    if (current === sequence) loading.value = false
  }
}

async function save() {
  if (busy.value) return
  busy.value = true
  message.value = ''
  error.value = ''
  try {
    const r = await fetch('/api/v1/logs/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrf },
      body: JSON.stringify(settings.value),
    })
    const body = await r.json()
    if (!r.ok) throw Error(t(body.error))
    settings.value = body
    message.value = t('日志配置已保存并生效。')
  } catch (e) {
    error.value = e instanceof Error ? t(e.message) : t('保存失败')
  } finally {
    busy.value = false
  }
}

async function select(value: string) {
  ++sequence
  loading.value = false
  tab.value = value
  error.value = ''
  message.value = ''
  if (value === 'settings') {
    try {
      const r = await fetch('/api/v1/logs/settings')
      if (!r.ok) throw Error(t('读取失败'))
      settings.value = await r.json()
    } catch {
      error.value = t('读取设置失败')
    }
  } else void load()
}

function resultLabel(result: string) {
  if (result === 'success') return t('成功')
  if (result === 'blocked') return t('已拦截')
  return t('失败')
}

function reasonLabel(entry: AuditEntry) {
  switch (entry.reason) {
    case 'password':
      return entry.result === 'success' ? t('密码登录') : t('密码错误')
    case 'unknown_user':
      return t('账户不存在')
    case 'rate_limited':
      return t('尝试过于频繁')
    case 'setup':
      return t('首次初始化')
    case 'logout':
      return t('退出登录')
    case 'password_changed':
      return t('修改密码')
    case 'sessions_revoked':
      return t('吊销其他会话')
    default:
      return entry.reason || '—'
  }
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  timer = setInterval(() => {
    if (props.autoRefresh) void load()
  }, 5000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <section class="panel log-workspace">
    <header class="panel-head">
      <h2>{{ t('日志') }}</h2>
      <button v-if="tab !== 'settings'" class="secondary" :disabled="loading" @click="load">
        <RefreshCw :size="15" :class="{ spinning: loading }" />{{ t('刷新') }}
      </button>
    </header>
    <nav class="log-tabs" role="tablist" :aria-label="t('日志分类')">
      <button
        v-for="item in tabs"
        :key="item.id"
        role="tab"
        :aria-selected="tab === item.id"
        :class="{ selected: tab === item.id }"
        @click="select(item.id)"
      >
        <component :is="item.icon" :size="16" />{{ item.name }}
      </button>
    </nav>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="message" class="probe-ok" role="status">{{ message }}</p>

    <div v-if="tab === 'access'" class="log-content">
      <p class="auth-hint">{{ t('实时访问记录，已隐藏请求凭据与查询参数。') }}</p>
      <AccessLogTable :events="events" :loading="loading" :limit="200" show-host />
    </div>

    <div v-else-if="tab === 'program'" class="log-content">
      <div class="program-toolbar">
        <span>{{ t('Viya 与 Caddy 程序日志') }}</span>
        <select v-model="filter" :aria-label="t('日志级别筛选')">
          <option value="ALL">{{ t('全部级别') }}</option>
          <option v-for="level in ['DEBUG', 'INFO', 'WARN', 'ERROR']" :key="level">{{ level }}</option>
        </select>
      </div>
      <div v-if="visibleProgram.length" class="program-log-list">
        <article v-for="(event, index) in visibleProgram" :key="index" class="program-log-row">
          <time>{{ new Date(event.time).toLocaleString() }}</time>
          <b class="log-level" :class="String(event.level).toLowerCase()">{{ event.level }}</b>
          <span>{{ event.source || 'viya' }}</span>
          <p>{{ event.msg }}<small v-if="event.error">{{ event.error }}</small></p>
        </article>
      </div>
      <p v-else class="revision-empty">{{ t('暂无程序日志') }}</p>
    </div>

    <div v-else-if="tab === 'audit'" class="log-content">
      <div class="program-toolbar">
        <span>{{ t('记录管理员登录的成败、来源 IP 与客户端，保留最近 200 条 / 90 天。') }}</span>
        <select v-model="auditFilter" :aria-label="t('登录结果筛选')">
          <option value="ALL">{{ t('全部结果') }}</option>
          <option value="success">{{ t('成功') }}</option>
          <option value="failure">{{ t('失败') }}</option>
          <option value="blocked">{{ t('已拦截') }}</option>
        </select>
      </div>
      <div v-if="visibleAudit.length" class="audit-list">
        <article v-for="entry in visibleAudit" :key="entry.id" class="audit-row">
          <time>{{ new Date(entry.created_at * 1000).toLocaleString() }}</time>
          <b class="audit-result" :class="entry.result">{{ resultLabel(entry.result) }}</b>
          <span class="audit-user">{{ entry.username || '—' }}</span>
          <code class="audit-ip">{{ entry.ip || '—' }}</code>
          <span class="audit-reason">{{ reasonLabel(entry) }}</span>
          <span class="audit-agent" :title="entry.user_agent">{{ entry.user_agent || '—' }}</span>
        </article>
      </div>
      <p v-else-if="loading" class="site-log-empty">{{ t('正在读取登录审计…') }}</p>
      <p v-else class="revision-empty">{{ t('暂无登录审计记录') }}</p>
    </div>

    <form v-else class="log-config-form" @submit.prevent="save">
      <label
        >{{ t('程序日志级别') }}<select v-model="settings.level">
          <option v-for="level in ['DEBUG', 'INFO', 'WARN', 'ERROR']" :key="level">{{ level }}</option>
        </select></label
      ><label
        >{{ t('访问日志单文件上限（MiB）') }}<input
          v-model.number="settings.access_mb"
          type="number"
          min="1"
          max="1024"
          required /></label
      ><label
        >{{ t('程序日志单文件上限（MiB）') }}<input
          v-model.number="settings.program_mb"
          type="number"
          min="1"
          max="1024"
          required /></label
      ><label
        >{{ t('保留归档文件数') }}<input
          v-model.number="settings.backups"
          type="number"
          min="1"
          max="30"
          required /></label
      ><label
        >{{ t('归档保留天数') }}<input v-model.number="settings.days" type="number" min="1" max="365" required
      /></label>
      <p class="auth-hint">
        {{
          t(
            '访问日志保持完整记录，避免影响流量统计。级别控制程序日志；大小限制分别作用于每个日志文件，达到上限自动轮转并压缩。',
          )
        }}
      </p>
      <button class="primary" :disabled="busy">{{ busy ? t('应用中…') : t('保存并应用') }}</button>
    </form>
  </section>
</template>

<style scoped>
.audit-list {
  border: 1px solid var(--line);
  border-radius: 12px;
  overflow: hidden;
}
.audit-row {
  display: grid;
  grid-template-columns: 148px 62px minmax(80px, 130px) minmax(96px, 140px) minmax(84px, 130px) minmax(0, 1fr);
  gap: 12px;
  align-items: center;
  padding: 11px 12px;
  border-bottom: 1px solid var(--line);
  font-size: 11px;
}
.audit-row:last-child {
  border-bottom: 0;
}
.audit-row time {
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}
.audit-result {
  justify-self: start;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.02em;
  border: 1px solid transparent;
}
.audit-result.success {
  color: color-mix(in srgb, var(--accent) 82%, var(--text));
  background: color-mix(in srgb, var(--accent) 14%, transparent);
  border-color: color-mix(in srgb, var(--accent) 32%, transparent);
}
.audit-result.failure {
  color: #e11d48;
  background: #e11d4814;
  border-color: #e11d4833;
}
.audit-result.blocked {
  color: #d97706;
  background: #d9770614;
  border-color: #d9770633;
}
.audit-user {
  color: var(--text);
  font-weight: 650;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.audit-ip {
  color: var(--muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.audit-reason {
  color: var(--text);
}
.audit-agent {
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
@media (max-width: 900px) {
  .audit-row {
    grid-template-columns: 1fr auto;
    gap: 4px 10px;
    align-items: baseline;
  }
  .audit-row time {
    grid-column: 1;
  }
  .audit-result {
    grid-column: 2;
    justify-self: end;
  }
  .audit-user,
  .audit-ip,
  .audit-reason,
  .audit-agent {
    grid-column: 1 / -1;
  }
  .audit-agent {
    font-size: 10px;
  }
}
</style>
