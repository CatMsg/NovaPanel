<template>
  <v-dialog transition="dialog-bottom-transition" width="900" scrollable>
    <v-card class="mieru-status-dialog" :loading="loading">
      <v-card-title class="mieru-status-dialog__header">
        <div>
          <div class="mieru-status-dialog__eyebrow">MIERU / MITA</div>
          <div class="mieru-status-dialog__title">Mieru 运行状态</div>
        </div>
        <div class="mieru-status-dialog__actions">
          <v-chip :color="status.running ? 'success' : 'error'" variant="tonal" size="small">
            {{ status.running ? '运行中' : '未运行' }}
          </v-chip>
          <v-btn
            color="warning"
            variant="tonal"
            size="small"
            prepend-icon="mdi-bug-outline"
            :loading="debugLoading"
            :disabled="Boolean(status.debug_active)"
            @click="enableDebug"
          >
            {{ status.debug_active ? debugLabel : '调试 10 分钟' }}
          </v-btn>
          <v-btn icon="mdi-refresh" variant="text" :loading="loading" aria-label="刷新" @click="load" />
          <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="$emit('close')" />
        </div>
      </v-card-title>
      <v-card-text class="mieru-status-dialog__body">
        <v-alert v-if="alert" type="warning" variant="tonal" :text="alert" />
        <template v-else>
          <section class="mieru-status-hero">
            <div>
              <span>节点</span>
              <h3>{{ status.tag || '-' }}</h3>
              <p>{{ status.server || '-' }} · {{ endpointPort }} · {{ status.transport || 'TCP' }}</p>
            </div>
            <div class="mieru-status-hero__state">
              <span class="mieru-status-hero__dot" :class="{ 'mieru-status-hero__dot--online': status.running }" />
              <div>
                <strong>{{ status.running ? '共享 mita 服务运行中' : 'mita 未启动' }}</strong>
                <small>{{ status.status || status.platform_note || '等待状态信息' }}</small>
              </div>
            </div>
          </section>

          <section class="mieru-status-metrics">
            <article><span>运行时间</span><strong>{{ formatDuration(status.uptime_seconds) }}</strong></article>
            <article><span>节点数量</span><strong>{{ status.endpoint_count ?? 0 }}</strong></article>
            <article><span>多路复用</span><strong>{{ compactMode(status.multiplexing) }}</strong></article>
            <article><span>握手模式</span><strong>{{ status.handshake_mode === 'HANDSHAKE_NO_WAIT' ? '0-RTT' : '标准' }}</strong></article>
          </section>

          <section class="mieru-status-metrics mieru-status-metrics--runtime">
            <article><span>当前连接</span><strong>{{ runtimeMetric('active_connections') }}</strong></article>
            <article><span>底层连接</span><strong>{{ runtimeMetric('underlay_connections') }}</strong></article>
            <article><span>解密失败</span><strong>{{ runtimeMetric('failed_decrypt') }}</strong></article>
            <article><span>异常 UDP</span><strong>{{ runtimeMetric('unsolicited_udp') }}</strong></article>
          </section>

          <v-alert
            v-if="status.last_error"
            type="error"
            variant="tonal"
            title="最近运行错误"
            :text="status.last_error"
          />

          <section class="mieru-status-section">
            <div class="mieru-status-section__heading">
              <div><span>SERVER</span><h4>运行信息</h4></div>
              <v-chip color="primary" variant="tonal" size="small">{{ shortVersion }}</v-chip>
            </div>
            <div class="mieru-status-details">
              <div><span>用户名</span><strong>{{ status.username || '-' }}</strong></div>
              <div><span>UDP MTU</span><strong>{{ status.mtu || '-' }}</strong></div>
              <div><span>节点模式</span><strong>{{ trafficPatternLabel }}</strong></div>
              <div><span>服务端模式</span><strong>{{ trafficPatternName(status.server_traffic_pattern) }}</strong></div>
              <div><span>1 天配额</span><strong>{{ quotaLabel(status.quota_1d_gb) }}</strong></div>
              <div><span>30 天配额</span><strong>{{ quotaLabel(status.quota_30d_gb) }}</strong></div>
              <div class="mieru-status-details__wide"><span>程序路径</span><strong>{{ status.binary || '-' }}</strong></div>
            </div>
          </section>

          <section class="mieru-status-section">
            <div class="mieru-status-section__heading">
              <div><span>ENDPOINT USER</span><h4>当前节点流量</h4></div>
              <v-chip color="primary" variant="tonal" size="small">{{ status.username || '-' }}</v-chip>
            </div>
            <div class="mieru-status-traffic">
              <div><span>最后活跃</span><strong>{{ userStat('last_active') }}</strong></div>
              <div><span>1 天下载</span><strong>{{ userStat('day_download') }}</strong></div>
              <div><span>1 天上传</span><strong>{{ userStat('day_upload') }}</strong></div>
              <div><span>7 天下载 / 上传</span><strong>{{ userStat('week_download') }} / {{ userStat('week_upload') }}</strong></div>
              <div><span>30 天下载</span><strong>{{ userStat('month_download') }}</strong></div>
              <div><span>30 天上传</span><strong>{{ userStat('month_upload') }}</strong></div>
            </div>
          </section>

          <section class="mieru-status-section">
            <div class="mieru-status-section__heading">
              <div><span>SHARED SESSIONS</span><h4>mita 全局连接</h4></div>
            </div>
            <div class="mieru-status-output mieru-status-output--single">
              <div><pre>{{ status.connections || '当前没有活动连接' }}</pre></div>
            </div>
          </section>

          <section class="mieru-status-section">
            <div class="mieru-status-section__heading">
              <div><span>LOGS</span><h4>最近日志</h4></div>
            </div>
            <pre class="mieru-status-logs">{{ recentLogs }}</pre>
          </section>
        </template>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import HttpUtils from '@/plugins/httputil'
import { push } from 'notivue'

export default {
  props: ['data', 'visible'],
  data() {
    return {
      loading: false,
      debugLoading: false,
      alert: '',
      status: <Record<string, any>>{},
    }
  },
  computed: {
    endpointPort() {
      return this.status.port_range || this.status.port || '-'
    },
    shortVersion() {
      const version = String(this.status.version ?? '').trim()
      return version ? version.split('\n')[0] : '版本未知'
    },
    recentLogs() {
      const logs = Array.isArray(this.status.logs) ? this.status.logs : []
      return logs.length ? logs.slice(-30).join('\n') : '暂无运行日志'
    },
    debugLabel() {
      const seconds = Math.max(0, Number(this.status.debug_remaining_seconds ?? 0))
      return seconds > 0 ? `调试中 ${Math.ceil(seconds / 60)} 分` : '调试中'
    },
    trafficPatternLabel() {
      return this.trafficPatternName(this.status.traffic_pattern)
    },
  },
  methods: {
    trafficPatternName(value?: string) {
      const labels: Record<string, string> = {
        DEFAULT: '默认',
        BALANCED: '均衡',
        ENHANCED: '增强伪装',
        CUSTOM: '自定义',
      }
      return labels[String(value ?? 'DEFAULT')] ?? '默认'
    },
    async load() {
      if (this.loading) return
      const tag = String(this.$props.data?.tag ?? '').trim()
      if (!tag) {
        this.alert = '缺少 Mieru 节点标识'
        return
      }
      this.loading = true
      this.alert = ''
      const response = await HttpUtils.get('api/mieru-status', { tag })
      if (response.success && response.obj) {
        this.status = response.obj
      } else {
        this.status = {}
        this.alert = response.msg || '加载 Mieru 状态失败'
      }
      this.loading = false
    },
    async enableDebug() {
      if (this.debugLoading || this.status.debug_active) return
      this.debugLoading = true
      const response = await HttpUtils.post('api/mieruDebug', null)
      if (response.success) {
        push.success({ message: 'Mieru DEBUG 日志已开启，10 分钟后自动恢复' })
        await this.load()
      }
      this.debugLoading = false
    },
    formatDuration(value?: number) {
      const seconds = Math.max(0, Number(value ?? 0))
      if (!seconds) return '-'
      if (seconds < 60) return `${Math.floor(seconds)} 秒`
      if (seconds < 3600) return `${Math.floor(seconds / 60)} 分`
      return `${Math.floor(seconds / 3600)} 时 ${Math.floor((seconds % 3600) / 60)} 分`
    },
    compactMode(value?: string) {
      return String(value ?? 'MULTIPLEXING_LOW').replace('MULTIPLEXING_', '')
    },
    runtimeMetric(name: string) {
      const value = this.status.metrics?.[name]
      return Number.isFinite(Number(value)) ? Number(value).toLocaleString() : '-'
    },
    userStat(name: string) {
      return String(this.status.user_stats?.[name] ?? '').trim() || '-'
    },
    quotaLabel(value?: number) {
      const quota = Number(value ?? 0)
      return quota > 0 ? `${quota.toLocaleString()} GB` : '不限量'
    },
  },
  watch: {
    visible(value: boolean) {
      if (value) {
        this.load()
      } else {
        this.status = {}
        this.alert = ''
      }
    },
  },
}
</script>

<style scoped>
.mieru-status-dialog {
  overflow: hidden;
  border-radius: 28px !important;
  color: rgb(var(--v-theme-on-surface));
  background:
    radial-gradient(circle at 92% 2%, rgba(var(--v-theme-primary), .13), transparent 34%),
    rgb(var(--v-theme-surface));
}

.mieru-status-dialog__header,
.mieru-status-dialog__actions,
.mieru-status-hero,
.mieru-status-section__heading {
  display: flex;
  align-items: center;
}

.mieru-status-dialog__header {
  justify-content: space-between;
  gap: 16px;
  padding: 22px 24px 16px;
}

.mieru-status-dialog__actions {
  gap: 4px;
}

.mieru-status-dialog__eyebrow,
.mieru-status-section__heading span,
.mieru-status-hero > div > span {
  color: rgb(var(--v-theme-primary));
  font-size: 10px;
  font-weight: 800;
  letter-spacing: .13em;
}

.mieru-status-dialog__title {
  margin-top: 3px;
  font-size: 22px;
  font-weight: 800;
}

.mieru-status-dialog__body {
  display: grid;
  gap: 14px;
  padding: 8px 24px 24px;
}

.mieru-status-hero {
  justify-content: space-between;
  gap: 20px;
  padding: 20px;
  border: 1px solid rgba(var(--v-theme-outline), .14);
  border-radius: 22px;
  background: rgba(var(--v-theme-surface-variant), .18);
}

.mieru-status-hero h3 {
  margin: 4px 0 2px;
  font-size: 24px;
}

.mieru-status-hero p,
.mieru-status-hero__state small {
  margin: 0;
  color: rgba(var(--v-theme-on-surface), .58);
}

.mieru-status-hero__state {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 230px;
  padding: 12px 14px;
  border-radius: 16px;
  background: rgba(var(--v-theme-on-surface), .05);
}

.mieru-status-hero__state div {
  display: grid;
}

.mieru-status-hero__dot {
  width: 10px;
  height: 10px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: rgb(var(--v-theme-error));
}

.mieru-status-hero__dot--online {
  background: rgb(var(--v-theme-success));
  box-shadow: 0 0 0 5px rgba(var(--v-theme-success), .13);
}

.mieru-status-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.mieru-status-metrics article,
.mieru-status-section,
.mieru-status-traffic div {
  border: 1px solid rgba(var(--v-theme-outline), .13);
  background: rgba(var(--v-theme-surface), .68);
}

.mieru-status-metrics article {
  display: grid;
  min-height: 96px;
  padding: 15px;
  border-radius: 18px;
}

.mieru-status-metrics span,
.mieru-status-details span,
.mieru-status-output span {
  color: rgba(var(--v-theme-on-surface), .52);
  font-size: 12px;
}

.mieru-status-metrics strong {
  align-self: end;
  overflow-wrap: anywhere;
  font-size: 18px;
}

.mieru-status-metrics--runtime article {
  min-height: 82px;
  background: rgba(var(--v-theme-primary), .045);
}

.mieru-status-section {
  padding: 18px;
  border-radius: 22px;
}

.mieru-status-section__heading {
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.mieru-status-section__heading h4 {
  margin: 2px 0 0;
  font-size: 18px;
}

.mieru-status-details {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.mieru-status-details div {
  display: grid;
  gap: 4px;
  padding: 11px 12px;
  border-radius: 14px;
  background: rgba(var(--v-theme-on-surface), .04);
}

.mieru-status-details__wide {
  grid-column: 1 / -1;
}

.mieru-status-details strong {
  overflow-wrap: anywhere;
}

.mieru-status-output {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.mieru-status-output--single {
  grid-template-columns: 1fr;
}

.mieru-status-traffic {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.mieru-status-traffic div {
  display: grid;
  gap: 7px;
  min-width: 0;
  padding: 12px;
  border-radius: 14px;
}

.mieru-status-traffic span {
  color: rgba(var(--v-theme-on-surface), .52);
  font-size: 12px;
}

.mieru-status-traffic strong {
  overflow-wrap: anywhere;
  font-size: 14px;
}

.mieru-status-output div {
  min-width: 0;
}

.mieru-status-output pre,
.mieru-status-logs {
  overflow: auto;
  max-height: 220px;
  margin: 6px 0 0;
  padding: 12px;
  border-radius: 14px;
  color: rgba(var(--v-theme-on-surface), .82);
  background: rgba(var(--v-theme-on-surface), .05);
  font: 11px/1.55 ui-monospace, SFMono-Regular, Menlo, monospace;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

@media (max-width: 700px) {
  .mieru-status-dialog__header,
  .mieru-status-hero {
    align-items: flex-start;
    flex-direction: column;
  }

  .mieru-status-dialog__header {
    padding: 18px 16px 12px;
  }

  .mieru-status-dialog__actions {
    width: 100%;
    justify-content: flex-end;
  }

  .mieru-status-dialog__body {
    padding: 6px 16px max(18px, env(safe-area-inset-bottom));
  }

  .mieru-status-hero__state {
    width: 100%;
    min-width: 0;
  }

  .mieru-status-metrics,
  .mieru-status-details,
  .mieru-status-output,
  .mieru-status-traffic {
    grid-template-columns: 1fr;
  }

  .mieru-status-details__wide {
    grid-column: auto;
  }
}
</style>
