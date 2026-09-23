<template>
  <section class="traffic-history">
    <div class="traffic-history__header">
      <div>
        <h3>{{ $t('ui.fleet.trafficHistory') }}</h3>
        <p>{{ periodLabel }}</p>
      </div>
      <v-chip v-if="history?.samples?.length" size="small" variant="tonal">
        {{ $t('ui.fleet.trafficSamples', { count: history.samples.length }) }}
      </v-chip>
    </div>
    <v-skeleton-loader v-if="loading" type="image" class="traffic-history__skeleton" />
    <v-alert v-else-if="error" type="info" variant="tonal" density="compact">{{ error }}</v-alert>
    <v-alert v-else-if="!history?.samples?.length" type="info" variant="tonal" density="compact">
      {{ $t('ui.fleet.trafficHistoryEmpty') }}
    </v-alert>
    <div v-else class="traffic-history__chart">
      <Line :data="chartData" :options="chartOptions" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import {
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
} from 'chart.js'
import { Line } from 'vue-chartjs'
import { i18n } from '@/locales'
import { trafficHistorySeries, type TrafficBudgetHistory } from '@/utils/trafficBudget'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

const props = defineProps<{
  history: TrafficBudgetHistory | null
  loading: boolean
  error?: string
}>()

const t = i18n.global.t
const series = computed(() => trafficHistorySeries(props.history))
const periodLabel = computed(() => {
  if (!props.history?.periodStart || !props.history?.periodEnd) return t('ui.fleet.trafficCurrentPeriod')
  const start = new Date(props.history.periodStart)
  const end = new Date(props.history.periodEnd)
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime())) return t('ui.fleet.trafficCurrentPeriod')
  return t('ui.fleet.trafficPeriodRange', { start: start.toLocaleString(), end: end.toLocaleString() })
})
const chartData = computed(() => ({
  labels: series.value.labels.map(value => new Date(value).toLocaleString()),
  datasets: [
    { label: t('ui.fleet.trafficBilled'), data: series.value.used, borderColor: '#0a84ff', backgroundColor: 'rgba(10,132,255,.12)', fill: true, tension: .25, pointRadius: 0 },
    { label: 'TX', data: series.value.tx, borderColor: '#14b8a6', backgroundColor: 'transparent', tension: .25, pointRadius: 0 },
    { label: 'RX', data: series.value.rx, borderColor: '#f59e0b', backgroundColor: 'transparent', tension: .25, pointRadius: 0 },
  ],
}))
const formatBytes = (value: number) => {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = Math.max(0, Number(value) || 0)
  let index = 0
  while (size >= 1000 && index < units.length - 1) {
    size /= 1000
    index += 1
  }
  return size === 0 ? '0 B' : `${size.toFixed(size >= 100 ? 0 : size >= 10 ? 1 : 2)} ${units[index]}`
}
const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' as const },
  plugins: {
    legend: { display: true },
    tooltip: {
      callbacks: {
        label: (context: any) => `${context.dataset.label}: ${formatBytes(Number(context.raw))}`,
      },
    },
  },
  scales: {
    x: { ticks: { maxTicksLimit: 6 } },
    y: { beginAtZero: true, ticks: { callback: (value: string | number) => formatBytes(Number(value)) } },
  },
}))
</script>

<style scoped>
.traffic-history { margin-top: 18px; padding: 18px; border: 1px solid var(--np-border); border-radius: 20px; background: var(--np-surface-muted); }
.traffic-history__header { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; margin-bottom: 14px; }
.traffic-history h3 { margin: 0; font-size: 16px; }
.traffic-history p { margin: 5px 0 0; color: var(--np-text-muted); font-size: 12px; line-height: 1.5; }
.traffic-history__chart { height: 260px; }
.traffic-history__skeleton { min-height: 220px; }
@media (max-width: 600px) {
  .traffic-history { padding: 14px; }
  .traffic-history__chart { height: 220px; }
}
</style>
