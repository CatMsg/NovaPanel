<template>
  <LogVue v-model="logModal.visible" :control="logModal" :visible="logModal.visible" />
  <Backup v-model="backupModal.visible" :control="backupModal" :visible="backupModal.visible" />
  <UsageStats v-model:visible="usageStatsModal.visible" />
  <v-container fluid class="fill-height main-shell" :loading="loading">
    <div class="main-shell__glow main-shell__glow--one"></div>
    <div class="main-shell__glow main-shell__glow--two"></div>
    <v-responsive :class="reloadItems.length>0 ? 'fill-height' : 'align-center'">
      <div class="main-shell__inner">
        <MainHero :logo-url="logoUrl" :items="heroStackItems" />

        <MainToolbar
          v-model:menu="menu"
          :menu-items="menuItems"
          :reload-items="reloadItems"
          @update:reload-items="reloadItems = $event"
          @backup="backupModal.visible = true"
          @log="logModal.visible = true"
          @stats="usageStatsModal.visible = true"
        />

        <v-row class="main-grid">
          <v-col cols="12" sm="6" md="3" v-for="i in reloadItems" :key="i">
            <v-card :class="['main-tile', `main-tile--${tileKind(i)}`]" rounded="xl" variant="flat">
              <div class="main-tile__accent"></div>
              <v-card-title class="main-tile__title">
                <span>{{ tileTitle(i) }}</span>
                <div class="main-tile__actions">
                  <template v-if="i == 'i-sys'">
                    <v-icon icon="mdi-update" color="primary"
                      @click="reloadSys()" size="small" v-tooltip:top="$t('actions.update')"
                      class="main-tile__action">
                    </v-icon>
                  </template>
                  <template v-if="i == 'h-net'">
                    <v-icon icon="mdi-information" color="primary" size="small"
                      v-tooltip:top="'↓' +
                      HumanReadable.sizeFormat(tilesData.net?.recv) + ' - ' +
                      HumanReadable.sizeFormat(tilesData.net?.sent) + '↑'"
                      class="main-tile__action">
                    </v-icon>
                  </template>
                </div>
              </v-card-title>
              <v-card-text class="main-tile__content" align="center" justify="center">
                <Gauge :tilesData="tilesData" :type="i" v-if="i.charAt(0) == 'g'" />
                <History :tilesData="tilesData" :type="i" v-if="i.charAt(0) == 'h'" />
                <template v-if="i == 'i-sys'">
                  <MainSystemCard :tiles-data="tilesData" />
                </template>
                <template v-if="i == 'i-sbd'">
                  <MainRuntimeCard :tiles-data="tilesData" :loading="loading" :onlines="onlines" @restart="restartSingbox()" />
                </template>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>
      </div>
    </v-responsive>
  </v-container>
</template>

<script lang="ts" setup>
import HttpUtils from '@/plugins/httputil'
import { HumanReadable } from '@/plugins/utils'
import Data, { reloadItemsStorageKey } from '@/store/modules/data'
import Gauge from '@/components/tiles/Gauge.vue'
import History from '@/components/tiles/History.vue'
import MainHero from '@/components/dashboard/MainHero.vue'
import MainToolbar from '@/components/dashboard/MainToolbar.vue'
import MainSystemCard from '@/components/dashboard/MainSystemCard.vue'
import MainRuntimeCard from '@/components/dashboard/MainRuntimeCard.vue'
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref } from 'vue'
import { i18n } from '@/locales'
import logoUrl from '@/assets/logo.png'
import { isPageVisible, onPageVisibilityChange } from '@/utils/pageVisibility'

const LogVue = defineAsyncComponent(() => import('@/layouts/modals/Logs.vue'))
const Backup = defineAsyncComponent(() => import('@/layouts/modals/Backup.vue'))
const UsageStats = defineAsyncComponent(() => import('@/layouts/modals/UsageStats.vue'))

const loading = ref(false)
const menu = ref(false)
const menuItems = [
  { title: i18n.global.t('main.gauges'), value: [
    { title: i18n.global.t('main.gauge.cpu'), value: "g-cpu" },
    { title: i18n.global.t('main.gauge.mem'), value: "g-mem" },
    { title: i18n.global.t('main.gauge.dsk'), value: "g-dsk" },
    { title: i18n.global.t('main.gauge.swp'), value: "g-swp" },
    ]
  },
  { title: i18n.global.t('main.charts'), value: [
    { title: i18n.global.t('main.chart.cpu'), value: "h-cpu" },
    { title: i18n.global.t('main.chart.mem'), value: "h-mem" },
    { title: i18n.global.t('main.chart.net'), value: "h-net" },
    { title: i18n.global.t('main.chart.pnet'), value: "hp-net" },
    { title: i18n.global.t('main.chart.dio'), value: "h-dio" },
    ]
  },
  { title: i18n.global.t('main.infos'), value: [
    { title: i18n.global.t('main.info.sys'), value: "i-sys" },
    { title: i18n.global.t('main.info.sbd'), value: "i-sbd" },
    ]
  },
]
const allMenuItems = menuItems.flatMap(cat => cat.value)

const tilesData = ref(<any>{})
const onlines = computed(() => Data().onlines ?? {})

const reloadItems = computed({
  get() { return Data().reloadItems },
  set(v:string[]) {
    Data().reloadItems = v
    v.length>0 ? localStorage.setItem(reloadItemsStorageKey,v.join(',')) : localStorage.removeItem(reloadItemsStorageKey)
    syncPollingState()
  }
})

const tileTitle = (key: string) => allMenuItems.find(item => item.value == key)?.title ?? key

const tileKind = (key: string) => {
  if (key == 'i-sys' || key == 'i-sbd') return 'info'
  return key.charAt(0) == 'g' ? 'gauge' : 'chart'
}

const heroStackItems = computed(() => [
  {
    label: i18n.global.t('main.info.firewall'),
    value: tilesData.value.sys?.firewallBackend || i18n.global.t('none'),
    icon: 'mdi-shield-lock-outline',
    tone: 'blue',
  },
  {
    label: i18n.global.t('main.info.running'),
    value: tilesData.value.sbd?.running ? i18n.global.t('yes') : i18n.global.t('no'),
    icon: 'mdi-rocket-launch-outline',
    tone: tilesData.value.sbd?.running ? 'green' : 'red',
  },
  {
    label: i18n.global.t('main.info.systemVersion'),
    value: tilesData.value.sys?.appVersion ? `v${tilesData.value.sys?.appVersion}` : '--',
    icon: 'mdi-tag-outline',
    tone: 'indigo',
  },
  {
    label: i18n.global.t('main.info.uptime'),
    value: HumanReadable.formatSecond((Date.now()/1000) - tilesData.value.sys?.bootTime),
    icon: 'mdi-timer-outline',
    tone: 'teal',
  },
])

const reloadData = async () => {
  if (!isPageVisible()) {
    return
  }
  let request = [...new Set(reloadItems.value.map(r => r.split('-')[1]))]
  if (tilesData.value?.sys?.appVersion) {
    request = request.filter(r => r != 'sys')
  }
  const data = await HttpUtils.get('api/status',{ r: request.join(',')})
  if (data.success) {
    tilesData.value = data.obj
  }
}

const reloadSys = async () => {
  const data = await HttpUtils.get('api/status',{ r: 'sys'})
  if (data.success) {
    tilesData.value.sys = data.obj.sys
  }
}

let timerId: ReturnType<typeof setTimeout> | null = null
let pollingActive = false
let stopVisibilityListener: (() => void) | null = null

const scheduleTick = () => {
  timerId = setTimeout(async () => {
    if (!pollingActive) {
      return
    }
    await reloadData()
    if (pollingActive) {
      scheduleTick()
    }
  }, 2000)
}

const startTimer = () => {
  if (pollingActive || reloadItems.value.length === 0 || !isPageVisible()) return
  pollingActive = true
  scheduleTick()
}

const stopTimer = () => {
  pollingActive = false
  if (timerId) {
    clearTimeout(timerId)
    timerId = null
  }
}

const syncPollingState = () => {
  if (reloadItems.value.length === 0 || !isPageVisible()) {
    stopTimer()
    return
  }
  if (!pollingActive) {
    void reloadData().then(() => {
      startTimer()
    })
  }
}

onMounted(async () => {
  loading.value = true
  if (Data().reloadItems.length != 0) {
    await reloadData()
    startTimer()
  }
  stopVisibilityListener = onPageVisibilityChange((visible) => {
    if (!visible) {
      stopTimer()
      return
    }
    syncPollingState()
  })
  loading.value = false
})

onBeforeUnmount(() => {
  stopTimer()
  stopVisibilityListener?.()
  stopVisibilityListener = null
})

const logModal = ref({ visible: false })

const backupModal = ref({ visible: false })

const usageStatsModal = ref({ visible: false })

const restartSingbox = async () => {
  loading.value = true
  await HttpUtils.post('api/restartSb',{})
  loading.value = false
}
</script>

<style scoped lang="scss">
.main-shell {
  position: relative;
  overflow: hidden;
  min-height: 100vh;
  padding: 24px 20px 36px;
  background: transparent;
}

.main-shell__inner {
  position: relative;
  z-index: 1;
  width: min(1440px, 100%);
  margin: 0 auto;
}

.main-shell__glow {
  position: absolute;
  border-radius: 999px;
  filter: blur(72px);
  opacity: 0.56;
  pointer-events: none;
}

.main-shell__glow--one {
  top: -120px;
  left: -110px;
  width: 300px;
  height: 300px;
  background: radial-gradient(circle, rgba(10, 132, 255, 0.22), transparent 68%);
}

.main-shell__glow--two {
  right: -140px;
  top: 110px;
  width: 360px;
  height: 360px;
  background: radial-gradient(circle, rgba(90, 200, 250, 0.18), transparent 68%);
}

.main-tile {
  border: 1px solid var(--np-border);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.2), transparent 28%),
    var(--np-surface);
  backdrop-filter: blur(30px) saturate(1.15);
  box-shadow: var(--np-shadow);
  position: relative;
  overflow: hidden;
}

.main-tile::before {
  content: '';
  position: absolute;
  inset: 0;
  z-index: 0;
  pointer-events: none;
  background:
    radial-gradient(circle at top left, rgba(255, 255, 255, 0.38), transparent 32%),
    radial-gradient(circle at bottom right, rgba(10, 132, 255, 0.08), transparent 42%);
  opacity: 0.9;
}

.main-tile > * {
  position: relative;
  z-index: 1;
}

.main-grid {
  margin-top: 16px;
}

.main-tile {
  position: relative;
  overflow: hidden;
  min-height: 220px;
  border-radius: 28px;
  transition:
    transform 180ms ease,
    box-shadow 180ms ease,
    border-color 180ms ease;
}

.main-tile:hover {
  transform: translateY(-3px);
  border-color: rgba(10, 132, 255, 0.2);
  box-shadow: 0 24px 70px rgba(15, 23, 42, 0.12);
}

.main-tile__accent {
  position: absolute;
  inset: 0 0 auto 0;
  height: 4px;
  background: linear-gradient(90deg, rgba(10, 132, 255, 1), rgba(90, 200, 250, 1));
  opacity: 0.88;
}

.main-tile--info .main-tile__accent {
  background: linear-gradient(90deg, rgba(52, 211, 153, 1), rgba(10, 132, 255, 1));
}

.main-tile__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-top: 16px;
  padding-bottom: 10px;
  font-weight: 700;
  letter-spacing: -0.01em;
}

.main-tile__actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.main-tile__action {
  cursor: pointer;
}

.main-tile__content {
  padding: 0 16px 18px;
}

:global(.v-theme--dark) .main-shell__glow--one {
  background: radial-gradient(circle, rgba(125, 211, 252, 0.18), transparent 68%);
}

:global(.v-theme--dark) .main-shell__glow--two {
  background: radial-gradient(circle, rgba(59, 130, 246, 0.12), transparent 70%);
}

:global(.v-theme--dark) .main-tile {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.1), transparent 24%),
    rgba(17, 24, 39, 0.96) !important;
  border-color: rgba(148, 163, 184, 0.18) !important;
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.08),
    0 24px 70px rgba(0, 0, 0, 0.3) !important;
  backdrop-filter: none !important;
  -webkit-backdrop-filter: none !important;
}

@media (max-width: 960px) {
  .main-shell {
    min-height: auto;
    overflow: visible;
    padding: 12px 10px 20px;
  }

  .main-shell__glow {
    display: none;
  }
}

@media (max-width: 600px) {
  .main-grid {
    margin-top: 12px;
  }

  .main-tile {
    min-height: 186px;
  }
}
</style>
