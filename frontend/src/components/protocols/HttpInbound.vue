<template>
  <v-card :subtitle="$t('types.http.serverTitle')">
    <v-row>
      <v-col cols="12" sm="8" md="6">
        <v-select
          v-model="version"
          :items="versionOptions"
          :label="$t('types.http.version')"
          hide-details
        />
      </v-col>
    </v-row>
    <v-alert v-if="requiresChoice" type="warning" variant="tonal" class="mt-3">
      {{ $t('types.http.errors.singleVersionRequired') }}
    </v-alert>
    <p class="http-inbound__hint">{{ $t('types.http.serverDefaultHint') }}</p>
    <v-alert v-if="needsTls && !tlsEnabled" type="error" variant="tonal" class="mt-3">
      {{ $t(`types.http.errors.${tlsIssue}`) }}
    </v-alert>
  </v-card>
</template>

<script lang="ts">
import {
  hasEnabledInboundTls,
  requiresHttpVersionSelection,
  selectedInboundHttpVersion,
  synchronizeHttpInboundClientVersion,
} from '@/utils/httpVersions'

export default {
  props: ['data', 'tlsConfigs'],
  computed: {
    version: {
      get(): number | undefined { return selectedInboundHttpVersion(this.$props.data.version) },
      set(value: number) {
        if (![1, 2, 3].includes(value)) return
        this.$props.data.version = value
        synchronizeHttpInboundClientVersion(this.$props.data)
      },
    },
    versionOptions(): Array<{ title: string; value: number }> {
      return [1, 2, 3].map(value => ({
        title: value === 1
          ? this.$t('types.http.version11')
          : this.$t('types.http.versionLabel', { version: value }),
        value,
      }))
    },
    requiresChoice(): boolean { return requiresHttpVersionSelection(this.$props.data.version) },
    needsTls(): boolean { return this.version === 2 || this.version === 3 },
    tlsIssue(): string { return this.version === 2 ? 'http2OnlyRequiresTls' : 'tlsRequired' },
    tlsEnabled(): boolean {
      return hasEnabledInboundTls(this.$props.data, this.$props.tlsConfigs ?? [])
    },
  },
}
</script>

<style scoped>
.http-inbound__hint { margin: 8px 12px 0; color: rgba(var(--v-theme-on-surface), .68); font-size: .82rem; }
</style>
