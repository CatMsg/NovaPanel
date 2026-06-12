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
                <v-avatar size="72" class="main-hero__avatar">
                  <v-img src="@/assets/logo.svg" alt="NovaPanel logo"></v-img>
                </v-avatar>
                <div>
                  <div class="main-hero__eyebrow">NovaPanel</div>
                  <h1 class="main-hero__title">{{ $t('main.hero.title') }}</h1>
                </div>
              </div>
              <p class="main-hero__subtitle">{{ $t('main.hero.subtitle') }}</p>
              <div class="main-hero__chips">
                <v-chip
                  v-for="stat in heroStats"
                  :key="stat.label"
                  class="main-hero__chip"
                  :color="stat.color"
                  variant="flat"
                  density="comfortable"
                >
                  <v-icon :icon="stat.icon" start size="small" />
                  <span class="main-hero__chip-label">{{ stat.label }}:</span>
                  <span>{{ stat.value }}</span>
                </v-chip>
              </div>
            </v-col>
            <v-col cols="12" lg="5">
              <v-card class="main-hero__panel" rounded="xl" variant="flat">
                <div class="main-hero__panel-title">{{ $t('main.hero.live') }}</div>
                <div class="main-hero__panel-grid">
                  <div class="main-hero__panel-item">
                    <span>{{ $t('main.info.firewall') }}</span>
                    <strong>{{ tilesData.sys?.firewallBackend || $t('none') }}</strong>
                  </div>
                  <div class="main-hero__panel-item">
                    <span>{{ $t('main.info.running') }}</span>
                    <strong>{{ tilesData.sbd?.running ? $t('yes') : $t('no') }}</strong>
                  </div>
                  <div class="main-hero__panel-item">
                    <span>{{ $t('version') }}</span>
                    <strong>v{{ tilesData.sys?.appVersion || '--' }}</strong>
                  </div>
                  <div class="main-hero__panel-item">
                    <span>{{ $t('main.info.uptime') }}</span>
                    <strong>{{ HumanReadable.formatSecond((Date.now()/1000) - tilesData.sys?.bootTime) }}</strong>
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

const heroStats = computed(() => [
  {
    label: i18n.global.t('main.info.host'),
    value: tilesData.value.sys?.hostName || i18n.global.t('none'),
    color: 'primary',
    icon: 'mdi-server',
  },
  {
    label: i18n.global.t('main.info.firewall'),
    value: tilesData.value.sys?.firewallBackend || i18n.global.t('none'),
    color: 'teal',
    icon: 'mdi-shield-cog-outline',
  },
  {
    label: i18n.global.t('main.info.running'),
    value: tilesData.value.sbd?.running ? i18n.global.t('yes') : i18n.global.t('no'),
    color: tilesData.value.sbd?.running ? 'success' : 'error',
    icon: 'mdi-rocket-launch-outline',
  },
  {
    label: i18n.global.t('version'),
    value: tilesData.value.sys?.appVersion ? `v${tilesData.value.sys?.appVersion}` : '--',
    color: 'blue',
    icon: 'mdi-tag-outline',
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
  padding: 32px 24px 40px;
  background:
    radial-gradient(circle at top left, rgba(33, 150, 243, 0.18), transparent 28%),
    radial-gradient(circle at top right, rgba(0, 188, 212, 0.14), transparent 26%),
    linear-gradient(180deg, rgba(244, 247, 252, 0.98) 0%, rgba(235, 241, 248, 0.96) 100%);
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
  filter: blur(18px);
  opacity: 0.6;
  pointer-events: none;
}

.main-shell__glow--one {
  top: -80px;
  left: -80px;
  width: 220px;
  height: 220px;
  background: rgba(33, 150, 243, 0.18);
}

.main-shell__glow--two {
  right: -120px;
  top: 120px;
  width: 320px;
  height: 320px;
  background: rgba(0, 188, 212, 0.14);
}

.main-hero,
.main-toolbar,
.main-tile,
.main-menu,
.main-hero__panel {
  border: 1px solid rgba(148, 163, 184, 0.18);
  background: rgba(255, 255, 255, 0.78);
  backdrop-filter: blur(18px);
  box-shadow: 0 20px 60px rgba(15, 23, 42, 0.08);
}

.main-hero {
  padding: 24px;
}

.main-hero__topline {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 18px;
}

.main-hero__badge {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border-radius: 999px;
  padding: 6px 12px;
  font-size: 0.8rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  background: linear-gradient(135deg, rgba(37, 99, 235, 0.12), rgba(8, 145, 178, 0.12));
  color: rgb(var(--v-theme-primary));
}

.main-hero__badge--soft {
  background: rgba(15, 23, 42, 0.04);
  color: rgba(15, 23, 42, 0.72);
}

.main-hero__content {
  row-gap: 16px;
}

.main-hero__brand {
  display: flex;
  align-items: center;
  gap: 16px;
}

.main-hero__avatar {
  border: 1px solid rgba(37, 99, 235, 0.18);
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.9), rgba(224, 242, 254, 0.8));
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.7);
}

.main-hero__eyebrow {
  font-size: 0.82rem;
  font-weight: 700;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: rgba(37, 99, 235, 0.9);
}

.main-hero__title {
  margin: 4px 0 0;
  font-size: clamp(2rem, 3vw, 3rem);
  line-height: 1.05;
  letter-spacing: -0.03em;
}

.main-hero__subtitle {
  max-width: 58ch;
  margin: 14px 0 0;
  color: rgba(15, 23, 42, 0.72);
  font-size: 1rem;
  line-height: 1.8;
}

.main-hero__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 18px;
}

.main-hero__chip {
  border-radius: 999px;
  color: #fff;
}

.main-hero__chip-label {
  margin-inline-end: 6px;
  opacity: 0.8;
}

.main-hero__panel {
  padding: 18px;
  background:
    linear-gradient(180deg, rgba(15, 23, 42, 0.03), rgba(255, 255, 255, 0.86)),
    rgba(255, 255, 255, 0.76);
}

.main-hero__panel-title {
  margin-bottom: 12px;
  font-size: 0.95rem;
  font-weight: 700;
  color: rgba(15, 23, 42, 0.7);
}

.main-hero__panel-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.main-hero__panel-item {
  border-radius: 18px;
  padding: 14px;
  background: rgba(248, 250, 252, 0.92);
  border: 1px solid rgba(148, 163, 184, 0.15);
}

.main-hero__panel-item span {
  display: block;
  font-size: 0.78rem;
  color: rgba(71, 85, 105, 0.8);
}

.main-hero__panel-item strong {
  display: block;
  margin-top: 6px;
  font-size: 1rem;
  color: rgba(15, 23, 42, 0.9);
}

.main-toolbar {
  margin-top: 18px;
  padding: 16px 18px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.main-toolbar__label {
  font-size: 0.9rem;
  font-weight: 700;
  color: rgba(15, 23, 42, 0.7);
}

.main-toolbar__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.main-toolbar__button {
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.96);
  border: 1px solid rgba(148, 163, 184, 0.18);
  color: rgba(15, 23, 42, 0.82);
  text-transform: none;
  letter-spacing: 0;
}

.main-grid {
  margin-top: 18px;
}

.main-tile {
  position: relative;
  overflow: hidden;
  min-height: 210px;
  transition:
    transform 180ms ease,
    box-shadow 180ms ease,
    border-color 180ms ease;
}

.main-tile:hover {
  transform: translateY(-4px);
  border-color: rgba(37, 99, 235, 0.22);
  box-shadow: 0 24px 64px rgba(15, 23, 42, 0.12);
}

.main-tile__accent {
  position: absolute;
  inset: 0 0 auto 0;
  height: 5px;
  background: linear-gradient(90deg, rgba(37, 99, 235, 1), rgba(6, 182, 212, 1));
}

.main-tile--info .main-tile__accent {
  background: linear-gradient(90deg, rgba(34, 197, 94, 1), rgba(59, 130, 246, 1));
}

.main-tile__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-top: 14px;
  padding-bottom: 10px;
  font-weight: 700;
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
  color: rgba(71, 85, 105, 0.82);
}

.main-info-grid__value {
  text-align: start;
}

.main-info-grid__restart {
  cursor: pointer;
}

.main-menu {
  overflow: hidden;
}

.main-menu__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.main-menu__group {
  margin: 0;
}

.main-menu__section {
  background: rgba(255, 255, 255, 0.72);
}

@media (max-width: 960px) {
  .main-shell {
    padding: 16px 12px 24px;
  }

  .main-hero,
  .main-toolbar {
    padding: 16px;
  }

  .main-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }

  .main-hero__panel-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 600px) {
  .main-hero__brand {
    align-items: flex-start;
  }

  .main-hero__title {
    font-size: 1.7rem;
  }

  .main-hero__subtitle {
    font-size: 0.95rem;
  }
}
</style>
