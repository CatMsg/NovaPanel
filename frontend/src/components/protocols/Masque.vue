<template>
  <v-card subtitle="MASQUE">
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model.number="data.port" label="Port" type="number" min="1" max="65535" hide-details></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-select v-model="data.network" :items="networkItems" label="Network" hide-details></v-select>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model="data.ip" label="IP" :disabled="data.network == 'h3-l4proxy'" hide-details></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model.number="data.mtu" label="MTU" type="number" min="576" max="9000" hide-details></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-switch v-model="data.udp" color="primary" label="UDP" hide-details></v-switch>
        </v-col>
      </v-row>

      <v-divider class="my-4"></v-divider>

      <v-row>
        <v-col cols="12" sm="6">
          <v-textarea v-model="data.private_key" label="Private Key" rows="3" auto-grow hide-details></v-textarea>
        </v-col>
        <v-col cols="12" sm="6">
          <v-textarea v-model="data.public_key" label="Public Key" rows="3" auto-grow hide-details></v-textarea>
        </v-col>
      </v-row>

      <v-row>
        <v-col cols="12" class="d-flex ga-2 align-center">
          <v-btn color="primary" variant="tonal" :loading="loading" @click="genMasqueKey">
            Generate Keys
          </v-btn>
          <v-chip color="info" variant="tonal" size="small">
            {{ data.network ?? 'quic' }}
          </v-chip>
        </v-col>
      </v-row>

      <v-row>
        <v-col cols="12">
          <div class="d-flex flex-wrap ga-2 align-center justify-space-between mb-2">
            <span class="text-caption text-medium-emphasis">mihomo config</span>
            <v-btn
              color="primary"
              variant="tonal"
              size="small"
              prepend-icon="mdi-content-copy"
              @click="copyMasqueConfig"
            >
              复制 OpenClash 节点
            </v-btn>
          </div>
          <v-textarea :model-value="masqueConfig" rows="10" auto-grow readonly hide-details></v-textarea>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script lang="ts">
import HttpUtils from '@/plugins/httputil'
import { push } from 'notivue'
import { i18n } from '@/locales'
import { buildMasqueConfig } from '@/plugins/masqueUtil'

export default {
  props: ['data'],
  data() {
    return {
      loading: false,
      networkItems: [
        { title: 'h3-l4proxy', value: 'h3-l4proxy' },
        { title: 'quic', value: 'quic' },
        { title: 'h2', value: 'h2' },
      ],
    }
  },
  computed: {
    masqueConfig() {
      return buildMasqueConfig(this.$props.data)
    },
  },
  methods: {
    async copyMasqueConfig() {
      const text = this.masqueConfig
      try {
        if (navigator.clipboard?.writeText) {
          await navigator.clipboard.writeText(text)
        } else {
          this.copyWithFallback(text)
        }
        push.success({ message: '已复制 OpenClash 节点配置' })
      } catch (error) {
        try {
          this.copyWithFallback(text)
          push.success({ message: '已复制 OpenClash 节点配置' })
        } catch {
          push.error({ message: '复制失败，请手动复制 mihomo config' })
        }
      }
    },
    copyWithFallback(text: string) {
      const textarea = document.createElement('textarea')
      textarea.value = text
      textarea.setAttribute('readonly', 'true')
      textarea.style.position = 'fixed'
      textarea.style.left = '-9999px'
      document.body.appendChild(textarea)
      textarea.select()
      const ok = document.execCommand('copy')
      document.body.removeChild(textarea)
      if (!ok) {
        throw new Error('copy command failed')
      }
    },
    async genMasqueKey() {
      this.loading = true
      const msg = await HttpUtils.get('api/keypairs', { k: 'masque' })
      this.loading = false
      let result = { private_key: '', public_key: '' }
      if (msg.success) {
        msg.obj.forEach((line: string) => {
          if (line.startsWith('PrivateKey')) {
            result.private_key = line.substring(12)
          }
          if (line.startsWith('PublicKey')) {
            result.public_key = line.substring(11)
          }
        })
        this.$props.data.private_key = result.private_key
        this.$props.data.public_key = result.public_key
      } else {
        push.error({
          message: i18n.global.t('error') + ': ' + msg.obj,
        })
      }
    },
  },
}
</script>
