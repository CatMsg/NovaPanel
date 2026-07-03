<template>
  <v-row class="main-info-grid">
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.host') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">{{ tilesData?.sys?.hostName }}</v-col>
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.cpu') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" variant="flat">
        <v-tooltip activator="parent" location="top" style="direction: ltr;">
          {{ tilesData?.sys?.cpuType }}
        </v-tooltip>
        {{ tilesData?.sys?.cpuCount }} {{ $t('main.info.core') }}
      </v-chip>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.firewall') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" color="primary" variant="flat">
        {{ tilesData?.sys?.firewallBackend }}
      </v-chip>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">IP</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" color="primary" variant="flat" v-if="tilesData?.sys?.ipv4?.length > 0">
        <v-tooltip activator="parent" location="top" style="direction: ltr;">
          <span v-html="tilesData?.sys?.ipv4?.join('<br />')"></span>
        </v-tooltip>
        IPv4
      </v-chip>
      <v-chip density="compact" color="primary" variant="flat" v-if="tilesData?.sys?.ipv6?.length > 0">
        <v-tooltip activator="parent" location="top" style="direction: ltr;">
          <span v-html="tilesData?.sys?.ipv6?.join('<br />')"></span>
        </v-tooltip>
        IPv6
      </v-chip>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">NovaPanel</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" color="blue">
        v{{ tilesData?.sys?.appVersion }}
      </v-chip>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.uptime') }}</v-col>
    <v-col
      cols="8"
      class="main-info-grid__value"
      v-tooltip:top="$t('main.info.startupTime') + ': ' + new Date((tilesData?.sys?.bootTime || 0) * 1000).toLocaleString(locale)"
    >
      {{ HumanReadable.formatSecond((Date.now() / 1000) - (tilesData?.sys?.bootTime || 0)) }}
    </v-col>
  </v-row>
</template>

<script setup lang="ts">
import { HumanReadable } from '@/plugins/utils'
import { locale } from '@/locales'

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

:global(.v-theme--dark) .main-info-grid__label {
  color: rgba(186, 202, 224, 0.78);
}
</style>
