<template>
  <v-card subtitle="HTTP">
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-text-field
        :label="$t('types.un')"
        hide-details
        v-model="username">
        </v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-text-field
        :label="$t('types.pw')"
        hide-details
        v-model="password">
        </v-text-field>
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-text-field
        :label="$t('transport.path')"
        hide-details
        v-model="data.path">
        </v-text-field>
      </v-col>
    </v-row>
    <Headers :data="data" />
    <v-expansion-panels class="http-advanced" variant="accordion">
      <v-expansion-panel value="advanced">
        <v-expansion-panel-title>{{ $t('types.http.advanced') }}</v-expansion-panel-title>
        <v-expansion-panel-text>
          <v-row>
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model.number="version"
                :items="versionOptions"
                :label="$t('types.http.version')"
                hide-details
              />
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-switch
                v-model="disableVersionFallback"
                color="primary"
                :label="$t('types.http.disableVersionFallback')"
                hide-details
              />
            </v-col>
          </v-row>
          <p class="http-advanced__hint">{{ $t('types.http.pathHostHint') }}</p>
          <v-alert v-if="issue" class="mt-3" type="error" variant="tonal">
            {{ $t(`types.http.errors.${issue}`) }}
          </v-alert>
        </v-expansion-panel-text>
      </v-expansion-panel>
    </v-expansion-panels>
  </v-card>
</template>

<script lang="ts">
import Headers from '@/components/Headers.vue'
import { httpOutboundIssue } from '@/utils/httpVersions'

export default {
  props: ['data'],
  data() {
    return {}
  },
  computed: {
    version: {
      get(): number { return Number(this.data.version ?? 0) },
      set(v:number) { this.data.version = v },
    },
    versionOptions(): Array<{ title: string; value: number }> {
      return [0, 1, 2, 3].map(version => ({
        title: version === 0 ? this.$t('types.http.auto') : this.$t('types.http.versionLabel', { version }),
        value: version,
      }))
    },
    disableVersionFallback: {
      get(): boolean { return this.data.disable_version_fallback === true },
      set(v:boolean) {
        if (v) this.data.disable_version_fallback = true
        else delete this.data.disable_version_fallback
      },
    },
    issue(): string { return httpOutboundIssue(this.data) ?? '' },
    username: {
      get(): string { return this.data.username?.length > 0 ? this.data.username : '' },
      set(v:string) { this.data.username = v.length > 0 ? v : undefined },
    },
    password: {
      get(): string { return this.data.password?.length > 0 ? this.data.password : '' },
      set(v:string) { this.data.password = v.length > 0 ? v : undefined },
    },
  },
  components: { Headers }
}
</script>

<style scoped>
.http-advanced { margin-top: 10px; }
.http-advanced__hint { margin: 8px 0 0; color: rgba(var(--v-theme-on-surface), .68); font-size: .82rem; }
</style>
