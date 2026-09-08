<template>
  <v-card class="masque-config" variant="flat">
    <v-card-text>
      <div class="masque-config__heading">
        <div>
          <span>HTTP/3 CONNECT-IP</span>
          <h3>多用户 MASQUE</h3>
          <p>一个 UDP 监听端口承载多个面板用户，每位用户自动生成独立密钥、隧道 IP 和会话。</p>
        </div>
        <v-chip color="primary" variant="tonal">QUIC</v-chip>
      </div>

      <v-row>
        <v-col cols="12" sm="6">
          <v-text-field v-model="data.server" label="订阅域名" hint="留空时使用当前面板访问域名" persistent-hint />
        </v-col>
        <v-col cols="12" sm="6">
          <v-text-field v-model="data.client_subnet" label="客户端地址池" placeholder="自动分配，例如 172.16.0.0/24" hint="每位用户会从该地址池获得独立 /32 地址" persistent-hint />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model.number="data.mtu" label="MTU" type="number" min="576" max="9000" hide-details />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model.number="data.keepalive" label="Keepalive（秒）" type="number" min="1" max="300" hide-details />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model="data.sni" label="SNI（可选）" hide-details />
        </v-col>
        <v-col cols="12" sm="6">
          <v-switch v-model="data.remote_dns_resolve" color="primary" label="远程 DNS 解析" hide-details />
        </v-col>
        <v-col cols="12" sm="6">
          <v-switch v-model="data.udp" color="primary" label="允许 UDP" hide-details disabled />
        </v-col>
      </v-row>

      <v-expansion-panels class="mt-3" variant="accordion">
        <v-expansion-panel title="服务端身份密钥">
          <v-expansion-panel-text>
            <v-alert type="info" variant="tonal" class="mb-4">首次保存可自动生成。重新生成会使已有用户订阅失效，需要重新拉取。</v-alert>
            <v-row>
              <v-col cols="12" sm="6">
                <v-textarea v-model="data.private_key" label="服务端私钥" rows="3" auto-grow hide-details />
              </v-col>
              <v-col cols="12" sm="6">
                <v-textarea v-model="data.public_key" label="服务端公钥" rows="3" auto-grow readonly hide-details />
              </v-col>
            </v-row>
            <v-btn class="mt-4" color="primary" variant="tonal" :loading="loading" prepend-icon="mdi-key-plus" @click="genMasqueKey">生成服务端密钥</v-btn>
          </v-expansion-panel-text>
        </v-expansion-panel>
      </v-expansion-panels>
    </v-card-text>
  </v-card>
</template>

<script lang="ts">
import HttpUtils from '@/plugins/httputil'
import { push } from 'notivue'

export default {
  props: ['data'],
  data() { return { loading: false } },
  created() {
    this.$props.data.network = 'quic'
    this.$props.data.udp = true
  },
  methods: {
    async genMasqueKey() {
      this.loading = true
      const msg = await HttpUtils.get('api/keypairs', { k: 'masque' })
      this.loading = false
      if (!msg.success) {
        push.error({ message: `生成密钥失败：${msg.obj}` })
        return
      }
      msg.obj.forEach((line: string) => {
        if (line.startsWith('PrivateKey')) this.$props.data.private_key = line.substring(12)
        if (line.startsWith('PublicKey')) this.$props.data.public_key = line.substring(11)
      })
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
@media (max-width: 600px) { .masque-config__heading { align-items: flex-start; } }
</style>
