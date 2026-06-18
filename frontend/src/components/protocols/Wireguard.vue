<template>
  <div class="wg-config">
    <v-alert
      class="wg-guide"
      color="primary"
      icon="mdi-information-outline"
      variant="tonal"
    >
      <div class="font-weight-medium">{{ $t('types.wg.guideTitle') }}</div>
      <div class="text-body-2">{{ $t('types.wg.guide') }}</div>
    </v-alert>

    <v-card class="wg-section" variant="outlined">
      <v-card-title class="wg-section-title">
        <v-icon icon="mdi-server-network" color="primary" />
        <div>
          <div>{{ $t('types.wg.localSection') }}</div>
          <div class="text-body-2 text-medium-emphasis font-weight-regular">{{ $t('types.wg.localSectionHint') }}</div>
        </div>
      </v-card-title>
      <v-card-text>
        <v-row>
          <v-col cols="12" md="6">
            <v-text-field
              v-model="data.private_key"
              :label="$t('types.wg.privKey')"
              :hint="$t('types.wg.privateHint')"
              append-inner-icon="mdi-key-star"
              @click:append-inner="newKey()"
              persistent-hint
            />
          </v-col>
          <v-col cols="12" md="6">
            <v-text-field
              v-model="public_key"
              readonly
              :label="$t('tls.pubKey')"
              :hint="$t('types.wg.publicHint')"
              append-inner-icon="mdi-refresh"
              @click:append-inner="getWgPubKey()"
              persistent-hint
            />
          </v-col>
          <v-col cols="12" md="8">
            <v-text-field
              v-model="address"
              :label="$t('types.wg.localIp') + ' ' + $t('commaSeparated')"
              :hint="$t('types.wg.localIpHint')"
              placeholder="10.10.0.1/24"
              persistent-hint
            />
          </v-col>
          <v-col cols="12" md="4">
            <v-text-field
              v-model.number="data.listen_port"
              :label="$t('in.port') + ' (UDP)'"
              :hint="$t('types.wg.listenPortHint')"
              type="number"
              min="1"
              persistent-hint
            />
          </v-col>
          <v-col cols="12" md="8">
            <v-text-field
              v-model="dns"
              :label="$t('dns.title') + ' ' + $t('commaSeparated')"
              :hint="$t('types.wg.dnsHint')"
              placeholder="1.1.1.1, 9.9.9.9"
              persistent-hint
            />
          </v-col>
          <v-col cols="12" md="4">
            <v-switch
              v-model="data.system"
              color="primary"
              :label="$t('types.wg.sysIf')"
              :hint="$t('types.wg.systemHint')"
              persistent-hint
            />
          </v-col>
          <v-col cols="12" md="4" v-if="data.system">
            <v-text-field v-model="ifName" :label="$t('types.wg.ifName')" />
          </v-col>
          <v-col cols="12" sm="6" md="4" v-if="data.udp_timeout != undefined">
            <v-text-field v-model.number="udp_timeout" label="UDP Timeout" type="number" min="0" :suffix="$t('date.m')" />
          </v-col>
          <v-col cols="12" sm="6" md="4" v-if="data.workers != undefined">
            <v-text-field v-model.number="data.workers" :label="$t('types.wg.worker')" type="number" min="1" />
          </v-col>
          <v-col cols="12" sm="6" md="4" v-if="data.mtu != undefined">
            <v-text-field v-model.number="data.mtu" label="MTU" type="number" min="0" />
          </v-col>
        </v-row>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-menu v-model="menu" :close-on-content-click="false" location="start">
          <template v-slot:activator="{ props }">
            <v-btn v-bind="props" prepend-icon="mdi-tune-variant" variant="tonal">{{ $t('types.wg.options') }}</v-btn>
          </template>
          <v-card>
            <v-list>
              <v-list-item><v-switch v-model="optionUdp" color="primary" label="UDP Timeout" hide-details /></v-list-item>
              <v-list-item><v-switch v-model="optionWorker" color="primary" :label="$t('types.wg.worker')" hide-details /></v-list-item>
              <v-list-item><v-switch v-model="optionMtu" color="primary" label="MTU" hide-details /></v-list-item>
            </v-list>
          </v-card>
        </v-menu>
      </v-card-actions>
    </v-card>

    <v-card class="wg-section" variant="outlined" v-if="data.peers != undefined">
      <v-card-title class="wg-section-title wg-client-header">
        <div class="wg-client-heading">
          <v-icon icon="mdi-account-multiple-outline" color="primary" />
          <div>
            <div>{{ $t('types.wg.clientsSection') }}</div>
            <div class="text-body-2 text-medium-emphasis font-weight-regular">{{ $t('types.wg.clientsSectionHint') }}</div>
          </div>
        </div>
        <v-btn color="primary" prepend-icon="mdi-account-plus" variant="tonal" @click="addPeer">
          {{ $t('types.wg.addClient') }}
        </v-btn>
      </v-card-title>
      <v-card-text>
        <v-alert v-if="data.peers.length === 0" color="info" icon="mdi-account-arrow-right-outline" variant="tonal">
          {{ $t('types.wg.noClients') }}
        </v-alert>
        <v-card class="wg-peer" variant="tonal" v-for="(p, index) in data.peers" :key="index">
          <v-card-title class="wg-peer-title">
            <span>{{ $t('objects.client') + ' ' + (Number(index) + 1) }}</span>
            <v-btn
              color="error"
              icon="mdi-delete-outline"
              size="small"
              variant="text"
              @click="delPeer(Number(index))"
            />
          </v-card-title>
          <v-card-text>
            <Peer :data="p" :ext="data.ext" @refreshPeerKey="$emit('refreshPeerKey', index)" />
          </v-card-text>
        </v-card>
      </v-card-text>
    </v-card>
  </div>
</template>

<script lang="ts">
import Peer from '@/components/WgPeer.vue'

export default {
  props: ['data'],
  emits: ['newWgKey', 'getWgPubKey', 'addPeer', 'delPeer', 'refreshPeerKey'],
  data() {
    return {
      menu: false,
    }
  },
  methods: {
    addPeer() {
      this.$emit('addPeer')
    },
    delPeer(id: number) {
      this.$emit('delPeer', id)
    },
    refreshPeerKey(id: number) {
      this.$emit('refreshPeerKey', id)
    },
    newKey() {
      this.$emit('newWgKey')
    },
    getWgPubKey() {
      const privKey = this.$props.data.private_key
      if (privKey.length == 0) return
      this.$emit('getWgPubKey', privKey)
    },
  },
  computed: {
    optionUdp: {
      get(): boolean { return this.$props.data.udp_timeout != undefined },
      set(v:boolean) { this.$props.data.udp_timeout = v ? "5m" : undefined }
    },
    optionRsrv: {
      get(): boolean { return this.$props.data.reserved != undefined },
      set(v:boolean) { this.$props.data.reserved = v ? [0,0,0] : undefined }
    },
    optionWorker: {
      get(): boolean { return this.$props.data.workers != undefined },
      set(v:boolean) { this.$props.data.workers = v ? 2 : undefined }
    },
    optionMtu: {
      get(): boolean { return this.$props.data.mtu != undefined },
      set(v:boolean) { this.$props.data.mtu = v ? 1408 : undefined }
    },
    ifName: {
      get() { return this.$props.data.name?? '' },
      set(v:string) { this.$props.data.name = v.length > 0 ? v : undefined }
    },
    address: {
      get() { return this.$props.data.address?.join(',') },
      set(v:string) { this.$props.data.address = v.length > 0 ? v.split(',') : undefined }
    },
    reserved: {
      get() { return this.$props.data.reserved?.join(',') },
      set(v:string) { 
        if(!v.endsWith(',')) {
          this.$props.data.reserved = v.length > 0 ? v.split(',').map(str => parseInt(str, 10)) : []
        }
      }
    },
    udp_timeout: {
      get() { return this.$props.data.udp_timeout ? parseInt(this.$props.data.udp_timeout.replace('m','')) : 5 },
      set(v:number) { this.$props.data.udp_timeout = v > 0 ? v + 'm' : '5m' }
    },
    public_key: {
      get() { return this.$props.data.ext?.public_key?? '' },
      set(v:string) {
        if (!this.$props.data.ext) this.$props.data.ext = { keys: [] }
        this.$props.data.ext.public_key = v
      }
    },
    dns: {
      get() { return this.$props.data.ext?.dns ?? '' },
      set(v:string) {
        if (!this.$props.data.ext) this.$props.data.ext = { keys: [] }
        this.$props.data.ext.dns = v
      }
    }
  },
  components: { Peer }
}
</script>
<style scoped>
.wg-config {
  display: grid;
  gap: 16px;
  padding-top: 12px;
}

.wg-guide,
.wg-section,
.wg-peer {
  border-radius: 18px;
}

.wg-section-title,
.wg-client-heading,
.wg-peer-title {
  display: flex;
  align-items: center;
  gap: 12px;
}

.wg-client-header,
.wg-peer-title {
  justify-content: space-between;
}

.wg-peer {
  margin-top: 14px;
}

@media (max-width: 600px) {
  .wg-client-header {
    align-items: stretch;
    flex-direction: column;
  }

  .wg-client-header .v-btn {
    width: 100%;
  }
}
</style>
