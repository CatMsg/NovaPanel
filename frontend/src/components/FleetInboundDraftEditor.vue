<template>
  <section class="fleet-inbound-draft">
    <v-select
      :model-value="draft.type"
      :items="protocolOptions"
      :label="$t('ui.fleet.remoteInboundProtocol')"
      variant="outlined"
      density="comfortable"
      @update:model-value="changeProtocol"
    />

    <v-alert type="info" variant="tonal" density="compact" class="mb-4">
      {{ $t('ui.fleet.remoteInboundUsersHint') }}
    </v-alert>

    <v-card v-if="draft.type === 'http'" variant="tonal" class="pa-4">
      <v-select
        v-model="draft.version"
        :items="httpVersions"
        :label="$t('types.http.version')"
        variant="outlined"
        density="comfortable"
        hide-details
      />
    </v-card>
    <Shadowsocks v-else-if="draft.type === 'shadowsocks'" direction="in" :data="draft" hide-credentials />
    <VMess v-else-if="draft.type === 'vmess'" :data="draft" hide-credentials />
    <Trojan v-else-if="draft.type === 'trojan'" :data="draft" hide-credentials />
    <Vless v-else-if="draft.type === 'vless'" :data="draft" hide-credentials />
    <Hysteria v-else-if="draft.type === 'hysteria'" direction="in" :data="draft" />
    <Hysteria2 v-else-if="draft.type === 'hysteria2'" direction="in" :data="draft" />
    <Naive v-else-if="draft.type === 'naive'" direction="in" :data="draft" />
    <Tuic v-else-if="draft.type === 'tuic'" direction="in" :data="draft" />
    <AnyTls v-else-if="draft.type === 'anytls'" direction="in" :data="draft" />
    <Mieru v-else-if="draft.type === 'mieru'" :data="draft" />
    <Masque v-else-if="draft.type === 'masque'" :data="draft" />
    <v-card v-else-if="draft.type === 'shadowtls'" variant="tonal" class="pa-4">
      <v-chip color="primary" variant="tonal" class="mb-3">ShadowTLS v3</v-chip>
      <v-text-field v-model="draft.handshake.server" label="Handshake server" variant="outlined" density="comfortable" />
      <v-text-field v-model.number="draft.handshake.server_port" :label="$t('ui.common.port')" type="number" min="1" max="65535" variant="outlined" density="comfortable" />
      <v-switch v-model="draft.strict_mode" label="Strict mode" color="primary" hide-details />
    </v-card>

    <v-expansion-panels v-if="hasAdvancedOptions" variant="accordion" class="mt-3">
      <v-expansion-panel value="advanced">
        <v-expansion-panel-title>{{ $t('ui.inboundForm.advanced') }}</v-expansion-panel-title>
        <v-expansion-panel-text>
          <Transport v-if="draft.transport" :data="draft" />
          <Multiplex v-if="draft.multiplex" direction="in" :data="draft" />
        </v-expansion-panel-text>
      </v-expansion-panel>
    </v-expansion-panels>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { createInbound, type Inbound } from '@/types/inbounds'
import Shadowsocks from '@/components/protocols/Shadowsocks.vue'
import VMess from '@/components/protocols/Vmess.vue'
import Trojan from '@/components/protocols/Trojan.vue'
import Vless from '@/components/protocols/Vless.vue'
import Hysteria from '@/components/protocols/Hysteria.vue'
import Hysteria2 from '@/components/protocols/Hysteria2.vue'
import Naive from '@/components/protocols/Naive.vue'
import Tuic from '@/components/protocols/Tuic.vue'
import AnyTls from '@/components/protocols/AnyTls.vue'
import Mieru from '@/components/protocols/Mieru.vue'
import Masque from '@/components/protocols/Masque.vue'
import Transport from '@/components/Transport.vue'
import Multiplex from '@/components/Multiplex.vue'

const props = defineProps<{ modelValue: Inbound }>()
const emit = defineEmits<{ 'update:modelValue': [value: Inbound] }>()
const draft = computed(() => props.modelValue as any)

const protocolOptions = [
  { title: 'Mixed', value: 'mixed' },
  { title: 'SOCKS', value: 'socks' },
  { title: 'HTTP', value: 'http' },
  { title: 'Shadowsocks', value: 'shadowsocks' },
  { title: 'VMess', value: 'vmess' },
  { title: 'Trojan', value: 'trojan' },
  { title: 'NaiveProxy', value: 'naive' },
  { title: 'Hysteria', value: 'hysteria' },
  { title: 'ShadowTLS', value: 'shadowtls' },
  { title: 'TUIC', value: 'tuic' },
  { title: 'Hysteria2', value: 'hysteria2' },
  { title: 'VLESS', value: 'vless' },
  { title: 'AnyTLS', value: 'anytls' },
  { title: 'Mieru', value: 'mieru' },
  { title: 'MASQUE', value: 'masque' },
]
const httpVersions = [
  { title: 'HTTP/1.1', value: 1 },
  { title: 'HTTP/2', value: 2 },
  { title: 'HTTP/3', value: 3 },
]
const hasAdvancedOptions = computed(() => Boolean(draft.value.transport || draft.value.multiplex))

function changeProtocol(type: string) {
  const next = createInbound(type as any, {
    id: 0,
    tag: '',
    listen: '::',
    listen_port: 0,
    addrs: [],
    out_json: {},
  } as any)
  emit('update:modelValue', next)
}
</script>

<style scoped>
.fleet-inbound-draft { display: grid; gap: 12px; }
</style>
