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
          <v-text-field
            v-model="dns.timeout" hide-details
            clearable @click:clear="delete dns.timeout"
            label="查询超时" placeholder="10s"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-checkbox v-model="dns.disable_cache" hide-details :label="$t('dns.disableCache')" />
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-checkbox v-model="dns.disable_expire" hide-details :label="$t('dns.disableExpire')" />
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-checkbox v-model="optimisticCache" hide-details label="乐观缓存" />
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
          <v-btn class="np-card-action" variant="text" color="warning" @click="openDnsDeleteDialog(item.tag)">
            <v-icon icon="mdi-file-remove" /><span>{{ $t('actions.del') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-col>
  </v-row>
  <v-dialog v-model="deleteDnsDialogVisible" max-width="560" width="calc(100vw - 32px)" scrollable>
    <v-card class="dns-delete-dialog" rounded="lg">
      <v-card-title>{{ $t('actions.del') }} {{ deleteDnsTargetTag }}</v-card-title>
      <v-divider></v-divider>
      <v-card-text class="dns-delete-dialog__content">
        <p>{{ $t('confirm') }}</p>
        <v-alert
          v-if="deleteDnsReferences.length > 0"
          class="mt-3"
          type="warning"
          variant="tonal"
          :title="$t('dns.references.deleteBlockedTitle')"
        >
          <p>{{ $t('dns.references.deleteBlockedText') }}</p>
          <ul class="dns-reference-list">
            <li v-for="(reference, referenceIndex) in deleteDnsReferences" :key="`${reference.kind}-${referenceIndex}`">
              {{ dnsReferenceLabel(reference) }}
            </li>
          </ul>
        </v-alert>
        <v-alert v-else class="mt-3" type="info" variant="tonal">
          {{ $t('dns.references.noDeleteDependencies') }}
        </v-alert>
      </v-card-text>
      <v-card-actions>
        <v-btn color="error" variant="outlined" :disabled="deleteDnsReferences.length > 0" @click="delDns(deleteDnsTargetTag)">
          {{ $t('yes') }}
        </v-btn>
        <v-btn color="success" variant="outlined" @click="deleteDnsDialogVisible = false">{{ $t('no') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import { computed, defineAsyncComponent, ref, onBeforeMount } from 'vue'
import { useI18n } from 'vue-i18n'
import { push } from 'notivue'
import { Config } from '@/types/config'
import { FindDiff } from '@/plugins/utils'
import { findDnsTagReferences, renameDnsTagReferences, type DnsExternalStore, type DnsTagReference } from '@/plugins/dnsReferences'
import PageHero from '@/components/PageHero.vue'
import EmptyState from '@/components/EmptyState.vue'

const DnsVue = defineAsyncComponent(() => import('@/layouts/modals/Dns.vue'))
const { t } = useI18n()

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

const externalStoreName: Record<DnsExternalStore, string> = {
  inbounds: 'inbound',
  outbounds: 'outbound',
  endpoints: 'endpoint',
  services: 'service',
}

const currentReferenceStores = () => ({
  inbounds: Data().inbounds ?? [],
  outbounds: Data().outbounds ?? [],
  endpoints: Data().endpoints ?? [],
  services: Data().services ?? [],
})

const dnsReferenceLabel = (reference: DnsTagReference): string => {
  switch (reference.kind) {
    case 'default-dns':
      return t('dns.references.defaultDns')
    case 'dns-rule':
      return t('dns.references.dnsRule', { index: reference.indexPath.join('.') })
    case 'route-resolve-rule':
      return t('dns.references.routeResolveRule', { index: reference.indexPath.join('.') })
    case 'config-resolver':
      return t('dns.references.configResolver', { path: reference.path })
    case 'external-resolver':
      return t('dns.references.externalResolver', {
        resource: t(`objects.${externalStoreName[reference.store]}`),
        tag: reference.tag,
        path: reference.path,
      })
  }
}

const deleteDnsDialogVisible = ref(false)
const deleteDnsTargetTag = ref('')
const deleteDnsReferences = computed(() => deleteDnsTargetTag.value
  ? findDnsTagReferences(appConfig.value, currentReferenceStores(), deleteDnsTargetTag.value)
  : [])

const openDnsDeleteDialog = (tag: string) => {
  deleteDnsTargetTag.value = tag
  deleteDnsDialogVisible.value = true
}

const finalDns = computed({
  get() { return dns.value?.final?? '' },
  set(v:string) { dns.value.final = v.length>0 ? v : undefined }
})

const optimisticCache = computed({
  get() {
    return typeof dns.value?.optimistic === 'object'
      ? dns.value.optimistic.enabled !== false
      : dns.value?.optimistic === true
  },
  set(enabled:boolean) {
    dns.value.optimistic = enabled ? true : undefined
  }
})


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
  const tag = typeof data?.tag === 'string' ? data.tag.trim() : ''
  if (!tag) {
    push.error({ title: t('dns.references.invalidTagTitle'), message: t('dns.references.invalidTagText') })
    return
  }

  const index = dnsModal.value.index
  const duplicate = dns.value.servers.some((server: any, serverIndex: number) =>
    serverIndex !== index && typeof server?.tag === 'string' && server.tag.trim() === tag
  )
  if (duplicate) {
    push.error({ title: t('dns.references.duplicateTagTitle'), message: t('dns.references.duplicateTagText') })
    return
  }

  const currentServer = index >= 0 ? dns.value.servers[index] : undefined
  if (index >= 0 && !currentServer) {
    push.error({ title: t('dns.references.renameBlockedTitle'), message: t('dns.references.staleEditText') })
    return
  }
  if (index >= 0) {
    let originalTag: unknown
    try {
      originalTag = JSON.parse(dnsModal.value.data)?.tag
    } catch {
      push.error({ title: t('dns.references.renameBlockedTitle'), message: t('dns.references.staleEditText') })
      return
    }
    if (currentServer.tag !== originalTag) {
      push.error({ title: t('dns.references.renameBlockedTitle'), message: t('dns.references.staleEditText') })
      return
    }
  }

  const oldTag = typeof currentServer?.tag === 'string' ? currentServer.tag : ''
  if (oldTag && oldTag !== tag) {
    const duplicateOldTag = dns.value.servers.some((server: any, serverIndex: number) =>
      serverIndex !== index && server?.tag === oldTag
    )
    if (duplicateOldTag) {
      push.error({ title: t('dns.references.renameBlockedTitle'), message: t('dns.references.ambiguousTagText') })
      return
    }

    const externalReferences = findDnsTagReferences(undefined, currentReferenceStores(), oldTag)
    if (externalReferences.length > 0) {
      const dependencies = externalReferences.map(dnsReferenceLabel).join('；')
      push.error({
        title: t('dns.references.renameBlockedTitle'),
        message: `${t('dns.references.renameBlockedText')} ${dependencies}`,
      })
      return
    }
  }

  const draft = JSON.parse(JSON.stringify(appConfig.value)) as Config
  const updatedServer = { ...data, tag }
  if (index === -1) {
    draft.dns.servers.push(updatedServer)
  } else {
    draft.dns.servers[index] = updatedServer
    if (oldTag && oldTag !== tag) renameDnsTagReferences(draft, oldTag, tag)
  }
  Data().config = draft
  dnsModal.value.visible = false
}

const delDns = (expectedTag: string) => {
  const matchingIndexes = dns.value.servers.flatMap((server: any, index: number) =>
    server?.tag === expectedTag ? [index] : []
  )
  if (matchingIndexes.length !== 1) {
    push.error({ title: t('dns.references.deleteBlockedTitle'), message: t('dns.references.staleDeleteText') })
    return
  }

  const references = findDnsTagReferences(appConfig.value, currentReferenceStores(), expectedTag)
  if (references.length > 0) {
    push.error({
      title: t('dns.references.deleteBlockedTitle'),
      message: `${t('dns.references.deleteBlockedText')} ${references.map(dnsReferenceLabel).join('；')}`,
    })
    return
  }

  const draft = JSON.parse(JSON.stringify(appConfig.value)) as Config
  const draftIndexes = draft.dns.servers.flatMap((server: any, serverIndex: number) =>
    server?.tag === expectedTag ? [serverIndex] : []
  )
  if (draftIndexes.length !== 1) return
  draft.dns.servers.splice(draftIndexes[0], 1)
  Data().config = draft
  deleteDnsDialogVisible.value = false
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
.dns-delete-dialog {
  --np-surface-strong: rgb(var(--v-theme-surface));
  max-height: calc(100dvh - 32px);
}
.dns-delete-dialog__content { max-height: min(60dvh, 480px); overflow-y: auto; }
.dns-reference-list { margin: 8px 0 0; padding-inline-start: 22px; overflow-wrap: anywhere; }

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
