<template>
  <section class="traffic-budget">
    <div class="traffic-budget__intro">
      <div>
        <span class="traffic-budget__eyebrow">VPS TRAFFIC BUDGET</span>
        <h2>服务器总流量保护</h2>
        <p>按 VPS 公网网卡总流量计量。达到“套餐总量 − 安全预留”时只停止代理数据面，面板、订阅和 SSH 保持可用。</p>
      </div>
      <v-chip :color="statusColor" variant="tonal" size="small">{{ statusLabel }}</v-chip>
    </div>

    <div v-if="status.enabled" class="traffic-budget__status">
      <div class="traffic-budget__status-head">
        <div><strong>{{ formatGB(status.usedBytes) }} GB</strong><span>本周期已计费</span></div>
        <div><strong>{{ formatGB(status.poolRemainingBytes) }} GB</strong><span>用户池剩余</span></div>
        <div><strong>{{ formatGB(status.providerRemainingBytes) }} GB</strong><span>服务商剩余</span></div>
        <div><strong>{{ status.interface || '—' }}</strong><span>计量网卡</span></div>
      </div>
      <v-progress-linear :model-value="Math.min(status.usedPercent || 0, 100)" :color="statusColor" height="10" rounded />
      <div class="traffic-budget__meta">
        <span>用户池 {{ formatGB(status.clientPoolBytes) }} GB</span>
        <span>RX {{ formatGB(status.meteredRxBytes) }} GB</span>
        <span>TX {{ formatGB(status.meteredTxBytes) }} GB</span>
        <span v-if="status.periodEnd">下次重置 {{ formatTime(status.periodEnd) }}</span>
      </div>
      <v-alert v-if="status.blocked" type="error" variant="tonal" density="compact" class="mt-4">
        已触发硬保护：代理数据面已停止。提高套餐/减少预留或等待新周期后会自动恢复。
      </v-alert>
      <v-alert v-else-if="status.error" type="warning" variant="tonal" density="compact" class="mt-4">{{ status.error }}</v-alert>
    </div>

    <v-divider class="my-5" />
    <v-row>
      <v-col cols="12" md="4"><v-switch v-model="enabled" color="primary" label="启用 VPS 总流量保护" hide-details /></v-col>
      <v-col cols="12" sm="6" md="4"><v-text-field v-model.number="limitGB" type="number" min="0" step="1" label="服务商套餐总流量" suffix="GB" hide-details /></v-col>
      <v-col cols="12" sm="6" md="4"><v-text-field v-model.number="reserveGB" type="number" min="0" step="1" label="安全预留" suffix="GB" hide-details /></v-col>
      <v-col cols="12" sm="6" md="4"><v-text-field v-model.number="offsetGB" type="number" min="0" step="0.1" label="本周期开启前已用" suffix="GB" hint="首次启用时可填服务商后台当前已用量；新周期自动清零" persistent-hint /></v-col>
      <v-col cols="12" sm="6" md="4"><v-select v-model="settings.trafficBudgetAccountingMode" :items="accountingModes" label="服务商计费方式" hide-details /></v-col>
      <v-col cols="12" sm="6" md="4"><v-text-field v-model.trim="settings.trafficBudgetInterface" label="公网网卡" placeholder="auto" hint="auto 自动识别默认路由网卡，也可填写 eth0 / ens3" persistent-hint /></v-col>
      <v-col cols="6" sm="3"><v-text-field v-model.number="cycleDay" type="number" min="1" max="31" label="每月重置日" hide-details /></v-col>
      <v-col cols="6" sm="3"><v-text-field v-model.number="cycleHour" type="number" min="0" max="23" label="重置小时" suffix=":00" hide-details /></v-col>
      <v-col cols="6" sm="3"><v-text-field v-model.number="warningPercent" type="number" min="1" max="98" label="预警阈值" suffix="%" hide-details /></v-col>
      <v-col cols="6" sm="3"><v-text-field v-model.number="criticalPercent" type="number" min="2" max="99" label="严重阈值" suffix="%" hide-details /></v-col>
    </v-row>
    <div class="traffic-budget__preview">
      <span>当前配置的用户可用池</span>
      <strong>{{ Math.max(0, limitGB - reserveGB).toFixed(1) }} GB</strong>
      <small>硬保护固定在用户池 100%；安全预留不会提供给代理流量。</small>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import HttpUtils from '@/plugins/httputil'

const props = defineProps<{ settings: Record<string, string> }>()
const settings = computed(() => props.settings)
const GB = 1_000_000_000

type BudgetStatus = {
  enabled: boolean; supported: boolean; interface: string; level: string; blocked: boolean; error?: string
  usedBytes: number; poolRemainingBytes: number; providerRemainingBytes: number; clientPoolBytes: number
  meteredRxBytes: number; meteredTxBytes: number; usedPercent: number; periodEnd?: string
}
const status = ref<BudgetStatus>({ enabled: false, supported: true, interface: '', level: 'disabled', blocked: false, usedBytes: 0, poolRemainingBytes: 0, providerRemainingBytes: 0, clientPoolBytes: 0, meteredRxBytes: 0, meteredTxBytes: 0, usedPercent: 0 })
let timer: ReturnType<typeof setInterval> | undefined

const asNumber = (key: string, fallback = 0) => Number(settings.value[key] || fallback)
const byteField = (key: string) => computed({ get: () => asNumber(key) / GB, set: (v: number) => { settings.value[key] = String(Math.round(Math.max(0, Number(v) || 0) * GB)) } })
const intField = (key: string, fallback: number) => computed({ get: () => asNumber(key, fallback), set: (v: number) => { settings.value[key] = String(Math.trunc(Number(v) || fallback)) } })
const enabled = computed({ get: () => settings.value.trafficBudgetEnabled === 'true', set: (v: boolean) => { settings.value.trafficBudgetEnabled = v ? 'true' : 'false' } })
const limitGB = byteField('trafficBudgetLimitBytes')
const reserveGB = byteField('trafficBudgetReserveBytes')
const offsetGB = byteField('trafficBudgetOffsetBytes')
const cycleDay = intField('trafficBudgetCycleDay', 1)
const cycleHour = intField('trafficBudgetCycleHour', 0)
const warningPercent = intField('trafficBudgetWarningPercent', 80)
const criticalPercent = intField('trafficBudgetCriticalPercent', 90)
const accountingModes = [
  { title: '仅出站（TX）', value: 'tx' },
  { title: '上行 + 下行（RX + TX）', value: 'rx_tx' },
  { title: '取较大方向（MAX）', value: 'max' },
]
const statusColor = computed(() => status.value.blocked || status.value.level === 'critical' || status.value.level === 'error' ? 'error' : status.value.level === 'warning' ? 'warning' : status.value.enabled ? 'success' : 'default')
const statusLabel = computed(() => status.value.blocked ? '硬保护' : status.value.level === 'critical' ? '严重' : status.value.level === 'warning' ? '预警' : status.value.level === 'error' ? '计量错误' : status.value.enabled ? (status.value.supported ? '正常' : '仅 Linux 生效') : '未启用')
const formatGB = (value = 0) => (Number(value || 0) / GB).toFixed(value >= 100 * GB ? 1 : 2)
const formatTime = (value: string) => new Date(value).toLocaleString()
const loadStatus = async () => { const msg = await HttpUtils.get('api/traffic-budget'); if (msg.success && msg.obj) status.value = msg.obj }
onMounted(() => { void loadStatus(); timer = setInterval(loadStatus, 10_000) })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<style scoped lang="scss">
.traffic-budget { color: var(--np-text-main); }
.traffic-budget__intro { display: flex; justify-content: space-between; gap: 18px; align-items: flex-start; }
.traffic-budget__intro h2 { margin: 4px 0 0; font-size: 22px; }
.traffic-budget__intro p { max-width: 760px; margin: 8px 0 0; color: var(--np-text-muted); line-height: 1.65; }
.traffic-budget__eyebrow { color: var(--np-accent); font-size: 10px; font-weight: 750; letter-spacing: .14em; }
.traffic-budget__status { margin-top: 18px; padding: 18px; border: 1px solid var(--np-border); border-radius: 20px; background: var(--np-surface-muted); }
.traffic-budget__status-head { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; margin-bottom: 14px; }
.traffic-budget__status-head strong, .traffic-budget__status-head span { display: block; }
.traffic-budget__status-head strong { font-size: 17px; overflow: hidden; text-overflow: ellipsis; }
.traffic-budget__status-head span, .traffic-budget__meta, .traffic-budget__preview small { color: var(--np-text-muted); font-size: 11px; }
.traffic-budget__meta { display: flex; flex-wrap: wrap; gap: 14px; margin-top: 9px; }
.traffic-budget__preview { display: flex; align-items: baseline; flex-wrap: wrap; gap: 9px; margin-top: 18px; padding: 14px 16px; border-radius: 16px; background: var(--np-surface-muted); }
.traffic-budget__preview strong { color: var(--np-accent); }
.traffic-budget__preview small { flex-basis: 100%; }
@media (max-width: 720px) { .traffic-budget__status-head { grid-template-columns: repeat(2, minmax(0,1fr)); } .traffic-budget__intro { flex-direction: column; } }
</style>
