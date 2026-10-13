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
          :maintenance="maintenanceStatus"
          :maintenance-pending="maintenanceActionPending"
          :maintenance-error="maintenanceError"
          @update:reload-items="reloadItems = $event"
          @backup="backupModal.visible = true"
          @log="logModal.visible = true"
          @stats="usageStatsModal.visible = true"
          @maintenance-request="requestMaintenanceChange"
        />

        <v-dialog v-model="pauseMaintenanceDialog" max-width="500" data-testid="maintenance-pause-confirmation">
          <v-card rounded="xl">
            <v-card-title>{{ $t('main.maintenance.confirmTitle') }}</v-card-title>
            <v-card-text>
              {{ $t('main.maintenance.confirmWarning') }}
            </v-card-text>
            <v-card-actions class="justify-end px-6 pb-5">
              <v-btn variant="text" @click="pauseMaintenanceDialog = false">{{ $t('main.maintenance.cancel') }}</v-btn>
              <v-btn
                color="warning"
                variant="flat"
                :disabled="maintenanceActionPending"
                data-testid="maintenance-pause-confirm"
                @click="confirmPauseMaintenance"
              >
                {{ $t('main.maintenance.confirmPause') }}
              </v-btn>
            </v-card-actions>
          </v-card>
        </v-dialog>

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
                <DeferredRender v-if="i.charAt(0) == 'h'">
                  <History :tilesData="tilesData" :type="i" />
                  <template #placeholder>
                    <div class="main-tile__placeholder">
                      <v-icon icon="mdi-chart-line" size="26" color="primary" />
                      <span>{{ $t('loading') }}</span>
                    </div>
                  </template>
                </DeferredRender>
                <template v-if="i == 'i-sys'">
                  <MainSystemCard :tiles-data="tilesData" />
                </template>
                <template v-if="i == 'i-sbd'">
                  <MainRuntimeCard
                    :tiles-data="tilesData"
                    :loading="loading"
                    :onlines="onlines"
                    :restart-disabled="maintenanceRestartDisabled"
                    @restart="restartSingbox()"
                  />
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
import api, { readApi } from '@/plugins/api'
import HttpUtils, { logout } from '@/plugins/httputil'
import { HumanReadable } from '@/plugins/utils'
import Data, { reloadItemsStorageKey } from '@/store/modules/data'
import Gauge from '@/components/tiles/Gauge.vue'
import DeferredRender from '@/components/dashboard/DeferredRender.vue'
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
const History = defineAsyncComponent(() => import('@/components/tiles/History.vue'))

const loading = ref(false)
const menu = ref(false)
type MaintenanceStatus = {
  enabled: boolean
  transitioning: boolean
  error?: string
}

const maintenanceStatus = ref<MaintenanceStatus | null>(null)
const maintenanceStatusError = ref<string | null>(null)
const maintenanceActionError = ref<string | null>(null)
const maintenanceActionPending = ref(false)
const pauseMaintenanceDialog = ref(false)
const maintenanceRestartDisabled = computed(() =>
  !maintenanceStatus.value ||
  maintenanceStatus.value.enabled ||
  maintenanceStatus.value.transitioning ||
  maintenanceStatusError.value !== null ||
  maintenanceActionPending.value,
)
let maintenanceStatusRequestId = 0
const maintenanceError = computed(() => {
  const t = i18n.global.t
  const detail = (value: string | null) => value?.trim() || t('main.maintenance.unknownError')
  const errors: string[] = []
  if (maintenanceActionError.value !== null) {
    errors.push(t('main.maintenance.actionFailed', { detail: detail(maintenanceActionError.value) }))
  }
  if (maintenanceStatusError.value !== null) {
    errors.push(t('main.maintenance.statusFailed', { detail: detail(maintenanceStatusError.value) }))
  }
  if (maintenanceStatus.value?.error) {
    errors.push(t('main.maintenance.reportedError', { detail: maintenanceStatus.value.error }))
  }
  return errors.join(' ')
})

const handleMaintenanceAuthError = (message?: string) => {
  if (message === 'Invalid login') void logout()
}

const maintenanceErrorDetail = (message?: string) => {
  if (message === 'Invalid login') return i18n.global.t('invalidLogin')
  if (message === 'Network Error') return i18n.global.t('main.maintenance.networkError')
  return message ?? ''
}
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

const refreshMaintenanceStatus = async () => {
  const requestId = ++maintenanceStatusRequestId
  try {
    const response = await readApi('api/maintenance-status')
    const message = response.data as { success?: boolean; msg?: string; obj?: Partial<MaintenanceStatus> | null }
    if (requestId !== maintenanceStatusRequestId) return
    if (!message?.success || typeof message.obj?.enabled !== 'boolean') {
      maintenanceStatusError.value = maintenanceErrorDetail(message?.msg)
      handleMaintenanceAuthError(message?.msg)
      return
    }
    maintenanceStatus.value = {
      enabled: message.obj.enabled,
      transitioning: message.obj.transitioning === true,
      error: typeof message.obj.error === 'string' ? message.obj.error : undefined,
    }
    maintenanceStatusError.value = null
  } catch (error) {
    if (requestId === maintenanceStatusRequestId) {
      maintenanceStatusError.value = maintenanceErrorDetail(error instanceof Error ? error.message : String(error))
    }
  }
}

const changeMaintenance = async (enabled: boolean) => {
  if (maintenanceActionPending.value) return
  maintenanceActionPending.value = true
  maintenanceActionError.value = null
  let actionError: string | null = null
  try {
    try {
      const response = await api.post('api/maintenance', { enabled }, {
        headers: { 'Content-Type': 'application/json' },
      })
      const message = response.data as { success?: boolean; msg?: string }
      if (!message?.success) {
        actionError = maintenanceErrorDetail(message?.msg)
        handleMaintenanceAuthError(message?.msg)
      }
    } catch (error) {
      actionError = maintenanceErrorDetail(error instanceof Error ? error.message : String(error))
    }
    await refreshMaintenanceStatus()
    if (actionError !== null) maintenanceActionError.value = actionError
  } finally {
    maintenanceActionPending.value = false
  }
}

const requestMaintenanceChange = (enabled: boolean) => {
  if (!maintenanceStatus.value || maintenanceStatus.value.transitioning || maintenanceActionPending.value) return
  if (enabled) {
    pauseMaintenanceDialog.value = true
    return
  }
  void changeMaintenance(false)
}

const confirmPauseMaintenance = () => {
  pauseMaintenanceDialog.value = false
  void changeMaintenance(true)
}

let timerId: ReturnType<typeof setTimeout> | null = null
let pollingActive = false
let stopVisibilityListener: (() => void) | null = null
let maintenanceTimerId: ReturnType<typeof setTimeout> | null = null

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

const stopMaintenanceTimer = () => {
  if (maintenanceTimerId) {
    clearTimeout(maintenanceTimerId)
    maintenanceTimerId = null
  }
}

const scheduleMaintenancePoll = () => {
  if (maintenanceTimerId || !isPageVisible()) return
  maintenanceTimerId = setTimeout(async () => {
    maintenanceTimerId = null
    if (!isPageVisible()) return
    await refreshMaintenanceStatus()
    scheduleMaintenancePoll()
  }, 15000)
}

onMounted(async () => {
  loading.value = true
  void refreshMaintenanceStatus()
  scheduleMaintenancePoll()
  if (Data().reloadItems.length != 0) {
    await reloadData()
    startTimer()
  }
  stopVisibilityListener = onPageVisibilityChange((visible) => {
    if (!visible) {
      stopTimer()
      stopMaintenanceTimer()
      return
    }
    syncPollingState()
    void refreshMaintenanceStatus()
    scheduleMaintenancePoll()
  })
  loading.value = false
})

onBeforeUnmount(() => {
  stopTimer()
  stopMaintenanceTimer()
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
    transform 180ms var(--np-ease-out),
    box-shadow 180ms ease,
    border-color 180ms ease;
}

@media (hover: hover) and (pointer: fine) {
  .main-tile:hover {
    transform: translateY(-3px);
    border-color: rgba(10, 132, 255, 0.2);
    box-shadow: 0 24px 70px rgba(15, 23, 42, 0.12);
  }
}

@media (prefers-reduced-motion: reduce) {
  .main-tile {
    transform: none !important;
    transition-duration: 0.01ms;
  }
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

.main-tile__placeholder {
  min-height: 158px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: rgba(var(--v-theme-on-surface), 0.62);
  font-size: 0.92rem;
}

.main-shell.v-theme--dark .main-shell__glow--one {
  background: radial-gradient(circle, rgba(125, 211, 252, 0.18), transparent 68%);
}

.main-shell.v-theme--dark .main-shell__glow--two {
  background: radial-gradient(circle, rgba(59, 130, 246, 0.12), transparent 70%);
}

.main-shell.v-theme--dark .main-tile {
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
