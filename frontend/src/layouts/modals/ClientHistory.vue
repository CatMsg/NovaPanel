<template>
  <v-dialog
    :model-value="visible"
    transition="dialog-bottom-transition"
    width="90%"
    max-width="1200"
    @update:model-value="onDialogUpdate"
  >
    <v-card class="rounded-lg history-dialog" :loading="loading">
      <v-card-title class="history-dialog__title">
        <div class="history-dialog__title-row">
          <div>
            {{ $t('client.history') }}
            <span v-if="clientName"> - {{ clientName }}</span>
          </div>
          <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" :title="$t('actions.close')" @click="$emit('close')" />
        </div>
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text class="history-dialog__body">
        <v-row class="history-dialog__filters" align="center" dense>
          <v-col cols="12" md="4">
            <v-text-field
              v-model="keyword"
              clearable
              density="compact"
              variant="underlined"
              prepend-inner-icon="mdi-magnify"
              :label="$t('search')"
              hide-details
            />
          </v-col>
          <v-col cols="6" md="2">
            <v-select v-model="timeRange" :items="timeRangeItems" density="compact" variant="outlined" :label="$t('client.timeRange')" hide-details />
          </v-col>
          <v-col cols="6" md="2">
            <v-select v-model="inboundFilter" :items="inboundOptions" density="compact" variant="outlined" :label="$t('pages.inbounds')" hide-details />
          </v-col>
          <v-col cols="6" md="2">
            <v-select v-model="outboundFilter" :items="outboundOptions" density="compact" variant="outlined" :label="$t('pages.outbounds')" hide-details />
          </v-col>
          <v-col cols="6" md="2" class="history-dialog__filter-actions">
            <v-btn block variant="tonal" color="primary" :disabled="filteredHistory.length === 0" @click="exportHistory">
              <v-icon icon="mdi-download-outline" start />{{ $t('client.exportHistory') }}
            </v-btn>
          </v-col>
        </v-row>
        <v-alert
          v-if="!loading && filteredHistory.length === 0"
          type="warning"
          variant="outlined"
          :text="$t('noData')"
        />
        <div v-else-if="smAndDown" ref="tableShell" class="history-dialog__mobile-shell">
          <div class="history-dialog__mobile-list">
            <div
              v-for="item in pagedHistory"
              :key="item.dateTime + '-' + item.destination"
              class="history-dialog__mobile-item"
            >
              <div class="history-dialog__mobile-head">
                <v-icon icon="mdi-history" size="18" color="primary" />
                <span dir="ltr">{{ dateFormatted(item.dateTime) }}</span>
              </div>
              <div class="history-dialog__mobile-main" dir="ltr">
                {{ item.domain || item.destination || '-' }}
              </div>
              <div class="history-dialog__mobile-grid">
                <div>
                  <span>{{ $t('pages.inbounds') }}</span>
                  <strong>{{ item.inbound || '-' }}</strong>
                </div>
                <div>
                  <span>{{ $t('pages.outbounds') }}</span>
                  <strong>{{ item.outbound || '-' }}</strong>
                </div>
                <div>
                  <span>{{ $t('network') }}</span>
                  <strong>{{ item.network || '-' }}</strong>
                </div>
                <div>
                  <span>{{ $t('protocol') }}</span>
                  <strong>{{ item.protocol || '-' }}</strong>
                </div>
              </div>
              <div
                v-if="item.destination && item.destination !== item.domain"
                class="history-dialog__mobile-destination"
                dir="ltr"
              >
                {{ item.destination }}
              </div>
            </div>
          </div>
          <v-pagination
            v-if="pageCount > 1"
            v-model="page"
            :length="pageCount"
            density="compact"
            rounded="circle"
            :total-visible="3"
            class="history-dialog__mobile-pagination"
          />
        </div>
        <div v-else ref="tableShell" class="history-dialog__table-shell">
          <div class="history-dialog__desktop-overview">
            <div class="history-dialog__desktop-stat">
              <span>{{ $t('client.history') }}</span>
              <strong>{{ history.length }}</strong>
            </div>
            <div class="history-dialog__desktop-stat">
              <span>{{ $t('client.filtered') }}</span>
              <strong>{{ hasFilters ? filteredHistory.length : $t('all') }}</strong>
            </div>
            <div class="history-dialog__desktop-stat">
              <span>{{ $t('rule.domain') }}</span>
              <strong>{{ desktopTopDomain }}</strong>
            </div>
          </div>
          <v-data-table
            :headers="headers"
            :items="filteredHistory"
            :items-per-page="10"
            item-value="dateTime"
            density="compact"
            :mobile="false"
            hide-no-data
            fixed-header
            width="100%"
            class="history-dialog__table elevation-1 rounded"
            @update:page="resetTableScroll"
          >
            <template v-slot:item.dateTime="{ value }">
              <v-chip class="history-dialog__date-chip" variant="text" dir="ltr" density="compact">
                {{ dateFormatted(value) }}
              </v-chip>
            </template>
            <template v-slot:item.domain="{ item, value }">
              <div class="history-dialog__primary-cell">
                <strong class="history-dialog__primary-text" dir="ltr" :title="value || item.destination || '-'">
                  {{ value || item.destination || '-' }}
                </strong>
                <span
                  v-if="item.destination && item.destination !== value"
                  class="history-dialog__secondary-text"
                  dir="ltr"
                  :title="item.destination"
                >
                  {{ item.destination }}
                </span>
              </div>
            </template>
            <template v-slot:item.inbound="{ value }">
              <v-chip size="small" variant="tonal" color="primary" class="history-dialog__tag-chip" :title="value || '-'">
                {{ value || '-' }}
              </v-chip>
            </template>
            <template v-slot:item.outbound="{ value }">
              <v-chip size="small" variant="outlined" color="primary" class="history-dialog__tag-chip" :title="value || '-'">
                {{ value || '-' }}
              </v-chip>
            </template>
            <template v-slot:item.network="{ value }">
              <v-chip size="small" variant="flat" color="surface-variant" class="history-dialog__tag-chip" :title="value || '-'">
                {{ value || '-' }}
              </v-chip>
            </template>
            <template v-slot:item.protocol="{ value }">
              <v-chip size="small" variant="flat" color="teal" class="history-dialog__tag-chip" :title="value || '-'">
                {{ value || '-' }}
              </v-chip>
            </template>
          </v-data-table>
        </div>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import { i18n } from '@/locales'
import { HistoryEntry } from '@/types/clients'
import { computed, nextTick, ref, watch } from 'vue'
import { useDisplay } from 'vuetify'

const { smAndDown } = useDisplay()

const props = defineProps<{
  visible: boolean
  id: number
}>()

const emit = defineEmits(['close'])

const loading = ref(false)
const clientName = ref('')
const history = ref<HistoryEntry[]>([])
const keyword = ref('')
const timeRange = ref('all')
const inboundFilter = ref('all')
const outboundFilter = ref('all')
const page = ref(1)
const tableShell = ref<HTMLElement | null>(null)
const itemsPerPage = 10

const headers = [
  { title: i18n.global.t('admin.date') + '-' + i18n.global.t('admin.time'), key: 'dateTime' },
  { title: i18n.global.t('rule.domain'), key: 'domain' },
  { title: i18n.global.t('pages.inbounds'), key: 'inbound' },
  { title: i18n.global.t('pages.outbounds'), key: 'outbound' },
  { title: i18n.global.t('network'), key: 'network' },
  { title: i18n.global.t('protocol'), key: 'protocol' },
]

const timeRangeItems = [
  { title: i18n.global.t('all'), value: 'all' },
  { title: i18n.global.t('client.lastDay'), value: 'day' },
  { title: i18n.global.t('client.lastWeek'), value: 'week' },
  { title: i18n.global.t('client.lastMonth'), value: 'month' },
]

const inboundOptions = computed(() => [
  { title: i18n.global.t('all'), value: 'all' },
  ...[...new Set(history.value.map(item => item.inbound).filter(Boolean))]
    .sort()
    .map(value => ({ title: value as string, value: value as string })),
])

const outboundOptions = computed(() => [
  { title: i18n.global.t('all'), value: 'all' },
  ...[...new Set(history.value.map(item => item.outbound).filter(Boolean))]
    .sort()
    .map(value => ({ title: value as string, value: value as string })),
])

const filteredHistory = computed(() => {
  const query = keyword.value.trim().toLowerCase()
  const rangeSeconds = timeRange.value === 'day'
    ? 86400
    : timeRange.value === 'week'
      ? 7 * 86400
      : timeRange.value === 'month'
        ? 30 * 86400
        : 0
  const minimumTime = rangeSeconds > 0 ? Date.now() / 1000 - rangeSeconds : 0

  return history.value.filter((item) => {
    if (minimumTime > 0 && item.dateTime < minimumTime) return false
    if (inboundFilter.value !== 'all' && item.inbound !== inboundFilter.value) return false
    if (outboundFilter.value !== 'all' && item.outbound !== outboundFilter.value) return false
    if (!query) return true
    const formattedDate = dateFormatted(item.dateTime).toLowerCase()
    return [
      formattedDate,
      item.domain,
      item.destination,
      item.inbound,
      item.outbound,
      item.network,
      item.protocol,
    ]
      .filter(Boolean)
      .some(value => String(value).toLowerCase().includes(query))
  })
})

const hasFilters = computed(() =>
  keyword.value.trim().length > 0
  || timeRange.value !== 'all'
  || inboundFilter.value !== 'all'
  || outboundFilter.value !== 'all',
)

const desktopTopDomain = computed(() => {
  const counts = new Map<string, number>()
  for (const item of filteredHistory.value) {
    const key = item.domain || item.destination || ''
    if (!key) continue
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  const sorted = [...counts.entries()].sort((a, b) => b[1] - a[1])
  return sorted[0]?.[0] ?? '-'
})

const pageCount = computed(() => Math.max(1, Math.ceil(filteredHistory.value.length / itemsPerPage)))

const pagedHistory = computed(() => {
  const start = (page.value - 1) * itemsPerPage
  return filteredHistory.value.slice(start, start + itemsPerPage)
})

const csvCell = (value: unknown) => `"${String(value ?? '').replaceAll('"', '""')}"`

const exportHistory = () => {
  if (filteredHistory.value.length === 0) return
  const rows = [
    ['Date', 'Domain', 'Destination', 'Inbound', 'Outbound', 'Network', 'Protocol'],
    ...filteredHistory.value.map(item => [
      dateFormatted(item.dateTime),
      item.domain,
      item.destination,
      item.inbound,
      item.outbound,
      item.network,
      item.protocol,
    ]),
  ]
  const csv = `\uFEFF${rows.map(row => row.map(csvCell).join(',')).join('\n')}`
  const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = `${clientName.value || 'client'}-history.csv`
  link.click()
  URL.revokeObjectURL(url)
}

const loadData = async () => {
  if (props.id <= 0) {
    history.value = []
    clientName.value = ''
    keyword.value = ''
    timeRange.value = 'all'
    inboundFilter.value = 'all'
    outboundFilter.value = 'all'
    page.value = 1
    return
  }

  loading.value = true
  try {
    const client = await Data().loadClients(props.id)
    clientName.value = client.name ?? ''
    history.value = Array.isArray(client.history) ? [...client.history] : []
    page.value = 1
  } catch {
    history.value = []
    clientName.value = ''
    page.value = 1
  } finally {
    loading.value = false
  }
}

const dateFormatted = (dt: number): string => {
  const locale = i18n.global.locale.value.replace('zh', 'zh-')
  const date = new Date(dt * 1000)
  return date.toLocaleString(locale)
}

const resetTableScroll = () => {
  nextTick(() => {
    requestAnimationFrame(() => {
      const wrapper = tableShell.value?.querySelector<HTMLElement>('.v-table__wrapper')
      if (wrapper) wrapper.scrollTop = 0
      if (tableShell.value) tableShell.value.scrollTop = 0
    })
  })
}

const onDialogUpdate = (v: boolean) => {
  if (!v) {
    history.value = []
    clientName.value = ''
    keyword.value = ''
    timeRange.value = 'all'
    inboundFilter.value = 'all'
    outboundFilter.value = 'all'
    page.value = 1
    emit('close')
  }
}

watch([keyword, timeRange, inboundFilter, outboundFilter], () => {
  page.value = 1
  resetTableScroll()
})

watch(page, resetTableScroll)

watch(pageCount, (count) => {
  if (page.value > count) page.value = count
})

watch(
  () => [props.visible, props.id],
  ([visible]) => {
    if (visible) {
      loadData()
    } else {
      history.value = []
      clientName.value = ''
      keyword.value = ''
      timeRange.value = 'all'
      inboundFilter.value = 'all'
      outboundFilter.value = 'all'
      page.value = 1
    }
  },
  { immediate: true },
)
</script>

<style scoped lang="scss">
.history-dialog {
  display: flex;
  flex-direction: column;
  height: min(90dvh, 900px);
  max-height: min(90vh, 900px);
}

.history-dialog__title {
  padding-bottom: 10px;
}

.history-dialog__title-row {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.history-dialog__body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
  flex: 1 1 auto;
  overflow: hidden;
}

.history-dialog__filters {
  flex: 0 0 auto;
}

.history-dialog__filter-actions {
  display: flex;
  align-items: center;
}

.history-dialog__table-shell {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
  flex-direction: column;
  gap: 12px;
  overflow: hidden;
}

.history-dialog__desktop-overview {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  flex: 0 0 auto;
}

.history-dialog__desktop-stat {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 6px;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 18px;
  background:
    linear-gradient(135deg, rgba(var(--v-theme-primary), 0.08), rgba(var(--v-theme-surface), 0.96)),
    rgba(var(--v-theme-surface), 0.94);
  box-shadow: 0 18px 36px rgba(15, 23, 42, 0.08);
  padding: 14px 16px;
}

.history-dialog__desktop-stat span {
  color: rgba(var(--v-theme-on-surface), 0.62);
  font-size: 0.76rem;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.history-dialog__desktop-stat strong {
  min-width: 0;
  overflow: hidden;
  color: rgb(var(--v-theme-on-surface));
  font-size: 1rem;
  font-weight: 800;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.history-dialog__table {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 22px !important;
  background:
    linear-gradient(180deg, rgba(var(--v-theme-primary), 0.04), rgba(var(--v-theme-surface), 0.98)),
    rgba(var(--v-theme-surface), 0.98);
  box-shadow: 0 24px 48px rgba(15, 23, 42, 0.08);
  overflow: hidden;
}

.history-dialog__table :deep(.v-table__wrapper) {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}

.history-dialog__table :deep(table) {
  table-layout: fixed;
  width: 100%;
}

.history-dialog__table :deep(thead th) {
  border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  background: rgba(var(--v-theme-primary), 0.06);
  color: rgba(var(--v-theme-on-surface), 0.74);
  font-size: 0.76rem;
  font-weight: 700 !important;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.history-dialog__table :deep(th),
.history-dialog__table :deep(td) {
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
}

.history-dialog__table :deep(tbody td) {
  border-bottom: 1px solid rgba(var(--v-border-color), calc(var(--v-border-opacity) * 0.72));
}

.history-dialog__table :deep(tbody tr) {
  height: 58px;
  transition: background-color 0.18s ease;
}

.history-dialog__table :deep(tbody tr:hover) {
  background: rgba(var(--v-theme-primary), 0.045);
}

.history-dialog__date-chip {
  max-width: 100%;
  padding-inline: 0;
}

.history-dialog__primary-cell {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.history-dialog__primary-text {
  display: block;
  min-width: 0;
  overflow: hidden;
  color: rgb(var(--v-theme-on-surface));
  font-size: 0.9rem;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.history-dialog__secondary-text {
  display: block;
  min-width: 0;
  overflow: hidden;
  color: rgba(var(--v-theme-on-surface), 0.58);
  font-size: 0.76rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.history-dialog__table-text,
.history-dialog__date-chip :deep(.v-chip__content) {
  display: block;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.history-dialog__table-text--muted {
  color: rgba(var(--v-theme-on-surface), 0.62);
}

.history-dialog__tag-chip {
  max-width: 100%;
}

.history-dialog__tag-chip :deep(.v-chip__content) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.history-dialog__table :deep(.v-table__wrapper::-webkit-scrollbar) {
  width: 8px;
  height: 8px;
}

.history-dialog__table :deep(.v-table__wrapper::-webkit-scrollbar-thumb) {
  background: rgba(var(--v-theme-on-surface), 0.24);
  border: 2px solid transparent;
  border-radius: 999px;
  background-clip: padding-box;
}

.history-dialog__table :deep(.v-data-table-footer) {
  flex: 0 0 auto;
  flex-wrap: wrap;
  gap: 6px;
  min-height: 52px;
  border-top: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  background: rgba(var(--v-theme-surface), 0.88);
}

.history-dialog__table :deep(.v-data-table-footer__items-per-page) {
  display: none;
}

.history-dialog__mobile-shell {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
}

.history-dialog__mobile-list {
  display: flex;
  min-height: 0;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 10px;
  overflow: auto;
  overscroll-behavior: contain;
  padding-right: 2px;
}

.history-dialog__mobile-item {
  display: flex;
  flex-direction: column;
  gap: 8px;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 8px;
  background: rgba(var(--v-theme-surface), 0.9);
  padding: 12px;
}

.history-dialog__mobile-head {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
  color: rgba(var(--v-theme-on-surface), 0.68);
  font-size: 0.78rem;
}

.history-dialog__mobile-head span,
.history-dialog__mobile-main,
.history-dialog__mobile-destination {
  min-width: 0;
  overflow-wrap: anywhere;
  word-break: break-word;
}

.history-dialog__mobile-main {
  color: rgb(var(--v-theme-on-surface));
  font-size: 0.95rem;
  font-weight: 700;
  line-height: 1.35;
}

.history-dialog__mobile-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.history-dialog__mobile-grid > div {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
  border-radius: 8px;
  background: rgba(var(--v-theme-on-surface), 0.045);
  padding: 8px;
}

.history-dialog__mobile-grid span {
  color: rgba(var(--v-theme-on-surface), 0.58);
  font-size: 0.72rem;
}

.history-dialog__mobile-grid strong {
  min-width: 0;
  overflow-wrap: anywhere;
  color: rgb(var(--v-theme-on-surface));
  font-size: 0.82rem;
  font-weight: 700;
  word-break: break-word;
}

.history-dialog__mobile-destination {
  border-top: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  padding-top: 8px;
  color: rgba(var(--v-theme-on-surface), 0.66);
  font-size: 0.78rem;
  line-height: 1.35;
}

.history-dialog__mobile-pagination {
  flex: 0 0 auto;
  justify-content: center;
  padding-top: 10px;
}

@media (max-width: 600px) {
  .history-dialog {
    height: calc(100dvh - 32px);
    max-height: calc(100vh - 96px);
  }

  .history-dialog__body {
    padding: 12px;
  }

  .history-dialog__filters {
    max-height: 210px;
    overflow: auto;
    padding-right: 2px;
  }
}

@media (max-width: 960px) {
  .history-dialog__desktop-overview {
    grid-template-columns: 1fr;
  }
}
</style>
