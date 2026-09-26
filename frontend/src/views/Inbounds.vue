<template>
  <InboundVue 
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :inTags="inTags"
    :tlsConfigs="tlsConfigs"
    @close="closeModal"
  />
  <Stats
    v-model="stats.visible"
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="closeStats"
  />
  <MieruStatus
    v-model="mieruStatus.visible"
    :visible="mieruStatus.visible"
    :data="mieruStatus.data"
    @close="closeMieruStatus"
  />
  <MasqueStatus
    v-model="masqueStatus.visible"
    :visible="masqueStatus.visible"
    :data="masqueStatus.data"
    @close="closeMasqueStatus"
  />
  <v-dialog v-model="deleteDialogOpen" max-width="420">
    <v-card rounded="xl" class="resource-delete-dialog">
      <v-card-title>{{ $t('actions.del') }} · {{ deleteTarget?.tag }}</v-card-title>
      <v-card-text>{{ $t('confirm') }}</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="deleteDialogOpen = false">{{ $t('no') }}</v-btn>
        <v-btn color="error" variant="tonal" :loading="deleteLoading" @click="confirmDelete">{{ $t('yes') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
  <v-card class="resource-hero resource-hero--inbounds" rounded="xl" variant="flat">
    <div class="resource-hero__topline">
      <span class="resource-hero__badge">{{ $t('pages.inbounds') }}</span>
    </div>
    <v-row class="resource-hero__content" align="center">
      <v-col cols="12" lg="8">
        <div class="resource-hero__title-row">
          <div class="resource-hero__icon">
            <v-icon icon="mdi-cloud-download-outline" size="32" />
          </div>
          <div>
            <h1 class="resource-hero__title">{{ $t('pages.inbounds') }}</h1>
            <p class="resource-hero__subtitle">{{ $t('ui.resource.inboundsSubtitle') }}</p>
          </div>
        </div>
        <div class="resource-hero__meta">
          <span>{{ $t('ui.resource.protocolCount', { count: new Set(inbounds.map(i => i.type)).size }) }}</span>
          <span>•</span>
          <span>{{ $t('ui.resource.onlineCount', { count: onlines.length }) }}</span>
          <span>•</span>
          <span>{{ $t('itemCount', { count: inbounds.length }) }}</span>
        </div>
      </v-col>
      <v-col v-if="inbounds.length > 0" cols="12" lg="4" class="resource-hero__actions">
        <v-btn color="primary" size="large" @click="showModal(0)">
          <v-icon icon="mdi-plus" start />
          {{ $t('actions.add') }}
        </v-btn>
      </v-col>
    </v-row>
  </v-card>

  <div v-if="inbounds.length > 0" class="resource-list-toolbar">
    <v-text-field v-model="search" :placeholder="$t('ui.resource.searchInbounds')" prepend-inner-icon="mdi-magnify" clearable hide-details density="comfortable" variant="solo-filled" />
    <span>{{ $t('itemCount', { count: filteredInbounds.length }) }}</span>
  </div>

  <v-row v-if="inbounds.length === 0" class="resource-grid">
    <v-col cols="12">
      <EmptyState
        icon="mdi-cloud-download-outline"
        :title="$t('ui.resource.noInbounds')"
        :description="$t('ui.resource.noInboundsHint')"
        :action="$t('actions.add')"
        @action="showModal(0)"
      />
    </v-col>
  </v-row>

  <v-data-table
    v-else-if="!smAndDown"
    class="resource-table"
    :headers="tableHeaders"
    :items="filteredInbounds"
    item-value="id"
    density="comfortable"
    hover
  >
    <template #item.tag="{ item }">
      <strong class="resource-table__primary">{{ item.tag }}</strong>
    </template>
    <template #item.address="{ item }">{{ item.listen || '0.0.0.0' }}</template>
    <template #item.port="{ item }">{{ item.listen_port ?? '-' }}</template>
    <template #item.tls="{ item }">
      <v-chip size="small" :color="['mieru', 'masque'].includes(item.type) ? 'secondary' : item.tls_id > 0 ? 'success' : 'default'" variant="tonal">
        {{ ['mieru', 'masque'].includes(item.type) ? $t('ui.resource.notApplicable') : item.tls_id > 0 ? $t('enable') : $t('disable') }}
      </v-chip>
    </template>
    <template #item.usersCount="{ item }">{{ inboundUsers(item).length || '-' }}</template>
    <template #item.online="{ item }">
      <v-chip v-if="onlines.includes(item.tag)" size="small" color="success" variant="tonal">{{ $t('online') }}</v-chip>
      <span v-else class="resource-table__muted">-</span>
    </template>
    <template #item.actions="{ item }">
      <div class="resource-table__actions">
        <v-btn icon="mdi-file-edit-outline" size="small" variant="text" :aria-label="$t('actions.edit')" :title="$t('actions.edit')" @click="showModal(item.id)" />
        <v-menu location="bottom end">
          <template #activator="{ props }"><v-btn v-bind="props" icon="mdi-dots-horizontal" size="small" variant="text" :aria-label="$t('ui.common.more')" /></template>
          <v-list density="compact" nav>
            <v-list-item v-if="!['mieru', 'masque'].includes(item.type)" prepend-icon="mdi-content-duplicate" :title="$t('actions.clone')" @click="clone(item.id)" />
            <v-list-item v-if="item.type === 'mieru' || item.type === 'masque'" prepend-icon="mdi-information-outline" :title="$t('status')" @click="item.type === 'mieru' ? showMieruStatus(item) : showMasqueStatus(item)" />
            <v-list-item v-if="Data().enableTraffic" prepend-icon="mdi-chart-line" :title="$t('stats.graphTitle')" @click="showStats(item.tag)" />
            <v-divider />
            <v-list-item prepend-icon="mdi-delete-outline" :title="$t('actions.del')" base-color="error" @click="askDelete(item)" />
          </v-list>
        </v-menu>
      </div>
    </template>
    <template #no-data>
      <EmptyState icon="mdi-magnify" :title="$t('ui.resource.noSearchResults')" :description="$t('ui.resource.noSearchResultsHint')" />
    </template>
  </v-data-table>

  <v-row v-else class="resource-grid">
    <v-col v-if="filteredInbounds.length === 0" cols="12">
      <EmptyState icon="mdi-magnify" :title="$t('ui.resource.noSearchResults')" :description="$t('ui.resource.noSearchResultsHint')" />
    </v-col>
    <v-col cols="12" sm="6" md="4" lg="3" v-for="item in filteredInbounds" :key="item.tag">
      <v-card class="resource-card" rounded="xl" variant="flat" :title="item.tag">
        <v-card-subtitle style="margin-top: -15px;">
          <v-row>
            <v-col>{{ item.type }}</v-col>
          </v-row>
        </v-card-subtitle>
        <v-card-text>
          <v-row>
            <v-col>{{ $t('in.addr') }}</v-col>
            <v-col>
              {{ item.listen }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>{{ $t('in.port') }}</v-col>
            <v-col>
              {{ item.listen_port }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>{{ $t('objects.tls') }}</v-col>
            <v-col>
              {{ ['mieru', 'masque'].includes(item.type) ? $t('ui.resource.notApplicable') : (item.tls_id > 0 ? $t('enable') : $t('disable')) }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>{{ $t('pages.clients') }}</v-col>
            <v-col>
              <template v-if="inboundUsers(item).length">
                <v-tooltip activator="parent" dir="ltr" location="bottom">
                  <span v-for="u in inboundUsers(item)" :key="u">{{ u }}<br /></span>
                </v-tooltip>
                {{ inboundUsers(item).length }}
              </template>
              <template v-else>-</template>
            </v-col>
          </v-row>
          <v-row>
            <v-col>{{ $t('online') }}</v-col>
            <v-col>
              <template v-if="onlines.includes(item.tag)">
                <v-chip density="comfortable" size="small" color="success" variant="flat">{{ $t('online') }}</v-chip>
              </template>
              <template v-else>-</template>
            </v-col>
          </v-row>
        </v-card-text>
        <v-divider></v-divider>
        <v-card-actions class="resource-card__actions">
          <v-btn class="np-card-action" variant="text" @click="showModal(item.id)">
            <v-icon icon="mdi-file-edit" /><span>{{ $t('actions.edit') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
          <v-btn class="np-card-action" variant="text" style="margin-inline-start:0;" color="warning" @click="askDelete(item)">
            <v-icon icon="mdi-file-remove" /><span>{{ $t('actions.del') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
          </v-btn>
          <v-btn v-if="!['mieru', 'masque'].includes(item.type)" class="np-card-action" variant="text" :loading="cloneLoading" @click="clone(item.id)">
            <v-icon icon="mdi-content-duplicate" /><span>{{ $t('actions.clone') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.clone')"></v-tooltip>
          </v-btn>
          <v-btn v-if="item.type == 'mieru'" class="np-card-action" variant="text" @click="showMieruStatus(item)">
            <v-icon icon="mdi-information-outline" /><span>{{ $t('status') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('status')"></v-tooltip>
          </v-btn>
          <v-btn v-if="item.type == 'masque'" class="np-card-action" variant="text" @click="showMasqueStatus(item)">
            <v-icon icon="mdi-shield-account-outline" /><span>{{ $t('status') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('status')"></v-tooltip>
          </v-btn>
          <v-btn class="np-card-action" variant="text" @click="showStats(item.tag)" v-if="Data().enableTraffic">
            <v-icon icon="mdi-chart-line" /><span>{{ $t('stats.graphTitle') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('stats.graphTitle')"></v-tooltip>
          </v-btn>
        </v-card-actions>
      </v-card>      
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import { Config } from '@/types/config'
import { computed, defineAsyncComponent, ref } from 'vue'
import { createInbound, Inbound } from '@/types/inbounds'
import RandomUtil from '@/plugins/randomUtil'
import EmptyState from '@/components/EmptyState.vue'
import { useDisplay } from 'vuetify'
import { useI18n } from 'vue-i18n'

const InboundVue = defineAsyncComponent(() => import('@/layouts/modals/Inbound.vue'))
const Stats = defineAsyncComponent(() => import('@/layouts/modals/Stats.vue'))
const MieruStatus = defineAsyncComponent(() => import('@/layouts/modals/MieruStatus.vue'))
const MasqueStatus = defineAsyncComponent(() => import('@/layouts/modals/MasqueStatus.vue'))

const appConfig = computed((): Config => {
  return <Config> Data().config
})
const { smAndDown } = useDisplay()
const { t } = useI18n()
const search = ref('')

const inbounds = computed((): Inbound[] => {
  return <Inbound[]> Data().inbounds
})

const filteredInbounds = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  if (!query) return inbounds.value
  return inbounds.value.filter(item => [item.tag, item.type, item.listen, item.listen_port, ...inboundUsers(item)]
    .some(value => String(value ?? '').toLocaleLowerCase().includes(query)))
})

const inboundUsers = (item: Inbound): string[] => (item as any).users ?? []

const tableHeaders = computed(() => [
  { title: t('objects.tag'), key: 'tag' },
  { title: t('protocol'), key: 'type' },
  { title: t('in.addr'), key: 'address' },
  { title: t('in.port'), key: 'port' },
  { title: t('objects.tls'), key: 'tls', sortable: false },
  { title: t('pages.clients'), key: 'usersCount' },
  { title: t('online'), key: 'online', sortable: false },
  { title: t('ui.common.more'), key: 'actions', sortable: false, align: 'end' as const },
])

const tlsConfigs = computed((): any[] => {
  return <any[]> Data().tlsConfigs
})

const inTags = computed((): string[] => {
  return [...inbounds.value?.map(i => i.tag), ...Data().endpoints?.filter((e:any) => e.listen_port > 0 && e.type != "masque").map((e:any) => e.tag)]
})

const onlines = computed(() => {
  return Data().onlines.inbound?? []
})

const modal = ref({
  visible: false,
  id: 0,
})

const deleteDialogOpen = ref(false)
const deleteLoading = ref(false)
const deleteTarget = ref<Inbound | null>(null)

const askDelete = (item: Inbound) => {
  deleteTarget.value = item
  deleteDialogOpen.value = true
}

const confirmDelete = async () => {
  if (!deleteTarget.value) return
  deleteLoading.value = true
  const success = await delInbound(deleteTarget.value.id)
  deleteLoading.value = false
  if (success) {
    deleteDialogOpen.value = false
    deleteTarget.value = null
  }
}

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.visible = true
}
const closeModal = () => {
  modal.value.visible = false
}

const delInbound = async (id: number) => {
  const inbound = inbounds.value.find(i => i.id == id)
  if (!inbound) return false
  return await Data().save("inbounds", "del", inbound.tag)
}

let cloneLoading = ref(false)

const clone = async (id: number) => {
  cloneLoading.value = true
  const inboundArray = await Data().loadInbounds([id])
  const inbound = inboundArray[0]
  let newTag = inbound.type + "-" + RandomUtil.randomSeq(3)
  const newInbound = createInbound(inbound.type, { ...inbound,
    id: 0,
    tag: newTag,
    listen_port: RandomUtil.randomIntRange(10000, 60000),
  })
  await Data().save("inbounds", "new", newInbound)
  cloneLoading.value = false
}

const stats = ref({
  visible: false,
  resource: "inbound",
  tag: "",
})

const showStats = (tag: string) => {
  stats.value.tag = tag
  stats.value.visible = true
}
const closeStats = () => {
  stats.value.visible = false
}

const mieruStatus = ref({
  visible: false,
  data: <any>{},
})

const showMieruStatus = (item: any) => {
  mieruStatus.value.data = item
  mieruStatus.value.visible = true
}

const closeMieruStatus = () => {
  mieruStatus.value.visible = false
}

const masqueStatus = ref({ visible: false, data: <any>{} })
const showMasqueStatus = (item: any) => {
  masqueStatus.value.data = item
  masqueStatus.value.visible = true
}
const closeMasqueStatus = () => {
  masqueStatus.value.visible = false
}
</script>

<style scoped lang="scss">
.resource-hero,
.resource-card {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.22), transparent 28%),
    var(--np-surface);
  border: 1px solid var(--np-border);
  box-shadow: var(--np-shadow);
  backdrop-filter: blur(28px) saturate(1.12);
}

.resource-hero {
  padding: 20px;
  margin-bottom: 18px;
  overflow: hidden;
}

.resource-list-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin: 0 0 12px; color: var(--np-text-muted); font-size: .78rem; }
.resource-list-toolbar :deep(.v-input) { max-width: 420px; }
.resource-table { overflow: hidden; border: 1px solid var(--np-border); border-radius: 18px; background: var(--np-surface); box-shadow: var(--np-shadow); backdrop-filter: blur(24px) saturate(1.08); }
.resource-table__primary { overflow-wrap: anywhere; }
.resource-table__muted { color: var(--np-text-muted); }
.resource-table__actions { display: flex; align-items: center; justify-content: flex-end; gap: 2px; }
.resource-table__empty { padding: 26px; color: var(--np-text-muted); text-align: center; }
.resource-delete-dialog { border: 1px solid var(--np-border); background: var(--np-surface); }

.resource-hero__topline {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 16px;
}

.resource-hero__badge {
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 12px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--np-accent);
  background: rgba(10, 132, 255, 0.08);
}

.resource-hero__content {
  min-height: 120px;
}

.resource-hero__title-row {
  display: flex;
  gap: 14px;
  align-items: flex-start;
}

.resource-hero__icon {
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  border-radius: 16px;
  color: var(--np-accent);
  background: linear-gradient(145deg, rgba(59, 130, 246, 0.16), rgba(14, 165, 233, 0.08));
}

.resource-hero__title {
  margin: 0;
  font-size: clamp(28px, 3vw, 40px);
  line-height: 1.1;
  font-weight: 800;
  letter-spacing: -0.03em;
}

.resource-hero__subtitle {
  margin: 12px 0 0;
  color: var(--np-text-muted);
  line-height: 1.7;
}

.resource-hero__meta {
  margin-top: 14px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  color: var(--np-text-muted);
  font-size: 13px;
}

.resource-hero__actions {
  display: flex;
  justify-content: flex-end;
}

.resource-grid {
  margin-top: 0;
}

.resource-card {
  overflow: hidden;
  min-height: 100%;
}

.resource-card :deep(.v-card-title) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.resource-card :deep(.v-card-text .v-col) {
  min-width: 0;
  overflow-wrap: anywhere;
}

.resource-card__actions {
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px;
}

.resource-card__actions :deep(.v-btn) {
  flex: 0 0 auto;
}

.v-theme--dark .resource-hero,
.v-theme--dark .resource-card {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.08), transparent 24%),
    rgba(11, 18, 31, 0.78);
  border-color: rgba(148, 163, 184, 0.16);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.3);
}

@media (max-width: 960px) {
  .resource-hero {
    padding: 16px;
  }

  .resource-hero__actions {
    justify-content: flex-start;
  }
}

@media (max-width: 600px) {
  .resource-hero__icon {
    width: 46px;
    height: 46px;
  }

  .resource-hero__title {
    font-size: 24px;
  }
}
</style>
