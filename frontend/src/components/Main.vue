<template>
  <LogVue v-model="logModal.visible" :control="logModal" :visible="logModal.visible" />
  <Backup v-model="backupModal.visible" :control="backupModal" :visible="backupModal.visible" />
  <UsageStats v-model:visible="usageStatsModal.visible" />
  <v-container fluid class="fill-height main-shell" :loading="loading">
    <div class="main-shell__glow main-shell__glow--one"></div>
    <div class="main-shell__glow main-shell__glow--two"></div>
    <v-responsive :class="reloadItems.length>0 ? 'fill-height' : 'align-center'">
      <div class="main-shell__inner">
        <v-card class="main-hero" rounded="xl" variant="flat">
          <div class="main-hero__topline">
            <span class="main-hero__badge">{{ $t('main.hero.badge') }}</span>
            <span class="main-hero__badge main-hero__badge--soft">{{ $t('main.tiles') }}</span>
          </div>
          <v-row class="main-hero__content" align="center">
            <v-col cols="12" lg="7">
              <div class="main-hero__brand">
                <div class="main-hero__brand-icon">
                  <v-img :src="logoUrl" alt="NovaPanel logo" cover />
                </div>
                <div class="main-hero__brand-copy">
                  <div class="main-hero__eyebrow">NovaPanel</div>
                </div>
              </div>
              <p class="main-hero__subtitle">{{ $t('main.hero.subtitle') }}</p>
            </v-col>
            <v-col cols="12" lg="5">
              <v-card class="main-hero__panel" rounded="xl" variant="flat">
                <div class="main-hero__panel-title">{{ $t('main.hero.live') }}</div>
                <div class="main-hero__status-list">
                  <div
                    v-for="(item, index) in heroStackItems"
                    :key="item.label"
                    class="main-hero__status-row"
                    :class="{ 'main-hero__status-row--last': index === heroStackItems.length - 1 }"
                  >
                    <span class="main-hero__status-row-left">
                      <span class="main-hero__status-row-icon" :class="`main-hero__status-row-icon--${item.tone}`">
                        <v-icon :icon="item.icon" size="small" />
                      </span>
                      <span class="main-hero__status-row-label">{{ item.label }}</span>
                    </span>
                    <strong class="main-hero__status-row-value">{{ item.value }}</strong>
                  </div>
                </div>
              </v-card>
            </v-col>
          </v-row>
        </v-card>

        <v-card class="main-toolbar" rounded="xl" variant="flat">
          <div class="main-toolbar__label">{{ $t('main.tiles') }}</div>
          <div class="main-toolbar__actions">
            <v-dialog v-model="menu" :close-on-content-click="false" transition="scale-transition" max-width="900">
              <template v-slot:activator="{ props }">
                <v-btn v-bind="props" class="main-toolbar__button" hide-details variant="flat">
                  <v-icon icon="mdi-star-plus" start />
                  {{ $t('main.tiles') }}
                </v-btn>
              </template>
              <v-card class="main-menu" rounded="xl" variant="flat">
                <v-card-title class="main-menu__title">
                  <span>{{ $t('main.tiles') }}</span>
                  <v-btn icon variant="text" @click="menu = false">
                    <v-icon icon="mdi-close"></v-icon>
                  </v-btn>
                </v-card-title>
                <v-divider></v-divider>
                <v-row v-for="items in menuItems" :key="items.title" density="compact" class="main-menu__group">
                  <v-col cols="12">
                    <v-card :subtitle="items.title" variant="flat" class="main-menu__section">
                      <v-card-text>
                        <v-row density="compact">
                          <v-col cols="12" md="6" lg="3" v-for="item in items.value" :key="item.value">
                            <v-switch
                              density="compact"
                              v-model="reloadItems"
                              :value="item.value"
                              color="primary"
                              :label="item.title"
                              hide-details
                            ></v-switch>
                          </v-col>
                        </v-row>
                      </v-card-text>
                    </v-card>
                  </v-col>
                </v-row>
              </v-card>
            </v-dialog>
            <v-btn class="main-toolbar__button" variant="flat" hide-details @click="backupModal.visible = true">
              <v-icon icon="mdi-backup-restore" start />
              {{ $t('main.backup.title') }}
            </v-btn>
            <v-btn class="main-toolbar__button" variant="flat" hide-details @click="logModal.visible = true">
              <v-icon icon="mdi-list-box-outline" start />
              {{ $t('basic.log.title') }}
            </v-btn>
            <v-btn class="main-toolbar__button" variant="flat" hide-details @click="usageStatsModal.visible = true">
              <v-icon icon="mdi-chart-box-outline" start />
              {{ $t('main.stats.title') }}
            </v-btn>
          </div>
        </v-card>

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
                  <v-row class="main-info-grid">
                    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.host') }}</v-col>
                    <v-col cols="8" class="main-info-grid__value">{{ tilesData.sys?.hostName }}</v-col>
                    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.cpu') }}</v-col>
                    <v-col cols="8" class="main-info-grid__value">
                      <v-chip density="compact" variant="flat">
                        <v-tooltip activator="parent" location="top" style="direction: ltr;">
                          {{ tilesData.sys?.cpuType }}
                        </v-tooltip>
                       {{ tilesData.sys?.cpuCount }} {{ $t('main.info.core') }}
                      </v-chip>
                    </v-col>
                    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.firewall') }}</v-col>
                    <v-col cols="8" class="main-info-grid__value">
                      <v-chip density="compact" color="primary" variant="flat">
                        {{ tilesData.sys?.firewallBackend }}
                      </v-chip>
                    </v-col>
                    <v-col cols="4" class="main-info-grid__label">IP</v-col>
                    <v-col cols="8" class="main-info-grid__value">
                      <v-chip density="compact" color="primary" variant="flat" v-if="tilesData.sys?.ipv4?.length>0">
                        <v-tooltip activator="parent" location="top" style="direction: ltr;">
                          <span v-html="tilesData.sys?.ipv4?.join('<br />')"></span>
                        </v-tooltip>
                        IPv4
                      </v-chip>
                      <v-chip density="compact" color="primary" variant="flat" v-if="tilesData.sys?.ipv6?.length>0">
                        <v-tooltip activator="parent" location="top" style="direction: ltr;">
                          <span v-html="tilesData.sys?.ipv6?.join('<br />')"></span>
                        </v-tooltip>
                        IPv6
                      </v-chip>
                    </v-col>
                    <v-col cols="4" class="main-info-grid__label">NovaPanel</v-col>
                    <v-col cols="8" class="main-info-grid__value">
                      <v-chip density="compact" color="blue">
                        v{{ tilesData.sys?.appVersion }}
                      </v-chip>
                    </v-col>
                    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.uptime') }}</v-col>
                    <v-col cols="8" class="main-info-grid__value" v-tooltip:top="$t('main.info.startupTime')
                      + ': ' + new Date((tilesData.sys?.bootTime || 0) * 1000).toLocaleString(locale)">
                      {{ HumanReadable.formatSecond((Date.now()/1000) - tilesData.sys?.bootTime) }}
                    </v-col>
                  </v-row>
                </template>
                <template v-if="i == 'i-sbd'">
                  <v-row class="main-info-grid">
                    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.running') }}</v-col>
                    <v-col cols="8" class="main-info-grid__value">
                      <v-chip density="compact" color="success" variant="flat" v-if="tilesData.sbd?.running">{{ $t('yes') }}</v-chip>
                      <v-chip density="compact" color="error" variant="flat" v-else>{{ $t('no') }}</v-chip>
                      <v-chip density="compact" color="transparent" v-if="tilesData.sbd?.running && !loading" class="main-info-grid__restart" @click="restartSingbox()">
                        <v-tooltip activator="parent" location="top">
                          {{ $t('actions.restartSb') }}
                        </v-tooltip>
                        <v-icon icon="mdi-restart" color="warning" />
                      </v-chip>
                    </v-col>
                    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.memory') }}</v-col>
                    <v-col cols="8" class="main-info-grid__value">
                      <v-chip density="compact" color="primary" variant="flat" v-if="tilesData.sbd?.stats?.Alloc">
                        {{ HumanReadable.sizeFormat(tilesData.sbd?.stats?.Alloc) }}
                      </v-chip>
                    </v-col>
                    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.threads') }}</v-col>
                    <v-col cols="8" class="main-info-grid__value">
                      <v-chip density="compact" color="primary" variant="flat" v-if="tilesData.sbd?.stats?.NumGoroutine">
                        {{ tilesData.sbd?.stats?.NumGoroutine }}
                      </v-chip>
                    </v-col>
                    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.uptime') }}</v-col>
                    <v-col cols="8" class="main-info-grid__value">{{ HumanReadable.formatSecond(tilesData.sbd?.stats?.Uptime) }}</v-col>
                    <v-col cols="4" class="main-info-grid__label">{{ $t('online') }}</v-col>
                    <v-col cols="8" class="main-info-grid__value">
                      <template v-if="tilesData.sbd?.running">
                        <v-chip density="compact" color="primary" variant="flat" v-if="Data().onlines.user">
                          <v-tooltip activator="parent" location="top" overflow="auto">
                            <span v-text="$t('pages.clients')" style="font-weight: bold;"></span><br/>
                            <span v-for="user in Data().onlines.user">{{ user }}<br /></span>
                          </v-tooltip>
                          {{ Data().onlines.user?.length }}
                        </v-chip>
                        <v-chip density="compact" color="success" variant="flat" v-if="Data().onlines.inbound">
                          <v-tooltip activator="parent" location="top" :text="$t('pages.inbounds')">
                            <span v-text="$t('pages.inbounds')" style="font-weight: bold;"></span><br/>
                            <span v-for="i in Data().onlines.inbound">{{ i }}<br /></span>
                          </v-tooltip>
                          {{ Data().onlines.inbound?.length }}
                        </v-chip>
                        <v-chip density="compact" color="info" variant="flat" v-if="Data().onlines.outbound">
                          <v-tooltip activator="parent" location="top" :text="$t('pages.outbounds')">
                            <span v-text="$t('pages.outbounds')" style="font-weight: bold;"></span><br/>
                            <span v-for="o in Data().onlines.outbound">{{ o }}<br /></span>
                          </v-tooltip>
                          {{ Data().onlines.outbound?.length }}
                        </v-chip>
                      </template>
                    </v-col>
                  </v-row>
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
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { i18n, locale } from '@/locales'
import LogVue from '@/layouts/modals/Logs.vue'
import Backup from '@/layouts/modals/Backup.vue'
import UsageStats from '@/layouts/modals/UsageStats.vue'
import logoUrl from '@/assets/logo.png'

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

const reloadItems = computed({
  get() { return Data().reloadItems },
  set(v:string[]) {
    if (Data().reloadItems.length == 0 && v.length>0) startTimer()
    if (Data().reloadItems.length > 0 && v.length == 0) stopTimer()
    Data().reloadItems = v
    v.length>0 ? localStorage.setItem(reloadItemsStorageKey,v.join(',')) : localStorage.removeItem(reloadItemsStorageKey)
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
  if (pollingActive) return
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

onMounted(async () => {
  loading.value = true
  if (Data().reloadItems.length != 0) {
    await reloadData()
    startTimer()
  }
  loading.value = false
})

onBeforeUnmount(() => {
  stopTimer()
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

.main-hero,
.main-toolbar,
.main-tile,
.main-menu,
.main-hero__panel {
  border: 1px solid var(--np-border);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.2), transparent 28%),
    var(--np-surface);
  backdrop-filter: blur(30px) saturate(1.15);
  box-shadow: var(--np-shadow);
}

.main-hero,
.main-toolbar,
.main-tile,
.main-menu,
.main-hero__panel {
  position: relative;
  overflow: hidden;
}

.main-hero::before,
.main-toolbar::before,
.main-menu::before,
.main-hero__panel::before,
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

.main-hero > *,
.main-toolbar > *,
.main-tile > *,
.main-menu > *,
.main-hero__panel > * {
  position: relative;
  z-index: 1;
}

.main-hero {
  padding: 28px;
  border-radius: 32px;
}

.main-hero__topline {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 18px;
}

.main-hero__badge {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border-radius: 999px;
  padding: 6px 12px;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  border: 1px solid rgba(10, 132, 255, 0.14);
  background: rgba(255, 255, 255, 0.36);
  color: var(--np-accent);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.55);
}

.main-hero__badge--soft {
  color: var(--np-text-muted);
  background: rgba(255, 255, 255, 0.26);
}

.main-hero__content {
  row-gap: 18px;
}

.main-hero__brand {
  display: flex;
  align-items: center;
  gap: 14px;
}

.main-hero__brand-icon {
  flex: 0 0 auto;
  width: 52px;
  height: 52px;
  overflow: hidden;
  border-radius: 18px;
  border: 1px solid rgba(125, 211, 252, 0.18);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.6), rgba(255, 255, 255, 0.28)),
    rgba(255, 255, 255, 0.46);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.7),
    0 12px 24px rgba(15, 23, 42, 0.08);
}

.main-hero__brand-icon :deep(img) {
  object-fit: cover;
}

.main-hero__brand-copy {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-top: 0;
}

.main-hero__eyebrow {
  display: inline-flex;
  align-self: flex-start;
  padding: 0;
  border-radius: 0;
  border: 0;
  background: transparent;
  font-size: clamp(1.6rem, 2.5vw, 2.2rem);
  font-weight: 800;
  line-height: 1;
  letter-spacing: -0.055em;
  text-transform: none;
  color: var(--np-accent);
}

.main-hero__subtitle {
  max-width: 54ch;
  margin: 16px 0 0;
  color: var(--np-text-muted);
  font-size: 0.98rem;
  line-height: 1.9;
}

.main-hero__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 18px;
}

.main-hero__chip {
  border-radius: 999px;
  border: 1px solid rgba(10, 132, 255, 0.12);
  background: rgba(255, 255, 255, 0.54);
  color: var(--np-text-main);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.55);
}

.main-hero__chip-label {
  margin-inline-end: 6px;
  opacity: 0.75;
}

.main-hero__panel {
  padding: 18px;
  border-radius: 28px;
  background: rgba(255, 255, 255, 0.3);
}

.main-hero__panel-title {
  margin-bottom: 14px;
  font-size: 0.92rem;
  font-weight: 700;
  color: var(--np-text-muted);
}

.main-hero__status-list {
  display: grid;
  gap: 0;
}

.main-hero__status-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 11px 0;
  border-bottom: 1px solid rgba(148, 163, 184, 0.14);
}

.main-hero__status-row--last {
  border-bottom: 0;
  padding-bottom: 4px;
}

.main-hero__status-row-left {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.main-hero__status-row-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  flex: 0 0 auto;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.68);
  color: var(--np-accent);
  border: 1px solid rgba(10, 132, 255, 0.12);
}

.main-hero__status-row-icon--green {
  color: rgb(34, 197, 94);
}

.main-hero__status-row-icon--red {
  color: rgb(239, 68, 68);
}

.main-hero__status-row-icon--indigo {
  color: rgb(79, 70, 229);
}

.main-hero__status-row-icon--teal {
  color: rgb(20, 184, 166);
}

.main-hero__status-row-label {
  min-width: 0;
  font-size: 0.82rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--np-text-muted);
}

.main-hero__status-row-value {
  flex: 0 0 auto;
  font-size: 0.98rem;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--np-text-main);
}

.main-toolbar {
  margin-top: 16px;
  padding: 14px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  border-radius: 28px;
}

.main-toolbar__label {
  font-size: 0.88rem;
  font-weight: 700;
  color: var(--np-text-muted);
}

.main-toolbar__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.main-toolbar__button {
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.5);
  border: 1px solid rgba(148, 163, 184, 0.15);
  color: var(--np-text-main);
  text-transform: none;
  letter-spacing: 0;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.5);
}

.main-toolbar__button:hover {
  transform: translateY(-1px);
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

.main-info-grid {
  margin-top: 0;
}

.main-info-grid__label {
  color: var(--np-text-muted);
}

.main-info-grid__value {
  text-align: start;
}

.main-info-grid__restart {
  cursor: pointer;
}

.main-menu {
  overflow: hidden;
  border-radius: 30px;
  background: rgba(255, 255, 255, 0.42);
}

.main-menu__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 20px;
}

.main-menu__group {
  margin: 0;
}

.main-menu__section {
  background: rgba(255, 255, 255, 0.28);
}

:global(.v-theme--dark) .main-shell__glow--one {
  background: radial-gradient(circle, rgba(125, 211, 252, 0.18), transparent 68%);
}

:global(.v-theme--dark) .main-shell__glow--two {
  background: radial-gradient(circle, rgba(59, 130, 246, 0.12), transparent 70%);
}

:global(.v-theme--dark) .main-hero,
:global(.v-theme--dark) .main-toolbar,
:global(.v-theme--dark) .main-tile,
:global(.v-theme--dark) .main-menu,
:global(.v-theme--dark) .main-hero__panel {
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

:global(.v-theme--dark) .main-hero__badge,
:global(.v-theme--dark) .main-toolbar__button,
:global(.v-theme--dark) .main-hero__chip,
:global(.v-theme--dark) .main-menu__section {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(148, 163, 184, 0.18);
}

:global(.v-theme--dark) .main-hero__status-row {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.12), rgba(255, 255, 255, 0.05)),
    rgba(24, 32, 48, 0.98);
  border-color: rgba(148, 163, 184, 0.2);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.07),
    0 16px 26px rgba(0, 0, 0, 0.18);
}

:global(.v-theme--dark) .main-hero__status-row-icon {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(148, 163, 184, 0.2);
}

:global(.v-theme--dark) .main-hero__status-row-label {
  color: rgba(186, 202, 224, 0.76);
}

:global(.v-theme--dark) .main-hero__status-row-value {
  color: rgba(237, 244, 255, 0.96);
}

:global(.v-theme--dark) .main-hero__badge--soft {
  color: rgba(186, 202, 224, 0.78);
}

:global(.v-theme--dark) .main-hero__brand-icon {
  border-color: rgba(125, 211, 252, 0.14);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.12), rgba(255, 255, 255, 0.04)),
    rgba(10, 16, 28, 0.82);
}

:global(.v-theme--dark) .main-toolbar__label,
:global(.v-theme--dark) .main-info-grid__label,
:global(.v-theme--dark) .main-hero__subtitle,
:global(.v-theme--dark) .main-hero__panel-title {
  color: rgba(186, 202, 224, 0.78);
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

  .main-hero,
  .main-toolbar {
    padding: 14px;
  }

  .main-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
}

@media (max-width: 600px) {
  .main-hero {
    padding: 12px;
    border-radius: 24px;
  }

  .main-hero__content {
    row-gap: 12px;
  }

  .main-hero__content > .v-col {
    padding-top: 0;
    padding-bottom: 0;
  }

  .main-hero__topline {
    display: none;
  }

  .main-hero__brand {
    align-items: center;
    gap: 10px;
  }

  .main-hero__brand-icon {
    width: 44px;
    height: 44px;
    border-radius: 15px;
  }

  .main-hero__eyebrow {
    font-size: clamp(1.35rem, 8vw, 1.65rem);
  }

  .main-hero__subtitle {
    margin-top: 10px;
    font-size: 0.9rem;
    line-height: 1.55;
  }

  .main-hero__chips {
    margin-top: 12px;
    gap: 8px;
    flex-wrap: nowrap;
    overflow-x: auto;
    padding-bottom: 2px;
    scrollbar-width: none;
  }

  .main-hero__chips::-webkit-scrollbar {
    display: none;
  }

  .main-hero__chip {
    min-height: 30px;
    padding-inline: 10px;
    font-size: 0.76rem;
  }

  .main-hero__panel {
    padding: 10px 12px 8px;
    border-radius: 20px;
  }

  .main-hero__panel-title {
    margin-bottom: 8px;
    font-size: 0.8rem;
  }

  .main-hero__status-list {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }

  .main-hero__status-row {
    min-width: 0;
    gap: 8px;
    padding: 10px 12px;
    border-radius: 18px;
    flex-direction: column;
    align-items: flex-start;
  }

  .main-hero__status-row-left {
    align-items: flex-start;
    gap: 8px;
    width: 100%;
  }

  .main-hero__status-row-icon {
    width: 28px;
    height: 28px;
  }

  .main-hero__status-row-label {
    font-size: 0.7rem;
    letter-spacing: 0.05em;
  }

  .main-hero__status-row-value {
    font-size: 0.9rem;
    margin-left: 0;
    align-self: flex-end;
  }

  .main-grid {
    margin-top: 12px;
  }

  .main-toolbar {
    margin-top: 12px;
    padding: 12px;
    gap: 10px;
  }

  .main-toolbar__label {
    margin-bottom: 2px;
  }

  .main-toolbar__actions {
    gap: 8px;
    flex-wrap: nowrap;
    overflow-x: auto;
    padding-bottom: 2px;
    scrollbar-width: none;
  }

  .main-toolbar__actions::-webkit-scrollbar {
    display: none;
  }

  .main-toolbar__button {
    flex: 0 0 auto;
    min-height: 34px;
    padding-inline: 12px;
  }

  .main-tile {
    min-height: 186px;
  }
}
</style>
