<template>
  <v-card class="snell-editor" variant="flat">
    <v-card-text>
      <v-select
        v-model="data.user_mode"
        :items="authOptions"
        :label="$t('types.snell.authMode')"
        variant="outlined"
        density="comfortable"
        class="mb-3"
      />
      <v-alert
        :type="data.user_mode === 'per-user' ? 'info' : 'warning'"
        variant="tonal"
        density="compact"
        class="mb-4"
      >
        {{ $t(data.user_mode === 'per-user' ? 'types.snell.perUserWarning' : 'types.snell.clientWarning') }}
      </v-alert>
      <v-alert type="info" variant="tonal" density="compact" class="mb-4 mode-guide">
        <div class="mode-guide__title">{{ $t('types.snell.modeHint') }}</div>
        <div><strong>{{ $t('types.snell.modeDefault') }}:</strong> {{ $t('types.snell.modeDefaultDescription') }}</div>
        <div><strong>{{ $t('types.snell.modeUnshaped') }}:</strong> {{ $t('types.snell.modeUnshapedDescription') }}</div>
        <div><strong>{{ $t('types.snell.modeUnsafeRaw') }}:</strong> {{ $t('types.snell.modeUnsafeRawDescription') }}</div>
      </v-alert>
      <v-row>
        <v-col v-if="!perTarget" cols="12" sm="7">
          <v-text-field
            v-model="data.psk"
            :label="$t('types.snell.psk')"
            :hint="$t('types.snell.pskHint')"
            persistent-hint
            type="password"
            autocomplete="new-password"
            variant="outlined"
            density="comfortable"
          />
        </v-col>
        <v-col v-else cols="12" sm="7">
          <v-alert type="success" variant="tonal" density="compact">
            {{ $t('types.snell.perTargetPsk') }}
          </v-alert>
        </v-col>
        <v-col cols="12" sm="5">
          <v-select
            v-model="data.mode"
            :items="modeOptions"
            :label="$t('types.snell.mode')"
            variant="outlined"
            density="comfortable"
            hide-details
          />
        </v-col>
      </v-row>
      <v-alert v-if="data.mode === 'unsafe-raw'" type="error" variant="tonal" density="compact" class="mt-3">
        {{ $t('types.snell.modeUnsafeWarning') }}
      </v-alert>
      <v-chip size="small" color="primary" variant="tonal">Snell v6</v-chip>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

withDefaults(defineProps<{ data: Record<string, any>; perTarget?: boolean }>(), {
  perTarget: false,
})

const { t } = useI18n()
const authOptions = computed(() => [
  { title: t('types.snell.authShared'), value: 'shared' },
  { title: t('types.snell.authPerUser'), value: 'per-user' },
])
const modeOptions = computed(() => [
  { title: t('types.snell.modeDefault'), value: 'default' },
  { title: t('types.snell.modeUnshaped'), value: 'unshaped' },
  { title: t('types.snell.modeUnsafeRaw'), value: 'unsafe-raw' },
])
</script>

<style scoped>
.snell-editor {
  margin-top: 12px;
  border: 1px solid rgba(var(--v-theme-primary), .16);
  background: linear-gradient(145deg, rgba(var(--v-theme-primary), .08), rgba(var(--v-theme-surface), .72));
}

.mode-guide__title {
  font-weight: 600;
  margin-bottom: 4px;
}
</style>
