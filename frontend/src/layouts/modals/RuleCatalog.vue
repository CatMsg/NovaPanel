<template>
  <v-dialog :model-value="visible" max-width="980" scrollable @update:model-value="onDialogUpdate">
    <v-card class="catalog-dialog" rounded="xl">
      <v-card-title class="catalog-dialog__title">
        <div>
          <span class="catalog-dialog__eyebrow">RULE CATALOG</span>
          <h2>规则目录</h2>
          <p>选择分类与出口，NovaPanel 会同时创建 SRS 规则集和路由规则。</p>
        </div>
        <v-btn icon="mdi-close" variant="text" aria-label="关闭" @click="$emit('close')" />
      </v-card-title>
      <v-divider />
      <v-card-text class="catalog-dialog__body">
        <v-text-field
          v-model="search"
          prepend-inner-icon="mdi-magnify"
          label="搜索分类"
          clearable
          hide-details
          class="mb-4"
        />
        <div class="catalog-grid">
          <button
            v-for="item in filteredCatalog"
            :key="item.id"
            type="button"
            class="catalog-item"
            :class="{ 'catalog-item--active': selectedId === item.id }"
            @click="selectItem(item)"
          >
            <v-icon :icon="item.icon" size="24" />
            <span><strong>{{ item.name }}</strong><small>{{ item.description }}</small></span>
            <v-icon v-if="selectedId === item.id" icon="mdi-check-circle" color="primary" />
          </button>
        </div>

        <v-divider class="my-5" />
        <v-row dense>
          <v-col cols="12" md="4">
            <v-select v-model="action" :items="actionItems" label="动作" hide-details />
          </v-col>
          <v-col v-if="action === 'route'" cols="12" md="8">
            <v-select v-model="outbound" :items="outboundTags" label="目标出口" hide-details />
          </v-col>
          <v-col cols="12" md="6">
            <v-select v-model="inbound" :items="inboundTags" label="仅限入站（可选）" clearable hide-details />
          </v-col>
          <v-col cols="12" md="6">
            <v-select v-model="user" :items="clients" label="仅限用户（可选）" clearable hide-details />
          </v-col>
          <v-col v-if="selected?.assets.length" cols="12" md="6">
            <v-select v-model="downloadDetour" :items="outboundTags" label="规则集下载出口（可选）" clearable hide-details />
          </v-col>
        </v-row>
        <v-alert class="mt-4" type="info" variant="tonal">
          远程分类来自 SagerNet 的 sing-box 规则集分支；保存时会先下载并校验，失败则自动回滚。
        </v-alert>
      </v-card-text>
      <v-card-actions class="catalog-dialog__actions">
        <v-btn variant="text" @click="$emit('close')">取消</v-btn>
        <v-btn color="primary" :loading="loading" :disabled="!canApply" @click="apply">创建并保存</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'
import { ruleCatalog, type RuleCatalogItem } from '@/data/ruleCatalog'

const props = defineProps<{
  visible: boolean
  outboundTags: string[]
  inboundTags: string[]
  clients: string[]
  loading?: boolean
}>()

const emit = defineEmits(['close', 'apply'])
const search = ref('')
const selectedId = ref(ruleCatalog[0].id)
const action = ref<'route' | 'reject'>('route')
const outbound = ref('')
const inbound = ref('')
const user = ref('')
const downloadDetour = ref('')

const selected = computed(() => ruleCatalog.find((item) => item.id === selectedId.value))
const filteredCatalog = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return ruleCatalog
  return ruleCatalog.filter((item) => `${item.name} ${item.description}`.toLowerCase().includes(keyword))
})
const actionItems = [
  { title: '路由到指定出口', value: 'route' },
  { title: '拒绝连接', value: 'reject' },
]
const canApply = computed(() => Boolean(selected.value && (action.value !== 'route' || outbound.value)))

function selectItem(item: RuleCatalogItem) {
  selectedId.value = item.id
  action.value = item.suggestedAction ?? 'route'
}

function apply() {
  if (!selected.value || !canApply.value) return
  emit('apply', {
    item: selected.value,
    action: action.value,
    outbound: outbound.value,
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
.catalog-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
.catalog-item { display: grid; grid-template-columns: 30px minmax(0, 1fr) 22px; align-items: center; gap: 10px; width: 100%; padding: 14px; border: 1px solid var(--np-border); border-radius: 18px; color: inherit; background: var(--np-surface-muted); text-align: left; cursor: pointer; transition: transform 140ms cubic-bezier(.23, 1, .32, 1), border-color 140ms ease; }
.catalog-item:active { transform: scale(.985); }
.catalog-item--active { border-color: rgba(10, 132, 255, .6); background: rgba(10, 132, 255, .1); }
.catalog-item span { min-width: 0; }
.catalog-item strong, .catalog-item small { display: block; }
.catalog-item small { margin-top: 3px; color: var(--np-text-muted); line-height: 1.35; }
.catalog-dialog__actions { justify-content: flex-end; padding: 14px 24px 22px; }
@media (max-width: 760px) {
  .catalog-dialog__title, .catalog-dialog__body { padding-inline: 16px; }
  .catalog-grid { grid-template-columns: 1fr; }
}
@media (prefers-reduced-motion: reduce) { .catalog-item { transition: none; } }
</style>
