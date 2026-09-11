<template>
  <section class="health-dashboard" aria-labelledby="outbound-health-title">
    <div class="health-dashboard__heading">
      <div>
        <span class="health-dashboard__eyebrow">OBSERVED OUTBOUND HEALTH</span>
        <h2 id="outbound-health-title">出口健康概览</h2>
        <p>当前进程内最近 {{ windowSize }} 次探测的观测结果，重启后会重新统计。</p>
      </div>
      <div class="health-dashboard__actions">
        <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="emit('refresh')">
          刷新
        </v-btn>
        <v-btn
          color="primary"
          variant="tonal"
          prepend-icon="mdi-speedometer"
          :loading="testingAll"
          :disabled="!canTestAll"
          @click="emit('testAll')"
        >
          测试全部
        </v-btn>
      </div>
    </div>

    <div class="health-summary" aria-label="出口健康统计">
      <div><strong>{{ summary.healthy }}</strong><span>健康</span></div>
      <div><strong>{{ summary.unhealthy }}</strong><span>异常</span></div>
      <div><strong>{{ summary.untested }}</strong><span>未测试</span></div>
      <div><strong>{{ summary.samples }}</strong><span>观测样本</span></div>
    </div>

    <div v-if="cards.length > 0" class="health-grid">
      <article
        v-for="item in cards"
        :key="item.tag"
        class="health-card"
        :class="`health-card--${item.health.status}`"
      >
        <div class="health-card__top">
          <div class="health-card__identity">
            <span class="health-status-dot" aria-hidden="true"></span>
            <div>
              <strong>{{ item.tag }}</strong>
              <small>{{ healthStatusLabel(item.health.status) }}</small>
            </div>
          </div>
          <v-btn
            icon="mdi-play"
            size="small"
            variant="text"
            :loading="checkLoading[item.tag]"
            :aria-label="`测试 ${item.tag}`"
            @click="emit('test', item.tag)"
          />
        </div>

        <div v-if="item.relations.length > 0" class="health-card__relations">
          <span v-for="relation in item.relations" :key="relation.key" :class="`relation-chip relation-chip--${relation.role}`">
            {{ relation.policy }} · {{ failoverRoleLabel(relation.role) }}
          </span>
        </div>

        <div class="health-card__exit">
          <span>公网出口</span>
          <strong class="health-card__ip">{{ item.health.publicIp || '待获取' }}</strong>
          <small>{{ formatRegion(item.health) }}</small>
        </div>

        <div class="health-metrics">
          <div><span>最近延迟</span><strong>{{ formatDelay(item.health) }}</strong></div>
          <div><span>成功均值</span><strong>{{ formatAverageDelay(item.health) }}</strong></div>
          <div><span>观测可用率</span><strong>{{ formatAvailability(item.health) }}</strong></div>
          <div><span>样本</span><strong>{{ item.health.samples }}</strong></div>
        </div>

        <div class="health-card__footer">
          <span>{{ item.health.lastChecked ? `最近 ${formatTime(item.health.lastChecked)}` : '尚无探测记录' }}</span>
          <span v-if="item.health.lastSuccess">成功 {{ formatTime(item.health.lastSuccess) }}</span>
        </div>
        <p v-if="item.health.status === 'unhealthy' && item.health.lastError" class="health-card__error">
          <v-icon icon="mdi-alert-circle-outline" size="16" />
          <span>{{ item.health.lastError }}</span>
        </p>
      </article>
    </div>
    <div v-else class="health-dashboard__empty">添加出站后即可开始健康观测。</div>
  </section>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import type { FailoverRole, HealthStatus, OutboundHealthCard, OutboundHealthSnapshot } from '@/types/outboundHealth'

const props = defineProps<{
  cards: OutboundHealthCard[]
  canTestAll: boolean
  checkLoading: Record<string, boolean | undefined>
  loading: boolean
  testingAll: boolean
  windowSize: number
}>()

const emit = defineEmits<{
  refresh: []
  testAll: []
  test: [tag: string]
}>()

const summary = computed(() => props.cards.reduce((result, item) => {
  result[item.health.status]++
  result.samples += item.health.samples
  return result
}, { healthy: 0, unhealthy: 0, untested: 0, samples: 0 }))

let regionNames: Intl.DisplayNames | undefined
try {
  regionNames = new Intl.DisplayNames([navigator.language], { type: 'region' })
} catch {
  regionNames = undefined
}

function healthStatusLabel(status: HealthStatus) {
  if (status === 'healthy') return '健康'
  if (status === 'unhealthy') return '异常'
  return '未测试'
}

function failoverRoleLabel(role: FailoverRole) {
  if (role === 'current') return '当前'
  if (role === 'candidate') return '候选'
  return '成员'
}

function formatDelay(health: OutboundHealthSnapshot) {
  return health.status === 'healthy' ? `${health.latestDelay} ms` : '—'
}

function formatAverageDelay(health: OutboundHealthSnapshot) {
  return health.successes > 0 ? `${Math.round(health.averageDelay)} ms` : '—'
}

function formatAvailability(health: OutboundHealthSnapshot) {
  return health.samples > 0 ? `${health.observedAvailability.toFixed(health.samples > 9 ? 1 : 0)}%` : '—'
}

function formatRegion(health: OutboundHealthSnapshot) {
  const country = health.countryCode ? (regionNames?.of(health.countryCode) || health.countryCode) : ''
  if (country && health.colo) return `${country} · ${health.colo}`
  return country || health.colo || '地区待获取'
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
</script>

<style scoped lang="scss">
.health-dashboard {
  position: relative;
  isolation: isolate;
  margin-bottom: 18px;
  padding: 20px;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.48);
  border-radius: 28px;
  background:
    radial-gradient(circle at 8% 0%, rgba(10, 132, 255, 0.12), transparent 28%),
    linear-gradient(145deg, rgba(255, 255, 255, 0.74), rgba(245, 249, 255, 0.54));
  box-shadow: 0 22px 60px rgba(35, 62, 98, 0.1), inset 0 1px rgba(255, 255, 255, 0.72);
  backdrop-filter: blur(30px) saturate(1.28);
}

.health-dashboard::after {
  position: absolute;
  z-index: -1;
  top: -88px;
  right: -72px;
  width: 240px;
  height: 240px;
  border-radius: 50%;
  background: rgba(90, 200, 250, 0.13);
  filter: blur(12px);
  content: '';
}

.health-dashboard__heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
}

.health-dashboard__heading h2 {
  margin: 3px 0 0;
  font-size: clamp(20px, 2vw, 26px);
  line-height: 1.15;
  letter-spacing: -0.025em;
}

.health-dashboard__heading p {
  margin: 8px 0 0;
  color: var(--np-text-muted);
  font-size: 13px;
}

.health-dashboard__eyebrow {
  color: var(--np-accent);
  font-size: 10px;
  font-weight: 750;
  letter-spacing: 0.14em;
}

.health-dashboard__actions {
  display: flex;
  flex: 0 0 auto;
  gap: 6px;
}

.health-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin: 18px 0;
  padding: 12px 4px;
  border-block: 1px solid var(--np-border);
}

.health-summary > div {
  display: flex;
  align-items: baseline;
  justify-content: center;
  gap: 7px;
  min-width: 0;
  border-inline-start: 1px solid var(--np-border);
}

.health-summary > div:first-child { border-inline-start: 0; }
.health-summary strong { font-size: 20px; letter-spacing: -0.025em; }
.health-summary span { color: var(--np-text-muted); font-size: 12px; }

.health-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 290px), 1fr));
  gap: 12px;
}

.health-card {
  --health-color: #8e8e93;
  position: relative;
  min-width: 0;
  padding: 16px;
  overflow: hidden;
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 21px;
  background: rgba(255, 255, 255, 0.72);
  box-shadow: 0 9px 26px rgba(36, 52, 78, 0.06), inset 0 1px rgba(255, 255, 255, 0.74);
  transition: transform var(--np-duration-fast) var(--np-ease-out), border-color var(--np-duration-fast) ease;
}

.health-card--healthy { --health-color: #30a46c; }
.health-card--unhealthy { --health-color: #ff453a; border-color: rgba(255, 69, 58, 0.22); }
.health-card__top, .health-card__identity, .health-card__footer { display: flex; align-items: center; }
.health-card__top { justify-content: space-between; gap: 12px; }
.health-card__identity { min-width: 0; gap: 10px; }
.health-card__identity div { min-width: 0; }
.health-card__identity strong, .health-card__identity small { display: block; }
.health-card__identity strong { overflow: hidden; font-size: 15px; text-overflow: ellipsis; white-space: nowrap; }
.health-card__identity small { margin-top: 1px; color: var(--health-color); font-size: 11px; font-weight: 650; }

.health-status-dot {
  width: 9px;
  height: 9px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--health-color);
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--health-color) 13%, transparent);
}

.health-card__relations { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 12px; }
.relation-chip { padding: 4px 8px; border-radius: 999px; color: var(--np-text-muted); background: rgba(118, 118, 128, 0.09); font-size: 10px; font-weight: 650; }
.relation-chip--current { color: #16774d; background: rgba(48, 164, 108, 0.12); }
.relation-chip--candidate { color: #146fa8; background: rgba(10, 132, 255, 0.11); }

.health-card__exit { margin: 15px 0; padding: 13px; border-radius: 15px; background: rgba(118, 118, 128, 0.065); }
.health-card__exit span, .health-card__exit small, .health-card__exit strong { display: block; }
.health-card__exit span, .health-card__exit small { color: var(--np-text-muted); font-size: 11px; }
.health-card__ip { margin: 3px 0 2px; overflow: hidden; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 14px; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }

.health-metrics { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px 8px; }
.health-metrics span, .health-metrics strong { display: block; }
.health-metrics span { color: var(--np-text-muted); font-size: 10px; }
.health-metrics strong { margin-top: 2px; font-size: 13px; }
.health-card__footer { justify-content: space-between; gap: 8px; margin-top: 15px; color: var(--np-text-muted); font-size: 10px; }

.health-card__error {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 10px 0 0;
  padding-top: 10px;
  color: #c7352d;
  border-top: 1px solid rgba(255, 69, 58, 0.14);
  font-size: 11px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.health-dashboard__empty { padding: 28px; color: var(--np-text-muted); text-align: center; }

:global(.v-theme--dark) .health-dashboard {
  border-color: rgba(125, 211, 252, 0.13);
  background:
    radial-gradient(circle at 8% 0%, rgba(14, 165, 233, 0.11), transparent 30%),
    linear-gradient(145deg, rgba(14, 25, 43, 0.88), rgba(7, 14, 27, 0.72));
  box-shadow: 0 25px 65px rgba(0, 0, 0, 0.31), inset 0 1px rgba(255, 255, 255, 0.07);
}

:global(.v-theme--dark) .health-card { border-color: rgba(148, 163, 184, 0.13); background: rgba(16, 27, 46, 0.84); box-shadow: 0 10px 28px rgba(0, 0, 0, 0.18), inset 0 1px rgba(255, 255, 255, 0.04); }
:global(.v-theme--dark) .health-card--unhealthy { border-color: rgba(255, 105, 97, 0.2); }
:global(.v-theme--dark) .relation-chip--current { color: #83dab0; }
:global(.v-theme--dark) .relation-chip--candidate { color: #82cfff; }
:global(.v-theme--dark) .health-card__error { color: #ff918a; }

@media (hover: hover) and (prefers-reduced-motion: no-preference) {
  .health-card:hover { transform: translateY(-2px); border-color: color-mix(in srgb, var(--health-color) 30%, var(--np-border)); }
}

@media (max-width: 960px) {
  .health-dashboard__heading { flex-direction: column; }
}

@media (max-width: 600px) {
  .health-dashboard { padding: 16px; border-radius: 24px; }
  .health-dashboard__actions { width: 100%; }
  .health-dashboard__actions .v-btn { flex: 1 1 0; }
  .health-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px 0; }
  .health-summary > div:nth-child(3) { border-inline-start: 0; }
}

@media (prefers-reduced-transparency: reduce) {
  .health-dashboard, :global(.v-theme--dark) .health-dashboard { background: var(--np-surface-strong); backdrop-filter: none; }
}

@media (prefers-reduced-motion: reduce) {
  .health-card { transition: none; }
}
</style>
