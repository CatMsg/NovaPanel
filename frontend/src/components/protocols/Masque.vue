<template>
  <v-card subtitle="MASQUE">
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model.number="data.port" label="Port" type="number" min="1" max="65535" hide-details></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-select v-model="data.network" :items="['quic', 'h2']" label="Network" hide-details></v-select>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model="data.ip" label="IP" hide-details></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-text-field v-model="data.ipv6" label="IPv6" hide-details></v-text-field>
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
          <v-textarea :model-value="masqueConfig" label="mihomo config" rows="10" auto-grow readonly hide-details></v-textarea>
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
    }
  },
  computed: {
    masqueConfig() {
      return buildMasqueConfig(this.$props.data)
    },
  },
  methods: {
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
