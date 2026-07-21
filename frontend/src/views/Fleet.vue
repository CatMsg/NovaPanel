<template>
  <v-container fluid class="fleet-shell">
    <div class="fleet-shell__glow fleet-shell__glow--one"></div>
    <div class="fleet-shell__glow fleet-shell__glow--two"></div>

    <div class="fleet-shell__inner">
      <v-card class="fleet-hero" rounded="xl" variant="flat">
        <div class="fleet-hero__topline">
          <span class="fleet-hero__badge">服务器集合</span>
          <span class="fleet-hero__badge fleet-hero__badge--soft">
            {{ initialLoading ? '正在检查…' : `${reachableCount}/${servers.length} 在线` }}
          </span>
        </div>
        <v-row class="fleet-hero__content" align="center">
          <v-col cols="12" lg="7">
            <div class="fleet-hero__title-row">
              <div class="fleet-hero__icon">
                <v-icon icon="mdi-server-network" size="32" />
              </div>
              <div>
                <h1 class="fleet-hero__title">服务器集合</h1>
                <p class="fleet-hero__subtitle">
                  从一个页面查看多台 NovaPanel 的运行状态、端口后端和核心健康情况。
                </p>
              </div>
            </div>
            <div class="fleet-hero__meta">
              <span>上次检查：{{ formattedCheckedAt }}</span>
              <span>•</span>
              <span>{{ remoteCount }} 台远端服务器</span>
            </div>
          </v-col>
          <v-col cols="12" lg="5" class="fleet-hero__actions">
            <v-btn
              variant="outlined"
              color="warning"
              :loading="batchAction === 'restart'"
              :disabled="loading || batchAction !== ''"
              @click="runBatchAction('restart')"
            >
              <v-icon icon="mdi-restart" start />
              一键重启
            </v-btn>
            <v-btn
              color="primary"
              :loading="batchAction === 'update'"
              :disabled="loading || batchAction !== ''"
              @click="runBatchAction('update')"
            >
              <v-icon icon="mdi-download-outline" start />
              一键更新
            </v-btn>
            <v-btn variant="outlined" :disabled="loading" @click="showConfig = true">
              <v-icon icon="mdi-server-plus" start />
              管理服务器
            </v-btn>
            <v-btn color="primary" :loading="loading" @click="loadFleet">
              <v-icon icon="mdi-refresh" start />
              刷新状态
            </v-btn>
          </v-col>
        </v-row>
        <v-alert v-if="batchMessage" class="fleet-batch-alert" variant="tonal" :type="batchMessageType">
          {{ batchMessage }}
        </v-alert>
      </v-card>

      <v-row class="fleet-summary" dense>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--one" rounded="xl" variant="flat">
            <div class="fleet-summary__label">服务器总数</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : servers.length }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--two" rounded="xl" variant="flat">
            <div class="fleet-summary__label">在线</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : reachableCount }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--three" rounded="xl" variant="flat">
            <div class="fleet-summary__label">核心运行</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : runningCount }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--four" rounded="xl" variant="flat">
            <div class="fleet-summary__label">异常</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : errorCount }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--five" rounded="xl" variant="flat">
            <div class="fleet-summary__label">在线用户</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : onlineUsersTotal }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--six" rounded="xl" variant="flat">
            <div class="fleet-summary__label">节点总数</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : endpointTotal }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col fleet-summary__col--last">
          <v-card class="fleet-summary__card fleet-summary__card--seven" rounded="xl" variant="flat">
            <div class="fleet-summary__label">配置差异</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : driftServerCount }}</div>
          </v-card>
        </v-col>
      </v-row>

      <v-alert v-if="!initialLoading && servers.length === 1" type="info" variant="tonal" rounded="xl" class="fleet-empty">
        当前只有本机。点击“管理服务器”添加远端 NovaPanel 的 API 地址和令牌，即可纳入集合。
      </v-alert>

      <v-row class="fleet-grid" dense>
        <v-col v-for="server in servers" :key="server.id" cols="12" md="6" xl="4">
          <v-card class="fleet-card" rounded="xl" variant="flat" @click="openDetails(server)">
            <div class="fleet-card__header">
              <div class="fleet-card__identity">
                <div class="fleet-card__icon" :class="statusClass(server)">
                  <v-icon :icon="server.id === 'local' ? 'mdi-home' : 'mdi-server'" />
                </div>
                <div class="fleet-card__name-wrap">
                  <div class="fleet-card__name">{{ server.name }}</div>
                  <div class="fleet-card__url">{{ server.url }}</div>
                </div>
              </div>
              <div class="fleet-card__chips">
                <v-chip v-if="server.driftCount" size="small" color="warning" variant="tonal">{{ server.driftCount }} 项差异</v-chip>
                <v-chip size="small" :color="server.reachable ? 'success' : server.enabled ? 'error' : 'secondary'" variant="flat">
                  {{ server.reachable ? '在线' : server.lastKnown ? '上次状态' : server.enabled ? '失联' : '已停用' }}
                </v-chip>
              </div>
            </div>

            <v-divider />

            <div class="fleet-card__metrics">
              <div class="fleet-metric">
                <span>版本</span>
                <strong>{{ server.System?.appVersion || '-' }}</strong>
              </div>
              <div class="fleet-metric">
                <span>延迟</span>
                <strong>{{ server.id === 'local' ? '本机' : server.reachable ? `${server.latencyMs} ms` : '-' }}</strong>
              </div>
              <div class="fleet-metric">
                <span>公网 IP</span>
                <strong>{{ server.PublicIP || '-' }}</strong>
              </div>
              <div class="fleet-metric">
                <span>运行时间</span>
                <strong>{{ formatUptime(server.Uptime) }}</strong>
              </div>
              <div class="fleet-metric">
                <span>防火墙</span>
                <strong>{{ server.portBackend || '-' }}</strong>
              </div>
              <div class="fleet-metric">
                <span>监听 / NAT</span>
                <strong>{{ server.reachable ? `${server.listeners} / ${server.natRules}` : '-' }}</strong>
              </div>
              <div class="fleet-metric">
                <span>用户在线</span>
                <strong>{{ server.OnlineUsers }} / {{ server.Clients }}</strong>
              </div>
              <div class="fleet-metric">
                <span>入站 / 出站</span>
                <strong>{{ server.Inbounds }} / {{ server.Outbounds }}</strong>
              </div>
              <div class="fleet-metric">
                <span>节点 / MASQUE</span>
                <strong>{{ server.Endpoints }} / {{ server.MasqueRunning }} / {{ server.MasqueTotal }}</strong>
              </div>
            </div>

            <div class="fleet-card__footer">
              <span class="fleet-core-state" :class="server.Core?.running ? 'is-running' : ''">
                <v-icon :icon="server.Core?.running ? 'mdi-check-circle' : 'mdi-alert-circle-outline'" size="16" />
                {{ server.Core?.running ? 'Sing-Box 运行中' : 'Sing-Box 未运行' }}
              </span>
              <span v-if="server.error" class="fleet-card__error" :title="server.error">{{ server.error }}</span>
            </div>
            <div class="fleet-card__actions">
              <v-btn size="small" variant="tonal" @click.stop="openDetails(server)">
                <v-icon icon="mdi-information-outline" start />详情
              </v-btn>
              <v-btn size="small" variant="text" @click.stop="openLogs(server)">
                <v-icon icon="mdi-text-box-outline" start />日志
              </v-btn>
              <v-btn size="small" variant="text" color="warning" :disabled="batchAction !== '' || !server.reachable" @click.stop="restartServer(server)">
                <v-icon icon="mdi-restart" start />重启
              </v-btn>
              <v-btn size="small" variant="text" color="primary" :loading="updateLoadingId === server.id" :disabled="batchAction !== '' || !server.reachable" @click.stop="updateServer(server)">
                <v-icon icon="mdi-download-outline" start />更新
              </v-btn>
              <v-btn size="small" variant="text" :loading="refreshLoadingId === server.id" @click.stop="refreshServer(server)">
                <v-icon icon="mdi-refresh" start />刷新
              </v-btn>
            </div>
          </v-card>
        </v-col>
      </v-row>
    </div>

    <v-dialog v-model="showConfig" max-width="860" scrollable>
      <v-card rounded="xl" class="fleet-dialog">
        <v-card-title class="fleet-dialog__title">
          <span>管理服务器集合</span>
          <v-btn icon="mdi-close" variant="text" @click="showConfig = false" />
        </v-card-title>
        <v-card-subtitle>
          远端地址填写面板根地址，例如 https://example.com:9999。令牌用于状态读取和远程运维，留空保持已保存令牌不变。
        </v-card-subtitle>
        <v-card-text>
          <div v-for="(item, index) in configs" :key="item.id || index" class="fleet-config-row">
            <v-text-field v-model="item.name" label="名称" density="compact" hide-details />
            <v-text-field v-model="item.url" label="面板地址" density="compact" hide-details />
            <v-text-field
              v-model="item.token"
              label="API 令牌"
              density="compact"
              hide-details
              type="password"
              :placeholder="item.tokenSet ? '已保存，留空保持不变' : ''"
            />
            <v-switch v-model="item.enabled" color="primary" hide-details density="compact" />
            <v-btn icon="mdi-delete-outline" color="error" variant="text" @click="removeConfig(index)" />
          </div>
          <v-btn variant="tonal" color="primary" class="fleet-dialog__add" @click="addConfig">
            <v-icon icon="mdi-plus" start />
            添加服务器
          </v-btn>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showConfig = false">取消</v-btn>
          <v-btn color="primary" :loading="saving" @click="saveConfig">保存并检查</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="showDetails" max-width="760" scrollable>
      <v-card rounded="xl" class="fleet-dialog" v-if="selectedServer">
        <v-card-title class="fleet-dialog__title">
          <span>{{ selectedServer.name }} · 服务器详情</span>
          <v-btn icon="mdi-close" variant="text" @click="showDetails = false" />
        </v-card-title>
        <v-card-text>
          <div class="fleet-detail__status">
            <v-chip :color="selectedServer.reachable ? 'success' : 'error'" variant="tonal">
              {{ selectedServer.reachable ? '在线' : '失联' }}
            </v-chip>
            <span>检查时间：{{ selectedServer.checkedAt ? new Date(selectedServer.checkedAt).toLocaleString() : '-' }}</span>
            <span>延迟：{{ selectedServer.id === 'local' ? '本机' : `${selectedServer.latencyMs} ms` }}</span>
          </div>
          <v-alert v-if="updateStates[selectedServer.id]" class="mt-4" variant="tonal" :type="updateAlertType(selectedServer)">
            <strong>更新状态：</strong>{{ updateStateLabel(selectedServer) }}
            <span v-if="updateStates[selectedServer.id]?.message"> · {{ updateStates[selectedServer.id].message }}</span>
          </v-alert>
          <div class="fleet-detail__grid">
            <div><span>地址</span><strong>{{ selectedServer.url }}</strong></div>
            <div><span>公网 IP</span><strong>{{ selectedServer.PublicIP || '-' }}</strong></div>
            <div><span>版本</span><strong>{{ selectedServer.System?.appVersion || '-' }}</strong></div>
            <div><span>运行时间</span><strong>{{ formatUptime(selectedServer.Uptime) }}</strong></div>
            <div><span>防火墙</span><strong>{{ selectedServer.portBackend || '-' }}</strong></div>
            <div><span>监听 / NAT</span><strong>{{ selectedServer.listeners }} / {{ selectedServer.natRules }}</strong></div>
            <div><span>用户 / 在线</span><strong>{{ selectedServer.Clients }} / {{ selectedServer.OnlineUsers }}</strong></div>
            <div><span>入站 / 出站</span><strong>{{ selectedServer.Inbounds }} / {{ selectedServer.Outbounds }}</strong></div>
            <div><span>节点 / MASQUE</span><strong>{{ selectedServer.Endpoints }} / {{ selectedServer.MasqueRunning }} / {{ selectedServer.MasqueTotal }}</strong></div>
          </div>
          <section v-if="selectedServer.configuration" class="fleet-config-compare">
            <div class="fleet-detail__log-head">
              <span>配置快照</span>
              <v-chip size="small" :color="selectedServer.driftCount ? 'warning' : 'success'" variant="tonal">
                {{ selectedServer.id === 'local' ? '对比基线' : selectedServer.driftCount ? `${selectedServer.driftCount} 项差异` : '与本机一致' }}
              </v-chip>
            </div>
            <div class="fleet-config-snapshot">
              <div><span>面板</span><strong>{{ selectedServer.configuration.webPort }} · {{ selectedServer.configuration.webPath }} · {{ tlsLabel(selectedServer.configuration.webTls) }}</strong></div>
              <div><span>订阅</span><strong>{{ selectedServer.configuration.subPort }} · {{ selectedServer.configuration.subPath }} · {{ tlsLabel(selectedServer.configuration.subTls) }}</strong></div>
              <div><span>面板域名</span><strong>{{ selectedServer.configuration.webDomain || '未设置' }}</strong></div>
              <div><span>订阅域名</span><strong>{{ selectedServer.configuration.subDomain || '未设置' }}</strong></div>
              <div><span>订阅角色</span><strong>{{ selectedServer.configuration.subMode === 'master' ? '主模式' : '从模式' }}</strong></div>
              <div><span>订阅开关</span><strong>Base64 {{ boolLabel(selectedServer.configuration.subEncode) }} · 用户信息 {{ boolLabel(selectedServer.configuration.subShowInfo) }}</strong></div>
            </div>
            <div v-if="selectedServer.drift?.length" class="fleet-drift-list">
              <div v-for="item in selectedServer.drift" :key="item.field" class="fleet-drift-item">
                <span>{{ item.label }}</span>
                <strong>{{ formatDriftValue(item.actual) }}</strong>
                <v-icon icon="mdi-arrow-left" size="16" />
                <small>基线 {{ formatDriftValue(item.expected) }}</small>
              </div>
            </div>
          </section>
          <v-alert v-else-if="selectedServer.reachable" type="info" variant="tonal" class="mt-4">远端版本尚未提供配置对比数据，更新后即可参与比较。</v-alert>
          <v-alert v-if="selectedServer.error" type="error" variant="tonal" class="mt-4">{{ selectedServer.error }}</v-alert>
          <div class="fleet-detail__log-head">
            <span>最近日志</span>
            <v-btn size="small" variant="tonal" :loading="logsLoading" @click="loadLogs(selectedServer)">刷新日志</v-btn>
          </div>
          <pre class="fleet-detail__logs">{{ logLines.length ? logLines.join('\n') : '点击“刷新日志”查看最近 100 行日志' }}</pre>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showDetails = false">关闭</v-btn>
          <v-btn color="warning" :loading="actionLoading" :disabled="batchAction !== '' || !selectedServer.reachable" @click="restartServer(selectedServer)">
            <v-icon icon="mdi-restart" start />重启面板
          </v-btn>
          <v-btn color="primary" :loading="updateLoadingId === selectedServer.id" :disabled="batchAction !== '' || !selectedServer.reachable" @click="updateServer(selectedServer)">
            <v-icon icon="mdi-download-outline" start />后台更新
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'
import HttpUtils from '@/plugins/httputil'

type FleetServer = {
  id: string
  name: string
  url: string
  enabled: boolean
  tokenSet?: boolean
  reachable: boolean
  latencyMs: number
  checkedAt?: string
  error?: string
  lastKnown?: boolean
  lastSuccessAt?: string
  System?: Record<string, any>
  Core?: Record<string, any>
  system?: Record<string, any>
  core?: Record<string, any>
  PublicIP: string
  Uptime: number
  OnlineUsers: number
  OnlineInbounds: number
  OnlineOutbounds: number
  Clients: number
  Inbounds: number
  Outbounds: number
  Endpoints: number
  MasqueTotal: number
  MasqueRunning: number
  portBackend?: string
  listeners: number
  natRules: number
  configuration?: FleetConfigProfile
  drift?: FleetConfigDrift[]
  driftCount: number
}

type FleetConfigProfile = {
  appVersion: string
  webPort: number
  webPath: string
  webTls: string
  webDomain?: string
  subPort: number
  subPath: string
  subTls: string
  subDomain?: string
  subMode: string
  subEncode: boolean
  subShowInfo: boolean
}

type FleetConfigDrift = { field: string; label: string; expected: unknown; actual: unknown }

type FleetConfig = {
  id: string
  name: string
  url: string
  token: string
  tokenSet?: boolean
  enabled: boolean
}

const loading = ref(true)
const saving = ref(false)
const showConfig = ref(false)
const showDetails = ref(false)
const servers = ref<FleetServer[]>([])
const configs = ref<FleetConfig[]>([])
const checkedAt = ref('')
const selectedServer = ref<FleetServer | null>(null)
const logLines = ref<string[]>([])
const logsLoading = ref(false)
const actionLoading = ref(false)
const batchAction = ref<'' | 'update' | 'restart'>('')
const batchMessage = ref('')
const batchMessageType = ref<'info' | 'success' | 'warning' | 'error'>('info')
const updateLoadingId = ref('')
const refreshLoadingId = ref('')
const updateStates = ref<Record<string, any>>({})

const normalizeServer = (server: any): FleetServer => ({
  ...server,
  System: server.system ?? server.System ?? {},
  Core: server.core ?? server.Core ?? {},
  PublicIP: server.publicIp ?? server.PublicIP ?? '',
  Uptime: server.uptime ?? server.Uptime ?? 0,
  OnlineUsers: server.onlineUsers ?? server.OnlineUsers ?? 0,
  OnlineInbounds: server.onlineInbounds ?? server.OnlineInbounds ?? 0,
  OnlineOutbounds: server.onlineOutbounds ?? server.OnlineOutbounds ?? 0,
  Clients: server.clients ?? server.Clients ?? 0,
  Inbounds: server.inbounds ?? server.Inbounds ?? 0,
  Outbounds: server.outbounds ?? server.Outbounds ?? 0,
  Endpoints: server.endpoints ?? server.Endpoints ?? 0,
  MasqueTotal: server.masqueTotal ?? server.MasqueTotal ?? 0,
  MasqueRunning: server.masqueRunning ?? server.MasqueRunning ?? 0,
  listeners: server.listeners ?? 0,
  natRules: server.natRules ?? 0,
  configuration: server.configuration,
  drift: Array.isArray(server.drift) ? server.drift : [],
  driftCount: server.driftCount ?? 0,
})

const remoteServers = computed(() => servers.value.filter((server) => server.id !== 'local'))
const reachableCount = computed(() => servers.value.filter((server) => server.reachable).length)
const runningCount = computed(() => servers.value.filter((server) => server.Core?.running).length)
const errorCount = computed(() => servers.value.filter((server) => server.error || !server.reachable).length)
const remoteCount = computed(() => remoteServers.value.length)
const onlineUsersTotal = computed(() => servers.value.reduce((total, server) => total + server.OnlineUsers, 0))
const endpointTotal = computed(() => servers.value.reduce((total, server) => total + server.Endpoints, 0))
const driftServerCount = computed(() => servers.value.filter((server) => server.driftCount > 0).length)
const initialLoading = computed(() => loading.value && servers.value.length === 0)
const formattedCheckedAt = computed(() => {
  if (!checkedAt.value) return '-'
  const date = new Date(checkedAt.value)
  return Number.isNaN(date.getTime()) ? checkedAt.value : date.toLocaleString()
})

const formatUptime = (seconds: number) => {
  if (!seconds || seconds < 1) return '-'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}天 ${hours}时`
  if (hours > 0) return `${hours}时 ${minutes}分`
  return `${Math.max(minutes, 1)}分`
}

const tlsLabel = (state: string) => ({ enabled: 'TLS 已启用', disabled: 'TLS 未启用', partial: 'TLS 配置不完整' } as Record<string, string>)[state] ?? state
const boolLabel = (value: boolean) => value ? '开' : '关'
const formatDriftValue = (value: unknown) => {
  if (typeof value === 'boolean') return boolLabel(value)
  return String(value ?? '-')
}

const loadFleet = async () => {
  loading.value = true
  try {
    const response = await HttpUtils.get('api/fleet')
    if (response.success && response.obj) {
      servers.value = (response.obj.servers ?? []).map(normalizeServer)
      checkedAt.value = response.obj.checkedAt ?? ''
      configs.value = remoteServers.value.map((server) => ({
        id: server.id,
        name: server.name,
        url: server.url,
        token: '',
        tokenSet: server.tokenSet,
        enabled: server.enabled,
      }))
    }
  } finally {
    loading.value = false
  }
}

const addConfig = () => {
  configs.value.push({ id: '', name: '', url: '', token: '', enabled: true })
}

const removeConfig = (index: number) => {
  configs.value.splice(index, 1)
}

const saveConfig = async () => {
  saving.value = true
  const response = await HttpUtils.post('api/fleetSave', {
    data: JSON.stringify(configs.value.map(({ id, name, url, token, enabled }) => ({ id, name, url, token, enabled }))),
  })
  if (response.success) {
    showConfig.value = false
    await loadFleet()
  }
  saving.value = false
}

const openDetails = (server: FleetServer) => {
  selectedServer.value = server
  logLines.value = []
  showDetails.value = true
  void loadUpdateStatus(server)
}

const loadUpdateStatus = async (server: FleetServer) => {
  const response = await HttpUtils.post('api/fleetAction', { id: server.id, action: 'update-status' })
  if (response.success) {
    updateStates.value = { ...updateStates.value, [server.id]: response.obj ?? {} }
  }
}

const refreshServer = async (server: FleetServer) => {
  refreshLoadingId.value = server.id
  const response = await HttpUtils.post('api/fleetRefresh', { id: server.id })
  if (response.success && response.obj) {
    const refreshed = normalizeServer(response.obj)
    servers.value = servers.value.map((item) => item.id === refreshed.id ? refreshed : item)
    if (selectedServer.value?.id === refreshed.id) selectedServer.value = refreshed
  }
  refreshLoadingId.value = ''
}

const loadLogs = async (server: FleetServer) => {
  logsLoading.value = true
  const response = await HttpUtils.post('api/fleetAction', { id: server.id, action: 'logs' })
  if (response.success) {
    logLines.value = Array.isArray(response.obj) ? response.obj.map((line) => String(line)) : []
  }
  logsLoading.value = false
}

const openLogs = async (server: FleetServer) => {
  openDetails(server)
  await loadLogs(server)
}

const runBatchAction = async (action: 'update' | 'restart') => {
  if (batchAction.value) return
  const remoteTargets = remoteServers.value.filter((server) => server.reachable && server.enabled)
  const localTarget = servers.value.find((server) => server.id === 'local' && server.reachable)
  const targets = [...remoteTargets, ...(localTarget ? [localTarget] : [])]
  if (!targets.length) {
    batchMessageType.value = 'warning'
    batchMessage.value = '没有可执行操作的在线服务器'
    return
  }
  const actionLabel = action === 'update' ? '更新' : '重启'
  const targetNames = targets.map((server) => server.name).join('、')
  if (!window.confirm(`将按远端服务器优先、本机最后的顺序${actionLabel}：${targetNames}\n\n是否继续？`)) return

  batchAction.value = action
  batchMessageType.value = 'info'
  batchMessage.value = `准备${actionLabel} ${targets.length} 台服务器，远端服务器优先`
  let remoteFailed = false
  let localFailed = false
  const failedRemoteNames: string[] = []

  for (let index = 0; index < targets.length; index += 1) {
    const server = targets[index]
    if (server.id === 'local' && remoteFailed) break
    batchMessage.value = `${actionLabel}中：${server.name}（${index + 1}/${targets.length}）`
    const response = await HttpUtils.post('api/fleetAction', { id: server.id, action })
    if (!response.success) {
      if (server.id !== 'local') {
        remoteFailed = true
        failedRemoteNames.push(server.name)
      }
      else localFailed = true
      batchMessageType.value = 'error'
      batchMessage.value = `${server.name}${actionLabel}失败：${response.msg}`
      if (server.id !== 'local') continue
      break
    }
  }

  if (remoteFailed) {
    batchAction.value = ''
    batchMessageType.value = 'warning'
    batchMessage.value = `远端${failedRemoteNames.join('、')}${actionLabel}失败，本机未执行`
    return
  }

  if (localFailed) {
    batchAction.value = ''
    return
  }

  batchAction.value = ''
  batchMessageType.value = 'success'
  batchMessage.value = localTarget
    ? `远端服务器已优先${action === 'update' ? '提交更新' : '完成重启'}，本机已最后执行`
    : `远端服务器已${action === 'update' ? '提交更新' : '完成重启'}`
  if (localTarget) {
    window.setTimeout(loadFleet, 4500)
  }
}

const restartServer = async (server: FleetServer) => {
  actionLoading.value = true
  const response = await HttpUtils.post('api/fleetAction', { id: server.id, action: 'restart' })
  if (response.success) {
    showDetails.value = false
    window.setTimeout(loadFleet, 4500)
  }
  actionLoading.value = false
}

const updateServer = async (server: FleetServer) => {
  updateLoadingId.value = server.id
  const response = await HttpUtils.post('api/fleetAction', { id: server.id, action: 'update' })
  if (response.success) {
    updateStates.value = { ...updateStates.value, [server.id]: response.obj ?? { state: 'queued' } }
    window.setTimeout(() => loadUpdateStatus(server), 1500)
    window.setTimeout(loadFleet, 4500)
  }
  updateLoadingId.value = ''
}

const updateStateLabel = (server: FleetServer) => {
  const state = updateStates.value[server.id]?.state
  return ({ queued: '排队中', running: '更新中', success: '已完成', failed: '失败', never: '未执行' } as Record<string, string>)[state] ?? state ?? '未知'
}

const updateAlertType = (server: FleetServer) => {
  const state = updateStates.value[server.id]?.state
  if (state === 'failed') return 'error'
  if (state === 'success') return 'success'
  return 'info'
}

const statusClass = (server: FleetServer) => {
  if (!server.enabled) return 'is-disabled'
  if (!server.reachable) return 'is-error'
  return 'is-online'
}

onMounted(loadFleet)
</script>

<style scoped lang="scss">
.fleet-shell {
  position: relative;
  overflow: hidden;
}

.fleet-shell__inner {
  position: relative;
  z-index: 1;
  width: min(1500px, 100%);
  margin: 0 auto;
  display: grid;
  gap: 18px;
}

.fleet-shell__glow {
  position: absolute;
  width: 360px;
  height: 360px;
  border-radius: 50%;
  filter: blur(80px);
  opacity: 0.22;
  pointer-events: none;
}

.fleet-shell__glow--one { background: #38bdf8; top: 0; right: 4%; }
.fleet-shell__glow--two { background: #2563eb; bottom: 10%; left: 10%; }

.fleet-hero,
.fleet-card,
.fleet-summary__card,
.fleet-dialog {
  border: 1px solid var(--np-border);
  background: var(--np-surface) !important;
  box-shadow: var(--np-shadow-soft);
  backdrop-filter: blur(22px) saturate(1.12);
}

.fleet-hero { padding: 22px 24px 24px; }
.fleet-hero__topline { display: flex; justify-content: space-between; gap: 10px; }
.fleet-hero__badge { border: 1px solid rgba(10, 132, 255, 0.18); border-radius: 999px; padding: 6px 11px; color: var(--np-accent); font-size: 0.75rem; font-weight: 700; letter-spacing: 0.08em; }
.fleet-hero__badge--soft { color: var(--np-text-muted); border-color: var(--np-border); letter-spacing: 0; }
.fleet-hero__content { margin-top: 12px; }
.fleet-hero__title-row { display: flex; align-items: center; gap: 14px; }
.fleet-hero__icon, .fleet-card__icon { display: grid; place-items: center; border-radius: 18px; color: var(--np-accent); background: rgba(10, 132, 255, 0.12); }
.fleet-hero__icon { width: 58px; height: 58px; }
.fleet-hero__title { margin: 0; font-size: clamp(1.65rem, 3vw, 2.45rem); letter-spacing: -0.05em; }
.fleet-hero__subtitle { margin: 6px 0 0; color: var(--np-text-muted); }
.fleet-hero__meta { display: flex; gap: 10px; margin-top: 18px; color: var(--np-text-muted); font-size: 0.8rem; }
.fleet-hero__actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-content: center; gap: 10px; }
.fleet-hero__actions .v-btn { width: 100%; min-width: 0; }
.fleet-batch-alert { margin-top: 16px; }

.fleet-summary__card { padding: 16px 18px; min-height: 92px; }
.fleet-summary { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); gap: 8px; margin: 0; }
.fleet-summary__col { flex: none; width: auto; max-width: none; padding: 0; }
.fleet-summary__label { color: var(--np-text-muted); font-size: 0.78rem; }
.fleet-summary__value { margin-top: 7px; font-size: 1.8rem; font-weight: 800; }
.fleet-summary__card--one { border-top: 3px solid #38bdf8; }
.fleet-summary__card--two { border-top: 3px solid #22c55e; }
.fleet-summary__card--three { border-top: 3px solid #a78bfa; }
.fleet-summary__card--four { border-top: 3px solid #fb7185; }
.fleet-summary__card--five { border-top: 3px solid #f59e0b; }
.fleet-summary__card--six { border-top: 3px solid #14b8a6; }
.fleet-summary__card--seven { border-top: 3px solid #f97316; }
.fleet-empty { border: 1px solid var(--np-border); }

.fleet-card { padding: 18px; height: 100%; }
.fleet-card__header, .fleet-card__identity, .fleet-card__footer { display: flex; align-items: center; }
.fleet-card__header { justify-content: space-between; gap: 12px; }
.fleet-card__chips { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }
.fleet-card__identity { gap: 12px; min-width: 0; }
.fleet-card__icon { width: 44px; height: 44px; flex: 0 0 auto; }
.fleet-card__icon.is-online { color: #22c55e; background: rgba(34, 197, 94, 0.12); }
.fleet-card__icon.is-error { color: #fb7185; background: rgba(251, 113, 133, 0.12); }
.fleet-card__icon.is-disabled { color: var(--np-text-muted); background: var(--np-surface-muted); }
.fleet-card__name-wrap { min-width: 0; }
.fleet-card__name { font-size: 1.05rem; font-weight: 750; }
.fleet-card__url { overflow: hidden; color: var(--np-text-muted); font-size: 0.76rem; text-overflow: ellipsis; white-space: nowrap; }
.fleet-card__metrics { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; padding: 18px 0; }
.fleet-metric { display: grid; gap: 3px; }
.fleet-metric span { color: var(--np-text-muted); font-size: 0.75rem; }
.fleet-metric strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.93rem; }
.fleet-card__footer { justify-content: space-between; gap: 10px; min-width: 0; color: var(--np-text-muted); font-size: 0.8rem; }
.fleet-core-state { display: inline-flex; align-items: center; gap: 5px; }
.fleet-core-state.is-running { color: #22c55e; }
.fleet-card__error { overflow: hidden; color: #fb7185; text-overflow: ellipsis; white-space: nowrap; }
.fleet-card__actions { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--np-border); }
.fleet-detail__status { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; color: var(--np-text-muted); font-size: 0.82rem; }
.fleet-detail__grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-top: 18px; }
.fleet-detail__grid > div { display: grid; gap: 4px; padding: 12px; border: 1px solid var(--np-border); border-radius: 14px; background: var(--np-surface-muted); }
.fleet-detail__grid span, .fleet-detail__log-head { color: var(--np-text-muted); font-size: 0.76rem; }
.fleet-detail__grid strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fleet-detail__log-head { display: flex; align-items: center; justify-content: space-between; margin-top: 18px; }
.fleet-detail__logs { max-height: 260px; margin: 8px 0 0; padding: 14px; overflow: auto; border: 1px solid var(--np-border); border-radius: 14px; background: var(--np-surface-muted); color: var(--np-text); white-space: pre-wrap; word-break: break-word; font: 0.76rem/1.55 ui-monospace, SFMono-Regular, Menlo, monospace; }
.fleet-config-compare { margin-top: 18px; }
.fleet-config-snapshot { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; margin-top: 8px; }
.fleet-config-snapshot > div { display: grid; gap: 4px; padding: 11px 12px; border: 1px solid var(--np-border); border-radius: 14px; background: var(--np-surface-muted); }
.fleet-config-snapshot span, .fleet-drift-item span, .fleet-drift-item small { color: var(--np-text-muted); font-size: 0.75rem; }
.fleet-config-snapshot strong { overflow-wrap: anywhere; font-size: 0.86rem; }
.fleet-drift-list { display: grid; gap: 8px; margin-top: 10px; }
.fleet-drift-item { display: grid; grid-template-columns: minmax(110px, .8fr) minmax(90px, 1fr) auto minmax(120px, 1fr); align-items: center; gap: 8px; padding: 10px 12px; border: 1px solid rgba(249, 115, 22, .22); border-radius: 13px; background: rgba(249, 115, 22, .07); }

@media (max-width: 600px) {
  .fleet-detail__grid, .fleet-config-snapshot { grid-template-columns: 1fr; }
  .fleet-drift-item { grid-template-columns: 1fr auto; }
  .fleet-drift-item span, .fleet-drift-item strong { grid-column: 1 / -1; }
  .fleet-card__actions .v-btn { flex: 1 1 auto; }
}

.fleet-dialog__title { display: flex; align-items: center; justify-content: space-between; }
.fleet-config-row { display: grid; grid-template-columns: 0.8fr 1.5fr 1.3fr auto auto; align-items: center; gap: 10px; padding: 10px 0; }
.fleet-dialog__add { margin-top: 8px; }

@media (max-width: 800px) {
  .fleet-hero { padding: 18px; }
  .fleet-hero__actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); width: 100%; }
  .fleet-hero__actions .v-btn { width: 100%; min-width: 0; }
  .fleet-hero__meta { flex-wrap: wrap; }
  .fleet-config-row { grid-template-columns: 1fr; padding: 14px 0; border-bottom: 1px solid var(--np-border); }
}

@media (max-width: 1279px) {
  .fleet-summary { grid-template-columns: repeat(4, minmax(0, 1fr)); }
}

@media (max-width: 599px) {
  .fleet-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .fleet-summary__col--last { grid-column: 1 / -1; }
}
</style>
