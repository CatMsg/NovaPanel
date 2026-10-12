<template>
  <v-card :subtitle="$t('types.http.serverTitle')">
    <v-row>
      <v-col cols="12" sm="8" md="6">
        <v-select
          v-model="versions"
          :items="versionOptions"
          :label="$t('types.http.versions')"
          multiple
          chips
          closable-chips
          hide-details
        />
      </v-col>
    </v-row>
    <p class="http-inbound__hint">{{ $t('types.http.serverDefaultHint') }}</p>
    <v-alert v-if="needsTls && !tlsEnabled" type="error" variant="tonal" class="mt-3">
      {{ $t('types.http.errors.tlsRequired') }}
    </v-alert>
  </v-card>
</template>

<script lang="ts">
import { effectiveInboundHttpVersions, hasEnabledInboundTls } from '@/utils/httpVersions'

export default {
  props: ['data', 'tlsConfigs'],
  computed: {
    versions: {
      get(): unknown[] { return effectiveInboundHttpVersions(this.$props.data) },
      set(value: unknown[]) { this.$props.data.version = value },
    },
    versionOptions(): Array<{ title: string; value: unknown }> {
      const values = new Set([1, 2, 3, ...this.versions])
      return Array.from(values).sort((a, b) => String(a).localeCompare(String(b), undefined, { numeric: true })).map(value => ({
        title: typeof value === 'number' && [1, 2, 3].includes(value)
          ? this.$t('types.http.versionLabel', { version: value })
          : this.$t('types.http.unknownVersion', { version: String(value) }),
        value,
      }))
    },
    needsTls(): boolean { return this.versions.includes(3) },
    tlsEnabled(): boolean {
      return hasEnabledInboundTls(this.$props.data, this.$props.tlsConfigs ?? [])
    },
  },
}
</script>

<style scoped>
.http-inbound__hint { margin: 8px 12px 0; color: rgba(var(--v-theme-on-surface), .68); font-size: .82rem; }
</style>
