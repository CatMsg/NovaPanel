<template>
  <v-container fluid class="fill-height ports-shell">
    <div class="ports-shell__glow ports-shell__glow--one"></div>
    <div class="ports-shell__glow ports-shell__glow--two"></div>

    <div class="ports-shell__inner">
      <v-card class="ports-hero" rounded="xl" variant="flat">
        <div class="ports-hero__topline">
          <span class="ports-hero__badge">{{ $t('pages.ports') }}</span>
          <span class="ports-hero__badge ports-hero__badge--soft">{{ status.backend || $t('unknown') }}</span>
        </div>

        <v-row class="ports-hero__content" align="center">
          <v-col cols="12" lg="8">
            <div class="ports-hero__title-row">
              <div class="ports-hero__icon">
                <v-icon icon="mdi-lan" size="32" />
              </div>
              <div>
                <h1 class="ports-hero__title">{{ $t('pages.ports') }}</h1>
                <p class="ports-hero__subtitle">
                  当前机器上的监听端口、NAT 规则和面板受管端口；发现漂移时可一键重建受管规则。
                </p>
              </div>
            </div>
            <div class="ports-hero__meta">
              <span>更新时间：{{ formattedCapturedAt }}</span>
              <span>•</span>
              <span>错误：{{ errors.length }}</span>
              <span>•</span>
              <span>受管端口：{{ status.managed_count ?? 0 }}</span>
            </div>
          </v-col>
          <v-col cols="12" lg="4" class="ports-hero__actions">
            <v-btn variant="outlined" :loading="repairing" @click="repairPorts">
              <v-icon icon="mdi-wrench-outline" start />
              修复端口规则
            </v-btn>
            <v-btn class="ports-hero__refresh" variant="flat" color="primary" :loading="loading" @click="loadPorts">
              <v-icon icon="mdi-refresh" start />
              刷新
            </v-btn>
          </v-col>
        </v-row>
      </v-card>

      <v-row class="ports-summary" dense>
        <v-col cols="12" sm="6" lg="3">
          <v-card class="ports-summary__card ports-summary__card--one" rounded="xl" variant="flat">
            <div class="ports-summary__label">监听端口</div>
            <div class="ports-summary__value">{{ listeners.length }}</div>
          </v-card>
        </v-col>
        <v-col cols="12" sm="6" lg="3">
          <v-card class="ports-summary__card ports-summary__card--two" rounded="xl" variant="flat">
            <div class="ports-summary__label">NAT IPv4</div>
            <div class="ports-summary__value">{{ natIpv4.length }}</div>
          </v-card>
        </v-col>
        <v-col cols="12" sm="6" lg="3">
          <v-card class="ports-summary__card ports-summary__card--three" rounded="xl" variant="flat">
            <div class="ports-summary__label">NAT IPv6</div>
            <div class="ports-summary__value">{{ natIpv6.length }}</div>
          </v-card>
        </v-col>
        <v-col cols="12" sm="6" lg="3">
          <v-card class="ports-summary__card ports-summary__card--four" rounded="xl" variant="flat">
            <div class="ports-summary__label">后端</div>
            <div class="ports-summary__value">{{ status.backend || $t('unknown') }}</div>
          </v-card>
        </v-col>
      </v-row>

      <v-alert
        v-if="errors.length > 0"
        class="ports-alert"
        color="warning"
        variant="tonal"
        rounded="xl"
        density="comfortable"
      >
        <div class="ports-alert__title">采集到的问题</div>
        <ul class="ports-alert__list">
          <li v-for="(error, index) in errors" :key="`${index}-${error}`">{{ error }}</li>
        </ul>
      </v-alert>

      <v-alert
        v-if="status.drift && (drift.status === 'drift' || drift.status === 'unknown')"
        class="ports-alert"
        :color="drift.status === 'drift' ? 'error' : 'warning'"
        variant="tonal"
        rounded="xl"
        density="comfortable"
      >
        <div class="ports-alert__title">
          {{ drift.status === 'drift' ? '端口规则发现漂移' : '端口规则暂时无法确认' }}
        </div>
        <div class="ports-drift__summary">
          期望 {{ drift.desired_rules }} 条，实际受管 {{ drift.actual_managed_rules }} 条；
          缺失 {{ drift.missing_count }}，重复 {{ drift.duplicate_count }}，残留 {{ drift.orphan_count }}。
        </div>
        <ul class="ports-alert__list">
          <li v-for="(issue, index) in drift.issues.slice(0, 8)" :key="`${issue.type}-${issue.chain}-${issue.protocol}-${issue.port}-${index}`">
            {{ issue.detail }}<span v-if="issue.owner_tag">（{{ issue.owner_tag }}）</span>
          </li>
        </ul>
        <div v-if="drift.issues.length > 8" class="ports-drift__more">还有 {{ drift.issues.length - 8 }} 项，请点击“修复端口规则”重建。</div>
      </v-alert>

      <v-row class="ports-panels" dense>
        <v-col cols="12" lg="6">
          <v-card class="ports-panel" rounded="xl" variant="flat">
            <v-card-title class="ports-panel__title">
              <span>监听列表</span>
              <v-chip size="small" variant="flat">{{ listeners.length }}</v-chip>
            </v-card-title>
            <v-divider />
            <div class="ports-panel__scroll">
              <v-table density="compact" class="ports-table">
                <thead>
                  <tr>
                    <th>协议</th>
                    <th>本地地址</th>
                    <th>端口</th>
                    <th>进程</th>
                    <th>PID</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="row in listeners" :key="`${row.protocol}-${row.local}-${row.pid}-${row.raw}`">
                    <td>
                      <v-chip size="x-small" variant="flat" :color="row.protocol === 'tcp' ? 'primary' : 'info'">
                        {{ row.protocol }}
                      </v-chip>
                    </td>
                    <td class="ports-table__mono">{{ row.local }}</td>
                    <td>{{ row.port }}</td>
                    <td>{{ row.process || '-' }}</td>
                    <td>{{ row.pid || '-' }}</td>
                  </tr>
                  <tr v-if="listeners.length === 0">
                    <td colspan="5" class="ports-table__empty">没有检测到监听端口</td>
                  </tr>
                </tbody>
              </v-table>
            </div>
          </v-card>
        </v-col>

        <v-col cols="12" lg="6">
          <v-card class="ports-panel" rounded="xl" variant="flat">
            <v-card-title class="ports-panel__title">
              <span>NAT 规则</span>
              <v-chip size="small" variant="flat">{{ natIpv4.length + natIpv6.length }}</v-chip>
            </v-card-title>
            <v-divider />
            <v-tabs v-model="natTab" density="compact" class="ports-tabs" grow>
              <v-tab value="ipv4">IPv4</v-tab>
              <v-tab value="ipv6">IPv6</v-tab>
            </v-tabs>
            <v-divider />
            <div class="ports-panel__scroll">
              <v-window v-model="natTab">
                <v-window-item value="ipv4">
                  <v-table density="compact" class="ports-table">
                    <thead>
                      <tr>
                        <th>链</th>
                        <th>协议</th>
                        <th>dport</th>
                        <th>目标</th>
                        <th>to-ports</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="row in natIpv4" :key="`ipv4-${row.chain}-${row.protocol}-${row.dport}-${row.raw}`">
                        <td>{{ row.chain || '-' }}</td>
                        <td>{{ row.protocol || '-' }}</td>
                        <td>{{ row.dport || '-' }}</td>
                        <td>{{ row.target || '-' }}</td>
                        <td>{{ row.to_ports || '-' }}</td>
                      </tr>
                      <tr v-if="natIpv4.length === 0">
                        <td colspan="5" class="ports-table__empty">没有检测到 IPv4 NAT 规则</td>
                      </tr>
                    </tbody>
                  </v-table>
                </v-window-item>
                <v-window-item value="ipv6">
                  <v-table density="compact" class="ports-table">
                    <thead>
                      <tr>
                        <th>链</th>
                        <th>协议</th>
                        <th>dport</th>
                        <th>目标</th>
                        <th>to-ports</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="row in natIpv6" :key="`ipv6-${row.chain}-${row.protocol}-${row.dport}-${row.raw}`">
                        <td>{{ row.chain || '-' }}</td>
                        <td>{{ row.protocol || '-' }}</td>
                        <td>{{ row.dport || '-' }}</td>
                        <td>{{ row.target || '-' }}</td>
                        <td>{{ row.to_ports || '-' }}</td>
                      </tr>
                      <tr v-if="natIpv6.length === 0">
                        <td colspan="5" class="ports-table__empty">没有检测到 IPv6 NAT 规则</td>
                      </tr>
                    </tbody>
                  </v-table>
                </v-window-item>
              </v-window>
            </div>
          </v-card>
        </v-col>
      </v-row>
    </div>
  </v-container>
</template>

<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'
import HttpUtils from '@/plugins/httputil'

type PortListenEntry = {
  protocol: string
  local: string
  port: number
  process?: string
  pid?: number
  raw: string
}

type PortNatEntry = {
  family: string
  table: string
  chain: string
  protocol?: string
  dport?: string
  target?: string
  to_ports?: string
  raw: string
}

type PortStatus = {
  backend?: string
  captured_at?: string
  listeners?: PortListenEntry[]
  nat_ipv4?: PortNatEntry[]
  nat_ipv6?: PortNatEntry[]
  errors?: string[]
  managed_count?: number
  drift?: PortDriftReport
}

type PortDriftIssue = {
  type: string
  severity: string
  detail: string
  owner_tag?: string
  chain?: string
  protocol?: string
  port?: string
}

type PortDriftReport = {
  status: string
  desired_rules: number
  actual_managed_rules: number
  issue_count: number
  missing_count: number
  duplicate_count: number
  orphan_count: number
  issues: PortDriftIssue[]
}

const loading = ref(false)
const repairing = ref(false)
const natTab = ref<'ipv4' | 'ipv6'>('ipv4')
const status = ref<PortStatus>({})

const listeners = computed(() => status.value.listeners ?? [])
const natIpv4 = computed(() => status.value.nat_ipv4 ?? [])
const natIpv6 = computed(() => status.value.nat_ipv6 ?? [])
const errors = computed(() => status.value.errors ?? [])
const drift = computed<PortDriftReport>(() => status.value.drift ?? {
  status: 'unknown',
  desired_rules: 0,
  actual_managed_rules: 0,
  issue_count: 0,
  missing_count: 0,
  duplicate_count: 0,
  orphan_count: 0,
  issues: [],
})
const formattedCapturedAt = computed(() => {
  if (!status.value.captured_at) return '-'
  const date = new Date(status.value.captured_at)
  if (Number.isNaN(date.getTime())) return status.value.captured_at
  return date.toLocaleString()
})

const loadPorts = async () => {
  loading.value = true
  try {
    const resp = await HttpUtils.get('api/ports')
    if (resp.success && resp.obj) {
      status.value = resp.obj as PortStatus
    } else {
      status.value = { errors: [resp.msg || 'load ports failed'] }
    }
  } finally {
    loading.value = false
  }
}

const repairPorts = async () => {
  repairing.value = true
  try {
    const resp = await HttpUtils.post('api/reconcilePorts', {})
    if (resp.success) {
      await loadPorts()
    }
  } finally {
    repairing.value = false
  }
}

onMounted(() => {
  loadPorts()
})
</script>

<style scoped lang="scss">
.ports-shell {
  position: relative;
  overflow: hidden;
}

.ports-shell__inner {
  position: relative;
  z-index: 1;
  width: min(1440px, 100%);
  margin: 0 auto;
  display: grid;
  gap: 20px;
}

.ports-shell__glow {
  position: absolute;
  border-radius: 999px;
  filter: blur(90px);
  opacity: 0.7;
  pointer-events: none;
}

.ports-shell__glow--one {
  width: 280px;
  height: 280px;
  top: 80px;
  left: -80px;
  background: rgba(59, 130, 246, 0.18);
}

.ports-shell__glow--two {
  width: 320px;
  height: 320px;
  right: -120px;
  bottom: 120px;
  background: rgba(14, 165, 233, 0.12);
}

.ports-hero,
.ports-summary__card,
.ports-panel,
.ports-alert {
  background: var(--np-surface-strong);
  backdrop-filter: blur(16px);
  border: 1px solid var(--np-border);
  box-shadow: var(--np-shadow);
  color: var(--np-text-main);
}

.ports-hero {
  padding: 20px;
}

.ports-hero__topline {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 18px;
}

.ports-hero__badge {
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 12px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: rgb(var(--v-theme-primary));
  background: rgba(59, 130, 246, 0.08);
}

.ports-hero__badge--soft {
  color: var(--np-text-muted);
  background: rgba(148, 163, 184, 0.12);
  text-transform: none;
  letter-spacing: 0;
}

.ports-hero__content {
  min-height: 130px;
}

.ports-hero__title-row {
  display: flex;
  gap: 14px;
  align-items: flex-start;
}

.ports-hero__icon {
  width: 52px;
  height: 52px;
  border-radius: 16px;
  display: grid;
  place-items: center;
  color: rgb(var(--v-theme-primary));
  background: linear-gradient(145deg, rgba(59, 130, 246, 0.16), rgba(14, 165, 233, 0.08));
}

.ports-hero__title {
  margin: 0;
  font-size: clamp(28px, 3vw, 40px);
  line-height: 1.1;
  font-weight: 800;
  letter-spacing: -0.03em;
  color: var(--np-text-main);
}

.ports-hero__subtitle {
  margin: 12px 0 0;
  color: var(--np-text-muted);
  line-height: 1.7;
}

.ports-hero__meta {
  margin-top: 14px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  color: var(--np-text-muted);
  font-size: 13px;
}

.ports-hero__actions {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 10px;
}

.ports-hero__refresh {
  min-width: 160px;
}

@media (max-width: 959px) {
  .ports-hero__actions {
    justify-content: stretch;
  }

  .ports-hero__actions .v-btn {
    flex: 1 1 180px;
  }
}

.ports-summary {
  margin: 0;
}

.ports-summary__card {
  padding: 18px 20px;
  min-height: 108px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  position: relative;
  overflow: hidden;
  transition: transform 180ms ease, box-shadow 180ms ease;
}

.ports-summary__card::before {
  content: '';
  position: absolute;
  inset: 0 auto auto 0;
  height: 4px;
  width: 100%;
  opacity: 0.85;
  background: linear-gradient(90deg, rgb(var(--v-theme-primary)), rgba(59, 130, 246, 0.18));
}

.ports-summary__card--one {
  transform: translateY(0);
}

.ports-summary__card--two {
  transform: translateY(8px);
}

.ports-summary__card--three {
  transform: translateY(16px);
}

.ports-summary__card--four {
  transform: translateY(24px);
}

.ports-summary__label {
  font-size: 13px;
  color: var(--np-text-muted);
}

.ports-summary__value {
  margin-top: 14px;
  font-size: 28px;
  font-weight: 800;
  line-height: 1.1;
  color: var(--np-text-main);
  word-break: break-word;
}

.ports-alert {
  padding: 8px 8px 8px 16px;
}

.ports-alert__title {
  font-weight: 700;
  margin-bottom: 6px;
  color: var(--np-text-main);
}

.ports-alert__list {
  margin: 0;
  padding-left: 18px;
}

.ports-drift__summary,
.ports-drift__more {
  color: var(--np-text-muted);
  line-height: 1.6;
}

.ports-drift__more {
  margin-top: 6px;
  font-size: 12px;
}

.ports-panels {
  align-items: stretch;
}

.ports-panel {
  overflow: hidden;
}

.ports-panel__title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  font-weight: 700;
}

.ports-panel__scroll {
  overflow: auto;
  max-height: 560px;
}

.ports-tabs {
  background: transparent;
}

.ports-table__mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  word-break: break-all;
}

.ports-table__empty {
  text-align: center;
  color: var(--np-text-muted);
  padding: 20px 12px;
}

:deep(.ports-table) {
  width: 100%;
  color: var(--np-text-main);
}

:deep(.ports-table thead th) {
  white-space: nowrap;
  font-size: 12px;
  color: var(--np-text-muted);
  background: transparent;
}

:deep(.ports-table tbody td) {
  vertical-align: top;
  color: var(--np-text-main);
}

:deep(.ports-table .v-table__wrapper) {
  color: var(--np-text-main);
}

:deep(.ports-table .v-table__wrapper table) {
  color: var(--np-text-main);
}

:deep(.ports-table .v-table__wrapper table thead th),
:deep(.ports-table .v-table__wrapper table tbody td) {
  color: inherit;
}

:global(.v-theme--dark) .ports-hero,
:global(.v-theme--dark) .ports-summary__card,
:global(.v-theme--dark) .ports-panel,
:global(.v-theme--dark) .ports-alert {
  background: var(--np-surface-strong);
  border-color: var(--np-border);
  box-shadow: var(--np-shadow);
}

:global(.v-theme--dark) .ports-hero__badge--soft {
  background: rgba(148, 163, 184, 0.16);
}

:global(.v-theme--dark) .ports-shell__glow--one {
  background: rgba(59, 130, 246, 0.14);
}

:global(.v-theme--dark) .ports-shell__glow--two {
  background: rgba(14, 165, 233, 0.1);
}

@media (max-width: 959px) {
  .ports-summary__card--one,
  .ports-summary__card--two,
  .ports-summary__card--three,
  .ports-summary__card--four {
    transform: none;
  }
}

@media (max-width: 959px) {
  .ports-hero__actions {
    justify-content: flex-start;
  }

  .ports-summary__value {
    font-size: 24px;
  }

  .ports-panel__scroll {
    max-height: 420px;
  }
}
</style>
