<template>
  <v-card class="mieru-editor" subtitle="Mieru / mita">
    <v-card-text>
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
            @update:model-value="syncRangeStart"
          />
        </v-col>

        <v-col cols="12" sm="6" md="4">
          <v-select v-model="data.multiplexing" :items="multiplexingItems" label="多路复用" hide-details />
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

</style>
