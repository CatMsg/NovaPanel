<template>
  <v-card subtitle="Hysteria">
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-text-field
        :label="$t('stats.upload')"
        hide-details
        type="number"
        :suffix="$t('stats.Mbps')"
        v-model.number="up_mbps">
        </v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-text-field
        :label="$t('stats.download')"
        hide-details
        type="number"
        :suffix="$t('stats.Mbps')"
        min="0"
        v-model.number="down_mbps">
        </v-text-field>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4">
       <v-text-field
       :label="$t('types.hy.obfs')"
        hide-details
        v-model="data.obfs">
        </v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="direction=='out'">
        <v-text-field
        :label="$t('types.hy.auth')"
        hide-details
        v-model="data.auth_str">
        </v-text-field>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4" v-if="direction=='out'">
        <Network :data="data" />
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-switch v-model="disablePathMTUDiscovery" color="primary" label="Disable path MTU discovery" hide-details></v-switch>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4" v-if="connectionReceiveWindow != undefined">
        <v-text-field
        label="Connection receive window"
        hide-details
        type="number"
        min="0"
        v-model.number="connectionReceiveWindow">
        </v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="streamReceiveWindow != undefined">
        <v-text-field
        label="Stream receive window"
        hide-details
        type="number"
        min="0"
        v-model.number="streamReceiveWindow">
        </v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="direction == 'in' && maxConcurrentStreams != undefined">
        <v-text-field
        label="Max concurrent streams"
        hide-details
        type="number"
        min="0"
        v-model.number="maxConcurrentStreams">
        </v-text-field>
      </v-col>
    </v-row>
    <v-card-actions>
      <v-spacer></v-spacer>
      <v-menu v-model="menu" :close-on-content-click="false" location="start">
        <template v-slot:activator="{ props }">
          <v-btn v-bind="props" hide-details variant="tonal">{{ $t('types.hy.hyOptions') }}</v-btn>
        </template>
        <v-card>
          <v-list>
            <v-list-item>
              <v-switch v-model="optionConnectionWindow" color="primary" label="Connection receive window" hide-details></v-switch>
            </v-list-item>
            <v-list-item v-if="direction=='out'">
              <v-switch v-model="optionStreamWindow" color="primary" label="Stream receive window" hide-details></v-switch>
            </v-list-item>
            <v-list-item v-if="direction=='in'">
              <v-switch v-model="optionStreamWindow" color="primary" label="Stream receive window" hide-details></v-switch>
            </v-list-item>
            <v-list-item v-if="direction=='in'">
              <v-switch v-model="optionMaxStreams" color="primary" label="Max concurrent streams" hide-details></v-switch>
            </v-list-item>
          </v-list>
        </v-card>
      </v-menu>
    </v-card-actions>
  </v-card>
</template>

<script lang="ts">
import Network from '@/components/Network.vue'

export default {
  props: ['direction','data'],
  data() {
    return {
      menu: false,
    }
  },
  computed: {
    disablePathMTUDiscovery: {
      get(): boolean { return this.$props.data.disable_path_mtu_discovery ?? this.$props.data.disable_mtu_discovery ?? false },
      set(v:boolean) {
        delete this.$props.data.disable_mtu_discovery
        this.$props.data.disable_path_mtu_discovery = v || undefined
      }
    },
    connectionReceiveWindow: {
      get() { return this.$props.data.connection_receive_window ?? this.$props.data.recv_window_conn },
      set(v:number|undefined) {
        delete this.$props.data.recv_window_conn
        this.$props.data.connection_receive_window = v
      }
    },
    streamReceiveWindow: {
      get() {
        const legacy = this.$props.direction == 'in' ? this.$props.data.recv_window_client : this.$props.data.recv_window
        return this.$props.data.stream_receive_window ?? legacy
      },
      set(v:number|undefined) {
        delete this.$props.data.recv_window
        delete this.$props.data.recv_window_client
        this.$props.data.stream_receive_window = v
      }
    },
    maxConcurrentStreams: {
      get() { return this.$props.data.max_concurrent_streams ?? this.$props.data.max_conn_client },
      set(v:number|undefined) {
        delete this.$props.data.max_conn_client
        this.$props.data.max_concurrent_streams = v
      }
    },
    optionConnectionWindow: {
      get(): boolean { return this.connectionReceiveWindow != undefined },
      set(v:boolean) { this.connectionReceiveWindow = v ? 15728640 : undefined }
    },
    optionStreamWindow: {
      get(): boolean { return this.streamReceiveWindow != undefined },
      set(v:boolean) { this.streamReceiveWindow = v ? 67108864 : undefined }
    },
    optionMaxStreams: {
      get(): boolean { return this.maxConcurrentStreams != undefined },
      set(v:boolean) { this.maxConcurrentStreams = v ? 1024 : undefined }
    },
    down_mbps: {
      get() { return this.$props.data.down_mbps ? this.$props.data.down_mbps : 0 },
      set(newValue:any) {
        if (newValue.length != 0 ){
          this.$props.data.down_mbps = newValue
          this.$props.data.down = "" + newValue + " Mbps"
        } else {
          this.$props.data.down_mbps = 0
          this.$props.data.down = "0 Mbps"
        }
      }
    },
    up_mbps: {
      get() { return this.$props.data.up_mbps ? this.$props.data.up_mbps : 0 },
      set(newValue:number) { this.$props.data.up_mbps = newValue > 0 ? newValue : 0 }
    },
  },
  components: { Network }
}
</script>
