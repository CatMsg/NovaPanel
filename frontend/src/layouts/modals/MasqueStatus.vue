<template>
  <v-dialog width="720" scrollable>
    <v-card :loading="loading">
      <v-card-title class="d-flex align-center justify-space-between">
        原生 MASQUE 运行状态
        <div><v-btn icon="mdi-refresh" variant="text" :loading="loading" :aria-label="$t('ui.common.refresh')" @click="load" /><v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="$emit('close')" /></div>
      </v-card-title>
      <v-card-text>
        <v-alert v-if="alert" type="warning" variant="tonal">{{ alert }}</v-alert>
        <template v-else>
          <h3>{{ status.tag }}</h3>
          <p>{{ status.host }}:{{ status.port }} · HTTP/3 CONNECT-IP</p>
          <v-chip class="my-4" :color="status.running ? 'success' : 'warning'">{{ status.running ? '原生监听运行中' : '未启动监听' }}</v-chip>
          <dl class="native-details">
            <div><dt>有效授权用户</dt><dd>{{ status.configured_users ?? 0 }}</dd></div>
            <div><dt>活动 TCP/UDP 代理连接</dt><dd>{{ status.active_connections ?? 0 }}</dd></div>
            <div><dt>监听</dt><dd>{{ status.bind_addr }}</dd></div>
            <div><dt>服务端隧道地址 / 地址池</dt><dd>{{ status.client_subnet }}</dd></div>
            <div><dt>证书来源</dt><dd>TLS 模板，由原生核心管理</dd></div>
            <div><dt>订阅兼容</dt><dd>{{ status.mihomo_compatible ? 'sing-box JSON + Clash / Mihomo' : '仅 sing-box JSON' }}</dd></div>
          </dl>
          <v-alert type="info" variant="tonal" class="mt-4">
            连接数不是 CONNECT-IP 隧道数。流量、在线用户和访问历史以面板核心统计为准。
            没有有效用户时不监听。同一用户的多条 Mihomo 隧道可共用地址；连接流会关联到对应隧道，关闭一台设备不会断开其他设备。流量与历史按用户汇总。
          </v-alert>
          <v-alert v-for="item in status.diagnostics ?? []" :key="item.id" type="info" variant="tonal" class="mt-4">{{ item.detail }}</v-alert>
        </template>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import HttpUtils from '@/plugins/httputil'
type Status = { tag?: string; host?: string; port?: number; running?: boolean; configured_users?: number; active_connections?: number; mihomo_compatible?: boolean; bind_addr?: string; client_subnet?: string; diagnostics?: { id: string; detail: string }[] }
export default {
  props: ['visible', 'data'],
  emits: ['close'],
  data() { return { status: {} as Status, loading: false, alert: '', timer: undefined as ReturnType<typeof setInterval> | undefined } },
  mounted() { if (this.$props.visible) this.load(); this.timer = setInterval(() => { if (this.$props.visible) this.load() }, 3000) },
  watch: { visible(value: boolean) { if (value) this.load() } },
  beforeUnmount() { if (this.timer) clearInterval(this.timer) },
  methods: {
    async load() {
      if (this.loading) return
      this.loading = true
      try {
        const response = await HttpUtils.get('api/masque-status', { tag: this.$props.data?.tag })
        if (response.success) { this.status = response.obj; this.alert = '' }
        else this.alert = response.msg || this.$t('in.masqueStatusReadFailed')
      } catch { this.alert = this.$t('in.masqueStatusReadFailed') }
      finally { this.loading = false }
    },
  },
}
</script>

<style scoped>
.native-details { display: grid; gap: 16px; margin: 16px 0; }
.native-details > div { display: flex; justify-content: space-between; gap: 20px; border-bottom: 1px solid var(--np-border); padding-bottom: 12px; }
.native-details dt { color: var(--np-text-muted); }
.native-details dd { text-align: right; overflow-wrap: anywhere; }
</style>
