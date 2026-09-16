<template>
  <v-card subtitle="Tun">
    <v-row>
      <v-col cols="12" sm="8">
        <v-text-field v-model="addrs" :label="$t('types.tun.addr') + ' ' + $t('commaSeparated')" placeholder="172.18.0.1/30" hide-details></v-text-field>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-text-field v-model="data.interface_name" :label="$t('types.tun.ifName')" placeholder="tun0" hide-details clearable @click:clear="delete data.interface_name"></v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-text-field type="number" v-model.number="data.mtu" label="MTU" hide-details></v-text-field>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-text-field
          type="number"
          v-model.number="udpTimeout"
          label="UDP timeout"
          min="1"
          :suffix="$t('date.m')"
          hide-details>
        </v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-select
          v-model="data.dns_mode"
          label="DNS 模式"
          :items="['disabled','native','hijack']"
          clearable
          @click:clear="delete data.dns_mode"
          hide-details
        ></v-select>
      </v-col>
    </v-row>
    <v-row>
      <v-col v-if="data.dns_mode && data.dns_mode !== 'disabled'" cols="12" sm="6">
        <v-text-field v-model="dnsAddresses" label="DNS 地址" placeholder="172.19.0.2" clearable hide-details></v-text-field>
      </v-col>
      <v-col cols="12" sm="6">
        <v-text-field v-model="data.netns" label="网络命名空间" clearable @click:clear="delete data.netns" hide-details></v-text-field>
      </v-col>
      <v-col cols="12" sm="6">
        <v-switch
          v-model="data.multi_queue"
          color="primary"
          label="Linux 多队列"
          hint="让原生 TUN 网络栈将流量分摊到多个 CPU 核心"
          persistent-hint
          hide-details="auto"
        ></v-switch>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-select
          v-model="data.udp_mapping"
          label="UDP 映射模式"
          :items="udpNatBehaviors"
          clearable
          @click:clear="delete data.udp_mapping"
          hide-details
        ></v-select>
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-select
          v-model="data.udp_filtering"
          label="UDP 过滤模式"
          :items="udpNatBehaviors"
          clearable
          @click:clear="delete data.udp_filtering"
          hide-details
        ></v-select>
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-text-field type="number" min="0" v-model.number="data.udp_nat_max" label="UDP NAT 上限" clearable @click:clear="delete data.udp_nat_max" hide-details></v-text-field>
      </v-col>
    </v-row>
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-switch v-model="autoRoute" color="primary" label="Auto Route" hide-details></v-switch>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute">
        <v-switch v-model="data.auto_redirect" color="primary" label="Auto Redirect" hide-details></v-switch>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute">
        <v-switch v-model="data.strict_route" color="primary" label="Strict Route" hide-details></v-switch>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute && data.auto_redirect">
        <v-switch v-model="data.exclude_mptcp" color="primary" :label="$t('types.tun.excludeMptcp')" hide-details></v-switch>
      </v-col>
      <v-col cols="12" sm="6" md="4" v-if="autoRoute && data.auto_redirect">
        <v-text-field
          type="number"
          v-model.number="fallbackRuleIndex"
          :label="$t('types.tun.fallbackRuleIndex')"
          min="0"
          hide-details>
        </v-text-field>
      </v-col>
    </v-row>
  </v-card>
</template>

<script lang="ts">

export default {
  props: ['data'],
  data() {
    return {
      menu: false,
      udpNatBehaviors: [
        { title: '端点独立', value: 'endpoint_independent' },
        { title: '地址相关', value: 'address_dependent' },
        { title: '地址与端口相关', value: 'address_and_port_dependent' },
      ],
    }
  },
  computed: {
    addrs: {
      get() { return this.$props.data.address?.join(',') },
      set(v:string) { this.$props.data.address = v.length > 0 ? v.split(',') : undefined }
    },
    dnsAddresses: {
      get() { return this.$props.data.dns_address?.join(',') ?? '' },
      set(v:string) {
        const addresses = v.split(',').map((item) => item.trim()).filter(Boolean)
        this.$props.data.dns_address = addresses.length > 0 ? addresses : undefined
      }
    },
    udpTimeout: {
      get() { return this.$props.data.udp_timeout ? parseInt(this.$props.data.udp_timeout.replace('m','')) : 5 },
      set(v:number) { this.$props.data.udp_timeout = v > 0 ? v + 'm' : '5m' }
    },
    autoRoute: {
      get() { return this.$props.data.auto_route ?? false },
      set(v:boolean) {
        this.$props.data.auto_route = v
        this.$props.data.auto_redirect = v ? false : undefined
        this.$props.data.strict_route = v ? false : undefined
      }
    },
    fallbackRuleIndex: {
      get() { return this.$props.data.auto_redirect_iproute2_fallback_rule_index ?? 32768 },
      set(v: number) {
        const val = typeof v === 'number' && !isNaN(v) && v >= 0 ? v : undefined
        this.$props.data.auto_redirect_iproute2_fallback_rule_index = val
      }
    }
  }
}
</script>
