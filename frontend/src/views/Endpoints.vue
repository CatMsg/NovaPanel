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
  <QrCode
    v-model="qrcode.visible"
    :visible="qrcode.visible"
    :data="qrcode.data"
    @close="closeQrCode"
  />
  <v-card class="resource-hero resource-hero--endpoints" rounded="xl" variant="flat">
    <div class="resource-hero__topline">
      <span class="resource-hero__badge">{{ $t('pages.endpoints') }}</span>
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
              管理 WireGuard、Warp 与 Tailscale 等端点，状态与配置入口聚合到一处。
            </p>
          </div>
        </div>
        <div class="resource-hero__meta">
          <span>在线 {{ onlines.length }}</span>
          <span>•</span>
          <span>总数 {{ endpoints.length }}</span>
        </div>
      </v-col>
      <v-col v-if="endpoints.length > 0" cols="12" lg="4" class="resource-hero__actions">
        <v-btn color="primary" size="large" @click="showModal(0)">
          <v-icon icon="mdi-plus" start />
          {{ $t('actions.add') }}
        </v-btn>
      </v-col>
    </v-row>
  </v-card>

  <v-card class="endpoint-aggregate-card" rounded="xl" variant="flat">
    <button class="endpoint-aggregate-card__head" type="button" @click="showEndpointAggregate = !showEndpointAggregate">
      <span class="endpoint-aggregate-card__icon"><v-icon icon="mdi-source-branch" /></span>
      <span class="endpoint-aggregate-card__copy">
        <strong>节点聚合</strong>
        <small>配置本机节点源，以及主节点使用的上游聚合出口。</small>
      </span>
      <v-chip size="small" variant="tonal">{{ endpointAggregateConfig.endpointMode == 'master' ? '主模式' : '从模式' }}</v-chip>
      <v-icon :icon="showEndpointAggregate ? 'mdi-chevron-up' : 'mdi-chevron-down'" />
    </button>
    <v-expand-transition>
      <div v-show="showEndpointAggregate" class="endpoint-aggregate">
        <div class="endpoint-aggregate__header">
          <div>
            <div class="endpoint-aggregate__title">聚合方式</div>
            <div class="endpoint-aggregate__desc">从模式暴露本机节点，主模式进一步汇总多个上游源。</div>
          </div>
          <v-select
            v-model="endpointAggregateConfig.endpointMode"
            :items="endpointModeItems"
            label="节点模式"
            density="compact"
            variant="outlined"
            hide-details
            class="endpoint-aggregate__mode"
          />
        </div>
        <v-text-field
          :model-value="endpointSourceURI"
          label="本机节点源"
          readonly
          density="comfortable"
          variant="outlined"
          hide-details
          append-inner-icon="mdi-content-copy"
          @click:append-inner="copyEndpointSourceURI"
        />
        <v-textarea
          v-if="endpointAggregateConfig.endpointMode == 'master'"
          v-model="endpointAggregateConfig.endpointSources"
          label="节点上游源"
          hint="每行填写一个 VPS 的本机节点源链接，推荐使用 format=json。"
          persistent-hint
          rows="3"
          auto-grow
          density="comfortable"
          variant="outlined"
        />
        <v-text-field
          v-if="endpointAggregateConfig.endpointMode == 'master'"
          :model-value="endpointAggregateURI"
          label="节点聚合出口"
          readonly
          density="comfortable"
          variant="outlined"
          hide-details
          append-inner-icon="mdi-content-copy"
          @click:append-inner="copyEndpointAggregateURI"
        />
        <div class="endpoint-aggregate__actions">
          <v-btn size="small" color="primary" variant="flat" :loading="endpointAggregateSaving" @click="saveEndpointAggregateConfig">
            <v-icon icon="mdi-content-save-outline" start />
            保存节点聚合
          </v-btn>
        </div>
      </div>
    </v-expand-transition>
  </v-card>

  <v-row class="resource-grid">
    <v-col v-if="endpoints.length === 0" cols="12">
      <EmptyState
        icon="mdi-cloud-tags-outline"
        title="暂无节点"
        description="添加 WireGuard、Warp 或 Tailscale 节点后，可在这里查看状态和客户端配置。"
        :action="$t('actions.add')"
        @action="showModal(0)"
      />
    </v-col>
    <v-col cols="12" sm="6" md="4" lg="3" v-for="(item, index) in <any[]>endpoints" :key="item.tag">
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
          <v-btn class="np-card-action" variant="text" @click="showModal(item.id)">
            <v-icon icon="mdi-file-edit" /><span>{{ $t('actions.edit') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
          <v-btn class="np-card-action" variant="text" style="margin-inline-start:0;" color="warning" @click="delOverlay[index] = true">
            <v-icon icon="mdi-file-remove" /><span>{{ $t('actions.del') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
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
            class="np-card-action"
            variant="text"
            @click="showQrCode(item.id)"
          >
            <v-icon icon="mdi-qrcode" /><span>QR</span>
            <v-tooltip activator="parent" location="top" text="WireGuard QR Code"></v-tooltip>
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
import { Endpoint } from '@/types/endpoints'
import HttpUtils from '@/plugins/httputil'
import { computed, defineAsyncComponent, onMounted, ref, watch } from 'vue'
import { push } from 'notivue'
import { useDisplay } from 'vuetify'
import EmptyState from '@/components/EmptyState.vue'

const EndpointVue = defineAsyncComponent(() => import('@/layouts/modals/Endpoint.vue'))
const Stats = defineAsyncComponent(() => import('@/layouts/modals/Stats.vue'))
const QrCode = defineAsyncComponent(() => import('@/layouts/modals/WgQrCode.vue'))
const { smAndDown } = useDisplay()
const showEndpointAggregate = ref(!smAndDown.value)
watch(smAndDown, (mobile) => {
  if (!mobile) showEndpointAggregate.value = true
})

const endpoints = computed((): Endpoint[] => {
  return <Endpoint[]> Data().endpoints
})

const endpointTags = computed((): any[] => {
  return endpoints.value?.map((o:Endpoint) => o.tag)
})

const onlines = computed(() => {
  return [...Data().onlines.inbound?? [], ...Data().onlines.outbound??[] ]
})

const endpointModeItems = [
  { title: '主模式', value: 'master' },
  { title: '从模式', value: 'slave' },
]

const endpointAggregateConfig = ref({
  endpointMode: 'slave',
  endpointSources: '',
})

const endpointAggregateSaving = ref(false)

const endpointSubBaseURI = computed(() => {
  const base = String(Data().subURI ?? '').trim()
  if (!base) return ''
  return base.replace(/\/$/, '')
})

const endpointSourceURI = computed(() => {
  if (!endpointSubBaseURI.value) return ''
  return endpointSubBaseURI.value + '/endpoints?format=json'
})

const endpointAggregateURI = computed(() => {
  if (!endpointSubBaseURI.value) return ''
  return endpointSubBaseURI.value + '/endpoints/aggregate?format=clash'
})

onMounted(() => {
  loadEndpointAggregateConfig()
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


const showQrCode = (id: number) => {
  qrcode.value.data = endpoints.value.findLast(o => o.id == id)
  qrcode.value.visible = true
}
const closeQrCode = () => {
  qrcode.value.visible = false
}

const loadEndpointAggregateConfig = async () => {
  const msg = await HttpUtils.get('api/settings')
  if (!msg.success || !msg.obj) return
  endpointAggregateConfig.value = {
    endpointMode: msg.obj.endpointMode == 'master' ? 'master' : 'slave',
    endpointSources: String(msg.obj.endpointSources ?? ''),
  }
}

const saveEndpointAggregateConfig = async () => {
  endpointAggregateSaving.value = true
  const preflight = await Data().preflightSave('settings', 'set', endpointAggregateConfig.value)
  if (!preflight || preflight.changed === false) {
    endpointAggregateSaving.value = false
    return
  }
  const msg = await HttpUtils.post('api/save', {
    object: 'settings',
    action: 'set',
    data: JSON.stringify(endpointAggregateConfig.value),
  })
  endpointAggregateSaving.value = false
  if (msg.success) {
    push.success({ message: '节点聚合设置已保存' })
    return
  }
  if (String(msg.msg ?? '').includes('no changes')) {
    push.success({ message: '节点聚合设置没有变化' })
    return
  }
  push.error({ message: '节点聚合设置保存失败' })
}

const copyEndpointSourceURI = async () => {
  await copyEndpointLink(endpointSourceURI.value, '本机节点源')
}

const copyEndpointAggregateURI = async () => {
  await copyEndpointLink(endpointAggregateURI.value, '节点聚合出口')
}

const copyEndpointLink = async (text: string, label: string) => {
  if (!text) {
    push.error({ message: `当前没有可用的${label}` })
    return
  }
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
    } else {
      const textarea = document.createElement('textarea')
      textarea.value = text
      textarea.setAttribute('readonly', 'true')
      textarea.style.position = 'fixed'
      textarea.style.left = '-9999px'
      document.body.appendChild(textarea)
      textarea.select()
      const ok = document.execCommand('copy')
      document.body.removeChild(textarea)
      if (!ok) throw new Error('copy failed')
    }
    push.success({ message: `已复制${label}` })
  } catch {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.setAttribute('readonly', 'true')
    textarea.style.position = 'fixed'
    textarea.style.left = '-9999px'
    document.body.appendChild(textarea)
    textarea.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(textarea)
    if (ok) {
      push.success({ message: `已复制${label}` })
      return
    }
    push.error({ message: `复制${label}失败` })
  }
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

.endpoint-aggregate-card {
  margin-bottom: 18px;
  padding: 16px 18px;
  border: 1px solid var(--np-border);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.18), transparent 48%),
    var(--np-surface);
  box-shadow: var(--np-shadow-soft);
}

.endpoint-aggregate-card__head {
  display: flex;
  width: 100%;
  min-width: 0;
  align-items: center;
  gap: 12px;
  padding: 0;
  border: 0;
  color: inherit;
  background: transparent;
  text-align: left;
  cursor: pointer;
}

.endpoint-aggregate-card__icon {
  display: grid;
  width: 40px;
  height: 40px;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid rgba(10, 132, 255, 0.16);
  border-radius: 13px;
  color: var(--np-accent);
  background: rgba(10, 132, 255, 0.09);
}

.endpoint-aggregate-card__copy {
  display: flex;
  min-width: 0;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 3px;
}

.endpoint-aggregate-card__copy strong {
  color: var(--np-text-main);
  font-size: 0.96rem;
}

.endpoint-aggregate-card__copy small {
  overflow: hidden;
  color: var(--np-text-muted);
  font-size: 0.76rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.endpoint-aggregate {
  margin-top: 16px;
  display: grid;
  gap: 12px;
  padding-top: 16px;
  border-top: 1px solid var(--np-border);
}

.endpoint-aggregate__header {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  justify-content: space-between;
}

.endpoint-aggregate__title {
  font-weight: 800;
  letter-spacing: -0.02em;
}

.endpoint-aggregate__desc {
  margin-top: 4px;
  color: var(--np-text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.endpoint-aggregate__mode {
  max-width: 150px;
}

.endpoint-aggregate__actions {
  display: flex;
  justify-content: flex-end;
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

.v-theme--dark .endpoint-aggregate-card {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.06), transparent 48%),
    rgba(11, 18, 31, 0.78);
  border-color: rgba(148, 163, 184, 0.18);
}

@media (max-width: 960px) {
  .resource-hero {
    padding: 16px;
  }

  .resource-hero__actions {
    justify-content: flex-start;
  }

  .endpoint-aggregate__header {
    flex-direction: column;
  }

  .endpoint-aggregate__mode {
    max-width: none;
    width: 100%;
  }

}

@media (max-width: 600px) {
  .endpoint-aggregate-card {
    margin-bottom: 12px;
    padding: 14px;
  }

  .endpoint-aggregate-card__copy small,
  .endpoint-aggregate-card__head > .v-chip {
    display: none;
  }

  .resource-hero__icon {
    width: 46px;
    height: 46px;
  }

  .resource-hero__title {
    font-size: 24px;
  }
}
</style>
