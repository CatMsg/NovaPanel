<template>
  <v-card class="mieru-editor" subtitle="Mieru / mita">
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="6" md="4">
          <v-select v-model="data.transport" :items="transportItems" label="传输协议" hide-details />
        </v-col>
        <v-col v-if="optionPortRange" cols="12" sm="6" md="8">
          <v-text-field
            v-model.trim="data.port_range"
            label="连续端口范围"
            placeholder="443-452"
            hint="客户端从范围内选择公网端口，NovaPanel 会统一转发到上方监听端口"
            persistent-hint
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
            hide-details
          />
        </v-col>
      </v-row>

      <v-alert
        v-if="privilegedListenPort"
        class="mt-4"
        type="warning"
        variant="tonal"
        density="compact"
      >
        当前监听端口属于 Linux 特权端口；NovaPanel 以系统服务运行时可以使用，但仍需避免与同传输层服务冲突。
      </v-alert>
    </v-card-text>
    <v-card-actions class="pt-0">
      <v-spacer />
      <v-menu v-model="menu" :close-on-content-click="false" location="start">
        <template #activator="{ props }">
          <v-btn v-bind="props" variant="tonal">Mieru 选项</v-btn>
        </template>
        <v-card>
          <v-list>
            <v-list-item>
              <v-switch v-model="optionPortRange" color="primary" label="连续端口范围" hide-details />
            </v-list-item>
          </v-list>
        </v-card>
      </v-menu>
    </v-card-actions>
  </v-card>
</template>

<script lang="ts">
export default {
  props: ['data'],
  data() {
    return {
      menu: false,
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
    privilegedListenPort(): boolean {
      const port = Number(this.$props.data.listen_port ?? 0)
      return Number.isInteger(port) && port >= 1 && port < 1024
    },
    optionPortRange: {
      get(): boolean {
        return Boolean(String(this.$props.data.port_range ?? '').trim())
      },
      set(enabled: boolean) {
        if (enabled) {
          const port = Number(this.$props.data.listen_port ?? 0)
          this.$props.data.port_range = port > 0 ? `${port}-${Math.min(port + 9, 65535)}` : ''
          return
        }
        delete this.$props.data.port_range
      },
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

</style>
