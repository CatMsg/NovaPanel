
<template>
  <ClientModal 
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :groups="groups"
    :inboundTags="inboundTags"
    @close="closeModal"
  />
  <ClientAddBulk 
    v-model="addBulkModal"
    :visible="addBulkModal"
    :groups="groups"
    :inboundTags="inboundTags"
    @close="closeAddBulk"
  />
  <ClientEditBulk 
    v-model="editBulkModal"
    :visible="editBulkModal"
    :inboundTags="inboundTags"
    :clients="clients"
    @close="closeEditBulk"
  />
  <QrCode
    v-model="qrcode.visible"
    :visible="qrcode.visible"
    :id="qrcode.id"
    @close="closeQrCode"
  />
  <Stats
    v-model="stats.visible"
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="closeStats"
  />
  <ClientHistory
    :visible="historyModal.visible"
    :id="historyModal.id"
    @close="closeHistory"
  />
  <v-card class="clients-hero" rounded="xl" variant="flat">
    <div class="clients-hero__topline">
      <span class="clients-hero__badge">{{ $t('pages.clients') }}</span>
      <span class="clients-hero__badge clients-hero__badge--soft">
        {{ filterSettings.enabled ? $t('search') : $t('main.hero.live') }}
      </span>
    </div>

    <v-row class="clients-hero__content" align="center">
      <v-col cols="12" lg="8">
        <div class="clients-hero__title-row">
          <div class="clients-hero__icon">
            <v-icon icon="mdi-account-group-outline" size="32" />
          </div>
          <div>
            <h1 class="clients-hero__title">{{ $t('pages.clients') }}</h1>
            <p class="clients-hero__subtitle">
              管理用户、批量调整、查看历史与在线状态，桌面端与移动端保持一致的操作入口。
            </p>
          </div>
        </div>
        <div class="clients-hero__meta">
          <span>总数 {{ clients.length }}</span>
          <span>•</span>
          <span>在线 {{ onlineCount }}</span>
          <span>•</span>
          <span>当前显示 {{ visibleClients.length }}</span>
        </div>
      </v-col>
      <v-col cols="12" lg="4" class="clients-hero__actions">
        <v-btn color="primary" size="large" class="clients-hero__primary" @click="showModal(0)">
          <v-icon icon="mdi-plus" start />
          {{ $t('actions.add') }}
        </v-btn>
        <v-menu v-model="actionMenu" :close-on-content-click="false" location="bottom center">
          <template v-slot:activator="{ props }">
            <v-btn v-bind="props" class="clients-hero__icon-btn" variant="flat" icon>
              <v-icon icon="mdi-tools" />
            </v-btn>
          </template>
          <v-list density="compact" nav>
            <v-list-item link @click="addBulk">
              <template v-slot:prepend>
                <v-icon icon="mdi-account-multiple-plus"></v-icon>
              </template>
              <v-list-item-title v-text="$t('actions.addbulk')"></v-list-item-title>
            </v-list-item>
            <v-list-item link @click="editBulk">
              <template v-slot:prepend>
                <v-icon icon="mdi-account-multiple-check"></v-icon>
              </template>
              <v-list-item-title v-text="$t('actions.editbulk')"></v-list-item-title>
            </v-list-item>
          </v-list>
        </v-menu>
        <v-menu v-model="filterMenu" :close-on-content-click="false" location="bottom center">
          <template v-slot:activator="{ props }">
            <v-btn v-bind="props" class="clients-hero__icon-btn" variant="flat" icon>
              <v-icon
                :icon="filterSettings.enabled ? 'mdi-filter-check-outline' : 'mdi-filter-menu-outline'"
                :color="filterSettings.enabled ? 'primary' : ''"
              />
            </v-btn>
          </template>
          <v-card class="clients-filter" rounded="xl">
            <v-container class="clients-filter__container">
              <v-row>
                <v-col>
                  <v-select
                    variant="underlined"
                    density="compact"
                    :label="$t('type')"
                    :items="filterItems"
                    v-model="filterSettings.state"
                  />
                </v-col>
              </v-row>
              <v-row>
                <v-col>
                  <v-select
                    variant="underlined"
                    density="compact"
                    :label="$t('client.group')"
                    :items="[ {title: $t('all'), value: '-'}, ...groups.map(g => ({ title: g.length>0 ? g : $t('none'), value: g}))]"
                    v-model="filterSettings.group"
                  />
                </v-col>
              </v-row>
              <v-row>
                <v-col>
                  <v-text-field
                    variant="underlined"
                    density="compact"
                    :label="$t('client.name')"
                    v-model="filterSettings.text"
                  />
                </v-col>
              </v-row>
            </v-container>
            <v-card-actions class="clients-filter__actions">
              <v-spacer />
              <v-btn color="blue-darken-1" variant="outlined" @click="clearFilter">
                {{ $t('actions.del') }}
              </v-btn>
              <v-btn color="blue-darken-1" variant="tonal" @click="doFilter">
                {{ $t('actions.update') }}
              </v-btn>
            </v-card-actions>
          </v-card>
        </v-menu>
      </v-col>
    </v-row>
  </v-card>

  <v-card class="clients-table-card" rounded="xl" variant="flat">
    <div class="clients-table-card__head">
      <div>
        <div class="clients-table-card__title">{{ $t('pages.clients') }}</div>
        <div class="clients-table-card__subtitle">
          桌面端默认按表格查看，历史、流量和批量操作都保留在同一页。
        </div>
      </div>
      <v-chip size="small" variant="flat" color="primary">
        {{ visibleClients.length }} / {{ clients.length }}
      </v-chip>
    </div>
    <v-divider />
    <v-data-table
      :headers="headers"
      :items="visibleClients"
      :hide-default-footer="visibleClients.length<=10"
      :items-per-page="itemPerPage"
      @update:items-per-page="setItemPerPage($event)"
      hide-no-data
      fixed-header
      item-value="name"
      :mobile="smAndDown"
      mobile-breakpoint="sm"
      width="100%"
      class="clients-table"
    >
        <template v-slot:item.inbounds="{ item }">
          <span>
          <v-tooltip activator="parent" dir="ltr" location="start" v-if="item.inbounds != ''">
            <span v-for="i in item.inbounds">{{ inbounds.find(inb => inb.id == i)?.tag }}<br /></span>
          </v-tooltip>
          {{ item.inbounds?.length }}
          </span>
        </template>
        <template v-slot:item.volume="{ item }">
          <div class="text-start" v-tooltip:top="'↓' + HumanReadable.sizeFormat(item.down) + ' - ' + HumanReadable.sizeFormat(item.up) + '↑'">
            <v-chip
              size="small"
              :color="item.volume==0 ? 'success' : item.volume<=(item.up + item.down)? 'error': ''"
              label
            >{{ HumanReadable.sizeFormat(item.up + item.down) + ' / ' + (item.volume == 0 ? $t('unlimited') : HumanReadable.sizeFormat(item.volume)) }}</v-chip>
          </div>
          <v-progress-linear
            :model-value="percent(item)"
            :color="percentColor(item)"
            v-if="item.volume>0"
            bottom
          >
          </v-progress-linear>
        </template>
        <template v-slot:item.expiry="{ item }">
          <div class="text-start">
            <v-tooltip v-if="item.expiry>0" activator="parent" location="top" :text="new Date(item.expiry * 1000).toLocaleString(locale)" />
            <v-chip
              size="small"
              :color="item.expiry==0 ? 'success' : item.expiry<=Date.now()/1000? 'error': ''"
              label
            >{{ HumanReadable.remainedDays(item.expiry) }}</v-chip>
          </div>
        </template>
        <template v-slot:item.online="{ item }">
          <div class="text-start">
            <template v-if="isOnline(item.name).value">
              <v-chip density="comfortable" size="small" color="success" variant="flat">{{ $t('online') }}</v-chip>
            </template>
            <template v-else>-</template>
          </div>
        </template>
        <template v-slot:item.actions="{ item }">
        <div class="clients-table__actions">
          <v-icon
            @click="showModal(item.id)"
          >
            mdi-pencil
          </v-icon>
          <v-menu
            v-model="delOverlay[clients.findIndex(c => c.id == item.id)]"
            :close-on-content-click="false"
            location="top center"
          >
            <template v-slot:activator="{ props }">
              <v-icon
                color="error"
                v-bind="props"
              >
                mdi-delete
              </v-icon>
            </template>
            <v-card :title="$t('actions.del')" rounded="lg">
              <v-divider></v-divider>
              <v-card-text>{{ $t('confirm') }}</v-card-text>
              <v-card-actions>
                <v-btn color="error" variant="outlined" @click="delClient(item.id)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delOverlay[clients.findIndex(c => c.id == item.id)] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-menu>
          <v-icon
            @click="showQrCode(item.id)"
          >
            mdi-qrcode
          </v-icon>
          <template v-if="Data().enableTraffic">
            <v-icon icon="mdi-chart-line" @click="showStats(item.name)">
              <v-tooltip activator="parent" location="top" :text="$t('stats.graphTitle')"></v-tooltip>
            </v-icon>
            <v-icon icon="mdi-history" @click="showHistory(item.id)">
              <v-tooltip activator="parent" location="top" :text="$t('client.history')"></v-tooltip>
            </v-icon>
          </template>
        </div>
      </template>
      </v-data-table>
  </v-card>
</template>
<style>
.v-data-table__tr--mobile td {
  height: fit-content;
  min-height: 36px !important;
}
.v-data-table__tr--mobile td div {
  min-width: 0;
  max-width: 100%;
  width: 100%;
  overflow-wrap: anywhere;
}

.clients-hero,
.clients-table-card {
  margin-top: 16px;
  padding: 20px;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.22), transparent 28%),
    var(--np-surface);
  border: 1px solid var(--np-border);
  box-shadow: var(--np-shadow);
  backdrop-filter: blur(28px) saturate(1.12);
}

.clients-hero {
  overflow: hidden;
}

.clients-hero__topline {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 16px;
}

.clients-hero__badge {
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 12px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--np-accent);
  background: rgba(10, 132, 255, 0.08);
}

.clients-hero__badge--soft {
  text-transform: none;
  letter-spacing: 0;
  color: var(--np-text-muted);
  background: rgba(148, 163, 184, 0.12);
}

.clients-hero__content {
  min-height: 132px;
}

.clients-hero__title-row {
  display: flex;
  gap: 14px;
  align-items: flex-start;
}

.clients-hero__icon {
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  border-radius: 16px;
  color: var(--np-accent);
  background: linear-gradient(145deg, rgba(59, 130, 246, 0.16), rgba(14, 165, 233, 0.08));
}

.clients-hero__title {
  margin: 0;
  font-size: clamp(28px, 3vw, 40px);
  line-height: 1.1;
  font-weight: 800;
  letter-spacing: -0.03em;
}

.clients-hero__subtitle {
  margin: 12px 0 0;
  color: var(--np-text-muted);
  line-height: 1.7;
}

.clients-hero__meta {
  margin-top: 14px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  color: var(--np-text-muted);
  font-size: 13px;
}

.clients-hero__actions {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.clients-hero__primary {
  min-width: 132px;
}

.clients-hero__icon-btn {
  width: 44px;
  height: 44px;
  min-width: 44px;
  border-radius: 999px;
}

.clients-filter {
  min-width: min(92vw, 360px);
}

.clients-filter__container {
  padding: 8px 16px 0;
}

.clients-filter__actions {
  padding: 0 16px 16px;
}

.clients-table-card {
  overflow: hidden;
}

.clients-table-card__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.clients-table-card__title {
  font-size: 1.02rem;
  font-weight: 700;
}

.clients-table-card__subtitle {
  margin-top: 6px;
  color: var(--np-text-muted);
  line-height: 1.6;
}

.clients-table {
  width: 100%;
}

.clients-table .v-table__wrapper {
  overflow-x: auto;
  scrollbar-gutter: stable both-edges;
}

.clients-table .v-table__wrapper table {
  min-width: 980px;
}

.clients-table thead th {
  white-space: nowrap;
  background: rgba(10, 132, 255, 0.06);
  color: var(--np-text-main);
}

.clients-table tbody td {
  color: var(--np-text-main);
}

.clients-table tbody tr:hover {
  background: rgba(10, 132, 255, 0.04);
}

.clients-table__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.clients-table__actions .v-icon {
  flex: 0 0 auto;
}

.clients-table .v-data-table-footer {
  flex-wrap: wrap;
  gap: 6px;
  min-height: 52px;
}

.v-theme--dark .clients-hero,
.v-theme--dark .clients-table-card {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.08), transparent 24%),
    rgba(11, 18, 31, 0.78);
  border-color: rgba(148, 163, 184, 0.16);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.3);
}

.v-theme--dark .clients-hero__badge--soft {
  background: rgba(148, 163, 184, 0.16);
  color: rgba(237, 244, 255, 0.76);
}

.v-theme--dark .clients-table thead th {
  background: rgba(125, 211, 252, 0.08);
  color: rgba(237, 244, 255, 0.92);
}

.v-theme--dark .clients-table tbody td {
  color: rgba(237, 244, 255, 0.94);
}

@media (max-width: 960px) {
  .clients-hero,
  .clients-table-card {
    padding: 16px;
  }

  .clients-hero__actions {
    justify-content: flex-start;
  }
}

@media (max-width: 600px) {
  .clients-hero__title-row {
    gap: 12px;
  }

  .clients-hero__icon {
    width: 46px;
    height: 46px;
  }

  .clients-hero__title {
    font-size: 24px;
  }

  .clients-table-card__head {
    flex-direction: column;
  }

  .clients-table .v-table__wrapper table {
    min-width: 0;
  }

  .clients-table__actions {
    justify-content: flex-start;
    flex-wrap: wrap;
  }

  .clients-table .v-data-table-footer__items-per-page {
    display: none;
  }
}
</style>
<script lang="ts" setup>
import Data from '@/store/modules/data'
import { Client } from '@/types/clients'
import { computed, defineAsyncComponent, ref } from 'vue'
import { HumanReadable } from '@/plugins/utils'
import { i18n, locale } from '@/locales'
import { useDisplay } from 'vuetify'

const ClientModal = defineAsyncComponent(() => import('@/layouts/modals/Client.vue'))
const ClientAddBulk = defineAsyncComponent(() => import('@/layouts/modals/ClientAddBulk.vue'))
const ClientEditBulk = defineAsyncComponent(() => import('@/layouts/modals/ClientEditBulk.vue'))
const ClientHistory = defineAsyncComponent(() => import('@/layouts/modals/ClientHistory.vue'))
const QrCode = defineAsyncComponent(() => import('@/layouts/modals/QrCode.vue'))
const Stats = defineAsyncComponent(() => import('@/layouts/modals/Stats.vue'))

const { smAndDown } = useDisplay()

const clients = computed((): any[] => {
  return Data().clients
})

const visibleClients = computed((): any[] => {
  return filterSettings.value.enabled ? filterSettings.value.filteredClients : clients.value
})

const onlineCount = computed((): number => {
  return Data().onlines?.user?.length ?? 0
})

const isOnline = (cname: string) => computed(() => {
  return Data().onlines?.user ? Data().onlines.user.includes(cname) : false
})

const inbounds = computed((): any[] => {
  return Data().inbounds?? []
})

const inboundTags = computed((): any[] => {
  if (!inbounds.value) return []
  return inbounds.value?.filter(i => i.tag != "" && i.users).map(i => { return { title: i.tag, value: i.id } })
})

const groups = computed((): string[] => {
  if (!clients.value) return []
  if (filterSettings?.value.enabled) return Array.from(new Set(filterSettings.value.filteredClients?.map(c => c.group)))
  return Array.from(new Set(clients.value?.map(c => c.group)))
})

const actionMenu = ref(false)
const filterMenu = ref(false)
const filterSettings = ref({
  enabled: false,
  state: '',
  group: '-',
  text: '',
  filteredClients: <any[]>[]
})

const filterItems = [
  { title: i18n.global.t('none'), value: '' },
  { title: i18n.global.t('disable'), value: 'disable' },
  { title: i18n.global.t('date.expired'), value: 'expired' },
  { title: i18n.global.t('online'), value: 'online' },
]

const headers = [
  { title: i18n.global.t('client.name'), key: 'name' },
  { title: i18n.global.t('client.desc'), key: 'desc' },
  { title: i18n.global.t('client.group'), key: 'group' },
  { title: i18n.global.t('pages.inbounds'), key: 'inbounds', width: 10 },
  { title: i18n.global.t('actions.action'), key: 'actions', sortable: false },
  { title: i18n.global.t('stats.volume'), key: 'volume' },
  { title: i18n.global.t('date.expiry'), key: 'expiry' },
  { title: i18n.global.t('online'), key: 'online' },
  { key: 'data-table-group', width: 0 },
]

const itemPerPage = ref(localStorage.getItem('items-per-page') || '10')

const setItemPerPage = (items: number) => {
  itemPerPage.value = items.toString()
  localStorage.setItem('items-per-page', items.toString())
}

const modal = ref({
  visible: false,
  id: 0,
})

const delOverlay = ref(new Array<boolean>(clients.value.length).fill(false))

const showModal = async (id: number) => {
  modal.value.id = id
  modal.value.visible = true
}
const closeModal = () => {
  modal.value.visible = false
}

const delClient = async (id: number) => {
  const index = clients.value.findIndex(c => c.id === id)
  const success = await Data().save("clients", "del", id)
  if (success) delOverlay.value[index] = false
}

const qrcode = ref({
  visible: false,
  id: 0,
})

const showQrCode = (id: number) => {
  qrcode.value.id = id
  qrcode.value.visible = true
}
const closeQrCode = () => {
  qrcode.value.visible = false
}

const stats = ref({
  visible: false,
  resource: "user",
  tag: "",
})

const historyModal = ref({
  visible: false,
  id: 0,
})

const showStats = (tag: string) => {
  stats.value.tag = tag
  stats.value.visible = true
}
const closeStats = () => {
  stats.value.visible = false
}

const showHistory = (id?: number) => {
  if (!id) return
  historyModal.value.id = id
  historyModal.value.visible = true
}

const closeHistory = () => {
  historyModal.value.visible = false
}

const doFilter = () => {
  let filteredClients = clients.value.slice()
  if (filterSettings.value.group != '-') {
    filteredClients = filteredClients.filter(c => c.group == filterSettings.value.group)
  }
  if (filterSettings.value.text.length>0) {
    const txt = filterSettings.value.text
    filteredClients = filteredClients.filter(c => c.name.search(txt) != -1 || c.desc.search(txt) != -1)
  }
  switch (filterSettings.value.state) {
    case "disable":
      filteredClients = filteredClients.filter(c => c.enable == false)
      break
    case "expired":
      filteredClients = filteredClients.filter(c => c.expiry > 0 && c.expiry < (Date.now()/1000) )
      break
    case "online":
      filteredClients = filteredClients.filter(c => Data().onlines?.user?.includes(c.name))
      break
  }
  filterSettings.value.filteredClients = filteredClients
  filterSettings.value.enabled = true
  filterMenu.value = false
}

const clearFilter = () => {
  filterSettings.value = {
    enabled: false,
    state: '',
    group: '-',
    text: '',
    filteredClients: <any[]>[]
  }
  filterMenu.value = false
}

const addBulkModal = ref(false)

const addBulk = () => {
  addBulkModal.value = true
  actionMenu.value = false
}

const closeAddBulk = () => {
  addBulkModal.value = false
}

const editBulkModal = ref(false)

const editBulk = () => {
  editBulkModal.value = true
  actionMenu.value = false
}

const closeEditBulk = () => {
  editBulkModal.value = false
}

const percent = (c: Client) => { return c.volume>0 ? Math.round((c.up+c.down) *100 / c.volume) : 0 }
const percentColor = (c: Client) => { return (c.up+c.down) >= c.volume ? 'error' : percent(c)>90 ? 'warning' : 'success' }

</script>
