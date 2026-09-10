<template>
  <DnsVue
    v-model="dnsModal.visible"
    :visible="dnsModal.visible"
    :index="dnsModal.index"
    :data="dnsModal.data"
    :tsTags="tsTags"
    :rslvdTags="rslvdTags"
    @close="closeDnsModal"
    @save="saveDnsModal"
  />
  <PageHero
    :eyebrow="$t('pages.dns')"
    :title="$t('pages.dns')"
    description="集中维护 DNS 服务器、默认解析器与缓存策略；DNS 分流规则统一在路由列表中管理。"
    icon="mdi-dns-outline"
  >
    <template #meta>
      <span>服务器 {{ dns.servers.length }}</span><span>•</span><span>默认 {{ finalDns || $t('dns.firstServer') }}</span>
    </template>
    <template #actions>
      <v-btn variant="outlined" color="warning" @click="saveConfig" :loading="loading" :disabled="stateChange">
        <v-icon icon="mdi-content-save-outline" start />{{ $t('actions.save') }}
      </v-btn>
    </template>
  </PageHero>
  <v-row class="dns-section np-section-card">
    <v-col class="dns-section__heading" cols="12"><h2>{{ $t('pages.basics') }}</h2><p>设置默认解析器、地址策略和缓存行为。</p></v-col>
    <v-col cols="12">
      <v-row>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-select
            hide-details
            :label="$t('dns.final')"
            :items="[ {title: $t('dns.firstServer'), value: ''}, ...dnsServerTags]"
            v-model="finalDns">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-select
            hide-details
            :label="$t('dns.domainStrategy')"
            clearable
            @click:clear="delete dns.strategy"
            :items="['prefer_ipv4','prefer_ipv6','ipv4_only','ipv6_only']"
            v-model="dns.strategy">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-text-field
            v-model="dns.client_subnet" hide-details
            clearable @click:clear="delete dns.client_subnet"
            :label="$t('dns.rule.action.clientSubnet')"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-text-field
            v-model.number="dns.cache_capacity"
            type="number" min="1024" hide-details
            clearable @click:clear="delete dns.cache_capacity"
            :label="$t('dns.cacheCapacity')"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-checkbox v-model="dns.disable_cache" hide-details :label="$t('dns.disableCache')" />
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-checkbox v-model="dns.disable_expire" hide-details :label="$t('dns.disableExpire')" />
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-checkbox v-model="dns.independent_cache" hide-details :label="$t('dns.independentCache')" />
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-checkbox v-model="dns.reverse_mapping" hide-details :label="$t('dns.reverseMapping')" />
        </v-col>
      </v-row>
    </v-col>
  </v-row>
  <v-row class="dns-section">
    <v-col class="dns-section__heading" cols="12">
      <div><h2>{{ $t('dns.title') }}</h2><p>可复用的解析服务器与 TLS 连接配置。</p></div>
      <v-btn color="primary" variant="tonal" @click="showDnsModal(-1)"><v-icon icon="mdi-plus" start />{{ $t('dns.add') }}</v-btn>
    </v-col>
    <v-col v-if="dns.servers.length === 0" cols="12">
      <EmptyState icon="mdi-server-network-outline" title="暂无 DNS 服务器" description="添加解析服务器后即可设为默认解析器或在路由列表中用于 DNS 分流。" />
    </v-col>
    <v-col cols="12" sm="6" md="4" lg="3" v-for="(item, index) in <any[]>dns.servers" :key="item.id">
      <v-card class="np-resource-card" rounded="xl" variant="flat" :title="item.tag">
        <v-card-subtitle style="margin-top: -15px;">
          <v-row>
            <v-col>{{ item.type }}</v-col>
          </v-row>
        </v-card-subtitle>
        <v-card-text>
          <v-row>
            <v-col>{{ $t('dns.server') }}</v-col>
            <v-col>
              {{ item.server?? '-' }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>{{ $t('in.port') }}</v-col>
            <v-col>
              {{ item.server_port?? '-' }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>{{ $t('objects.tls') }}</v-col>
            <v-col>
              {{ Object.hasOwn(item,'tls') ? $t(item.tls?.enabled ? 'enable' : 'disable') : '-'  }}
            </v-col>
          </v-row>
        </v-card-text>
        <v-divider></v-divider>
        <v-card-actions class="np-resource-card__actions">
          <v-btn class="np-card-action" variant="text" @click="showDnsModal(index)">
            <v-icon icon="mdi-file-edit" /><span>{{ $t('actions.edit') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
          <v-btn class="np-card-action" variant="text" color="warning" @click="delDnsOverlay[index] = true">
            <v-icon icon="mdi-file-remove" /><span>{{ $t('actions.del') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
          </v-btn>
          <v-overlay
            v-model="delDnsOverlay[index]"
            contained
            class="align-center justify-center"
          >
            <v-card :title="$t('actions.del')" rounded="lg">
              <v-divider></v-divider>
              <v-card-text>{{ $t('confirm') }}</v-card-text>
              <v-card-actions>
                <v-btn color="error" variant="outlined" @click="delDns(index)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delDnsOverlay[index] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-overlay>
        </v-card-actions>
      </v-card>
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import { computed, defineAsyncComponent, ref, onBeforeMount } from 'vue'
import { Config } from '@/types/config'
import { FindDiff } from '@/plugins/utils'
import PageHero from '@/components/PageHero.vue'
import EmptyState from '@/components/EmptyState.vue'

const DnsVue = defineAsyncComponent(() => import('@/layouts/modals/Dns.vue'))

const oldConfig = ref(<any>{})
const loading = ref(false)

const appConfig = computed((): Config => {
  return <Config> Data().config
})

onBeforeMount( async () => {
  // fix old configs
  if (!appConfig.value.dns) appConfig.value.dns = { servers: [], rules: [] }
  if (!appConfig.value.dns.servers) appConfig.value.dns.servers = []
  if (!appConfig.value.dns.rules) appConfig.value.dns.rules = []

  loading.value = true
  while (Data().lastLoad == 0) {
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  oldConfig.value = JSON.parse(JSON.stringify(Data().config))
  loading.value = false
})

const tsTags = computed((): string[] => {
  return Data().endpoints?.filter((e:any) => e.type == "tailscale").map((e:any) => e.tag)
})

const rslvdTags = computed((): string[] => {
  return Data().services?.filter((e:any) => e.type == "resolved").map((e:any) => e.tag)
})

const stateChange = computed(() => {
  return FindDiff.deepCompare(appConfig.value.dns,oldConfig.value.dns)
})

const saveConfig = async () => {
  loading.value = true
  const success = await Data().save("config", "set", appConfig.value)
  if (success) {
    oldConfig.value = JSON.parse(JSON.stringify(Data().config))
  }
  loading.value = false
}

const dns = computed((): any => {
  return appConfig.value.dns
})

const dnsServerTags = computed((): string[] => {
  return dns.value?.servers?.filter((s:any) => s.tag && s.tag != "")?.map((s:any) => s.tag) ?? []
})

const finalDns = computed({
  get() { return dns.value?.final?? '' },
  set(v:string) { dns.value.final = v.length>0 ? v : undefined }
})


let delDnsOverlay = ref(new Array<boolean>)

const dnsModal = ref({
  visible: false,
  index: -1,
  data: "",
})

const showDnsModal = (index: number) => {
  dnsModal.value.index = index
  dnsModal.value.data = index == -1 ? '' : JSON.stringify(dns.value.servers[index])
  dnsModal.value.visible = true
}

const closeDnsModal = () => {
  dnsModal.value.visible = false
}

const saveDnsModal = (data:any) => {
  // New or Edit
  if (dnsModal.value.index == -1) {
    dns.value.servers.push(data)
  } else {
    dns.value.servers[dnsModal.value.index] = data
  }
  dnsModal.value.visible = false
}

const delDns = (index: number) => {
  dns.value.servers.splice(index,1)
  delDnsOverlay.value[index] = false
}

</script>

<style scoped>
.dns-section {
  margin: 0 0 18px;
  border: 1px solid var(--np-border);
  border-radius: 26px;
  background: var(--np-surface-muted);
}

.dns-section.np-section-card { padding: 12px; }
.dns-section__heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.dns-section__heading h2 { margin: 0; font-size: 18px; letter-spacing: -0.02em; }
.dns-section__heading p { margin: 5px 0 0; color: var(--np-text-muted); font-size: 13px; }

.dns-section :deep(.v-checkbox .v-label),
.dns-section :deep(.v-switch .v-label) {
  white-space: normal;
  line-height: 1.35;
}

@media (max-width: 599px) {
  .dns-section { border-radius: 22px; }
  .dns-section__heading { align-items: flex-start; }
  .dns-section__heading .v-btn { min-width: 42px; padding-inline: 10px; }
  .dns-section__heading .v-btn :deep(.v-btn__content) { font-size: 0; }
  .dns-section__heading .v-btn :deep(.v-icon) { margin: 0; font-size: 20px; }
}
</style>
