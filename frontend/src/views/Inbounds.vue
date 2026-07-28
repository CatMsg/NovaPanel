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
            <p class="resource-hero__subtitle">
              统一管理入站协议、监听端口和用户绑定，桌面端与移动端都更容易扫视。
            </p>
          </div>
        </div>
        <div class="resource-hero__meta">
          <span>协议 {{ new Set(inbounds.map(i => i.type)).size }}</span>
          <span>•</span>
          <span>在线 {{ onlines.length }}</span>
          <span>•</span>
          <span>总数 {{ inbounds.length }}</span>
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

  <v-row class="resource-grid">
    <v-col v-if="inbounds.length === 0" cols="12">
      <EmptyState
        icon="mdi-cloud-download-outline"
        title="暂无入站"
        description="创建入站后，可在这里统一管理协议、监听端口和用户绑定。"
        :action="$t('actions.add')"
        @action="showModal(0)"
      />
    </v-col>
    <v-col cols="12" sm="6" md="4" lg="3" v-for="(item, index) in <any[]>inbounds" :key="item.tag">
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
              {{ item.tls_id > 0 ? $t('enable') : $t('disable') }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>{{ $t('pages.clients') }}</v-col>
            <v-col>
              <template v-if="item.users">
                <v-tooltip activator="parent" dir="ltr" location="bottom" v-if="item.users.length > 0">
                  <span v-for="u in item.users" :key="u">{{ u }}<br /></span>
                </v-tooltip>
                {{ item.users.length }}
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
                <v-btn color="error" variant="outlined" @click="delInbound(item.id)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delOverlay[index] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-overlay>
          <v-btn class="np-card-action" variant="text" :loading="cloneLoading" @click="clone(item.id)">
            <v-icon icon="mdi-content-duplicate" /><span>{{ $t('actions.clone') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.clone')"></v-tooltip>
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

const InboundVue = defineAsyncComponent(() => import('@/layouts/modals/Inbound.vue'))
const Stats = defineAsyncComponent(() => import('@/layouts/modals/Stats.vue'))

const appConfig = computed((): Config => {
  return <Config> Data().config
})

const inbounds = computed((): Inbound[] => {
  return <Inbound[]> Data().inbounds
})

const tlsConfigs = computed((): any[] => {
  return <any[]> Data().tlsConfigs
})

const inTags = computed((): string[] => {
  return [...inbounds.value?.map(i => i.tag), ...Data().endpoints?.filter((e:any) => e.listen_port > 0 && e.type != "masque" && e.type != "mieru").map((e:any) => e.tag)]
})

const onlines = computed(() => {
  return Data().onlines.inbound?? []
})

const modal = ref({
  visible: false,
  id: 0,
})

let delOverlay = ref(new Array<boolean>)

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.visible = true
}
const closeModal = () => {
  modal.value.visible = false
}

const delInbound = async (id: number) => {
  const index = inbounds.value.findIndex(i => i.id == id)
  const tag = inbounds.value[index].tag

  const success = await Data().save("inbounds", "del", tag)
  if (success) delOverlay.value[index] = false
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
