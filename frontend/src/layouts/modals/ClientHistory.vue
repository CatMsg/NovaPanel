<template>
  <v-dialog
    :model-value="visible"
    transition="dialog-bottom-transition"
    width="90%"
    max-width="1200"
    @update:model-value="onDialogUpdate"
  >
    <v-card class="rounded-lg history-dialog" :loading="loading">
      <v-card-title>
        <v-row>
          <v-col>
            {{ $t('client.history') }}
            <span v-if="clientName"> - {{ clientName }}</span>
          </v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto">
            <v-icon icon="mdi-close" @click="$emit('close')" />
          </v-col>
        </v-row>
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text class="history-dialog__body">
        <v-row class="mb-2 history-dialog__filters" align="center">
          <v-col cols="12" md="8">
            <v-text-field
              v-model="keyword"
              clearable
              density="compact"
              variant="underlined"
              prepend-inner-icon="mdi-magnify"
              :label="$t('search')"
            />
          </v-col>
          <v-col cols="12" md="4" class="text-md-end">
            <v-chip density="compact" variant="tonal" color="primary">
              {{ filteredHistory.length }} / {{ history.length }}
            </v-chip>
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
            <template v-slot:item.domain="{ value }">
              <span class="history-dialog__table-text" dir="ltr" :title="value || '-'">{{ value || '-' }}</span>
            </template>
            <template v-slot:item.destination="{ value }">
              <span class="history-dialog__table-text" dir="ltr" :title="value || '-'">{{ value || '-' }}</span>
            </template>
            <template v-slot:item.inbound="{ value }">
              <span class="history-dialog__table-text" :title="value || '-'">{{ value || '-' }}</span>
            </template>
            <template v-slot:item.outbound="{ value }">
              <span class="history-dialog__table-text" :title="value || '-'">{{ value || '-' }}</span>
            </template>
            <template v-slot:item.network="{ value }">
              <span class="history-dialog__table-text" :title="value || '-'">{{ value || '-' }}</span>
            </template>
            <template v-slot:item.protocol="{ value }">
              <span class="history-dialog__table-text" :title="value || '-'">{{ value || '-' }}</span>
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
const page = ref(1)
const tableShell = ref<HTMLElement | null>(null)
const itemsPerPage = 10

const headers = [
  { title: i18n.global.t('admin.date') + '-' + i18n.global.t('admin.time'), key: 'dateTime' },
  { title: i18n.global.t('objects.domain'), key: 'domain' },
  { title: 'Destination', key: 'destination' },
  { title: i18n.global.t('pages.inbounds'), key: 'inbound' },
  { title: i18n.global.t('pages.outbounds'), key: 'outbound' },
  { title: i18n.global.t('network'), key: 'network' },
  { title: i18n.global.t('protocol'), key: 'protocol' },
]

const filteredHistory = computed(() => {
  const query = keyword.value.trim().toLowerCase()
  if (!query) {
    return history.value
  }

  return history.value.filter((item) => {
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

const pageCount = computed(() => Math.max(1, Math.ceil(filteredHistory.value.length / itemsPerPage)))

const pagedHistory = computed(() => {
  const start = (page.value - 1) * itemsPerPage
  return filteredHistory.value.slice(start, start + itemsPerPage)
})

const loadData = async () => {
  if (props.id <= 0) {
    history.value = []
    clientName.value = ''
    keyword.value = ''
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
    page.value = 1
    emit('close')
  }
}

watch(keyword, () => {
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

.history-dialog__table-shell {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
}

.history-dialog__table {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  height: 100%;
  min-height: 0;
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

.history-dialog__table :deep(th),
.history-dialog__table :deep(td) {
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
}

.history-dialog__table :deep(tbody tr) {
  height: 44px;
}

.history-dialog__date-chip {
  max-width: 100%;
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
}
</style>
