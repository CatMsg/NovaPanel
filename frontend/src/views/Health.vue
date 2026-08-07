<template>
  <div class="health-shell page-shell">
    <div class="health-shell__glow health-shell__glow--one" />
    <div class="health-shell__glow health-shell__glow--two" />

    <v-card class="health-hero glass-card" elevation="0">
      <v-row align="center">
        <v-col cols="12" md="8">
          <div class="health-hero__badges">
            <span class="health-badge">{{ $t('ui.health.badge') }}</span>
            <span class="health-badge health-badge--soft">{{ statusLabel }}</span>
          </div>
          <div class="health-hero__title-row">
            <div class="health-hero__icon"><v-icon icon="mdi-heart-pulse" size="30" /></div>
            <div>
              <h1>{{ $t('ui.health.title') }}</h1>
              <p>{{ $t('ui.health.subtitle') }}</p>
            </div>
          </div>
          <div class="health-hero__meta">
            <span>{{ $t('ui.health.checkedAt', { time: checkedAt }) }}</span>
            <span>{{ $t('ui.health.duration', { value: report?.durationMs ?? 0 }) }}</span>
          </div>
        </v-col>
        <v-col cols="12" md="4" class="health-hero__actions">
          <v-btn variant="tonal" prepend-icon="mdi-content-copy" :disabled="!report" @click="copyReport">{{ $t('ui.health.copyReport') }}</v-btn>
          <v-btn color="primary" prepend-icon="mdi-radar" :loading="loading" @click="loadHealth(true)">{{ $t('ui.health.deepCheck') }}</v-btn>
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

    <v-alert v-if="report?.status === 'critical'" type="error" variant="tonal" :title="$t('ui.health.criticalTitle')" :text="$t('ui.health.criticalText')" />

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
        <v-btn v-if="check.action" size="small" variant="text" append-icon="mdi-arrow-right" @click="handleAction(check.action)">{{ $t('ui.health.action') }}</v-btn>
      </v-card>
    </section>

    <v-card class="health-diagnostics glass-card" elevation="0">
      <div class="health-diagnostics__header">
        <div>
          <h2>{{ $t('ui.health.diagnostics') }}</h2>
          <p>{{ $t('ui.health.diagnosticsHint') }}</p>
        </div>
        <v-btn variant="tonal" prepend-icon="mdi-lan-check" :loading="reconciling" @click="reconcilePorts">{{ $t('ui.health.rebuildAll') }}</v-btn>
      </div>
      <div v-if="portIssues.length" class="port-issues">
        <div v-for="issue in portIssues" :key="issue.id" class="port-issue">
          <div class="port-issue__icon">
            <v-icon :icon="portIssueIcon(issue.severity)" :color="portIssueColor(issue.severity)" />
          </div>
          <div class="port-issue__body">
            <div class="port-issue__heading">
              <strong>{{ portIssueLabel(issue.type) }}</strong>
              <v-chip v-if="issue.owner_tag" size="x-small" variant="tonal">{{ portOwnerLabel(issue.scope) }} · {{ issue.owner_tag }}</v-chip>
            </div>
            <span>{{ issue.detail }}</span>
            <small v-if="issue.port">{{ issue.family || $t('ui.health.allFamilies') }} · {{ issue.protocol?.toUpperCase() }} {{ issue.port }} → {{ issue.to_ports || '-' }}</small>
          </div>
          <v-btn
            v-if="issue.repairable"
            size="small"
            color="primary"
            variant="tonal"
            :loading="repairingIssueId === issue.id"
            :disabled="repairingIssueId !== '' || reconciling"
            @click="repairPortIssue(issue)"
          >
            {{ $t('ui.health.repairItem') }}
          </v-btn>
          <v-chip v-else size="small" color="secondary" variant="tonal">{{ $t('ui.health.manual') }}</v-chip>
        </div>
      </div>
      <v-alert v-else type="success" variant="tonal" class="mt-4" :text="$t('ui.health.noRepairable')" />
    </v-card>

    <v-card class="alert-settings glass-card" elevation="0">
      <div class="alert-settings__header">
        <div>
          <div class="alert-settings__eyebrow">{{ $t('ui.health.proactive') }}</div>
          <h2>{{ $t('ui.health.alerts') }}</h2>
          <p>{{ $t('ui.health.alertHint') }}</p>
        </div>
        <div class="alert-settings__header-actions">
          <v-switch v-model="alerts.enabled" color="primary" :label="$t('ui.health.enableAlerts')" hide-details inset />
          <v-btn
            variant="tonal"
            :prepend-icon="showAlertSettings ? 'mdi-chevron-up' : 'mdi-tune-variant'"
            @click="showAlertSettings = !showAlertSettings"
          >
            {{ showAlertSettings ? $t('ui.health.collapse') : $t('ui.health.configure') }}
          </v-btn>
        </div>
      </div>
      <v-expand-transition>
      <v-row v-show="showAlertSettings" class="mt-2">
        <v-col cols="12" md="7">
          <v-text-field v-model="alerts.telegramToken" :label="alerts.telegramTokenSet ? $t('ui.health.tokenSaved') : 'Telegram Bot Token'" prepend-inner-icon="mdi-send-check-outline" type="password" autocomplete="new-password" />
        </v-col>
        <v-col cols="12" md="5">
          <v-text-field v-model="alerts.telegramChatId" label="Telegram Chat ID" prepend-inner-icon="mdi-account-tie" />
        </v-col>
        <v-col cols="6" md="3">
          <v-number-input v-model="alerts.intervalMinutes" :label="$t('ui.health.interval')" :min="1" :max="1440" control-variant="stacked" />
        </v-col>
        <v-col cols="6" md="3">
          <v-number-input v-model="alerts.cooldownMinutes" :label="$t('ui.health.cooldown')" :min="1" :max="10080" control-variant="stacked" />
        </v-col>
        <v-col cols="12" md="6" class="alert-settings__actions">
          <v-btn variant="tonal" prepend-icon="mdi-send-check-outline" :loading="testingAlert" @click="testAlert">{{ $t('ui.health.testAlert') }}</v-btn>
          <v-btn color="primary" prepend-icon="mdi-content-save-outline" :loading="savingAlerts" @click="saveAlerts">{{ $t('ui.health.saveAlerts') }}</v-btn>
        </v-col>
      </v-row>
      </v-expand-transition>
    </v-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { push } from 'notivue'
import HttpUtils from '@/plugins/httputil'
import { i18n } from '@/locales'

interface HealthCheck { id: string; title: string; status: string; summary: string; detail?: string; action?: string }
interface HealthReport { status: string; checkedAt: string; durationMs: number; summary: Record<string, number>; checks: HealthCheck[]; diagnostics: Record<string, unknown> }
interface AlertSettings { enabled: boolean; telegramToken: string; telegramTokenSet: boolean; telegramChatId: string; intervalMinutes: number; cooldownMinutes: number }
interface PortDriftIssue { id: string; type: string; severity: string; scope?: string; family?: string; protocol?: string; port?: string; to_ports?: string; owner_tag?: string; detail: string; repairable: boolean }

const router = useRouter()
const t = i18n.global.t
const loading = ref(false)
const reconciling = ref(false)
const repairingIssueId = ref('')
const report = ref<HealthReport | null>(null)
const savingAlerts = ref(false)
const testingAlert = ref(false)
const showAlertSettings = ref(false)
const alerts = ref<AlertSettings>({ enabled: false, telegramToken: '', telegramTokenSet: false, telegramChatId: '', intervalMinutes: 5, cooldownMinutes: 60 })

const statusLabel = computed(() => ({ healthy: t('ui.health.allHealthy'), warning: t('ui.health.needsAttention'), critical: t('ui.health.critical') } as Record<string, string>)[report.value?.status ?? ''] ?? t('ui.health.waiting'))
const checkedAt = computed(() => report.value?.checkedAt ? new Date(report.value.checkedAt).toLocaleString() : '-')
const summaryCards = computed(() => [
  { key: 'ok', label: t('ui.health.ok'), value: report.value?.summary?.ok ?? 0, icon: 'mdi-check-circle-outline' },
  { key: 'warning', label: t('ui.health.warning'), value: report.value?.summary?.warning ?? 0, icon: 'mdi-alert-outline' },
  { key: 'error', label: t('ui.health.error'), value: report.value?.summary?.error ?? 0, icon: 'mdi-close-circle-outline' },
  { key: 'info', label: t('ui.health.info'), value: report.value?.summary?.info ?? 0, icon: 'mdi-information-outline' },
])
const portIssues = computed<PortDriftIssue[]>(() => {
  const diagnostics = report.value?.diagnostics as any
  return Array.isArray(diagnostics?.ports?.drift?.issues) ? diagnostics.ports.drift.issues : []
})

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

const repairPortIssue = async (issue: PortDriftIssue) => {
  repairingIssueId.value = issue.id
  const msg = await HttpUtils.post('api/repairPortIssue', { issueId: issue.id })
  repairingIssueId.value = ''
  if (msg.success) {
    push.success({ message: t('ui.health.repaired') })
    await loadHealth(true)
  }
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
  if (msg.success) push.success({ message: t('ui.health.testSent') })
}

const copyReport = async () => {
  if (!report.value) return
  try {
    await navigator.clipboard.writeText(JSON.stringify(report.value, null, 2))
    push.success({ message: t('ui.health.reportCopied') })
  } catch {
    push.error({ message: t('ui.health.copyFailed') })
  }
}

const handleAction = async (action: string) => {
  const routes: Record<string, string> = { ports: '/ports', settings: '/settings', inbounds: '/inbounds', endpoints: '/endpoints', admins: '/admins', fleet: '/fleet' }
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
const checkStatusLabel = (status: string) => ({ ok: t('ui.health.ok'), warning: t('ui.health.warning'), error: t('ui.health.error'), info: t('ui.health.info') } as Record<string, string>)[status] ?? status
const portIssueLabel = (type: string) => ({ missing: t('ui.health.missingRule'), duplicate: t('ui.health.duplicateRule'), orphan: t('ui.health.orphanRule'), unexpected: t('ui.health.targetMismatch'), 'desired-duplicate': t('ui.health.duplicateConfig'), 'inspection-error': t('ui.health.inspectionFailed'), unsupported: t('ui.health.unsupported') } as Record<string, string>)[type] ?? type
const portOwnerLabel = (scope?: string) => ({ inbound: t('ui.health.inbound'), endpoint: t('ui.health.endpoint'), panel: t('ui.health.panel') } as Record<string, string>)[scope ?? ''] ?? t('ui.health.managedRule')
const portIssueIcon = (severity: string) => ({ error: 'mdi-alert-circle-outline', warning: 'mdi-alert-outline', info: 'mdi-information-outline' } as Record<string, string>)[severity] ?? 'mdi-information-outline'
const portIssueColor = (severity: string) => ({ error: 'error', warning: 'warning', info: 'info' } as Record<string, string>)[severity] ?? 'info'

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
.port-issues { display: grid; gap: 10px; margin-top: 18px; }
.port-issue { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 12px; padding: 13px 14px; border: 1px solid var(--np-border); border-radius: 16px; background: var(--np-surface-muted); }
.port-issue__icon { display: grid; place-items: center; width: 36px; height: 36px; border-radius: 12px; background: var(--np-surface); }
.port-issue__body { display: grid; gap: 4px; min-width: 0; }
.port-issue__heading { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.port-issue__body span, .port-issue__body small { color: var(--np-text-muted); overflow-wrap: anywhere; }
.alert-settings { margin-top: 14px; padding: 24px; border-radius: 24px; }
.alert-settings__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; }
.alert-settings__header-actions { display: flex; align-items: center; gap: 10px; }
.alert-settings__header h2 { margin: 3px 0 0; font-size: 20px; }
.alert-settings__header p { margin: 8px 0 0; color: var(--np-text-muted); }
.alert-settings__eyebrow { color: rgb(var(--v-theme-primary)); font-size: 11px; font-weight: 800; letter-spacing: .12em; text-transform: uppercase; }
.alert-settings__actions { display: flex; align-items: center; justify-content: flex-end; gap: 10px; }
@media (max-width: 959px) { .health-hero__actions { justify-content: flex-start; } .health-grid { grid-template-columns: 1fr; } }
@media (max-width: 599px) { .health-hero { border-radius: 24px; } .health-hero__title-row { align-items: center; } .health-hero__icon { width: 48px; height: 48px; } .health-summary__card { min-height: 96px; padding: 14px; gap: 10px; } .health-summary__icon { width: 36px; height: 36px; } .health-summary__value { font-size: 24px; } .health-check { flex-wrap: wrap; } .health-diagnostics__header, .alert-settings__header { align-items: stretch; flex-direction: column; } .alert-settings__header-actions { justify-content: space-between; } .port-issue { grid-template-columns: auto minmax(0, 1fr); } .port-issue > .v-btn, .port-issue > .v-chip { grid-column: 1 / -1; width: 100%; } .alert-settings__actions { justify-content: stretch; } .alert-settings__actions .v-btn { flex: 1; } }
</style>
