<template>
    <TlsVue 
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :data="modal.data"
    @close="closeModal"
    @save="saveModal"
  />
  <v-card class="resource-hero resource-hero--tls" rounded="xl" variant="flat">
    <div class="resource-hero__topline">
      <span class="resource-hero__badge">{{ $t('objects.tls') }}</span>
    </div>
    <v-row class="resource-hero__content" align="center">
      <v-col cols="12" lg="8">
        <div class="resource-hero__title-row">
          <div class="resource-hero__icon">
            <v-icon icon="mdi-certificate-outline" size="32" />
          </div>
          <div>
            <h1 class="resource-hero__title">{{ $t('objects.tls') }}</h1>
            <p class="resource-hero__subtitle">
              集中管理证书配置、ACME、ECH 与 Reality 选项，减少在多个弹窗里来回切换。
            </p>
          </div>
        </div>
        <div class="resource-hero__meta">
          <span>总数 {{ tlsConfigs.length }}</span>
          <span>•</span>
          <span>已绑定入站 {{ tlsConfigs.filter(t => tlsInbounds(t.id).length > 0).length }}</span>
        </div>
      </v-col>
      <v-col v-if="tlsConfigs.length > 0" cols="12" lg="4" class="resource-hero__actions">
        <v-btn color="primary" size="large" @click="showModal(0)">
          <v-icon icon="mdi-plus" start />
          {{ $t('actions.add') }}
        </v-btn>
      </v-col>
    </v-row>
  </v-card>

  <v-row class="resource-grid">
    <v-col v-if="tlsConfigs.length === 0" cols="12">
      <EmptyState
        icon="mdi-certificate-outline"
        title="暂无 TLS 配置"
        description="添加证书配置后，可供入站和服务统一引用。"
        :action="$t('actions.add')"
        @action="showModal(0)"
      />
    </v-col>
    <v-col cols="12" sm="6" md="4" lg="3" v-for="(item, index) in <any[]>tlsConfigs" :key="item.id">
      <v-card class="resource-card" rounded="xl" variant="flat" :title="item.name">
        <v-card-subtitle style="margin-top: -15px;">
          {{ item.server?.server_name?.length>0 ? item.server.server_name : "-" }}
        </v-card-subtitle>
        <v-card-text>
          <v-row>
            <v-col>{{ $t('pages.inbounds') }}</v-col>
            <v-col>
              <template v-if="tlsInbounds(item.id).length>0">
                <v-tooltip activator="parent" dir="ltr" location="bottom">
                  <span v-for="i in tlsInbounds(item.id)" :key="i">{{ i }}<br /></span>
                </v-tooltip>
                {{ tlsInbounds(item.id).length }}
              </template>
              <template v-else>-</template>
            </v-col>
          </v-row>
          <v-row>
            <v-col>ACME</v-col>
            <v-col>
              {{ $t(item.server?.acme == undefined ? 'no' : 'yes') }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>ECH</v-col>
            <v-col>
              {{ $t(item.server?.ech == undefined ? 'no' : 'yes') }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>Reality</v-col>
            <v-col>
              {{ $t(item.server?.reality == undefined ? 'no' : 'yes') }}
            </v-col>
          </v-row>
        </v-card-text>
        <v-divider></v-divider>
        <v-card-actions class="resource-card__actions">
          <v-btn class="np-card-action" variant="text" @click="showModal(item.id)">
            <v-icon icon="mdi-file-edit" /><span>{{ $t('actions.edit') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
          <v-btn v-if="tlsInbounds(item.id).length == 0" class="np-card-action" variant="text" style="margin-inline-start:0;" color="warning" @click="delOverlay[index] = true">
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
                <v-btn color="error" variant="outlined" @click="delTls(item.id)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delOverlay[index] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-overlay>
          <v-btn class="np-card-action" variant="text" @click="clone(item)">
            <v-icon icon="mdi-content-duplicate" /><span>{{ $t('actions.clone') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.clone')"></v-tooltip>
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import { computed, defineAsyncComponent, ref } from 'vue'
import { Inbound } from '@/types/inbounds'
import { tls } from '@/types/tls'
import EmptyState from '@/components/EmptyState.vue'

const TlsVue = defineAsyncComponent(() => import('@/layouts/modals/Tls.vue'))

const tlsConfigs = computed((): any[] => {
  return Data().tlsConfigs
})

const inbounds = computed((): Inbound[] => {
  return Data().inbounds
})

const tlsInbounds = (id: number): string[] => {
  return inbounds.value.filter(i => i.tls_id == id).map(i => i.tag)  
}

const modal = ref({
  visible: false,
  id: 0,
  data: "",
})

const delOverlay = ref(new Array<boolean>(tlsConfigs.value.length).fill(false))

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.data = id == 0 ? '{}' : JSON.stringify(tlsConfigs.value.findLast(t => t.id == id))
  modal.value.visible = true
}
const clone = (obj: any) => {
  let data = JSON.parse(JSON.stringify(obj))
  data.id = 0
  while (tlsConfigs.value.findIndex(t => t.name == data.name) != -1){
    data.name += "-copy"
  }
  saveModal(data)
}
const closeModal = () => {
  modal.value.visible = false
}
const saveModal = async (data:tls) => {
  const success = await Data().save("tls", data.id > 0 ? "edit" : "new", data)
  if (success) modal.value.visible = false
}

const delTls = async (id: number) => {
  const index = tlsConfigs.value.findIndex(t => t.id == id)
  const success = await Data().save("tls", "del", id)
  if (success) delOverlay.value[index] = false
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
