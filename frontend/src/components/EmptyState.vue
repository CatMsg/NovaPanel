<template>
  <v-card class="empty-state" rounded="xl" variant="flat">
    <div class="empty-state__icon"><v-icon :icon="icon" size="30" /></div>
    <div class="empty-state__copy">
      <h2>{{ title }}</h2>
      <p>{{ description }}</p>
    </div>
    <v-btn v-if="action" color="primary" variant="tonal" @click="$emit('action')">
      <v-icon icon="mdi-plus" start />{{ action }}
    </v-btn>
  </v-card>
</template>

<script setup lang="ts">
defineProps<{ icon: string; title: string; description: string; action?: string }>()
defineEmits<{ action: [] }>()
</script>

<style scoped>
.empty-state {
  display: flex;
  align-items: center;
  gap: 16px;
  min-height: 126px;
  padding: 22px;
  border: 1px dashed rgba(10, 132, 255, 0.26);
  background: var(--np-surface-muted);
  box-shadow: none;
}

.empty-state__icon {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 52px;
  height: 52px;
  border-radius: 17px;
  color: var(--np-accent);
  background: rgba(10, 132, 255, 0.1);
}

.empty-state__copy {
  flex: 1;
  min-width: 0;
}

.empty-state h2 {
  margin: 0;
  font-size: 17px;
}

.empty-state p {
  margin: 7px 0 0;
  color: var(--np-text-muted);
  line-height: 1.55;
}

@media (max-width: 599px) {
  .empty-state {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .empty-state .v-btn {
    width: 100%;
  }
}
</style>
