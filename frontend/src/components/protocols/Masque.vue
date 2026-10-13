<template>
  <v-card class="masque-config" variant="flat">
    <v-card-text>
      <div class="masque-config__heading">
        <div><span>sing-box · HTTP/3 CONNECT-IP</span><h3>原生 MASQUE</h3>
          <p>单个 UDP 端口，多用户账号密码认证。使用内置用户态协议栈，不创建系统 TUN 或 NAT 规则。</p>
        </div>
        <v-chip color="primary" variant="tonal">HTTP/3</v-chip>
      </div>
      <v-alert type="info" variant="tonal" class="mb-4">
        需要选择启用的 TLS 模板。默认同时支持 sing-box JSON 与 Clash / Mihomo 订阅；如只使用 sing-box，可关闭兼容开关。
        TCP/UDP 按用户统计、限速并记录历史；不开放客户端路由通告、隧道间直连或 ICMP 转发。没有有效用户时不启动监听。
      </v-alert>
      <v-switch v-model="data.mihomo_compatible" label="兼容 Clash / Mihomo 订阅（密钥认证）" color="primary" hide-details class="mb-4" />
      <v-alert v-if="data.mihomo_compatible" type="info" variant="tonal" class="mb-4">
        同一 UDP 端口同时支持 sing-box 账号认证和 Mihomo 密钥认证，共用原生核心统计和限速。
        TLS 模板必须使用 ECDSA 证书，不能使用 RSA。同一面板用户可在多台设备同时连接并共用该用户的隧道地址；服务器按连接流关联回对应隧道，不会互相挤掉。流量、限速与历史按用户汇总。未定义通用 MASQUE 单节点 URI，请使用订阅。
      </v-alert>
      <v-row>
        <v-col cols="12" sm="6"><v-text-field v-model="data.server" label="订阅域名" hint="留空使用当前面板域名" persistent-hint /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="serverAddress" label="服务端隧道地址 / 地址池" placeholder="自动分配，例如 172.16.0.1/24" hint="原生服务自动分配用户隧道地址" persistent-hint /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.mtu" label="MTU" type="number" min="1280" max="9000" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="data.path" label="CONNECT-IP 路径（可选）" hint="默认使用原生 MASQUE 路径模板" persistent-hint /></v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script lang="ts">
export default {
  props: ['data'],
  computed: {
    serverAddress: {
      get(): string { return this.$props.data.address?.[0] ?? '' },
      set(value: string) {
        if (value.trim()) this.$props.data.address = [value.trim()]
        else delete this.$props.data.address
      },
    },
  },
}
</script>

<style scoped>
.masque-config { margin-top: 12px; border: 1px solid var(--np-border); background: linear-gradient(145deg, rgba(10, 132, 255, 0.08), rgba(255, 255, 255, 0.04)); }
.masque-config__heading { display: flex; justify-content: space-between; gap: 16px; margin-bottom: 18px; }
.masque-config__heading span { color: var(--np-accent); font-size: 12px; letter-spacing: .08em; }
.masque-config__heading h3 { margin: 4px 0; font-size: 22px; }
.masque-config__heading p { margin: 0; color: var(--np-text-muted); line-height: 1.6; }
</style>
