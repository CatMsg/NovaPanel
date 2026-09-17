<template>
  <div class="sessions-page page-shell">
    <v-card class="sessions-hero glass-card" elevation="0">
      <div class="sessions-hero__main">
        <div class="sessions-hero__eyebrow">{{ $t('ui.sessions.badge') }}</div>
        <div class="sessions-hero__title-row">
          <div class="sessions-hero__icon"><v-icon icon="mdi-lan" size="30" /></div>
          <div>
            <h1>{{ $t('ui.sessions.title') }}</h1>
            <p>{{ $t('ui.sessions.subtitle') }}</p>
          </div>
        </div>
        <div class="sessions-hero__meta">
          <span>{{ $t('ui.sessions.checkedAt', { time: checkedAt }) }}</span>
          <span>•</span>
          <span>{{ $t('ui.sessions.autoRefresh') }}</span>
        </div>
      </div>
      <v-btn color="primary" variant="tonal" prepend-icon="mdi-refresh" :loading="loading" @click="loadSessions">
        {{ $t('ui.common.refresh') }}
      </v-btn>
    </v-card>

    <v-row class="sessions-summary">
      <v-col v-for="item in summaryCards" :key="item.key" cols="6" md="3">
        <v-card class="sessions-summary__card glass-card" elevation="0">
          <div class="sessions-summary__icon" :class="`sessions-summary__icon--${item.key}`"><v-icon :icon="item.icon" /></div>
          <div><strong>{{ item.value }}</strong><span>{{ item.label }}</span></div>
        </v-card>
      </v-col>
    </v-row>

    <v-alert
      v-if="errorMessages.length"
      type="warning"
      variant="tonal"
      closable
      class="mb-4"
      :title="$t('ui.sessions.partialFailure')"
      :text="errorMessages.join(' · ')"
    />

    <v-card class="sessions-content glass-card" elevation="0">
      <div class="sessions-toolbar">
        <v-text-field v-model="query" clearable hide-details prepend-inner-icon="mdi-magnify" :label="$t('ui.sessions.search')" />
        <v-select v-model="serverFilter" :items="serverOptions" hide-details :label="$t('ui.sessions.server')" />
        <v-select v-model="userFilter" :items="userOptions" hide-details :label="$t('ui.sessions.user')" />
        <v-select v-model="networkFilter" :items="networkOptions" hide-details :label="$t('network')" />
      </div>

      <v-alert v-if="!loading && filteredSessions.length === 0" type="info" variant="tonal" :text="$t('ui.sessions.empty')" />

      <v-data-table
        v-else-if="!smAndDown"
        :headers="headers"
        :items="filteredSessions"
        item-value="rowId"
        :items-per-page="15"
        density="comfortable"
        class="sessions-table"
        hover
      >
        <template #item.serverName="{ item }">
          <v-chip size="small" variant="tonal" color="primary">{{ item.serverName }}</v-chip>
        </template>
        <template #item.target="{ item }">
          <div class="sessions-target">
            <strong :title="item.domain || item.destination">{{ item.domain || item.destination || '-' }}</strong>
            <span v-if="item.domain && item.destination && item.domain !== item.destination">{{ item.destination }}</span>
          </div>
        </template>
        <template #item.path="{ item }">
          <div class="sessions-path"><span>{{ item.inbound || '-' }}</span><v-icon icon="mdi-arrow-right" size="14" /><span>{{ item.outbound || '-' }}</span></div>
        </template>
        <template #item.traffic="{ item }">
          <div class="sessions-traffic"><span>↑ {{ formatBytes(item.upload) }}</span><span>↓ {{ formatBytes(item.download) }}</span></div>
        </template>
        <template #item.startedAt="{ item }"><span :title="formatDate(item.startedAt)">{{ elapsed(item.startedAt) }}</span></template>
        <template #item.actions="{ item }">
          <v-btn icon="mdi-link-off" size="small" variant="text" color="error" :aria-label="$t('ui.sessions.disconnect')" @click="requestClose(item)" />
        </template>
      </v-data-table>

      <div v-else class="sessions-mobile">
        <v-card v-for="item in filteredSessions" :key="item.rowId" class="sessions-mobile__card" elevation="0">
          <div class="sessions-mobile__head">
            <div><v-chip size="x-small" color="primary" variant="tonal">{{ item.serverName }}</v-chip><strong>{{ item.user || $t('ui.sessions.anonymous') }}</strong></div>
            <v-btn icon="mdi-link-off" size="small" variant="text" color="error" :aria-label="$t('ui.sessions.disconnect')" @click="requestClose(item)" />
          </div>
          <div class="sessions-mobile__target">{{ item.domain || item.destination || '-' }}</div>
          <div v-if="item.domain && item.destination && item.domain !== item.destination" class="sessions-mobile__destination">{{ item.destination }}</div>
          <div class="sessions-mobile__grid">
            <div><span>{{ $t('network') }}</span><strong>{{ item.network }} · {{ item.protocol || item.kind }}</strong></div>
            <div><span>{{ $t('ui.sessions.duration') }}</span><strong>{{ elapsed(item.startedAt) }}</strong></div>
            <div><span>{{ $t('pages.inbounds') }}</span><strong>{{ item.inbound || '-' }}</strong></div>
            <div><span>{{ $t('pages.outbounds') }}</span><strong>{{ item.outbound || '-' }}</strong></div>
            <div><span>{{ $t('ui.sessions.upload') }}</span><strong>{{ formatBytes(item.upload) }}</strong></div>
            <div><span>{{ $t('ui.sessions.download') }}</span><strong>{{ formatBytes(item.download) }}</strong></div>
          </div>
        </v-card>
      </div>
    </v-card>

    <v-dialog v-model="closeDialog" max-width="440">
      <v-card rounded="xl">
        <v-card-title>{{ $t('ui.sessions.disconnect') }}</v-card-title>
        <v-card-text>{{ $t('ui.sessions.disconnectConfirm', { target: closeTarget?.domain || closeTarget?.destination || closeTarget?.user || '-' }) }}</v-card-text>
        <v-card-actions><v-spacer /><v-btn variant="text" @click="closeDialog = false">{{ $t('no') }}</v-btn><v-btn color="error" variant="tonal" :loading="closing" @click="confirmClose">{{ $t('yes') }}</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useDisplay } from 'vuetify'
import HttpUtils from '@/plugins/httputil'
import { i18n } from '@/locales'
import { isPageVisible, onPageVisibilityChange } from '@/utils/pageVisibility'

interface SessionRow {
  id: string
  serverId: string
  serverName: string
  kind: string
  inbound?: string
  outbound?: string
  user?: string
  network: string
  source?: string
  destination?: string
  domain?: string
  protocol?: string
  rule?: string
  startedAt: string
  upload: number
  download: number
  rowId?: string
}

const { smAndDown } = useDisplay()
const t = i18n.global.t
const sessions = ref<SessionRow[]>([])
const errors = ref<Record<string, string>>({})
const checkedAtRaw = ref('')
const loading = ref(false)
const closing = ref(false)
const query = ref('')
const serverFilter = ref('all')
const userFilter = ref('all')
const networkFilter = ref('all')
const closeDialog = ref(false)
const closeTarget = ref<SessionRow | null>(null)
let refreshTimer: ReturnType<typeof setInterval> | undefined
let stopVisibilityListener: (() => void) | undefined

const headers = [
  { title: t('ui.sessions.server'), key: 'serverName' },
  { title: t('ui.sessions.user'), key: 'user' },
  { title: t('ui.sessions.target'), key: 'target', sortable: false },
  { title: t('ui.sessions.path'), key: 'path', sortable: false },
  { title: t('network'), key: 'network' },
  { title: t('ui.sessions.traffic'), key: 'traffic', sortable: false },
  { title: t('ui.sessions.duration'), key: 'startedAt' },
  { title: '', key: 'actions', sortable: false, align: 'end' as const },
]

const withRowID = computed(() => sessions.value.map(item => ({ ...item, rowId: `${item.serverId}:${item.id}` })))
const optionList = (values: string[]) => [{ title: t('all'), value: 'all' }, ...Array.from(new Set(values.filter(Boolean))).sort().map(value => ({ title: value, value }))]
const serverOptions = computed(() => optionList(withRowID.value.map(item => item.serverName)))
const userOptions = computed(() => optionList(withRowID.value.map(item => item.user || '')))
const networkOptions = computed(() => optionList(withRowID.value.map(item => item.network)))
const filteredSessions = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return withRowID.value.filter(item => {
    if (serverFilter.value !== 'all' && item.serverName !== serverFilter.value) return false
    if (userFilter.value !== 'all' && item.user !== userFilter.value) return false
    if (networkFilter.value !== 'all' && item.network !== networkFilter.value) return false
    if (!needle) return true
    return [item.serverName, item.user, item.domain, item.destination, item.inbound, item.outbound, item.protocol]
      .some(value => String(value ?? '').toLowerCase().includes(needle))
  })
})
const users = computed(() => new Set(sessions.value.map(item => item.user).filter(Boolean)).size)
const totalUpload = computed(() => sessions.value.reduce((sum, item) => sum + Number(item.upload || 0), 0))
const totalDownload = computed(() => sessions.value.reduce((sum, item) => sum + Number(item.download || 0), 0))
const summaryCards = computed(() => [
  { key: 'active', icon: 'mdi-lan', label: t('ui.sessions.active'), value: sessions.value.length },
  { key: 'users', icon: 'mdi-account-multiple-outline', label: t('ui.sessions.users'), value: users.value },
  { key: 'upload', icon: 'mdi-cloud-upload', label: t('ui.sessions.upload'), value: formatBytes(totalUpload.value) },
  { key: 'download', icon: 'mdi-cloud-download', label: t('ui.sessions.download'), value: formatBytes(totalDownload.value) },
])
const checkedAt = computed(() => checkedAtRaw.value ? new Date(checkedAtRaw.value).toLocaleTimeString() : '-')
const errorMessages = computed(() => Object.entries(errors.value).map(([server, message]) => `${server}: ${message}`))

const loadSessions = async () => {
  if (loading.value || !isPageVisible()) return
  loading.value = true
  try {
    const response = await HttpUtils.get('api/sessions')
    if (response.success) {
      sessions.value = response.obj?.sessions ?? []
      errors.value = response.obj?.errors ?? {}
      checkedAtRaw.value = response.obj?.checkedAt ?? new Date().toISOString()
    }
  } finally {
    loading.value = false
  }
}

const requestClose = (item: SessionRow) => { closeTarget.value = item; closeDialog.value = true }
const confirmClose = async () => {
  if (!closeTarget.value) return
  closing.value = true
  try {
    const response = await HttpUtils.post('api/sessionClose', { serverId: closeTarget.value.serverId, id: closeTarget.value.id })
    if (response.success) {
      closeDialog.value = false
      closeTarget.value = null
      await loadSessions()
    }
  } finally {
    closing.value = false
  }
}

const formatBytes = (value: number) => {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}
const elapsed = (value: string) => {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000))
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}
const formatDate = (value: string) => new Date(value).toLocaleString()

onMounted(() => {
  void loadSessions()
  refreshTimer = setInterval(() => void loadSessions(), 5000)
  stopVisibilityListener = onPageVisibilityChange(visible => { if (visible) void loadSessions() })
})
onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
  stopVisibilityListener?.()
})
</script>

<style scoped>
.sessions-page { position: relative; }
.sessions-hero { display: flex; align-items: center; justify-content: space-between; gap: 24px; padding: 28px 30px; margin-bottom: 18px; overflow: hidden; }
.sessions-hero::after { content: ''; position: absolute; width: 260px; height: 260px; right: -90px; top: -150px; border-radius: 50%; background: radial-gradient(circle, rgba(var(--v-theme-primary), .18), transparent 68%); pointer-events: none; }
.sessions-hero__main { position: relative; z-index: 1; }
.sessions-hero__eyebrow { color: rgb(var(--v-theme-primary)); font-size: .72rem; font-weight: 800; letter-spacing: .16em; text-transform: uppercase; }
.sessions-hero__title-row { display: flex; align-items: center; gap: 15px; margin-top: 8px; }
.sessions-hero__title-row h1 { font-size: clamp(1.6rem, 3vw, 2.25rem); line-height: 1.1; }
.sessions-hero__title-row p { margin-top: 6px; color: rgba(var(--v-theme-on-surface), .72); }
.sessions-hero__icon { width: 54px; height: 54px; display: grid; place-items: center; border-radius: 18px; color: rgb(var(--v-theme-primary)); background: rgba(var(--v-theme-primary), .12); }
.sessions-hero__meta { display: flex; gap: 9px; margin-top: 14px; color: rgba(var(--v-theme-on-surface), .64); font-size: .82rem; }
.sessions-summary { margin-bottom: 4px; }
.sessions-summary__card { display: flex; align-items: center; gap: 13px; min-height: 92px; padding: 18px; }
.sessions-summary__card strong, .sessions-summary__card span { display: block; }
.sessions-summary__card strong { font-size: 1.35rem; }
.sessions-summary__card span { color: rgba(var(--v-theme-on-surface), .64); font-size: .8rem; }
.sessions-summary__icon { width: 42px; height: 42px; display: grid; place-items: center; border-radius: 14px; background: rgba(var(--v-theme-primary), .12); color: rgb(var(--v-theme-primary)); }
.sessions-summary__icon--users { color: #0f9d7a; background: rgba(15, 157, 122, .12); }
.sessions-summary__icon--upload { color: #d97706; background: rgba(217, 119, 6, .12); }
.sessions-summary__icon--download { color: #0284c7; background: rgba(2, 132, 199, .12); }
.sessions-content { padding: 20px; }
.sessions-toolbar { display: grid; grid-template-columns: minmax(260px, 1fr) repeat(3, minmax(150px, .42fr)); gap: 12px; margin-bottom: 18px; }
.sessions-target { display: flex; flex-direction: column; min-width: 180px; max-width: 320px; }
.sessions-target strong, .sessions-target span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sessions-target span { color: rgba(var(--v-theme-on-surface), .64); font-size: .74rem; }
.sessions-path { display: flex; align-items: center; gap: 6px; white-space: nowrap; }
.sessions-traffic { display: grid; gap: 2px; font-variant-numeric: tabular-nums; white-space: nowrap; }
.sessions-mobile { display: grid; gap: 12px; }
.sessions-mobile__card { padding: 17px; border: 1px solid rgba(var(--v-theme-on-surface), .08); background: rgba(var(--v-theme-surface), .62); }
.sessions-mobile__head { display: flex; justify-content: space-between; gap: 12px; align-items: center; }
.sessions-mobile__head > div { display: flex; align-items: center; gap: 9px; min-width: 0; }
.sessions-mobile__target { margin-top: 14px; font-weight: 750; overflow-wrap: anywhere; }
.sessions-mobile__destination { color: rgba(var(--v-theme-on-surface), .64); font-size: .75rem; overflow-wrap: anywhere; }
.sessions-mobile__grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-top: 15px; }
.sessions-mobile__grid div { min-width: 0; }
.sessions-mobile__grid span, .sessions-mobile__grid strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sessions-mobile__grid span { color: rgba(var(--v-theme-on-surface), .64); font-size: .7rem; }
.sessions-mobile__grid strong { margin-top: 3px; font-size: .86rem; }
@media (max-width: 959px) { .sessions-toolbar { grid-template-columns: 1fr 1fr; } }
@media (max-width: 599px) { .sessions-hero { align-items: flex-start; padding: 21px 18px; } .sessions-hero > .v-btn { min-width: 44px; padding-inline: 10px; } .sessions-hero__title-row p { font-size: .84rem; } .sessions-hero__meta { flex-wrap: wrap; } .sessions-content { padding: 13px; } .sessions-toolbar { grid-template-columns: 1fr; } }
</style>
