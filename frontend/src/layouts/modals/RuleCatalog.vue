<template>
  <v-dialog :model-value="visible" max-width="1040" scrollable @update:model-value="onDialogUpdate">
    <v-card class="catalog-dialog" rounded="xl">
      <v-card-title class="catalog-dialog__title">
        <div>
          <span class="catalog-dialog__eyebrow">RULE CATALOG</span>
          <h2>规则目录</h2>
          <p>选择规则集后按需指定流量出口或 DNS；默认只添加规则集，不改变现有路由。</p>
        </div>
        <v-btn icon="mdi-close" variant="text" aria-label="关闭" @click="$emit('close')" />
      </v-card-title>
      <v-divider />
      <v-card-text class="catalog-dialog__body">
        <v-btn-toggle v-model="source" mandatory color="primary" variant="tonal" density="comfortable" class="catalog-source-toggle">
          <v-btn value="popular"><v-icon icon="mdi-star-four-points-outline" start />常用规则</v-btn>
          <v-btn value="remote"><v-icon icon="mdi-cloud-search-outline" start />远端规则库</v-btn>
        </v-btn-toggle>

        <v-text-field
          v-model="search"
          prepend-inner-icon="mdi-magnify"
          :label="source === 'remote' ? '搜索远端规则，例如 netflix、openai、jp' : '搜索常用分类'"
          clearable
          hide-details
          class="catalog-search"
        />

        <template v-if="source === 'popular'">
          <div class="catalog-grid">
            <button
              v-for="item in filteredCatalog"
              :key="item.id"
              type="button"
              class="catalog-item"
              :class="{ 'catalog-item--active': selected?.id === item.id }"
              @click="selectItem(item)"
            >
              <v-icon :icon="item.icon" size="24" />
              <span><strong>{{ item.name }}</strong><small>{{ item.description }}</small></span>
              <v-icon v-if="selected?.id === item.id" icon="mdi-check-circle" color="primary" />
            </button>
          </div>
          <div v-if="filteredCatalog.length === 0" class="catalog-empty">
            <v-icon icon="mdi-magnify-close" size="30" />
            <strong>没有匹配的常用规则</strong>
            <span>切换到远端规则库可搜索完整目录。</span>
          </div>
        </template>

        <template v-else>
          <div class="catalog-remote-toolbar">
            <v-chip-group v-model="remoteKind" mandatory selected-class="text-primary">
              <v-chip value="all" filter>全部</v-chip>
              <v-chip value="geosite" filter>GeoSite</v-chip>
              <v-chip value="geoip" filter>GeoIP</v-chip>
            </v-chip-group>
            <span v-if="remoteTotal > 0">找到 {{ remoteTotal }} 项</span>
          </div>
          <v-progress-linear v-if="remoteLoading" indeterminate color="primary" rounded class="mb-3" />
          <div v-if="remoteItems.length" class="catalog-grid catalog-grid--remote">
            <button
              v-for="item in remoteItems"
              :key="item.id"
              type="button"
              class="catalog-item catalog-item--remote"
              :class="{ 'catalog-item--active': selected?.id === item.id }"
              @click="selectItem(item)"
            >
              <v-icon :icon="item.icon" size="23" />
              <span><strong>{{ item.name }}</strong><small>{{ item.description }}</small></span>
              <v-icon v-if="selected?.id === item.id" icon="mdi-check-circle" color="primary" />
            </button>
          </div>
          <div v-else-if="!remoteLoading" class="catalog-empty">
            <v-icon :icon="search.trim() ? 'mdi-database-search-outline' : 'mdi-text-search'" size="30" />
            <strong>{{ search.trim() ? '远端规则库中没有匹配项' : '输入关键词搜索完整规则库' }}</strong>
            <span>覆盖 SagerNet sing-geosite 与 sing-geoip 的全部 SRS 规则。</span>
          </div>
          <v-pagination
            v-if="remotePages > 1"
            v-model="remotePage"
            :length="remotePages"
            :total-visible="7"
            density="comfortable"
            class="catalog-pagination"
          />
        </template>

        <v-divider class="my-5" />
        <div v-if="selected" class="catalog-selection">
          <v-icon :icon="selected.icon" color="primary" />
          <div><span>当前选择</span><strong>{{ selected.name }}</strong></div>
          <code v-if="selected.assets[0]">{{ selected.assets[0].tag }}</code>
        </div>
        <v-alert v-else type="info" variant="tonal" density="compact" class="mb-4">请先选择一项规则。</v-alert>
        <div class="catalog-purpose">
          <v-select v-model="action" :items="actionItems" label="流量处理" hide-details />
          <v-select
            v-model="dnsServer"
            :items="dnsServerTags"
            label="DNS 解析（可选）"
            clearable
            hide-details
            no-data-text="暂无 DNS 服务器"
          />
        </div>
        <v-select
          v-if="action === 'route'"
          v-model="outbound"
          :items="outboundTags"
          label="目标出口"
          class="mt-3"
          hide-details
        />
        <v-expansion-panels class="catalog-advanced mt-3" variant="accordion">
          <v-expansion-panel>
            <v-expansion-panel-title>高级选项</v-expansion-panel-title>
            <v-expansion-panel-text>
              <v-row dense>
                <v-col v-if="action !== 'none'" cols="12" md="6"><v-select v-model="inbound" :items="inboundTags" label="仅限入站" clearable hide-details /></v-col>
                <v-col v-if="action !== 'none'" cols="12" md="6"><v-select v-model="user" :items="clients" label="仅限用户" clearable hide-details /></v-col>
                <v-col v-if="selected?.assets.length" cols="12"><v-select v-model="downloadDetour" :items="outboundTags" label="规则集下载出口" clearable hide-details /></v-col>
              </v-row>
            </v-expansion-panel-text>
          </v-expansion-panel>
        </v-expansion-panels>
        <v-alert class="mt-4" type="info" variant="tonal">
          远端目录来自 SagerNet 官方 sing-box 规则集分支；保存时会下载并校验，失败则自动回滚。
        </v-alert>
      </v-card-text>
      <v-card-actions class="catalog-dialog__actions">
        <v-btn variant="text" @click="$emit('close')">取消</v-btn>
        <v-btn color="primary" :loading="loading" :disabled="!canApply" @click="apply">{{ applyLabel }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ruleCatalog, type RuleCatalogItem } from '@/data/ruleCatalog'
import HttpUtils from '@/plugins/httputil'

interface RemoteCatalogEntry {
  kind: 'geosite' | 'geoip'
  name: string
  tag: string
  url: string
}

const props = defineProps<{
  visible: boolean
  outboundTags: string[]
  inboundTags: string[]
  clients: string[]
  dnsServerTags: string[]
  loading?: boolean
}>()

const emit = defineEmits(['close', 'apply'])
const source = ref<'popular' | 'remote'>('popular')
const search = ref('')
const selected = ref<RuleCatalogItem | null>(ruleCatalog[0])
const action = ref<'none' | 'route' | 'reject'>('none')
const outbound = ref('')
const dnsServer = ref('')
const inbound = ref('')
const user = ref('')
const downloadDetour = ref('')
const remoteKind = ref<'all' | 'geosite' | 'geoip'>('all')
const remoteItems = ref<RuleCatalogItem[]>([])
const remoteLoading = ref(false)
const remoteTotal = ref(0)
const remotePage = ref(1)
const remotePageSize = 48
let searchTimer: ReturnType<typeof setTimeout> | undefined
let searchGeneration = 0

function defaultOutbound(tags = props.outboundTags) {
  if (tags.includes('direct')) return 'direct'
  return tags.length === 1 ? tags[0] : ''
}

const filteredCatalog = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return ruleCatalog
  return ruleCatalog.filter((item) => `${item.name} ${item.description} ${item.assets.map((asset) => asset.tag).join(' ')}`.toLowerCase().includes(keyword))
})
const remotePages = computed(() => Math.ceil(remoteTotal.value / remotePageSize))
const actionItems = [
  { title: '保持默认（不添加路由）', value: 'none' },
  { title: '指定出口', value: 'route' },
  { title: '拒绝访问', value: 'reject' },
]
const canApply = computed(() => Boolean(
  selected.value &&
  (selected.value.assets.length > 0 || action.value !== 'none') &&
  (action.value !== 'route' || outbound.value)
))
const applyLabel = computed(() => {
  if (action.value === 'none' && !dnsServer.value) return '添加规则集'
  if (action.value === 'none') return '添加并配置 DNS'
  return '创建并保存'
})

function selectItem(item: RuleCatalogItem) {
  selected.value = item
  action.value = item.suggestedAction ?? 'none'
}

function toCatalogItem(entry: RemoteCatalogEntry): RuleCatalogItem {
  const label = entry.kind === 'geosite' ? 'GeoSite 域名规则' : 'GeoIP 地址规则'
  return {
    id: `remote-${entry.kind}-${entry.name}`,
    name: entry.name,
    description: `${label} · SagerNet/${entry.kind === 'geosite' ? 'sing-geosite' : 'sing-geoip'}`,
    icon: entry.kind === 'geosite' ? 'mdi-web' : 'mdi-ip-network-outline',
    assets: [{ tag: entry.tag, url: entry.url }],
  }
}

async function searchRemoteCatalog() {
  const keyword = search.value.trim()
  if (source.value !== 'remote' || !keyword) {
    remoteItems.value = []
    remoteTotal.value = 0
    remoteLoading.value = false
    return
  }
  const generation = ++searchGeneration
  remoteLoading.value = true
  try {
    const response = await HttpUtils.get('api/rule-catalog', {
      q: keyword,
      kind: remoteKind.value,
      page: remotePage.value,
      pageSize: remotePageSize,
    })
    if (generation !== searchGeneration) return
    if (response.success) {
      remoteItems.value = (response.obj?.items ?? []).map(toCatalogItem)
      remoteTotal.value = response.obj?.total ?? 0
    } else {
      remoteItems.value = []
      remoteTotal.value = 0
    }
  } finally {
    if (generation === searchGeneration) remoteLoading.value = false
  }
}

function scheduleRemoteSearch(resetPage = false) {
  if (resetPage) remotePage.value = 1
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(searchRemoteCatalog, 280)
}

watch(search, () => {
  if (source.value === 'remote') {
    selected.value = null
    scheduleRemoteSearch(true)
  }
})
watch(remoteKind, () => {
  selected.value = null
  scheduleRemoteSearch(true)
})
watch(remotePage, () => scheduleRemoteSearch())
watch(source, (value) => {
  selected.value = value === 'popular' ? ruleCatalog[0] : null
  if (value === 'remote') scheduleRemoteSearch(true)
})
watch(() => props.visible, (value) => {
  if (!value) return
  action.value = selected.value?.suggestedAction ?? 'none'
  dnsServer.value = ''
  inbound.value = ''
  user.value = ''
  downloadDetour.value = ''
  if (!props.outboundTags.includes(outbound.value)) outbound.value = defaultOutbound()
  if (source.value === 'remote') scheduleRemoteSearch()
})
watch(() => props.outboundTags, (tags) => {
  if (!tags.includes(outbound.value)) outbound.value = defaultOutbound(tags)
}, { immediate: true })
onBeforeUnmount(() => {
  searchGeneration++
  if (searchTimer) clearTimeout(searchTimer)
})

function apply() {
  if (!selected.value || !canApply.value) return
  emit('apply', {
    item: selected.value,
    action: action.value,
    outbound: outbound.value,
    dnsServer: dnsServer.value,
    inbound: inbound.value,
    user: user.value,
    downloadDetour: downloadDetour.value,
  })
}

function onDialogUpdate(value: boolean) {
  if (!value) emit('close')
}
</script>

<style scoped>
.catalog-dialog { background: var(--np-surface); }
.catalog-dialog__title { display: flex; justify-content: space-between; gap: 20px; padding: 24px; }
.catalog-dialog__title h2 { margin: 2px 0 0; font-size: 24px; }
.catalog-dialog__title p { margin: 6px 0 0; color: var(--np-text-muted); font-size: 13px; white-space: normal; }
.catalog-dialog__eyebrow { color: var(--np-accent); font-size: 11px; font-weight: 700; letter-spacing: .12em; }
.catalog-dialog__body { padding: 22px 24px; }
.catalog-source-toggle { margin-bottom: 14px; }
.catalog-search { margin-bottom: 16px; }
.catalog-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
.catalog-grid--remote { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.catalog-item { display: grid; grid-template-columns: 30px minmax(0, 1fr) 22px; align-items: center; gap: 10px; width: 100%; padding: 14px; border: 1px solid var(--np-border); border-radius: 18px; color: inherit; background: var(--np-surface-muted); text-align: left; cursor: pointer; transition: transform 140ms cubic-bezier(.23, 1, .32, 1), border-color 140ms ease, background-color 140ms ease; }
.catalog-item:active { transform: scale(.985); }
.catalog-item--active { border-color: rgba(10, 132, 255, .6); background: rgba(10, 132, 255, .1); }
.catalog-item span { min-width: 0; }
.catalog-item strong, .catalog-item small { display: block; }
.catalog-item strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.catalog-item small { margin-top: 3px; color: var(--np-text-muted); line-height: 1.35; }
.catalog-remote-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 44px; margin-bottom: 8px; }
.catalog-remote-toolbar > span { color: var(--np-text-muted); font-size: 12px; white-space: nowrap; }
.catalog-empty { display: grid; justify-items: center; gap: 5px; padding: 40px 16px; border: 1px dashed var(--np-border); border-radius: 20px; color: var(--np-text-muted); text-align: center; }
.catalog-empty strong { color: var(--np-text); }
.catalog-empty span { font-size: 12px; }
.catalog-pagination { margin-top: 16px; }
.catalog-selection { display: flex; align-items: center; gap: 10px; margin-bottom: 16px; padding: 11px 14px; border: 1px solid rgba(10, 132, 255, .25); border-radius: 16px; background: rgba(10, 132, 255, .07); }
.catalog-selection div { display: grid; min-width: 0; }
.catalog-selection span { color: var(--np-text-muted); font-size: 11px; }
.catalog-selection code { margin-left: auto; overflow: hidden; color: var(--np-text-muted); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.catalog-purpose { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.catalog-advanced { border: 1px solid var(--np-border); border-radius: 16px; overflow: hidden; }
.catalog-advanced :deep(.v-expansion-panel) { background: var(--np-surface-muted); }
.catalog-dialog__actions { justify-content: flex-end; padding: 14px 24px 22px; }
@media (hover: hover) and (pointer: fine) { .catalog-item:hover { border-color: rgba(10, 132, 255, .35); } }
@media (max-width: 760px) {
  .catalog-dialog__title, .catalog-dialog__body { padding-inline: 16px; }
  .catalog-dialog__title p { max-width: 34ch; }
  .catalog-source-toggle { width: 100%; }
  .catalog-source-toggle :deep(.v-btn) { flex: 1; }
  .catalog-grid, .catalog-grid--remote { grid-template-columns: 1fr; }
  .catalog-purpose { grid-template-columns: 1fr; }
  .catalog-remote-toolbar { align-items: flex-start; flex-direction: column; }
  .catalog-selection { align-items: flex-start; }
  .catalog-selection code { max-width: 45%; }
}
@media (prefers-reduced-motion: reduce) { .catalog-item { transition: none; } }
</style>
