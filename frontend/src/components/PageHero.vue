<template>
  <v-card class="page-hero" rounded="xl" variant="flat">
    <div class="page-hero__badges">
      <span class="page-hero__badge">{{ eyebrow }}</span>
      <span v-if="status" class="page-hero__badge page-hero__badge--soft">{{ status }}</span>
    </div>
    <v-row class="page-hero__content" align="center">
      <v-col cols="12" lg="8">
        <div class="page-hero__title-row">
          <div class="page-hero__icon">
            <v-icon :icon="icon" size="30" />
          </div>
          <div class="page-hero__copy">
            <h1>{{ title }}</h1>
            <p>{{ description }}</p>
          </div>
        </div>
        <div v-if="$slots.meta" class="page-hero__meta">
          <slot name="meta" />
        </div>
      </v-col>
      <v-col v-if="$slots.actions" cols="12" lg="4" class="page-hero__actions">
        <slot name="actions" />
      </v-col>
    </v-row>
  </v-card>
</template>

<script setup lang="ts">
defineProps<{
  eyebrow: string
  title: string
  description: string
  icon: string
  status?: string
}>()
</script>

<style scoped>
.page-hero {
  padding: clamp(18px, 3vw, 28px);
  margin-bottom: 18px;
  border: 1px solid var(--np-border);
  background:
    linear-gradient(135deg, rgba(255, 255, 255, 0.18), transparent 38%),
    var(--np-surface-strong);
  box-shadow: var(--np-shadow);
  overflow: hidden;
}

.page-hero__badges,
.page-hero__meta,
.page-hero__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.page-hero__badge {
  padding: 6px 12px;
  border: 1px solid rgba(10, 132, 255, 0.16);
  border-radius: 999px;
  color: var(--np-accent);
  background: rgba(10, 132, 255, 0.08);
  font-size: 12px;
  font-weight: 750;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.page-hero__badge--soft {
  border-color: var(--np-border);
  color: var(--np-text-muted);
  background: var(--np-surface-muted);
  letter-spacing: 0;
  text-transform: none;
}

.page-hero__content {
  min-height: 112px;
  margin-top: 10px;
}

.page-hero__title-row {
  display: flex;
  align-items: flex-start;
  gap: 15px;
}

.page-hero__icon {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 54px;
  height: 54px;
  border-radius: 18px;
  color: var(--np-accent);
  background: linear-gradient(145deg, rgba(10, 132, 255, 0.18), rgba(20, 184, 166, 0.08));
}

.page-hero__copy {
  min-width: 0;
}

.page-hero h1 {
  margin: 0;
  font-size: clamp(28px, 3vw, 40px);
  line-height: 1.08;
  font-weight: 800;
  letter-spacing: -0.04em;
}

.page-hero p {
  max-width: 760px;
  margin: 10px 0 0;
  color: var(--np-text-muted);
  line-height: 1.65;
}

.page-hero__meta {
  margin-top: 16px;
  color: var(--np-text-muted);
  font-size: 13px;
}

.page-hero__actions {
  justify-content: flex-end;
}

@media (max-width: 959px) {
  .page-hero__actions {
    justify-content: flex-start;
  }
}

@media (max-width: 599px) {
  .page-hero {
    border-radius: 24px !important;
  }

  .page-hero__title-row {
    align-items: center;
  }

  .page-hero__icon {
    width: 46px;
    height: 46px;
    border-radius: 15px;
  }

  .page-hero h1 {
    font-size: 25px;
  }

  .page-hero__actions :deep(.v-btn) {
    flex: 1 1 140px;
  }
}
</style>
