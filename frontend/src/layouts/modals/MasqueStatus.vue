<template>
  <v-dialog transition="dialog-bottom-transition" width="920" scrollable>
    <v-card class="masque-dialog" :loading="loading">
      <v-card-title class="masque-dialog__header">
        <div>
          <div class="masque-dialog__eyebrow">HTTP/3 CONNECT-IP</div>
          <div class="masque-dialog__title">MASQUE 运行诊断</div>
        </div>
        <div class="masque-dialog__header-actions">
          <v-chip :color="status.running ? 'success' : 'error'" variant="tonal" size="small">
            {{ status.running ? '服务运行中' : '服务未运行' }}
          </v-chip>
          <v-btn icon="mdi-refresh" variant="text" :loading="loading" aria-label="刷新" @click="load" />
          <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="$emit('close')" />
        </div>
      </v-card-title>

      <v-card-text class="masque-status">
        <v-alert v-if="alert" type="warning" variant="tonal" class="mb-4" :text="alert"></v-alert>
        <template v-else>
          <section class="masque-hero">
            <div>
              <div class="masque-hero__label">节点</div>
              <h3>{{ status.tag || '-' }}</h3>
              <p>{{ status.host || '-' }}:{{ status.port || '-' }} · {{ status.network || 'quic' }}</p>
            </div>
            <div class="masque-hero__session" :class="{ 'masque-hero__session--online': status.session_active }">
              <span class="masque-hero__pulse"></span>
              <div>
                <strong>{{ status.session_active ? '客户端已连接' : '等待客户端' }}</strong>
                <small>{{ status.client_addr || '暂无活动 CONNECT-IP 会话' }}</small>
              </div>
            </div>
          </section>

          <section class="masque-metrics">
            <article class="masque-metric">
              <span>当前会话</span>
              <strong>{{ status.session_active ? `#${status.session_id}` : '-' }}</strong>
              <small>{{ formatDuration(status.session_uptime_seconds) }}</small>
            </article>
            <article class="masque-metric">
              <span>客户端上传</span>
              <strong>{{ formatBytes(status.rx_bytes) }}</strong>
              <small>{{ formatPackets(status.rx_packets) }}</small>
            </article>
            <article class="masque-metric">
              <span>客户端下载</span>
              <strong>{{ formatBytes(status.tx_bytes) }}</strong>
              <small>{{ formatPackets(status.tx_packets) }}</small>
            </article>
            <article class="masque-metric">
              <span>累计连接</span>
              <strong>{{ status.total_sessions ?? 0 }}</strong>
              <small>接管 {{ status.takeover_count ?? 0 }} 次</small>
            </article>
          </section>

          <v-alert
            v-if="status.start_error || status.last_error"
            type="error"
            variant="tonal"
            class="mb-4"
            :title="status.start_error ? '节点启动失败' : '最近一次会话异常'"
            :text="status.start_error || status.last_error"
          ></v-alert>

          <section class="masque-section">
            <div class="masque-section__heading">
              <div>
                <span>运行检查</span>
                <h4>逐项诊断</h4>
              </div>
              <small>每 3 秒自动刷新</small>
            </div>
            <div class="masque-diagnostics">
              <article v-for="item in status.diagnostics || []" :key="item.id" class="masque-diagnostic">
                <span class="masque-diagnostic__dot" :class="`masque-diagnostic__dot--${item.status}`"></span>
                <div>
                  <strong>{{ item.title }}</strong>
                  <small>{{ item.detail }}</small>
                </div>
                <v-chip :color="diagnosticColor(item.status)" variant="tonal" size="x-small">
                  {{ diagnosticLabel(item.status) }}
                </v-chip>
              </article>
            </div>
          </section>

          <section class="masque-section">
            <div class="masque-section__heading">
              <div>
                <span>TLS 与监听</span>
                <h4>证书热重载</h4>
              </div>
              <v-chip color="primary" variant="tonal" size="small">
                已重载 {{ status.cert_reload_count ?? 0 }} 次
              </v-chip>
            </div>
            <div class="masque-details">
              <div><span>监听地址</span><strong>{{ status.bind_addr || '-' }}</strong></div>
              <div><span>证书来源</span><strong>{{ certSource(status.cert_source) }}</strong></div>
              <div><span>证书有效期</span><strong>{{ formatDate(status.cert_not_after) }}</strong></div>
              <div><span>最近重载</span><strong>{{ formatDate(status.cert_last_reload_at) }}</strong></div>
              <div class="masque-details__wide"><span>证书文件</span><strong>{{ status.cert_file || '节点内置证书' }}</strong></div>
              <div class="masque-details__wide"><span>密钥文件</span><strong>{{ status.key_file || '节点私钥动态生成' }}</strong></div>
            </div>
            <v-alert
              v-if="status.cert_reload_error || status.cert_error"
              type="error"
              variant="tonal"
              class="mt-4"
              title="证书加载异常"
              :text="status.cert_reload_error || status.cert_error"
            ></v-alert>
          </section>
        </template>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import HttpUtils from '@/plugins/httputil'

type MasqueDiagnostic = {
  id: string
  status: 'ok' | 'warning' | 'error' | 'info'
  title: string
  detail: string
}

type MasqueStatus = {
  tag?: string
  host?: string
  port?: number
  network?: string
  running?: boolean
  bind_addr?: string
  session_active?: boolean
  session_id?: number
  session_uptime_seconds?: number
  client_addr?: string
  total_sessions?: number
  takeover_count?: number
  rx_bytes?: number
  tx_bytes?: number
  rx_packets?: number
  tx_packets?: number
  start_error?: string
  last_error?: string
  cert_file?: string
  key_file?: string
  cert_source?: string
  cert_not_after?: string
  cert_last_reload_at?: string
  cert_reload_count?: number
  cert_reload_error?: string
  cert_error?: string
  diagnostics?: MasqueDiagnostic[]
}

export default {
  props: ['data', 'visible'],
  data() {
    return {
      loading: false,
      alert: '',
      status: <MasqueStatus>{},
      refreshTimer: <number | null>null,
    }
  },
  beforeUnmount() {
    this.stopPolling()
  },
  methods: {
    async load() {
      if (this.loading) return
      const tag = String(this.$props.data?.tag ?? '').trim()
      if (!tag) {
        this.alert = '缺少 MASQUE 节点标识'
        this.status = {}
        return
      }
      this.loading = true
      this.alert = ''
      const resp = await HttpUtils.get('api/masque-status', { tag })
      if (resp.success && resp.obj) {
        this.status = resp.obj as MasqueStatus
      } else {
        this.status = {}
        this.alert = resp.msg || '加载 MASQUE 状态失败'
      }
      this.loading = false
    },
    startPolling() {
      this.stopPolling()
      this.load()
      this.refreshTimer = window.setInterval(() => this.load(), 3000)
    },
    stopPolling() {
      if (this.refreshTimer !== null) {
        window.clearInterval(this.refreshTimer)
        this.refreshTimer = null
      }
    },
    reset() {
      this.stopPolling()
      this.status = {}
      this.alert = ''
      this.loading = false
    },
    formatBytes(value?: number) {
      const bytes = Number(value ?? 0)
      if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
      const units = ['B', 'KB', 'MB', 'GB', 'TB']
      const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
      return `${(bytes / Math.pow(1024, index)).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
    },
    formatPackets(value?: number) {
      return `${Number(value ?? 0).toLocaleString()} 个数据包`
    },
    formatDuration(value?: number) {
      const seconds = Math.max(0, Number(value ?? 0))
      if (!seconds) return '当前未连接'
      if (seconds < 60) return `${Math.floor(seconds)} 秒`
      if (seconds < 3600) return `${Math.floor(seconds / 60)} 分 ${Math.floor(seconds % 60)} 秒`
      return `${Math.floor(seconds / 3600)} 时 ${Math.floor((seconds % 3600) / 60)} 分`
    },
    formatDate(value?: string) {
      if (!value) return '-'
      const date = new Date(value)
      return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
    },
    diagnosticColor(status: string) {
      return ({ ok: 'success', warning: 'warning', error: 'error', info: 'info' } as Record<string, string>)[status] || 'info'
    },
    diagnosticLabel(status: string) {
      return ({ ok: '正常', warning: '注意', error: '异常', info: '信息' } as Record<string, string>)[status] || status
    },
    certSource(source?: string) {
      return source === 'file' ? '证书文件' : source === 'endpoint-key' ? '节点内置证书' : source || '-'
    },
  },
  watch: {
    visible(v: boolean) {
      if (v) {
        this.startPolling()
      } else {
        this.reset()
      }
    },
  },
}
</script>

<style scoped>
.masque-dialog {
  border-radius: 28px !important;
  overflow: hidden;
  color: rgb(var(--v-theme-on-surface));
  background:
    radial-gradient(circle at 92% 2%, rgba(var(--v-theme-primary), .13), transparent 34%),
    rgb(var(--v-theme-surface));
}

.masque-dialog__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 22px 24px 16px;
}

.masque-dialog__eyebrow,
.masque-section__heading span,
.masque-hero__label {
  color: rgb(var(--v-theme-primary));
  font-size: 10px;
  font-weight: 800;
  letter-spacing: .13em;
  text-transform: uppercase;
}

.masque-dialog__title {
  margin-top: 3px;
  font-size: 22px;
  font-weight: 800;
}

.masque-dialog__header-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.masque-status {
  display: grid;
  gap: 16px;
  padding: 8px 24px 24px;
}

.masque-hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 20px;
  border: 1px solid rgba(var(--v-theme-outline), .14);
  border-radius: 22px;
  background: rgba(var(--v-theme-surface-variant), .18);
}

.masque-hero h3 {
  margin: 4px 0 2px;
  font-size: 24px;
}

.masque-hero p {
  margin: 0;
  color: rgba(var(--v-theme-on-surface), .58);
}

.masque-hero__session {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 220px;
  padding: 12px 14px;
  border-radius: 16px;
  background: rgba(var(--v-theme-on-surface), .05);
}

.masque-hero__session div {
  display: grid;
}

.masque-hero__session small {
  margin-top: 3px;
  color: rgba(var(--v-theme-on-surface), .55);
}

.masque-hero__pulse {
  width: 10px;
  height: 10px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: rgb(var(--v-theme-outline));
}

.masque-hero__session--online .masque-hero__pulse {
  background: rgb(var(--v-theme-success));
  box-shadow: 0 0 0 5px rgba(var(--v-theme-success), .13);
}

.masque-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.masque-metric {
  display: grid;
  min-height: 108px;
  padding: 15px;
  border: 1px solid rgba(var(--v-theme-outline), .13);
  border-radius: 18px;
  background: rgba(var(--v-theme-surface), .72);
}

.masque-metric span,
.masque-details span {
  color: rgba(var(--v-theme-on-surface), .52);
  font-size: 12px;
}

.masque-metric strong {
  align-self: end;
  font-size: 21px;
}

.masque-metric small {
  color: rgba(var(--v-theme-on-surface), .5);
}

.masque-section {
  padding: 18px;
  border: 1px solid rgba(var(--v-theme-outline), .13);
  border-radius: 22px;
  background: rgba(var(--v-theme-surface), .64);
}

.masque-section__heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.masque-section__heading h4 {
  margin: 2px 0 0;
  font-size: 18px;
}

.masque-section__heading small {
  color: rgba(var(--v-theme-on-surface), .48);
}

.masque-diagnostics {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.masque-diagnostic {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 12px;
  border-radius: 15px;
  background: rgba(var(--v-theme-surface-variant), .18);
}

.masque-diagnostic div {
  display: grid;
}

.masque-diagnostic small {
  margin-top: 2px;
  color: rgba(var(--v-theme-on-surface), .52);
  word-break: break-word;
}

.masque-diagnostic__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: rgb(var(--v-theme-info));
}

.masque-diagnostic__dot--ok { background: rgb(var(--v-theme-success)); }
.masque-diagnostic__dot--warning { background: rgb(var(--v-theme-warning)); }
.masque-diagnostic__dot--error { background: rgb(var(--v-theme-error)); }

.masque-details {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.masque-details > div {
  display: grid;
  gap: 5px;
  padding: 12px;
  border-radius: 14px;
  background: rgba(var(--v-theme-surface-variant), .18);
}

.masque-details strong {
  overflow-wrap: anywhere;
  font-size: 13px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.masque-details__wide {
  grid-column: 1 / -1;
}

@media (max-width: 720px) {
  .masque-dialog {
    border-radius: 22px 22px 0 0 !important;
  }

  .masque-dialog__header {
    align-items: flex-start;
    padding: 18px 16px 12px;
  }

  .masque-dialog__header-actions > .v-chip {
    display: none;
  }

  .masque-status {
    padding: 6px 14px max(18px, env(safe-area-inset-bottom));
  }

  .masque-hero {
    align-items: stretch;
    flex-direction: column;
    padding: 16px;
  }

  .masque-hero__session {
    min-width: 0;
  }

  .masque-metrics,
  .masque-diagnostics,
  .masque-details {
    grid-template-columns: 1fr 1fr;
  }

  .masque-metric {
    min-height: 96px;
  }

  .masque-diagnostic {
    grid-column: 1 / -1;
  }
}
</style>
