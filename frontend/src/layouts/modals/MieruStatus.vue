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
                <strong>{{ status.running ? 'mita 正在提供服务' : 'mita 未启动' }}</strong>
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
              <div class="mieru-status-details__wide"><span>程序路径</span><strong>{{ status.binary || '-' }}</strong></div>
            </div>
          </section>

          <section class="mieru-status-section">
            <div class="mieru-status-section__heading">
              <div><span>SESSIONS</span><h4>连接与用户</h4></div>
            </div>
            <div class="mieru-status-output">
              <div><span>活动连接</span><pre>{{ status.connections || '当前没有活动连接' }}</pre></div>
              <div><span>用户流量</span><pre>{{ status.users || '暂无用户统计' }}</pre></div>
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

export default {
  props: ['data', 'visible'],
  data() {
    return {
      loading: false,
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
  },
  methods: {
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
.mieru-status-section {
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
  .mieru-status-output {
    grid-template-columns: 1fr;
  }

  .mieru-status-details__wide {
    grid-column: auto;
  }
}
</style>
