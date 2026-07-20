<template>
  <div class="health-shell page-shell">
    <div class="health-shell__glow health-shell__glow--one" />
    <div class="health-shell__glow health-shell__glow--two" />

    <v-card class="health-hero glass-card" elevation="0">
      <v-row align="center">
        <v-col cols="12" md="8">
          <div class="health-hero__badges">
            <span class="health-badge">系统诊断</span>
            <span class="health-badge health-badge--soft">{{ statusLabel }}</span>
          </div>
          <div class="health-hero__title-row">
            <div class="health-hero__icon"><v-icon icon="mdi-heart-pulse" size="30" /></div>
            <div>
              <h1>健康与诊断</h1>
              <p>集中检查核心、数据库、端口规则、TLS、订阅服务和 MASQUE 运行状态。</p>
            </div>
          </div>
          <div class="health-hero__meta">
            <span>检查时间：{{ checkedAt }}</span>
            <span>耗时：{{ report?.durationMs ?? 0 }} ms</span>
          </div>
        </v-col>
        <v-col cols="12" md="4" class="health-hero__actions">
          <v-btn variant="tonal" prepend-icon="mdi-content-copy" :disabled="!report" @click="copyReport">复制报告</v-btn>
          <v-btn color="primary" prepend-icon="mdi-radar" :loading="loading" @click="loadHealth(true)">深度检查</v-btn>
        </v-col>
      </v-row>
    </v-card>

    <v-row class="health-summary">
      <v-col v-for="item in summaryCards" :key="item.key" cols="6" sm="3">
        <v-card class="health-summary__card glass-card" elevation="0">
          <div class="health-summary__icon" :class="`health-summary__icon--${item.key}`"><v-icon :icon="item.icon" /></div>
          <div>
            <div class="health-summary__value">{{ item.value }}</div>
            <div class="health-summary__label">{{ item.label }}</div>
          </div>
        </v-card>
      </v-col>
    </v-row>

    <v-alert v-if="report?.status === 'critical'" type="error" variant="tonal" title="存在需要处理的问题" text="建议先处理红色检查项，再修改配置或执行批量更新。" />

    <section class="health-grid">
      <v-card v-for="check in report?.checks ?? []" :key="check.id" class="health-check glass-card" :class="`health-check--${check.status}`" elevation="0">
        <div class="health-check__status"><v-icon :icon="statusIcon(check.status)" :color="statusColor(check.status)" size="24" /></div>
        <div class="health-check__body">
          <div class="health-check__heading">
            <h2>{{ check.title }}</h2>
            <v-chip size="small" :color="statusColor(check.status)" variant="tonal">{{ checkStatusLabel(check.status) }}</v-chip>
          </div>
          <p class="health-check__summary">{{ check.summary }}</p>
          <p v-if="check.detail" class="health-check__detail">{{ check.detail }}</p>
        </div>
        <v-btn v-if="check.action" size="small" variant="text" append-icon="mdi-arrow-right" @click="handleAction(check.action)">处理</v-btn>
      </v-card>
    </section>

    <v-card class="health-diagnostics glass-card" elevation="0">
      <div class="health-diagnostics__header">
        <div>
          <h2>诊断说明</h2>
          <p>深度检查会绕过状态缓存，重新读取数据库、磁盘、监听端口和 NAT 规则。</p>
        </div>
        <v-btn variant="tonal" prepend-icon="mdi-lan-check" :loading="reconciling" @click="reconcilePorts">重建端口规则</v-btn>
      </div>
    </v-card>

    <v-card class="alert-settings glass-card" elevation="0">
      <div class="alert-settings__header">
        <div>
          <div class="alert-settings__eyebrow">主动通知</div>
          <h2>告警通知</h2>
          <p>通过 Telegram 推送健康异常，仅在状态变化或冷却时间到期后发送，避免重复轰炸。</p>
        </div>
        <v-switch v-model="alerts.enabled" color="primary" label="启用告警" hide-details inset />
      </div>
      <v-row class="mt-2">
        <v-col cols="12" md="7">
          <v-text-field v-model="alerts.telegramToken" :label="alerts.telegramTokenSet ? 'Telegram Bot Token（已配置，留空不修改）' : 'Telegram Bot Token'" prepend-inner-icon="mdi-send-check-outline" type="password" autocomplete="new-password" />
        </v-col>
        <v-col cols="12" md="5">
          <v-text-field v-model="alerts.telegramChatId" label="Telegram Chat ID" prepend-inner-icon="mdi-account-tie" />
        </v-col>
        <v-col cols="6" md="3">
          <v-number-input v-model="alerts.intervalMinutes" label="检查间隔（分钟）" :min="1" :max="1440" control-variant="stacked" />
        </v-col>
        <v-col cols="6" md="3">
          <v-number-input v-model="alerts.cooldownMinutes" label="重复冷却（分钟）" :min="1" :max="10080" control-variant="stacked" />
        </v-col>
        <v-col cols="12" md="6" class="alert-settings__actions">
          <v-btn variant="tonal" prepend-icon="mdi-send-check-outline" :loading="testingAlert" @click="testAlert">测试通知</v-btn>
          <v-btn color="primary" prepend-icon="mdi-content-save-outline" :loading="savingAlerts" @click="saveAlerts">保存告警</v-btn>
        </v-col>
      </v-row>
    </v-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { push } from 'notivue'
import HttpUtils from '@/plugins/httputil'

interface HealthCheck { id: string; title: string; status: string; summary: string; detail?: string; action?: string }
interface HealthReport { status: string; checkedAt: string; durationMs: number; summary: Record<string, number>; checks: HealthCheck[]; diagnostics: Record<string, unknown> }
interface AlertSettings { enabled: boolean; telegramToken: string; telegramTokenSet: boolean; telegramChatId: string; intervalMinutes: number; cooldownMinutes: number }

const router = useRouter()
const loading = ref(false)
const reconciling = ref(false)
const report = ref<HealthReport | null>(null)
const savingAlerts = ref(false)
const testingAlert = ref(false)
const alerts = ref<AlertSettings>({ enabled: false, telegramToken: '', telegramTokenSet: false, telegramChatId: '', intervalMinutes: 5, cooldownMinutes: 60 })

const statusLabel = computed(() => ({ healthy: '全部正常', warning: '需要关注', critical: '发现异常' } as Record<string, string>)[report.value?.status ?? ''] ?? '等待检查')
const checkedAt = computed(() => report.value?.checkedAt ? new Date(report.value.checkedAt).toLocaleString() : '-')
const summaryCards = computed(() => [
  { key: 'ok', label: '正常', value: report.value?.summary?.ok ?? 0, icon: 'mdi-check-circle-outline' },
  { key: 'warning', label: '警告', value: report.value?.summary?.warning ?? 0, icon: 'mdi-alert-outline' },
  { key: 'error', label: '异常', value: report.value?.summary?.error ?? 0, icon: 'mdi-close-circle-outline' },
  { key: 'info', label: '信息', value: report.value?.summary?.info ?? 0, icon: 'mdi-information-outline' },
])

const loadHealth = async (force = false) => {
  loading.value = true
  const msg = await HttpUtils.get('api/health', force ? { force: 1 } : {})
  loading.value = false
  if (msg.success) report.value = msg.obj as HealthReport
}

const reconcilePorts = async () => {
  reconciling.value = true
  const msg = await HttpUtils.post('api/reconcilePorts', {})
  reconciling.value = false
  if (msg.success) await loadHealth(true)
}

const loadAlerts = async () => {
  const msg = await HttpUtils.get('api/alert-settings')
  if (msg.success && msg.obj) alerts.value = { ...alerts.value, ...(msg.obj as AlertSettings), telegramToken: '' }
}

const saveAlerts = async () => {
  savingAlerts.value = true
  const msg = await HttpUtils.post('api/alertSave', { data: JSON.stringify(alerts.value) })
  savingAlerts.value = false
  if (msg.success) await loadAlerts()
}

const testAlert = async () => {
  testingAlert.value = true
  const msg = await HttpUtils.post('api/alertTest', {})
  testingAlert.value = false
  if (msg.success) push.success({ message: '测试通知已发送' })
}

const copyReport = async () => {
  if (!report.value) return
  try {
    await navigator.clipboard.writeText(JSON.stringify(report.value, null, 2))
    push.success({ message: '诊断报告已复制' })
  } catch {
    push.error({ message: '复制失败，请检查浏览器剪贴板权限' })
  }
}

const handleAction = async (action: string) => {
  const routes: Record<string, string> = { ports: '/ports', settings: '/settings', endpoints: '/endpoints', admins: '/admins', fleet: '/fleet' }
  if (action === 'reconcile-ports') return reconcilePorts()
  if (action === 'restart-core') {
    const msg = await HttpUtils.post('api/restartSb', {})
    if (msg.success) window.setTimeout(() => loadHealth(true), 1200)
    return
  }
  if (routes[action]) await router.push(routes[action])
}

const statusColor = (status: string) => ({ ok: 'success', warning: 'warning', error: 'error', info: 'info' } as Record<string, string>)[status] ?? 'info'
const statusIcon = (status: string) => ({ ok: 'mdi-check-circle', warning: 'mdi-alert', error: 'mdi-close-circle', info: 'mdi-information' } as Record<string, string>)[status] ?? 'mdi-information'
const checkStatusLabel = (status: string) => ({ ok: '正常', warning: '警告', error: '异常', info: '信息' } as Record<string, string>)[status] ?? status

onMounted(() => {
  loadHealth(false)
  loadAlerts()
})
</script>

<style scoped>
.health-shell { position: relative; isolation: isolate; }
.health-shell__glow { position: fixed; width: 360px; height: 360px; border-radius: 50%; filter: blur(90px); opacity: .18; pointer-events: none; z-index: -1; }
.health-shell__glow--one { top: 12%; right: 4%; background: #0a84ff; }
.health-shell__glow--two { bottom: 2%; left: 14%; background: #14b8a6; }
.glass-card { border: 1px solid var(--np-border); background: var(--np-surface-strong); backdrop-filter: blur(24px) saturate(1.18); box-shadow: var(--np-shadow-soft); }
.health-hero { padding: clamp(22px, 4vw, 38px); border-radius: 30px; }
.health-hero__badges, .health-hero__meta, .health-hero__actions { display: flex; flex-wrap: wrap; gap: 10px; }
.health-badge { padding: 6px 12px; border-radius: 999px; color: rgb(var(--v-theme-primary)); background: rgba(10,132,255,.1); font-size: 12px; font-weight: 700; letter-spacing: .08em; }
.health-badge--soft { color: var(--np-text-muted); background: var(--np-surface-muted); letter-spacing: 0; }
.health-hero__title-row { display: flex; align-items: flex-start; gap: 16px; margin-top: 22px; }
.health-hero__icon { width: 56px; height: 56px; border-radius: 18px; display: grid; place-items: center; color: rgb(var(--v-theme-primary)); background: linear-gradient(145deg, rgba(10,132,255,.18), rgba(20,184,166,.08)); flex: 0 0 auto; }
.health-hero h1 { margin: 0; font-size: clamp(30px, 4vw, 44px); line-height: 1.05; letter-spacing: -.04em; }
.health-hero p { margin: 12px 0 0; color: var(--np-text-muted); line-height: 1.7; }
.health-hero__meta { margin-top: 18px; color: var(--np-text-muted); font-size: 13px; }
.health-hero__actions { justify-content: flex-end; }
.health-summary { margin-top: 4px; }
.health-summary__card { min-height: 112px; padding: 18px; border-radius: 22px; display: flex; align-items: center; gap: 16px; }
.health-summary__icon { width: 42px; height: 42px; border-radius: 14px; display: grid; place-items: center; background: var(--np-surface-muted); }
.health-summary__icon--ok { color: rgb(var(--v-theme-success)); }
.health-summary__icon--warning { color: rgb(var(--v-theme-warning)); }
.health-summary__icon--error { color: rgb(var(--v-theme-error)); }
.health-summary__icon--info { color: rgb(var(--v-theme-info)); }
.health-summary__value { font-size: 30px; font-weight: 800; line-height: 1; }
.health-summary__label { margin-top: 8px; color: var(--np-text-muted); font-size: 13px; }
.health-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
.health-check { min-height: 142px; padding: 20px; border-radius: 24px; display: flex; align-items: flex-start; gap: 14px; position: relative; overflow: hidden; }
.health-check::before { content: ''; position: absolute; inset: 0 auto 0 0; width: 4px; background: rgb(var(--v-theme-info)); }
.health-check--ok::before { background: rgb(var(--v-theme-success)); }
.health-check--warning::before { background: rgb(var(--v-theme-warning)); }
.health-check--error::before { background: rgb(var(--v-theme-error)); }
.health-check__body { flex: 1; min-width: 0; }
.health-check__heading { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.health-check h2, .health-diagnostics h2 { margin: 0; font-size: 17px; }
.health-check__summary { margin: 12px 0 0; font-weight: 650; }
.health-check__detail { margin: 7px 0 0; color: var(--np-text-muted); font-size: 13px; line-height: 1.5; overflow-wrap: anywhere; }
.health-diagnostics { padding: 22px; border-radius: 24px; }
.health-diagnostics__header { display: flex; align-items: center; justify-content: space-between; gap: 18px; }
.health-diagnostics p { margin: 8px 0 0; color: var(--np-text-muted); }
.alert-settings { margin-top: 14px; padding: 24px; border-radius: 24px; }
.alert-settings__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; }
.alert-settings__header h2 { margin: 3px 0 0; font-size: 20px; }
.alert-settings__header p { margin: 8px 0 0; color: var(--np-text-muted); }
.alert-settings__eyebrow { color: rgb(var(--v-theme-primary)); font-size: 11px; font-weight: 800; letter-spacing: .12em; text-transform: uppercase; }
.alert-settings__actions { display: flex; align-items: center; justify-content: flex-end; gap: 10px; }
@media (max-width: 959px) { .health-hero__actions { justify-content: flex-start; } .health-grid { grid-template-columns: 1fr; } }
@media (max-width: 599px) { .health-hero { border-radius: 24px; } .health-hero__title-row { align-items: center; } .health-hero__icon { width: 48px; height: 48px; } .health-summary__card { min-height: 96px; padding: 14px; gap: 10px; } .health-summary__icon { width: 36px; height: 36px; } .health-summary__value { font-size: 24px; } .health-check { flex-wrap: wrap; } .health-diagnostics__header, .alert-settings__header { align-items: stretch; flex-direction: column; } .alert-settings__actions { justify-content: stretch; } .alert-settings__actions .v-btn { flex: 1; } }
</style>
