<template>
  <v-row class="main-info-grid">
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.host') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">{{ formatSystemValue(tilesData?.sys?.hostName) }}</v-col>
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.cpu') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" variant="flat" v-if="Number(tilesData?.sys?.cpuCount) > 0">
        <v-tooltip activator="parent" location="top" style="direction: ltr;">
          {{ tilesData?.sys?.cpuType }}
        </v-tooltip>
        {{ tilesData?.sys?.cpuCount }} {{ $t('main.info.core') }}
      </v-chip>
      <span v-else>-</span>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.firewall') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" color="primary" variant="flat" v-if="formatSystemValue(tilesData?.sys?.firewallBackend) !== '-'">
        {{ formatSystemValue(tilesData?.sys?.firewallBackend) }}
      </v-chip>
      <span v-else>-</span>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">IP</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" color="primary" variant="flat" v-if="tilesData?.sys?.ipv4?.length > 0">
        <v-tooltip activator="parent" location="top" style="direction: ltr;">
          <span v-for="address in tilesData?.sys?.ipv4" :key="address" class="ip-address">{{ address }}</span>
        </v-tooltip>
        IPv4
      </v-chip>
      <v-chip density="compact" color="primary" variant="flat" v-if="tilesData?.sys?.ipv6?.length > 0">
        <v-tooltip activator="parent" location="top" style="direction: ltr;">
          <span v-for="address in tilesData?.sys?.ipv6" :key="address" class="ip-address">{{ address }}</span>
        </v-tooltip>
        IPv6
      </v-chip>
      <span v-if="!tilesData?.sys?.ipv4?.length && !tilesData?.sys?.ipv6?.length">-</span>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">NovaPanel</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" color="blue" v-if="formatSystemValue(tilesData?.sys?.appVersion) !== '-'">
        v{{ formatSystemValue(tilesData?.sys?.appVersion) }}
      </v-chip>
      <span v-else>-</span>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.uptime') }}</v-col>
    <v-col
      cols="8"
      class="main-info-grid__value"
      v-tooltip:top="$t('main.info.startupTime') + ': ' + formatSystemStartupTime(tilesData?.sys?.bootTime, locale)"
    >
      {{ formatSystemUptime(tilesData?.sys?.bootTime) }}
    </v-col>
  </v-row>
</template>

<script setup lang="ts">
import { locale } from '@/locales'
import { formatSystemStartupTime, formatSystemUptime, formatSystemValue } from '@/utils/systemInfo'

defineProps<{
  tilesData: any
}>()
</script>

<style scoped lang="scss">
.main-info-grid {
  margin-top: 0;
}

.main-info-grid__label {
  color: var(--np-text-muted);
}

.main-info-grid__value {
  text-align: start;
}

.ip-address {
  display: block;
}

:global(.v-theme--dark .main-info-grid__label) {
  color: rgba(186, 202, 224, 0.78);
}
</style>
