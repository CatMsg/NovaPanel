<template>
  <v-dialog transition="dialog-bottom-transition" width="90%" max-width="800">
    <v-card class="rounded-lg stats-dialog" :loading="loading">
      <v-card-title class="stats-dialog__title">
        <span>{{ $t('stats.graphTitle') }}</span>
        <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="$emit('close')" />
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text class="stats-dialog__body">
        <div class="stats-dialog__resource">
          {{ $t('objects.' + resource) + " : " + tag }}
        </div>
        <v-radio-group
          v-model="limit"
          class="stats-dialog__periods"
          @update:model-value="loadData"
          density="compact"
          :loading="loading"
          inline
          hide-details
        >
          <v-radio v-for="p in periods" :key="p.value" :label="p.title" :value="p.value" />
        </v-radio-group>
        <v-container class="stats-dialog__chart">
          <v-skeleton-loader
            class="mx-auto border"
            width="95%"
            type="image"
            v-if="loading"
          />
          <template v-else>
            <v-alert :text="$t('noData')" type="warning" variant="outlined" v-if="alert" />
            <Line v-if="loaded" :data="usage" :options="<any>options" />
          </template>
        </v-container>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { HumanReadable } from '@/plugins/utils'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
} from 'chart.js'
import { ref } from 'vue'
import { Line } from 'vue-chartjs'
ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
)
ChartJS.defaults.font.family = 'Vazirmatn'
export default {
  components: {
    Line
  },
  props: ['visible','resource','tag'],
  data() {
    return {
      loading: false,
      loaded: false,
      alert: false,
      intervalId: <any>0,
      limit: 1,
      periods: [
        { value: 1, title: i18n.global.n(1) + i18n.global.t('date.h')},
        { value: 6, title: i18n.global.n(6) + i18n.global.t('date.h')},
        { value: 12, title: i18n.global.n(12) + i18n.global.t('date.h')},
        { value: 24, title: i18n.global.n(1) + i18n.global.t('date.d')},
        { value: 48, title: i18n.global.n(2) + i18n.global.t('date.d')},
        { value: 240, title: i18n.global.n(10) + i18n.global.t('date.d')},
        { value: 480, title: i18n.global.n(20) + i18n.global.t('date.d')},
        { value: 720, title: i18n.global.n(30) + i18n.global.t('date.d')},
        { value: 1440, title: i18n.global.n(60) + i18n.global.t('date.d')},
        { value: 2160, title: i18n.global.n(90) + i18n.global.t('date.d')},
      ],
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: {
          intersect: false,
          mode: 'index',
        },
        elements: {
          point: { pointStyle: 'crossRot' }
        },
        plugins: {
          tooltip: {
            callbacks: {
              label: (ctx:any) => {
                return `${ctx.dataset.label}: ${HumanReadable.sizeFormat(Number(ctx.raw || 0))}`
              },
              footer: (items:any[]) => {
                return HumanReadable.sizeFormat(items.reduce((acc, c) => acc + c.raw, 0))
              }
            }
          }
        },
        scales: {
          y: {
            grid: {
              color: '#777777',
            },
            beginAtZero: true,
            ticks: {
              callback: function(label:any, index: number) {
                return label == 0 ? 0 : HumanReadable.sizeFormat(label,0)
              },
              count: 10
            }
          }
        }
      },
      usage: ref(<any>{}),
    }
  },
  beforeUnmount() {
    this.stopPolling()
  },
  methods: {
    stopPolling() {
      if (this.intervalId) {
        clearInterval(this.intervalId)
        this.intervalId = 0
      }
    },
    async loadData() {
      this.loading = true
      const data = await HttpUtils.get('api/stats', { resource: this.resource, tag: this.tag, limit: this.limit })
      if (data.success && data.obj) {
        const obj = <any[]>data.obj
        const l = String(i18n.global.locale) == 'fa' ? "fa-IR" : "en-US"
        const points = new Map<number, { up: number | null; down: number | null }>()
        obj.forEach(row => {
          const dateTime = Number(row.dateTime)
          if (!Number.isFinite(dateTime)) return

          const point = points.get(dateTime) || { up: null, down: null }
          const key = row.direction ? 'up' : 'down'
          point[key] = (point[key] || 0) + Number(row.traffic || 0)
          points.set(dateTime, point)
        })
        const dates = [...points.keys()].sort((a, b) => a - b)
        this.usage = {
          labels: dates.map(dateTime => this.genLable(dateTime * 1000, l)),
          datasets: [
            {
              label: i18n.global.t('stats.upload'),
              backgroundColor: 'rgba(255, 165, 0, 0.4)',
              borderColor: 'rgba(255, 165, 0)',
              fill: true,
              data: dates.map(dateTime => points.get(dateTime)?.up ?? null),
            },
            {
              label: i18n.global.t('stats.download'),
              backgroundColor: 'rgba(0, 128, 0, 0.2)',
              borderColor: 'rgba(0, 128, 0)',
              fill: true,
              data: dates.map(dateTime => points.get(dateTime)?.down ?? null),
            }
          ],
        }
        this.loaded = dates.length > 0
        this.alert = dates.length === 0
      } else {
        this.alert = true
        this.loaded = false
      }
      this.loading = false
    },
    genLable(step:number, locale: string) {
      return new Date(step).toLocaleString(locale,{
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      })
    },
  },
  watch: {
    visible(v) {
      if (v) {
        this.limit = 1
        this.loadData()
        this.stopPolling()
        this.intervalId = setInterval(() => {
          this.loadData()
        }, 10000)
      } else {
        this.loaded = false
        this.alert = false
        this.usage.labels = []
        if (this.usage.datasets) {
          this.usage.datasets[0].data = []
          this.usage.datasets[1].data = []
        }
        this.stopPolling()
      }
    }
  }
}
</script>

<style scoped lang="scss">
.stats-dialog__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.stats-dialog__body {
  display: grid;
  gap: 10px;
  padding: 10px 16px 16px;
}

.stats-dialog__resource {
  min-width: 0;
  overflow: hidden;
  text-align: center;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.stats-dialog__periods {
  overflow-x: auto;
  padding-bottom: 2px;
}

.stats-dialog__periods :deep(.v-selection-control-group) {
  flex-wrap: wrap;
  justify-content: center;
}

.stats-dialog__chart {
  height: clamp(280px, 40vh, 430px);
  padding: 8px;
}

@media (max-width: 600px) {
  .stats-dialog__body {
    padding-inline: 10px;
  }

  .stats-dialog__periods :deep(.v-selection-control) {
    flex: 0 0 20%;
    min-width: 64px;
  }

  .stats-dialog__chart {
    height: 320px;
    padding-inline: 0;
  }
}
</style>
