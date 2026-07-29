<template>
  <v-card class="mieru-editor" subtitle="Mieru / mita">
    <v-card-text>
      <v-alert
        type="info"
        variant="tonal"
        density="compact"
        class="mb-4"
        text="免域名证书的抗识别代理。客户端和服务器时间差需小于 4 分钟，建议保持 NTP 同步。"
      />
      <v-row>
        <v-col cols="12" md="6">
          <v-text-field v-model.trim="data.server" label="服务器域名或公网 IP" hide-details />
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-select v-model="data.transport" :items="transportItems" label="传输协议" hide-details />
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-switch v-model="usePortRange" color="primary" label="使用端口范围" hide-details />
        </v-col>
        <v-col v-if="!usePortRange" cols="12" sm="6" md="4">
          <v-text-field v-model.number="data.port" label="端口" type="number" min="1025" max="65535" hide-details />
        </v-col>
        <v-col v-else cols="12" sm="6" md="4">
          <v-text-field
            v-model.trim="data.port_range"
            label="端口范围"
            placeholder="20000-20020"
            hint="最多连续 512 个端口"
            persistent-hint
          />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model.trim="data.username" label="用户名" autocomplete="off" hide-details />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field
            v-model="data.password"
            label="密码"
            :type="showPassword ? 'text' : 'password'"
            append-inner-icon="mdi-key-outline"
            autocomplete="new-password"
            hide-details
            @click:append-inner="showPassword = !showPassword"
          />
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
            label="UDP MTU"
            type="number"
            min="1280"
            max="1500"
            :disabled="data.transport !== 'UDP'"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-select
            v-model="data.traffic_pattern"
            :items="trafficPatternItems"
            label="流量模式"
            hint="增强伪装会增加少量延迟和带宽开销"
            persistent-hint
          />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field
            v-model.number="data.quota_1d_gb"
            label="1 天流量上限"
            suffix="GB"
            type="number"
            min="0"
            hint="0 表示不限量"
            persistent-hint
          />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field
            v-model.number="data.quota_30d_gb"
            label="30 天流量上限"
            suffix="GB"
            type="number"
            min="0"
            hint="0 表示不限量"
            persistent-hint
          />
        </v-col>
      </v-row>

      <v-alert
        type="info"
        variant="tonal"
        density="compact"
        class="mt-4"
        text="多个 Mieru 节点共用 mita 服务；服务端采用最高级别的流量模式，订阅仍按每个节点各自设置输出。"
      />

      <v-divider class="my-4" />

      <div class="mieru-editor__config-head">
        <div>
          <span>MIHOMO CONFIG</span>
          <strong>OpenClash 节点配置</strong>
        </div>
        <v-btn color="primary" variant="tonal" size="small" prepend-icon="mdi-content-copy" @click="copyConfig">
          复制节点
        </v-btn>
      </div>
      <v-textarea :model-value="mieruConfig" rows="7" auto-grow readonly hide-details />
    </v-card-text>
  </v-card>
</template>

<script lang="ts">
import { push } from 'notivue'
import { buildMieruConfig } from '@/plugins/mieruUtil'

export default {
  props: ['data'],
  data() {
    return {
      showPassword: false,
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
          const port = Number(this.$props.data.port ?? 0)
          this.$props.data.port_range = port > 0 ? `${port}-${Math.min(port + 9, 65535)}` : ''
          this.$props.data.port = 0
        } else {
          const first = Number(String(this.$props.data.port_range ?? '').split('-')[0])
          this.$props.data.port = Number.isInteger(first) && first > 0 ? first : 0
          this.$props.data.port_range = ''
        }
      },
    },
    mieruConfig() {
      return buildMieruConfig(this.$props.data)
    },
  },
  methods: {
    async copyConfig() {
      try {
        await navigator.clipboard.writeText(this.mieruConfig)
        push.success({ message: '已复制 OpenClash 节点配置' })
      } catch {
        push.error({ message: '复制失败，请手动复制配置' })
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

.mieru-editor__config-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.mieru-editor__config-head div {
  display: grid;
  gap: 2px;
}

.mieru-editor__config-head span {
  color: rgb(var(--v-theme-primary));
  font-size: 10px;
  font-weight: 800;
  letter-spacing: .12em;
}

.mieru-editor__config-head strong {
  font-size: 15px;
}

@media (max-width: 600px) {
  .mieru-editor__config-head {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
