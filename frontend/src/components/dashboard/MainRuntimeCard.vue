<template>
  <v-row class="main-info-grid">
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.running') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" color="success" variant="flat" v-if="tilesData?.sbd?.running">{{ $t('yes') }}</v-chip>
      <v-chip density="compact" color="error" variant="flat" v-else>{{ $t('no') }}</v-chip>
      <v-chip density="compact" color="transparent" v-if="tilesData?.sbd?.running && !loading" class="main-info-grid__restart" @click="emit('restart')">
        <v-tooltip activator="parent" location="top">
          {{ $t('actions.restartSb') }}
        </v-tooltip>
        <v-icon icon="mdi-restart" color="warning" />
      </v-chip>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.memory') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" color="primary" variant="flat" v-if="tilesData?.sbd?.stats?.Alloc">
        {{ HumanReadable.sizeFormat(tilesData?.sbd?.stats?.Alloc) }}
      </v-chip>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.threads') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <v-chip density="compact" color="primary" variant="flat" v-if="tilesData?.sbd?.stats?.NumGoroutine">
        {{ tilesData?.sbd?.stats?.NumGoroutine }}
      </v-chip>
    </v-col>
    <v-col cols="4" class="main-info-grid__label">{{ $t('main.info.uptime') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">{{ HumanReadable.formatSecond(tilesData?.sbd?.stats?.Uptime) }}</v-col>
    <v-col cols="4" class="main-info-grid__label">{{ $t('online') }}</v-col>
    <v-col cols="8" class="main-info-grid__value">
      <template v-if="tilesData?.sbd?.running">
        <v-chip density="compact" color="primary" variant="flat" v-if="onlines?.user?.length">
          <v-tooltip activator="parent" location="top" overflow="auto">
            <span v-text="$t('pages.clients')" style="font-weight: bold;"></span><br />
            <span v-for="user in onlines.user" :key="user">{{ user }}<br /></span>
          </v-tooltip>
          {{ onlines.user.length }}
        </v-chip>
        <v-chip density="compact" color="success" variant="flat" v-if="onlines?.inbound?.length">
          <v-tooltip activator="parent" location="top" :text="$t('pages.inbounds')">
            <span v-text="$t('pages.inbounds')" style="font-weight: bold;"></span><br />
            <span v-for="inbound in onlines.inbound" :key="inbound">{{ inbound }}<br /></span>
          </v-tooltip>
          {{ onlines.inbound.length }}
        </v-chip>
        <v-chip density="compact" color="info" variant="flat" v-if="onlines?.outbound?.length">
          <v-tooltip activator="parent" location="top" :text="$t('pages.outbounds')">
            <span v-text="$t('pages.outbounds')" style="font-weight: bold;"></span><br />
            <span v-for="outbound in onlines.outbound" :key="outbound">{{ outbound }}<br /></span>
          </v-tooltip>
          {{ onlines.outbound.length }}
        </v-chip>
      </template>
    </v-col>
  </v-row>
</template>

<script setup lang="ts">
import { HumanReadable } from '@/plugins/utils'

defineProps<{
  tilesData: any
  loading: boolean
  onlines: {
    user?: string[]
    inbound?: string[]
    outbound?: string[]
  }
}>()

const emit = defineEmits<{
  (e: 'restart'): void
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

.main-info-grid__restart {
  cursor: pointer;
}

:global(.v-theme--dark .main-info-grid__label) {
  color: rgba(186, 202, 224, 0.78);
}
</style>
