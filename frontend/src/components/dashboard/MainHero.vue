<template>
  <v-card class="main-hero" rounded="xl" variant="flat">
    <div class="main-hero__topline">
      <span class="main-hero__badge">{{ $t('main.hero.badge') }}</span>
      <span class="main-hero__badge main-hero__badge--soft">{{ $t('main.tiles') }}</span>
    </div>
    <v-row class="main-hero__content" align="center">
      <v-col cols="12" lg="7">
        <div class="main-hero__brand">
          <div class="main-hero__brand-icon">
            <v-img :src="logoUrl" alt="NovaPanel logo" cover />
          </div>
          <div class="main-hero__brand-copy">
            <div class="main-hero__eyebrow">NovaPanel</div>
          </div>
        </div>
        <p class="main-hero__subtitle">{{ $t('main.hero.subtitle') }}</p>
      </v-col>
      <v-col cols="12" lg="5">
        <v-card class="main-hero__panel" rounded="xl" variant="flat">
          <div class="main-hero__panel-head">
            <div class="main-hero__panel-title">
              <span class="main-hero__live-dot"></span>
              {{ $t('main.hero.live') }}
            </div>
            <span class="main-hero__panel-count">{{ items.length }}</span>
          </div>
          <div class="main-hero__status-list">
            <div
              v-for="(item, index) in items"
              :key="item.label"
              class="main-hero__status-row"
              :class="{ 'main-hero__status-row--last': index === items.length - 1 }"
            >
              <span class="main-hero__status-row-left">
                <span class="main-hero__status-row-icon" :class="`main-hero__status-row-icon--${item.tone}`">
                  <v-icon :icon="item.icon" size="small" />
                </span>
                <span class="main-hero__status-row-label">{{ item.label }}</span>
              </span>
              <strong class="main-hero__status-row-value">{{ item.value }}</strong>
            </div>
          </div>
        </v-card>
      </v-col>
    </v-row>
  </v-card>
</template>

<script setup lang="ts">
defineProps<{
  logoUrl: string
  items: Array<{
    label: string
    value: string
    icon: string
    tone: string
  }>
}>()
</script>

<style scoped lang="scss">
.main-hero,
.main-hero__panel {
  border: 1px solid var(--np-border);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.2), transparent 28%),
    var(--np-surface);
  backdrop-filter: blur(30px) saturate(1.15);
  box-shadow: var(--np-shadow);
}

.main-hero,
.main-hero__panel {
  position: relative;
  overflow: hidden;
}

.main-hero::before,
.main-hero__panel::before {
  content: '';
  position: absolute;
  inset: 0;
  z-index: 0;
  pointer-events: none;
  background:
    radial-gradient(circle at top left, rgba(255, 255, 255, 0.38), transparent 32%),
    radial-gradient(circle at bottom right, rgba(10, 132, 255, 0.08), transparent 42%);
  opacity: 0.9;
}

.main-hero > *,
.main-hero__panel > * {
  position: relative;
  z-index: 1;
}

.main-hero {
  padding: 28px;
  border-radius: 32px;
}

.main-hero__topline {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 18px;
}

.main-hero__badge {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border-radius: 999px;
  padding: 6px 12px;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  border: 1px solid rgba(10, 132, 255, 0.14);
  background: rgba(255, 255, 255, 0.36);
  color: var(--np-accent);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.55);
}

.main-hero__badge--soft {
  color: var(--np-text-muted);
  background: rgba(255, 255, 255, 0.26);
}

.main-hero__content {
  row-gap: 18px;
}

.main-hero__brand {
  display: flex;
  align-items: center;
  gap: 14px;
}

.main-hero__brand-icon {
  flex: 0 0 auto;
  width: 52px;
  height: 52px;
  overflow: hidden;
  border-radius: 18px;
  border: 1px solid rgba(125, 211, 252, 0.18);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.6), rgba(255, 255, 255, 0.28)),
    rgba(255, 255, 255, 0.46);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.7),
    0 12px 24px rgba(15, 23, 42, 0.08);
}

.main-hero__brand-icon :deep(img) {
  object-fit: cover;
}

.main-hero__brand-copy {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-top: 0;
}

.main-hero__eyebrow {
  display: inline-flex;
  align-self: flex-start;
  padding: 0;
  border-radius: 0;
  border: 0;
  background: transparent;
  font-size: clamp(1.6rem, 2.5vw, 2.2rem);
  font-weight: 800;
  line-height: 1;
  letter-spacing: -0.055em;
  text-transform: none;
  color: var(--np-accent);
}

.main-hero__subtitle {
  max-width: 54ch;
  margin: 16px 0 0;
  color: var(--np-text-muted);
  font-size: 0.98rem;
  line-height: 1.9;
}

.main-hero__panel {
  padding: 18px;
  border-radius: 28px;
  background: rgba(255, 255, 255, 0.3);
}

.main-hero__panel-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 0.92rem;
  font-weight: 700;
  color: var(--np-text-muted);
}

.main-hero__panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.main-hero__live-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: rgb(34, 197, 94);
  box-shadow: 0 0 0 5px rgba(34, 197, 94, 0.12);
}

.main-hero__panel-count {
  display: grid;
  min-width: 26px;
  height: 26px;
  place-items: center;
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 999px;
  color: var(--np-text-muted);
  background: rgba(255, 255, 255, 0.26);
  font-size: 0.72rem;
  font-weight: 800;
}

.main-hero__status-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.main-hero__status-row {
  display: flex;
  min-width: 0;
  min-height: 88px;
  flex-direction: column;
  align-items: stretch;
  justify-content: space-between;
  gap: 12px;
  padding: 12px;
  border: 1px solid rgba(148, 163, 184, 0.14);
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.34);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.38);
}

.main-hero__status-row--last {
  border-bottom: 1px solid rgba(148, 163, 184, 0.14);
}

.main-hero__status-row-left {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.main-hero__status-row-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  flex: 0 0 auto;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.68);
  color: var(--np-accent);
  border: 1px solid rgba(10, 132, 255, 0.12);
}

.main-hero__status-row-icon--green {
  color: rgb(34, 197, 94);
}

.main-hero__status-row-icon--red {
  color: rgb(239, 68, 68);
}

.main-hero__status-row-icon--indigo {
  color: rgb(79, 70, 229);
}

.main-hero__status-row-icon--teal {
  color: rgb(20, 184, 166);
}

.main-hero__status-row-label {
  min-width: 0;
  font-size: 0.82rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--np-text-muted);
}

.main-hero__status-row-value {
  min-width: 0;
  padding-left: 38px;
  overflow: hidden;
  font-size: 0.98rem;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--np-text-main);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.main-hero.v-theme--dark,
.main-hero.v-theme--dark .main-hero__panel {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.1), transparent 24%),
    rgba(17, 24, 39, 0.96) !important;
  border-color: rgba(148, 163, 184, 0.18) !important;
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.08),
    0 24px 70px rgba(0, 0, 0, 0.3) !important;
  backdrop-filter: none !important;
  -webkit-backdrop-filter: none !important;
}

.main-hero.v-theme--dark .main-hero__badge {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(148, 163, 184, 0.18);
}

.main-hero.v-theme--dark .main-hero__status-row {
  background:
    linear-gradient(145deg, rgba(255, 255, 255, 0.07), transparent 62%),
    rgba(22, 30, 44, 0.72);
  border-color: rgba(148, 163, 184, 0.16);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.06),
    0 10px 22px rgba(0, 0, 0, 0.12);
}

.main-hero.v-theme--dark .main-hero__panel-count {
  border-color: rgba(148, 163, 184, 0.18);
  background: rgba(255, 255, 255, 0.07);
}

.main-hero.v-theme--dark .main-hero__status-row-icon {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(148, 163, 184, 0.2);
}

.main-hero.v-theme--dark .main-hero__status-row-label {
  color: rgba(186, 202, 224, 0.76);
}

.main-hero.v-theme--dark .main-hero__status-row-value {
  color: rgba(237, 244, 255, 0.96);
}

.main-hero.v-theme--dark .main-hero__badge--soft {
  color: rgba(186, 202, 224, 0.78);
}

.main-hero.v-theme--dark .main-hero__brand-icon {
  border-color: rgba(125, 211, 252, 0.14);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.12), rgba(255, 255, 255, 0.04)),
    rgba(10, 16, 28, 0.82);
}

.main-hero.v-theme--dark .main-hero__subtitle,
.main-hero.v-theme--dark .main-hero__panel-title {
  color: rgba(186, 202, 224, 0.78);
}

@media (max-width: 960px) {
  .main-hero {
    padding: 14px;
  }
}

@media (max-width: 600px) {
  .main-hero {
    padding: 12px;
    border-radius: 24px;
  }

  .main-hero__content {
    row-gap: 12px;
  }

  .main-hero__content > .v-col {
    padding-top: 0;
    padding-bottom: 0;
  }

  .main-hero__topline {
    display: none;
  }

  .main-hero__brand {
    align-items: center;
    gap: 10px;
  }

  .main-hero__brand-icon {
    width: 44px;
    height: 44px;
    border-radius: 15px;
  }

  .main-hero__eyebrow {
    font-size: clamp(1.35rem, 8vw, 1.65rem);
  }

  .main-hero__subtitle {
    margin-top: 10px;
    font-size: 0.9rem;
    line-height: 1.55;
  }

  .main-hero__panel {
    padding: 10px 12px 8px;
    border-radius: 20px;
  }

  .main-hero__panel-title {
    font-size: 0.8rem;
  }

  .main-hero__panel-head {
    margin-bottom: 8px;
  }

  .main-hero__status-list {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }

  .main-hero__status-row {
    min-width: 0;
    gap: 8px;
    padding: 10px 12px;
    border-radius: 18px;
    flex-direction: column;
    align-items: flex-start;
  }

  .main-hero__status-row-left {
    align-items: flex-start;
    gap: 8px;
    width: 100%;
  }

  .main-hero__status-row-icon {
    width: 28px;
    height: 28px;
  }

  .main-hero__status-row-label {
    font-size: 0.7rem;
    letter-spacing: 0.05em;
  }

  .main-hero__status-row-value {
    font-size: 0.9rem;
    padding-left: 36px;
  }
}
</style>
