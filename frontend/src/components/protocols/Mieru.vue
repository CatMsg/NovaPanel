<template>
  <v-card class="mieru-editor" subtitle="Mieru / mita">
    <v-card-text>
      <v-alert
        type="info"
        variant="tonal"
        density="compact"
        class="mb-4"
        text="每台服务器只运行一个 Mieru 入站和一个共享 mita 进程。用户由下方用户列表绑定，流量与到期限制沿用用户管理中的配置。"
      />

      <v-row>
        <v-col cols="12" sm="6" md="4">
          <v-select v-model="data.transport" :items="transportItems" label="传输协议" hide-details />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-switch v-model="usePortRange" color="primary" label="使用连续端口范围" hide-details />
        </v-col>
        <v-col v-if="usePortRange" cols="12" md="4">
          <v-text-field
            v-model.trim="data.port_range"
            label="端口范围"
            placeholder="20000-20020"
            hint="起始值会同步为监听端口，最多连续 512 个端口"
            persistent-hint
            @update:model-value="syncRangeStart"
          />
        </v-col>
        <v-col v-else cols="12" md="4" class="mieru-editor__port-note">
          <span>监听端口</span>
          <strong>{{ data.listen_port || '-' }}</strong>
          <small>单端口模式使用上方“端口”字段。</small>
        </v-col>

        <v-col cols="12" sm="6" md="4">
          <v-select v-model="data.multiplexing" :items="multiplexingItems" label="多路复用" hide-details />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-select v-model="data.handshake_mode" :items="handshakeItems" label="握手模式" hide-details />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field
            v-model.number="data.mtu"
            label="MTU"
            type="number"
            min="1280"
            max="1500"
            hide-details
          />
        </v-col>
        <v-col cols="12">
          <v-select
            v-model="data.traffic_pattern"
            :items="trafficPatternItems"
            label="流量模式"
            hint="增强伪装会增加少量延迟和带宽开销"
            persistent-hint
          />
        </v-col>
      </v-row>

      <v-alert
        type="warning"
        variant="tonal"
        density="compact"
        class="mt-4"
        text="客户端与服务器时间差需小于 4 分钟。建议保持 NTP 同步；0-RTT 会降低重连时延，但存在重放风险。"
      />
    </v-card-text>
  </v-card>
</template>

<script lang="ts">
export default {
  props: ['data'],
  data() {
    return {
      transportItems: [
        { title: 'TCP（推荐）', value: 'TCP' },
        { title: 'UDP', value: 'UDP' },
      ],
      multiplexingItems: [
        { title: '低（推荐）', value: 'MULTIPLEXING_LOW' },
        { title: '关闭', value: 'MULTIPLEXING_OFF' },
        { title: '中', value: 'MULTIPLEXING_MIDDLE' },
        { title: '高', value: 'MULTIPLEXING_HIGH' },
      ],
      handshakeItems: [
        { title: '标准握手（推荐）', value: 'HANDSHAKE_STANDARD' },
        { title: '0-RTT', value: 'HANDSHAKE_NO_WAIT' },
      ],
      trafficPatternItems: [
        { title: '默认（性能优先）', value: 'DEFAULT' },
        { title: '均衡', value: 'BALANCED' },
        { title: '增强伪装', value: 'ENHANCED' },
      ],
    }
  },
  computed: {
    usePortRange: {
      get(): boolean {
        return Boolean(String(this.$props.data.port_range ?? '').trim())
      },
      set(enabled: boolean) {
        if (enabled) {
          const port = Number(this.$props.data.listen_port ?? 0)
          this.$props.data.port_range = port > 0 ? `${port}-${Math.min(port + 9, 65535)}` : ''
          return
        }
        this.$props.data.port_range = ''
      },
    },
  },
  methods: {
    syncRangeStart(value: string) {
      const match = String(value ?? '').trim().match(/^(\d+)-(\d+)$/)
      if (!match) return
      const start = Number(match[1])
      if (Number.isInteger(start) && start >= 1025 && start <= 65535) {
        this.$props.data.listen_port = start
      }
    },
  },
}
</script>

<style scoped>
.mieru-editor {
  border: 1px solid rgba(var(--v-theme-primary), .14);
  background:
    radial-gradient(circle at 92% 4%, rgba(var(--v-theme-primary), .09), transparent 34%),
    rgba(var(--v-theme-surface), .72);
}

.mieru-editor__port-note {
  display: grid;
  align-content: center;
  gap: 2px;
  min-height: 56px;
  padding-inline: 20px;
  border: 1px solid rgba(var(--v-theme-on-surface), .12);
  border-radius: 12px;
}

.mieru-editor__port-note span,
.mieru-editor__port-note small {
  color: rgba(var(--v-theme-on-surface), .62);
}

.mieru-editor__port-note strong {
  font-size: 1rem;
}
</style>
