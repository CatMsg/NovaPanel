<template>
  <OutboundVue
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :data="modal.data"
    :tags="outboundTags"
    @close="closeModal"
  />
  <OutboundBulk
    v-model="bulkModal.visible"
    :visible="bulkModal.visible"
    :outboundTags="outboundTags"
    @close="closeBulkModal"
  />
  <Stats
    v-model="stats.visible"
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="closeStats"
  />
  <StrategyBuilder
    :visible="strategyModal"
    :member-tags="outboundTags"
    :loading="strategyLoading"
    @close="strategyModal = false"
    @save="saveStrategy"
  />
  <ProxyChainBuilder
    :visible="chainModal"
    :outbounds="outbounds"
    :all-tags="outboundTags"
    :loading="chainLoading"
    @close="chainModal = false"
    @save="saveChain"
  />
  <v-card class="resource-hero resource-hero--outbounds" rounded="xl" variant="flat">
    <div class="resource-hero__topline">
      <span class="resource-hero__badge">{{ $t('pages.outbounds') }}</span>
    </div>
    <v-row class="resource-hero__content" align="center">
      <v-col cols="12" lg="7">
        <div class="resource-hero__title-row">
          <div class="resource-hero__icon">
            <v-icon icon="mdi-cloud-upload-outline" size="32" />
          </div>
          <div>
            <h1 class="resource-hero__title">{{ $t('pages.outbounds') }}</h1>
            <p class="resource-hero__subtitle">
              统一管理出口节点、批量新增和延迟测试，常用操作集中在同一处。
            </p>
          </div>
        </div>
        <div class="resource-hero__meta">
          <span>在线 {{ onlines.length }}</span>
          <span>•</span>
          <span>总数 {{ outbounds.length }}</span>
          <span>•</span>
          <span>已测试 {{ Object.keys(checkResults).length }}</span>
        </div>
      </v-col>
      <v-col cols="12" lg="5" class="resource-hero__actions">
        <v-btn color="primary" variant="tonal" size="large" @click="strategyModal = true">
          <v-icon icon="mdi-call-split" start />策略组
        </v-btn>
        <v-btn v-if="outbounds.length > 0" color="primary" size="large" @click="showModal(0)">
          <v-icon icon="mdi-plus" start />
          {{ $t('actions.add') }}
        </v-btn>
        <v-menu location="bottom end">
          <template #activator="{ props }">
            <v-btn v-bind="props" variant="outlined" size="large" append-icon="mdi-chevron-down">更多</v-btn>
          </template>
          <v-list density="compact" nav min-width="210">
            <v-list-item prepend-icon="mdi-link-variant" title="代理链" :disabled="outbounds.length === 0" @click="chainModal = true" />
            <v-list-item prepend-icon="mdi-playlist-plus" :title="$t('actions.addbulk')" @click="showBulkModal" />
            <v-list-item prepend-icon="mdi-speedometer" :title="$t('actions.testAll') || 'Test all'" :disabled="testingAll || outbounds.length === 0" @click="checkAllOutbounds" />
          </v-list>
        </v-menu>
      </v-col>
    </v-row>
  </v-card>

  <section v-if="failoverStatuses.length > 0" class="failover-panel">
    <div class="failover-panel__heading">
      <div><span>AUTOMATIC FAILOVER</span><h2>有序故障回退</h2></div>
      <v-btn icon="mdi-refresh" variant="text" :loading="failoverLoading" aria-label="刷新回退状态" @click="loadFailoverStatus" />
    </div>
    <div class="failover-grid">
      <article v-for="status in failoverStatuses" :key="status.policy.tag" class="failover-card">
        <div class="failover-card__top">
          <div><strong>{{ status.policy.tag }}</strong><small>{{ status.policy.members.join(' → ') }}</small></div>
          <v-chip size="small" :color="status.error ? 'warning' : 'success'" variant="tonal">{{ status.error ? '需检查' : '运行中' }}</v-chip>
        </div>
        <div class="failover-card__state">
          <span>当前出口</span><strong>{{ status.current || '等待首次探测' }}</strong>
          <span>候选出口</span><strong>{{ status.candidate || '-' }}</strong>
        </div>
        <p v-if="status.error">{{ status.error }}</p>
        <div class="failover-card__footer">
          <small>{{ status.lastChecked ? `最近探测 ${formatStatusTime(status.lastChecked)}` : '尚未探测' }}</small>
          <v-btn size="small" variant="text" color="warning" @click="deleteFailover(status.policy.tag)">停止回退</v-btn>
        </div>
      </article>
    </div>
  </section>

  <v-row class="resource-grid">
    <v-col v-if="outbounds.length === 0" cols="12">
      <EmptyState
        icon="mdi-cloud-upload-outline"
        title="暂无出站"
        description="添加单个出站或使用批量新增，随后可直接执行延迟测试。"
        :action="$t('actions.add')"
        @action="showModal(0)"
      />
    </v-col>
    <v-col cols="12" sm="6" md="4" lg="3" v-for="(item, index) in <any[]>outbounds" :key="item.tag">
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
          <v-row>
            <v-col>{{ $t('online') }}</v-col>
            <v-col>
              <template v-if="onlines.includes(item.tag)">
                <v-chip density="comfortable" size="small" color="success" variant="flat">{{ $t('online') }}</v-chip>
              </template>
              <template v-else>-</template>
            </v-col>
          </v-row>
          <v-row>
            <v-col>{{ $t('out.delay') }}</v-col>
            <v-col>
              <v-progress-circular
                v-if="checkResults[item.tag]?.loading"
                indeterminate
                size="20"
              />
              <v-icon
                icon="mdi-speedometer"
                v-else
                @click="checkOutbound(item.tag)"
              >
                <v-tooltip activator="parent" location="top" :text="$t('actions.test')"></v-tooltip>
              </v-icon>
              <template v-if="checkResults[item.tag]?.loading == false">
                <template v-if="checkResults[item.tag]">
                  <v-chip
                    v-if="checkResults[item.tag].success"
                    density="compact"
                    size="small"
                    color="success"
                    variant="flat"
                  >
                    {{ checkResults[item.tag].data?.Delay + $t('date.ms') }}
                  </v-chip>
                  <v-tooltip v-else location="top" :text="checkResults[item.tag].errorMessage || $t('failed')">
                    <template v-slot:activator="{ props }">
                      <v-icon v-bind="props" size="small" color="error" icon="mdi-close-circle" />
                    </template>
                  </v-tooltip>
                </template>
              </template>
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
                <v-btn color="error" variant="outlined" @click="delOutbound(item.tag)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delOverlay[index] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-overlay>
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
import HttpUtils from '@/plugins/httputil'
import { Outbound } from '@/types/outbounds'
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import EmptyState from '@/components/EmptyState.vue'

const OutboundVue = defineAsyncComponent(() => import('@/layouts/modals/Outbound.vue'))
const OutboundBulk = defineAsyncComponent(() => import('@/layouts/modals/OutboundBulk.vue'))
const Stats = defineAsyncComponent(() => import('@/layouts/modals/Stats.vue'))
const StrategyBuilder = defineAsyncComponent(() => import('@/layouts/modals/StrategyBuilder.vue'))
const ProxyChainBuilder = defineAsyncComponent(() => import('@/layouts/modals/ProxyChainBuilder.vue'))

interface CheckResult {
  loading?: boolean
  success: boolean
  data?: { OK?: boolean; Delay?: number; Error?: string } | null
  errorMessage?: string
}

const checkResults = ref<Record<string, CheckResult>>({})

const checkOutbound = async (tag: string) => {
  checkResults.value = { ...checkResults.value, [tag]: { loading: true, success: false } }
  const msg = await HttpUtils.get('api/checkOutbound', { tag })
  const success = msg.success && msg.obj?.OK
  const errorMessage = success ? undefined : (msg.obj?.Error ?? msg.msg ?? '')
  checkResults.value = {
    ...checkResults.value,
    [tag]: { loading: false, success, data: msg.obj ?? null, errorMessage }
  }
}

const testingAll = ref(false)

const checkAllOutbounds = async () => {
  const list = outbounds.value
  if (list.length === 0) return
  testingAll.value = true
  try {
    await Promise.all(list.map((o) => checkOutbound(o.tag)))
  } finally {
    testingAll.value = false
  }
}

const outbounds = computed((): Outbound[] => {
  return <Outbound[]> Data().outbounds
})

const strategyModal = ref(false)
const strategyLoading = ref(false)
const chainModal = ref(false)
const chainLoading = ref(false)
const failoverStatuses = ref<any[]>([])
const failoverLoading = ref(false)

async function saveStrategy(payload: any) {
  if (Data().checkTag('outbound', 0, payload.outbound.tag)) return
  strategyLoading.value = true
  try {
    const saved = await Data().save('outbounds', 'new', payload.outbound)
    if (!saved) return
    if (payload.policy) {
      const response = await HttpUtils.post('api/failoverSave', payload.policy)
      if (!response.success) {
        await Data().save('outbounds', 'del', payload.outbound.tag)
        return
      }
    }
    strategyModal.value = false
    await loadFailoverStatus()
  } finally {
    strategyLoading.value = false
  }
}

async function saveChain(payload: { target: string; detour: string }) {
  const source = outbounds.value.find((item) => item.tag === payload.target)
  if (!source) return
  const updated: any = JSON.parse(JSON.stringify(source))
  if (payload.detour) updated.detour = payload.detour
  else delete updated.detour
  chainLoading.value = true
  try {
    const saved = await Data().save('outbounds', 'edit', updated)
    if (saved) chainModal.value = false
  } finally {
    chainLoading.value = false
  }
}

async function loadFailoverStatus() {
  failoverLoading.value = true
  try {
    const response = await HttpUtils.get('api/failover-status')
    if (response.success) failoverStatuses.value = response.obj ?? []
  } finally {
    failoverLoading.value = false
  }
}

async function deleteFailover(tag: string) {
  const response = await HttpUtils.post('api/failoverDelete', { tag })
  if (response.success) await loadFailoverStatus()
}

function formatStatusTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

onMounted(loadFailoverStatus)

const outboundTags = computed((): string[] => {
  return [...Data().outbounds?.map((o:Outbound) => o.tag), ...Data().endpoints?.filter((e:any) => e.type != "masque").map((e:any) => e.tag)]
})

const onlines = computed(() => {
  return Data().onlines.outbound?? []
})

const modal = ref({
  visible: false,
  id: 0,
  data: "",
})

let delOverlay = ref(new Array<boolean>)

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.data = id == 0 ? '' : JSON.stringify(outbounds.value.findLast(o => o.id == id))
  modal.value.visible = true
}

const closeModal = () => {
  modal.value.visible = false
}

const bulkModal = ref({ visible: false })

const showBulkModal = () => {
  bulkModal.value.visible = true
}

const closeBulkModal = () => {
  bulkModal.value.visible = false
}

const stats = ref({
  visible: false,
  resource: "outbound",
  tag: "",
})

const delOutbound = async (tag: string) => {
  const index = outbounds.value.findIndex(i => i.tag == tag)
  const success = await Data().save("outbounds", "del", tag)
  if (success) {
    delOverlay.value[index] = false
    await HttpUtils.post('api/failoverDelete', { tag })
    await loadFailoverStatus()
  }
}

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
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.resource-card {
  overflow: hidden;
  min-height: 100%;
}

.resource-grid {
  margin-top: 0;
}

.failover-panel {
  margin-bottom: 18px;
  padding: 18px;
  border: 1px solid var(--np-border);
  border-radius: 26px;
  background: var(--np-surface-muted);
}

.failover-panel__heading,
.failover-card__top,
.failover-card__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.failover-panel__heading span {
  color: var(--np-accent);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: .12em;
}

.failover-panel__heading h2 {
  margin: 2px 0 0;
  font-size: 19px;
}

.failover-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 12px;
  margin-top: 14px;
}

.failover-card {
  padding: 16px;
  border: 1px solid var(--np-border);
  border-radius: 20px;
  background: var(--np-surface);
}

.failover-card__top div {
  min-width: 0;
}

.failover-card__top strong,
.failover-card__top small {
  display: block;
}

.failover-card__top small,
.failover-card__footer small,
.failover-card p {
  color: var(--np-text-muted);
}

.failover-card__top small {
  margin-top: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.failover-card__state {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 7px 14px;
  margin: 15px 0;
  font-size: 13px;
}

.failover-card__state span {
  color: var(--np-text-muted);
}

.failover-card p {
  margin: 0 0 10px;
  font-size: 12px;
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
