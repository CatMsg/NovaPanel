<template>
  <EndpointVue 
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :data="modal.data"
    :tags="endpointTags"
    @close="closeModal"
  />
  <Stats
    v-model="stats.visible"
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="closeStats"
  />
  <MasqueStatus
    v-model="masqueStatus.visible"
    :visible="masqueStatus.visible"
    :data="masqueStatus.data"
    @close="closeMasqueStatus"
  />
  <QrCode
    v-model="qrcode.visible"
    :visible="qrcode.visible"
    :data="qrcode.data"
    @close="closeQrCode"
  />
  <v-card class="resource-hero resource-hero--endpoints" rounded="xl" variant="flat">
    <div class="resource-hero__topline">
      <span class="resource-hero__badge">{{ $t('pages.endpoints') }}</span>
      <span class="resource-hero__badge resource-hero__badge--soft">{{ endpoints.length }} items</span>
    </div>
    <v-row class="resource-hero__content" align="center">
      <v-col cols="12" lg="8">
        <div class="resource-hero__title-row">
          <div class="resource-hero__icon">
            <v-icon icon="mdi-cloud-tags" size="32" />
          </div>
          <div>
            <h1 class="resource-hero__title">{{ $t('pages.endpoints') }}</h1>
            <p class="resource-hero__subtitle">
              管理节点管理与 MASQUE、WireGuard 等端点，状态与复制入口聚合到一处。
            </p>
          </div>
        </div>
        <div class="resource-hero__meta">
          <span>在线 {{ onlines.length }}</span>
          <span>•</span>
          <span>总数 {{ endpoints.length }}</span>
          <span>•</span>
          <span>MASQUE {{ endpoints.filter(e => e.type == 'masque').length }}</span>
        </div>
      </v-col>
      <v-col cols="12" lg="4" class="resource-hero__actions">
        <v-btn color="primary" size="large" @click="showModal(0)">
          <v-icon icon="mdi-plus" start />
          {{ $t('actions.add') }}
        </v-btn>
      </v-col>
    </v-row>
  </v-card>

  <v-row class="resource-grid">
    <v-col cols="12" sm="6" md="4" lg="3" v-for="(item, index) in <any[]>endpoints" :key="item.tag">
      <v-card class="resource-card" rounded="xl" variant="flat" :title="item.tag">
        <v-card-subtitle style="margin-top: -15px;">
          <v-row>
            <v-col>{{ item.type }}</v-col>
          </v-row>
        </v-card-subtitle>
        <v-card-text>
          <template v-if="item.type == 'masque'">
            <v-row>
              <v-col>Server</v-col>
              <v-col>{{ item.server ?? '-' }}</v-col>
            </v-row>
            <v-row>
              <v-col>Port</v-col>
              <v-col>{{ item.port ?? '-' }}</v-col>
            </v-row>
            <v-row>
              <v-col>Network</v-col>
              <v-col>{{ item.network ?? '-' }}</v-col>
            </v-row>
            <v-row>
              <v-col>IP</v-col>
              <v-col>{{ item.ip ?? '-' }}</v-col>
            </v-row>
          </template>
          <template v-else>
            <v-row>
              <v-col>{{ $t('in.addr') }}</v-col>
              <v-col>
                {{ item.address?.length>0 ? item.address[0] : '-' }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>{{ $t('in.port') }}</v-col>
              <v-col>
                {{ item.listen_port>0 ? item.listen_port : '-' }}
              </v-col>
            </v-row>
            <v-row>
              <v-col>{{ $t('types.wg.peers') }}</v-col>
              <v-col>
                {{ item.peers?.length?? '-'  }}
              </v-col>
            </v-row>
          </template>
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
        <v-card-actions class="endpoint-actions">
          <v-btn icon="mdi-file-edit" @click="showModal(item.id)">
            <v-icon />
            <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
          <v-btn icon="mdi-file-remove" style="margin-inline-start:0;" color="warning" @click="delOverlay[index] = true">
            <v-icon />
            <v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
          </v-btn>
          <v-btn v-if="item.type == 'masque'" icon="mdi-content-copy" @click="copyMasque(item)">
            <v-icon />
            <v-tooltip activator="parent" location="top" text="Copy config"></v-tooltip>
          </v-btn>
          <v-btn v-if="item.type == 'masque'" icon="mdi-information-outline" @click="showMasqueStatus(item.id)">
            <v-icon />
            <v-tooltip activator="parent" location="top" text="MASQUE status"></v-tooltip>
          </v-btn>
          <v-overlay
            v-model="delOverlay[index]"
            contained
            class="align-center justify-center"
          >
            <v-card :title="$t('actions.del')" rounded="lg">
              <v-divider></v-divider>
              <v-card-text>{{ $t('confirm') }}</v-card-text>
              <v-card-actions>
                <v-btn color="error" variant="outlined" @click="delEndpoint(item.tag)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delOverlay[index] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-overlay>
          <v-btn
            v-if="item.type == 'wireguard' && item.peers?.length>0"
            icon="mdi-qrcode"
            @click="showQrCode(item.id)"
          >
            <v-icon />
            <v-tooltip activator="parent" location="top" text="WireGuard QR Code"></v-tooltip>
          </v-btn>
          <v-btn icon="mdi-chart-line" @click="showStats(item.tag)" v-if="Data().enableTraffic">
            <v-icon />
            <v-tooltip activator="parent" location="top" :text="$t('stats.graphTitle')"></v-tooltip>
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import { Endpoint } from '@/types/endpoints'
import { buildMasqueConfig } from '@/plugins/masqueUtil'
import { computed, defineAsyncComponent, ref } from 'vue'

const EndpointVue = defineAsyncComponent(() => import('@/layouts/modals/Endpoint.vue'))
const Stats = defineAsyncComponent(() => import('@/layouts/modals/Stats.vue'))
const MasqueStatus = defineAsyncComponent(() => import('@/layouts/modals/MasqueStatus.vue'))
const QrCode = defineAsyncComponent(() => import('@/layouts/modals/WgQrCode.vue'))

const endpoints = computed((): Endpoint[] => {
  return <Endpoint[]> Data().endpoints
})

const endpointTags = computed((): any[] => {
  return endpoints.value?.map((o:Endpoint) => o.tag)
})

const onlines = computed(() => {
  return [...Data().onlines.inbound?? [], ...Data().onlines.outbound??[] ]
})

const modal = ref({
  visible: false,
  id: 0,
  data: "",
})

let delOverlay = ref(new Array<boolean>)

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.data = id == 0 ? '' : JSON.stringify(endpoints.value.findLast(o => o.id == id))
  modal.value.visible = true
}

const closeModal = () => {
  modal.value.visible = false
}

const stats = ref({
  visible: false,
  resource: "endpoint",
  tag: "",
})

const delEndpoint = async (tag: string) => {
  const index = endpoints.value.findIndex(i => i.tag == tag)
  const success = await Data().save("endpoints", "del", tag)
  if (success) delOverlay.value[index] = false
}

const showStats = (tag: string) => {
  stats.value.tag = tag
  stats.value.visible = true
}
const closeStats = () => {
  stats.value.visible = false
}

const qrcode = ref({
  visible: false,
  data: <any>{},
})

const masqueStatus = ref({
  visible: false,
  data: <any>{},
})

const showQrCode = (id: number) => {
  qrcode.value.data = endpoints.value.findLast(o => o.id == id)
  qrcode.value.visible = true
}
const closeQrCode = () => {
  qrcode.value.visible = false
}

const showMasqueStatus = (id: number) => {
  masqueStatus.value.data = endpoints.value.findLast(o => o.id == id)
  masqueStatus.value.visible = true
}

const closeMasqueStatus = () => {
  masqueStatus.value.visible = false
}

const copyMasque = async (item: any) => {
  const text = buildMasqueConfig(item)
  await navigator.clipboard.writeText(text)
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

.resource-hero__badge--soft {
  text-transform: none;
  letter-spacing: 0;
  color: var(--np-text-muted);
  background: rgba(148, 163, 184, 0.12);
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

.endpoint-actions {
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px;
}

.endpoint-actions :deep(.v-btn) {
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
