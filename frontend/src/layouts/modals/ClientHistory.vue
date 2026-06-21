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
        <div v-else ref="tableShell" class="history-dialog__table-shell">
          <v-data-table
            v-model:items-per-page="itemsPerPage"
            :headers="headers"
            :items="filteredHistory"
            item-value="dateTime"
            density="compact"
            :mobile="smAndDown"
            mobile-breakpoint="sm"
            hide-no-data
            fixed-header
            width="100%"
            class="history-dialog__table elevation-1 rounded"
            @update:items-per-page="resetTableScroll"
            @update:page="resetTableScroll"
          >
            <template v-slot:item.dateTime="{ value }">
              <v-chip variant="text" dir="ltr" density="compact">
                {{ dateFormatted(value) }}
              </v-chip>
            </template>
            <template v-slot:item.domain="{ value }">
              <span dir="ltr">{{ value || '-' }}</span>
            </template>
            <template v-slot:item.destination="{ value }">
              <span dir="ltr">{{ value || '-' }}</span>
            </template>
            <template v-slot:item.inbound="{ value }">
              {{ value || '-' }}
            </template>
            <template v-slot:item.outbound="{ value }">
              {{ value || '-' }}
            </template>
            <template v-slot:item.network="{ value }">
              {{ value || '-' }}
            </template>
            <template v-slot:item.protocol="{ value }">
              {{ value || '-' }}
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
const itemsPerPage = ref(10)
const tableShell = ref<HTMLElement | null>(null)

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

const loadData = async () => {
  if (props.id <= 0) {
    history.value = []
    clientName.value = ''
    keyword.value = ''
    return
  }

  loading.value = true
  try {
    const client = await Data().loadClients(props.id)
    clientName.value = client.name ?? ''
    history.value = Array.isArray(client.history) ? [...client.history] : []
  } catch {
    history.value = []
    clientName.value = ''
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
    })
  })
}

const onDialogUpdate = (v: boolean) => {
  if (!v) {
    history.value = []
    clientName.value = ''
    keyword.value = ''
    emit('close')
  }
}

watch(
  () => [props.visible, props.id],
  ([visible]) => {
    if (visible) {
      loadData()
    } else {
      history.value = []
      clientName.value = ''
      keyword.value = ''
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

@media (max-width: 600px) {
  .history-dialog {
    height: calc(100dvh - 32px);
    max-height: calc(100vh - 96px);
  }
}
</style>
