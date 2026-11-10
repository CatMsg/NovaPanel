<template>
  <v-card class="snell-editor" variant="flat">
    <v-card-text>
      <v-alert type="warning" variant="tonal" density="compact" class="mb-4">
        {{ $t('types.snell.clientWarning') }}
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
          <v-text-field
            :model-value="$t('types.snell.modeUnshaped')"
            :label="$t('types.snell.mode')"
            readonly
            variant="outlined"
            density="comfortable"
            hide-details
          />
        </v-col>
      </v-row>
      <v-chip size="small" color="primary" variant="tonal">Snell v6</v-chip>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ data: Record<string, any>; perTarget?: boolean }>(), {
  perTarget: false,
})
</script>

<style scoped>
.snell-editor {
  margin-top: 12px;
  border: 1px solid rgba(var(--v-theme-primary), .16);
  background: linear-gradient(145deg, rgba(var(--v-theme-primary), .08), rgba(var(--v-theme-surface), .72));
}
</style>
