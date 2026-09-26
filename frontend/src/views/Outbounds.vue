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
  <v-dialog v-model="deleteDialogOpen" max-width="420">
    <v-card rounded="xl" class="resource-delete-dialog">
      <v-card-title>{{ $t('actions.del') }} · {{ deleteTarget }}</v-card-title>
      <v-card-text>{{ $t('confirm') }}</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="deleteDialogOpen = false">{{ $t('no') }}</v-btn>
        <v-btn color="error" variant="tonal" :loading="deleteLoading" @click="confirmDelete">{{ $t('yes') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
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
            <p class="resource-hero__subtitle">{{ $t('ui.resource.outboundsSubtitle') }}</p>
          </div>
        </div>
        <div class="resource-hero__meta">
          <span>{{ $t('ui.resource.onlineCount', { count: onlines.length }) }}</span>
          <span>•</span>
          <span>{{ $t('itemCount', { count: outbounds.length }) }}</span>
          <span>•</span>
          <span>{{ $t('ui.resource.testedCount', { count: Object.keys(checkResults).length }) }}</span>
        </div>
      </v-col>
      <v-col cols="12" lg="5" class="resource-hero__actions">
        <v-btn color="primary" variant="tonal" size="large" @click="strategyModal = true">
          <v-icon icon="mdi-call-split" start />{{ $t('ui.resource.strategyGroup') }}
        </v-btn>
        <v-btn v-if="outbounds.length > 0" color="primary" size="large" @click="showModal(0)">
          <v-icon icon="mdi-plus" start />
          {{ $t('actions.add') }}
        </v-btn>
        <v-menu location="bottom end">
          <template #activator="{ props }">
            <v-btn v-bind="props" variant="outlined" size="large" append-icon="mdi-chevron-down">{{ $t('ui.common.more') }}</v-btn>
          </template>
          <v-list density="compact" nav min-width="210">
            <v-list-item prepend-icon="mdi-link-variant" :title="$t('ui.resource.proxyChain')" :disabled="outbounds.length === 0" @click="chainModal = true" />
            <v-list-item prepend-icon="mdi-playlist-plus" :title="$t('actions.addbulk')" @click="showBulkModal" />
            <v-list-item prepend-icon="mdi-speedometer" :title="$t('actions.testAll')" :disabled="testingAll || outbounds.length === 0" @click="checkAllOutbounds" />
          </v-list>
        </v-menu>
      </v-col>
    </v-row>
  </v-card>

  <OutboundHealthDashboard
    :cards="healthCards"
    :check-loading="healthCheckLoading"
    :can-test-all="outbounds.length > 0"
    :loading="healthLoading"
    :testing-all="testingAll"
    :window-size="healthWindowSize"
    @refresh="refreshDashboard"
    @test="checkOutbound"
    @test-all="checkAllOutbounds"
  />

  <section v-if="failoverStatuses.length > 0" class="failover-panel">
    <div class="failover-panel__heading">
      <div><span>{{ $t('ui.resource.automaticFailover') }}</span><h2>{{ $t('ui.resource.failoverTitle') }}</h2></div>
      <v-btn icon="mdi-refresh" variant="text" :loading="failoverLoading" :aria-label="$t('ui.resource.refreshFailover')" @click="loadFailoverStatus" />
    </div>
    <div class="failover-grid">
      <article v-for="status in failoverStatuses" :key="status.policy.tag" class="failover-card">
        <div class="failover-card__top">
          <div><strong>{{ status.policy.tag }}</strong><small>{{ status.policy.members.join(' → ') }}</small></div>
          <v-chip size="small" :color="status.error ? 'warning' : 'success'" variant="tonal">{{ status.error ? $t('ui.resource.needsCheck') : $t('ui.resource.running') }}</v-chip>
        </div>
        <div class="failover-card__state">
          <span>{{ $t('ui.resource.currentOutbound') }}</span><strong>{{ status.current || $t('ui.resource.awaitingProbe') }}</strong>
          <span>{{ $t('ui.resource.candidateOutbound') }}</span><strong>{{ status.candidate || '-' }}</strong>
        </div>
        <div class="failover-members">
          <div v-for="member in status.policy.members" :key="member" class="failover-member">
            <span class="health-status-dot" :class="`health-status-dot--${healthFor(member).status}`" aria-hidden="true"></span>
            <div class="failover-member__name">
              <strong>{{ member }}</strong>
              <small>{{ memberRoleLabel(status, member) }}</small>
            </div>
            <div class="failover-member__route">
              <strong>{{ formatMemberProbe(status, member) }}</strong>
              <small>{{ compactIdentity(healthFor(member)) }}</small>
            </div>
            <span class="failover-member__availability">{{ formatAvailability(healthFor(member)) }}</span>
          </div>
        </div>
        <p v-if="status.error">{{ status.error }}</p>
        <div class="failover-card__footer">
          <small>{{ status.lastChecked ? $t('ui.resource.lastProbe', { time: formatStatusTime(status.lastChecked) }) : $t('ui.resource.notProbed') }}</small>
          <v-btn size="small" variant="text" color="warning" @click="deleteFailover(status.policy.tag)">{{ $t('ui.resource.stopFailover') }}</v-btn>
        </div>
      </article>
    </div>
  </section>

  <div v-if="outbounds.length > 0" class="resource-list-toolbar">
    <v-text-field v-model="search" :placeholder="$t('ui.resource.searchOutbounds')" prepend-inner-icon="mdi-magnify" clearable hide-details density="comfortable" variant="solo-filled" />
    <span>{{ $t('itemCount', { count: filteredOutbounds.length }) }}</span>
  </div>

  <v-row v-if="outbounds.length === 0" class="resource-grid">
    <v-col cols="12">
      <EmptyState
        icon="mdi-cloud-upload-outline"
        :title="$t('ui.resource.noOutbounds')"
        :description="$t('ui.resource.noOutboundsHint')"
        :action="$t('actions.add')"
        @action="showModal(0)"
      />
    </v-col>
  </v-row>

  <v-data-table
    v-else-if="!smAndDown"
    class="resource-table"
    :headers="tableHeaders"
    :items="filteredOutbounds"
    item-value="id"
    density="comfortable"
    hover
  >
    <template #item.tag="{ item }"><strong class="resource-table__primary">{{ item.tag }}</strong></template>
    <template #item.server="{ item }">{{ item.server ?? '-' }}</template>
    <template #item.server_port="{ item }">{{ item.server_port ?? '-' }}</template>
    <template #item.tls="{ item }">
      <v-chip v-if="Object.hasOwn(item, 'tls')" size="small" :color="item.tls?.enabled ? 'success' : 'default'" variant="tonal">{{ $t(item.tls?.enabled ? 'enable' : 'disable') }}</v-chip>
      <span v-else class="resource-table__muted">-</span>
    </template>
    <template #item.online="{ item }">
      <v-chip v-if="onlines.includes(item.tag)" size="small" color="success" variant="tonal">{{ $t('online') }}</v-chip>
      <span v-else class="resource-table__muted">-</span>
    </template>
    <template #item.delay="{ item }">
      <v-progress-circular v-if="checkResults[item.tag]?.loading" indeterminate size="18" />
      <v-chip v-else-if="checkResults[item.tag]?.success" size="small" color="success" variant="tonal">{{ checkResults[item.tag].data?.Delay }}{{ $t('date.ms') }}</v-chip>
      <v-tooltip v-else-if="checkResults[item.tag]" :text="checkResults[item.tag].errorMessage || $t('failed')">
        <template #activator="{ props }"><v-icon v-bind="props" size="small" color="error" icon="mdi-close-circle" /></template>
      </v-tooltip>
      <v-btn v-else icon="mdi-speedometer" size="small" variant="text" :aria-label="$t('actions.test')" :title="$t('actions.test')" @click="checkOutbound(item.tag)" />
    </template>
    <template #item.actions="{ item }">
      <div class="resource-table__actions">
        <v-btn icon="mdi-file-edit-outline" size="small" variant="text" :aria-label="$t('actions.edit')" :title="$t('actions.edit')" @click="showModal(item.id)" />
        <v-menu location="bottom end">
          <template #activator="{ props }"><v-btn v-bind="props" icon="mdi-dots-horizontal" size="small" variant="text" :aria-label="$t('ui.common.more')" /></template>
          <v-list density="compact" nav>
            <v-list-item prepend-icon="mdi-speedometer" :title="$t('actions.test')" :disabled="checkResults[item.tag]?.loading" @click="checkOutbound(item.tag)" />
            <v-list-item v-if="Data().enableTraffic" prepend-icon="mdi-chart-line" :title="$t('stats.graphTitle')" @click="showStats(item.tag)" />
            <v-divider />
            <v-list-item prepend-icon="mdi-delete-outline" :title="$t('actions.del')" base-color="error" @click="askDelete(item.tag)" />
          </v-list>
        </v-menu>
      </div>
    </template>
    <template #no-data>
      <EmptyState icon="mdi-magnify" :title="$t('ui.resource.noSearchResults')" :description="$t('ui.resource.noSearchResultsHint')" />
    </template>
  </v-data-table>

  <v-row v-else class="resource-grid">
    <v-col v-if="filteredOutbounds.length === 0" cols="12">
      <EmptyState icon="mdi-magnify" :title="$t('ui.resource.noSearchResults')" :description="$t('ui.resource.noSearchResultsHint')" />
    </v-col>
    <v-col cols="12" sm="6" md="4" lg="3" v-for="item in filteredOutbounds" :key="item.tag">
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
          <v-btn class="np-card-action" variant="text" style="margin-inline-start:0;" color="warning" @click="askDelete(item.tag)">
            <v-icon icon="mdi-file-remove" /><span>{{ $t('actions.del') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
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
import HttpUtils from '@/plugins/httputil'
import { Outbound } from '@/types/outbounds'
import type {
  FailoverPolicy,
  FailoverRelation,
  FailoverRole,
  FailoverStatus,
  OutboundHealthSnapshot,
} from '@/types/outboundHealth'
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref } from 'vue'
import EmptyState from '@/components/EmptyState.vue'
import OutboundHealthDashboard from '@/components/outbounds/OutboundHealthDashboard.vue'
import { useDisplay } from 'vuetify'
import { useI18n } from 'vue-i18n'

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

interface StrategyPayload {
  outbound: Outbound
  policy?: FailoverPolicy
}

const healthWindowSize = 64

const checkResults = ref<Record<string, CheckResult>>({})
const healthCheckLoading = computed(() => Object.fromEntries(
  Object.entries(checkResults.value).map(([tag, result]) => [tag, result.loading]),
))
const healthSnapshots = ref<OutboundHealthSnapshot[]>([])
const healthLoading = ref(false)
let healthPollTimer: ReturnType<typeof setInterval> | undefined
let identityRefreshTimer: ReturnType<typeof setTimeout> | undefined

const emptyHealth = (tag: string): OutboundHealthSnapshot => ({
  tag,
  status: 'untested',
  latestDelay: 0,
  averageDelay: 0,
  observedAvailability: 0,
  samples: 0,
  successes: 0,
  failures: 0,
})

const healthByTag = computed(() => new Map(healthSnapshots.value.map((health) => [health.tag, health])))

function healthFor(tag: string): OutboundHealthSnapshot {
  return healthByTag.value.get(tag) ?? emptyHealth(tag)
}

async function loadOutboundHealth(silent = false) {
  if (!silent) healthLoading.value = true
  try {
    const response = await HttpUtils.get('api/outbound-health')
    if (response.success && Array.isArray(response.obj)) {
      healthSnapshots.value = response.obj as OutboundHealthSnapshot[]
    }
  } finally {
    if (!silent) healthLoading.value = false
  }
}

function scheduleIdentityHealthRefresh() {
  if (identityRefreshTimer) clearTimeout(identityRefreshTimer)
  identityRefreshTimer = setTimeout(() => loadOutboundHealth(true), 1800)
}

const checkOutbound = async (tag: string, refreshHealth = true) => {
  checkResults.value = { ...checkResults.value, [tag]: { loading: true, success: false } }
  const msg = await HttpUtils.get('api/checkOutbound', { tag })
  const success = msg.success && msg.obj?.OK
  const errorMessage = success ? undefined : (msg.obj?.Error ?? msg.msg ?? '')
  checkResults.value = {
    ...checkResults.value,
    [tag]: { loading: false, success, data: msg.obj ?? null, errorMessage }
  }
  if (refreshHealth) {
    await loadOutboundHealth(true)
    if (success) scheduleIdentityHealthRefresh()
  }
}

const testingAll = ref(false)

const checkAllOutbounds = async () => {
  const list = outbounds.value
  if (list.length === 0) return
  testingAll.value = true
  try {
    await Promise.all(list.map((o) => checkOutbound(o.tag, false)))
  } finally {
    testingAll.value = false
    await loadOutboundHealth(true)
    scheduleIdentityHealthRefresh()
  }
}

const outbounds = computed((): Outbound[] => {
  return <Outbound[]> Data().outbounds
})
const { smAndDown } = useDisplay()
const { t } = useI18n()
const search = ref('')
const filteredOutbounds = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  if (!query) return outbounds.value
  return outbounds.value.filter(item => [item.tag, item.type, item.server, item.server_port]
    .some(value => String(value ?? '').toLocaleLowerCase().includes(query)))
})
const tableHeaders = computed(() => [
  { title: t('objects.tag'), key: 'tag' },
  { title: t('protocol'), key: 'type' },
  { title: t('in.addr'), key: 'server' },
  { title: t('in.port'), key: 'server_port' },
  { title: t('objects.tls'), key: 'tls', sortable: false },
  { title: t('online'), key: 'online', sortable: false },
  { title: t('out.delay'), key: 'delay', sortable: false },
  { title: t('ui.common.more'), key: 'actions', sortable: false, align: 'end' as const },
])

const strategyModal = ref(false)
const strategyLoading = ref(false)
const chainModal = ref(false)
const chainLoading = ref(false)
const failoverStatuses = ref<FailoverStatus[]>([])
const failoverLoading = ref(false)

async function saveStrategy(payload: StrategyPayload) {
  if (Data().checkTag('outbound', 0, payload.outbound.tag)) return
  strategyLoading.value = true
  try {
    const saved = await Data().save('outbounds', 'new', payload.outbound)
    if (!saved) return
    if (payload.policy) {
      const response = await HttpUtils.post('api/failoverSave', payload.policy, {
        headers: { 'Content-Type': 'application/json' },
      })
      if (!response.success) {
        await Data().save('outbounds', 'del', payload.outbound.tag)
        return
      }
    }
    strategyModal.value = false
    await refreshDashboard()
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
    if (response.success && Array.isArray(response.obj)) {
      failoverStatuses.value = response.obj as FailoverStatus[]
    }
  } finally {
    failoverLoading.value = false
  }
}

async function deleteFailover(tag: string) {
  const response = await HttpUtils.post('api/failoverDelete', { tag }, {
    headers: { 'Content-Type': 'application/json' },
  })
  if (response.success) await refreshDashboard()
}

async function refreshDashboard() {
  await Promise.all([loadOutboundHealth(), loadFailoverStatus()])
}

function formatStatusTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function formatDelay(delay: number, status: OutboundHealthSnapshot['status']) {
  return status === 'healthy' ? `${delay} ms` : '—'
}

function formatAvailability(health: OutboundHealthSnapshot) {
  return health.samples > 0 ? `${health.observedAvailability.toFixed(health.samples > 9 ? 1 : 0)}%` : '—'
}

let regionNames: Intl.DisplayNames | undefined
try {
  regionNames = new Intl.DisplayNames([navigator.language], { type: 'region' })
} catch {
  regionNames = undefined
}

function formatRegion(health: OutboundHealthSnapshot) {
  const country = health.countryCode
    ? (regionNames?.of(health.countryCode) || health.countryCode)
    : ''
  if (country && health.colo) return `${country} · ${health.colo}`
  return country || health.colo || t('ui.resource.regionPending')
}

function compactIdentity(health: OutboundHealthSnapshot) {
  const ip = health.publicIp || t('ui.resource.ipPending')
  const region = formatRegion(health)
  return region === t('ui.resource.regionPending') ? ip : `${ip} · ${region}`
}

function memberRoleLabel(status: FailoverStatus, member: string) {
  const roles: string[] = []
  if (status.current === member) roles.push(t('ui.resource.currentRole'))
  if (status.candidate === member) roles.push(t('ui.resource.candidateRole'))
  if (roles.length === 0) roles.push(t('ui.resource.memberRole'))
  return roles.join(' · ')
}

function formatMemberProbe(status: FailoverStatus, member: string) {
  const probe = status.probes.find((item) => item.tag === member)
  if (probe) return probe.ok ? `${probe.delay} ms` : t('ui.resource.probeFailed')
  const health = healthFor(member)
  return formatDelay(health.latestDelay, health.status)
}

onMounted(async () => {
  await refreshDashboard()
  healthPollTimer = setInterval(() => loadOutboundHealth(true), 15_000)
})

onBeforeUnmount(() => {
  if (healthPollTimer) clearInterval(healthPollTimer)
  if (identityRefreshTimer) clearTimeout(identityRefreshTimer)
})

const failoverRelations = computed(() => {
  const relations = new Map<string, FailoverRelation[]>()
  for (const status of failoverStatuses.value) {
    for (const member of status.policy.members) {
      const role: FailoverRole = status.current === member
        ? 'current'
        : status.candidate === member ? 'candidate' : 'member'
      const current = relations.get(member) ?? []
      current.push({ key: `${status.policy.tag}:${member}`, policy: status.policy.tag, role })
      relations.set(member, current)
    }
  }
  return relations
})

const healthCards = computed(() => {
  const tags = new Set<string>()
  for (const outbound of outbounds.value) tags.add(outbound.tag)
  for (const status of failoverStatuses.value) {
    for (const member of status.policy.members) tags.add(member)
  }
  return [...tags].map((tag) => ({
    tag,
    health: healthFor(tag),
    relations: failoverRelations.value.get(tag) ?? [],
  }))
})

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

const deleteDialogOpen = ref(false)
const deleteLoading = ref(false)
const deleteTarget = ref('')

const askDelete = (tag: string) => {
  deleteTarget.value = tag
  deleteDialogOpen.value = true
}

const confirmDelete = async () => {
  if (!deleteTarget.value) return
  deleteLoading.value = true
  const success = await delOutbound(deleteTarget.value)
  deleteLoading.value = false
  if (success) {
    deleteDialogOpen.value = false
    deleteTarget.value = ''
  }
}

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
  const success = await Data().save("outbounds", "del", tag)
  if (success) {
    await HttpUtils.post('api/failoverDelete', { tag }, {
      headers: { 'Content-Type': 'application/json' },
    })
    await refreshDashboard()
  }
  return success
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

.resource-list-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin: 0 0 12px; color: var(--np-text-muted); font-size: .78rem; }
.resource-list-toolbar :deep(.v-input) { max-width: 420px; }
.resource-table { overflow: hidden; border: 1px solid var(--np-border); border-radius: 18px; background: var(--np-surface); box-shadow: var(--np-shadow); backdrop-filter: blur(24px) saturate(1.08); }
.resource-table__primary { overflow-wrap: anywhere; }
.resource-table__muted { color: var(--np-text-muted); }
.resource-table__actions { display: flex; align-items: center; justify-content: flex-end; gap: 2px; }
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

@media (max-width: 599px) {
  .resource-list-toolbar { align-items: stretch; flex-direction: column; gap: 8px; }
  .resource-list-toolbar :deep(.v-input) { max-width: none; }
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

.failover-members {
  display: grid;
  gap: 2px;
  margin: 4px 0 14px;
  padding: 6px;
  border-radius: 15px;
  background: rgba(118, 118, 128, 0.06);
}

.failover-member {
  display: grid;
  grid-template-columns: auto minmax(70px, 0.75fr) minmax(100px, 1.4fr) auto;
  align-items: center;
  gap: 9px;
  min-width: 0;
  padding: 8px;
  border-radius: 11px;
}

.failover-member + .failover-member {
  border-top: 1px solid var(--np-border);
  border-top-left-radius: 0;
  border-top-right-radius: 0;
}

.failover-member__name,
.failover-member__route {
  min-width: 0;
}

.failover-member__name strong,
.failover-member__name small,
.failover-member__route strong,
.failover-member__route small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.failover-member__name strong,
.failover-member__route strong {
  font-size: 12px;
}

.failover-member__name small,
.failover-member__route small,
.failover-member__availability {
  color: var(--np-text-muted);
  font-size: 10px;
}

.failover-member__availability {
  font-variant-numeric: tabular-nums;
}

.v-theme--dark .resource-hero,
.v-theme--dark .resource-card {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.08), transparent 24%),
    rgba(11, 18, 31, 0.78);
  border-color: rgba(148, 163, 184, 0.16);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.3);
}

.health-status-dot {
  --health-color: #8e8e93;
  width: 8px;
  height: 8px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--health-color);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--health-color) 13%, transparent);
}

.health-status-dot--healthy { --health-color: #30a46c; }
.health-status-dot--unhealthy { --health-color: #ff453a; }
.health-status-dot--untested { --health-color: #8e8e93; }

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

  .failover-member {
    grid-template-columns: auto minmax(0, 1fr) auto;
  }

  .failover-member__route {
    grid-column: 2 / -1;
  }
}
</style>
