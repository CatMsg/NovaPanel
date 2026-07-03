<template>
  <v-card class="main-toolbar" rounded="xl" variant="flat">
    <div class="main-toolbar__label">{{ $t('main.tiles') }}</div>
    <div class="main-toolbar__actions">
      <v-dialog :model-value="menu" :close-on-content-click="false" transition="scale-transition" max-width="900" @update:model-value="emit('update:menu', $event)">
        <template #activator="{ props }">
          <v-btn v-bind="props" class="main-toolbar__button" hide-details variant="flat">
            <v-icon icon="mdi-star-plus" start />
            {{ $t('main.tiles') }}
          </v-btn>
        </template>
        <v-card class="main-menu" rounded="xl" variant="flat">
          <v-card-title class="main-menu__title">
            <span>{{ $t('main.tiles') }}</span>
            <v-btn icon variant="text" @click="emit('update:menu', false)">
              <v-icon icon="mdi-close"></v-icon>
            </v-btn>
          </v-card-title>
          <v-divider></v-divider>
          <v-row v-for="items in menuItems" :key="items.title" density="compact" class="main-menu__group">
            <v-col cols="12">
              <v-card :subtitle="items.title" variant="flat" class="main-menu__section">
                <v-card-text>
                  <v-row density="compact">
                    <v-col cols="12" md="6" lg="3" v-for="item in items.value" :key="item.value">
                      <v-switch
                        density="compact"
                        :model-value="reloadItems"
                        :value="item.value"
                        color="primary"
                        :label="item.title"
                        hide-details
                        @update:model-value="updateReloadItems"
                      ></v-switch>
                    </v-col>
                  </v-row>
                </v-card-text>
              </v-card>
            </v-col>
          </v-row>
        </v-card>
      </v-dialog>
      <v-btn class="main-toolbar__button" variant="flat" hide-details @click="emit('backup')">
        <v-icon icon="mdi-backup-restore" start />
        {{ $t('main.backup.title') }}
      </v-btn>
      <v-btn class="main-toolbar__button" variant="flat" hide-details @click="emit('log')">
        <v-icon icon="mdi-list-box-outline" start />
        {{ $t('basic.log.title') }}
      </v-btn>
      <v-btn class="main-toolbar__button" variant="flat" hide-details @click="emit('stats')">
        <v-icon icon="mdi-chart-box-outline" start />
        {{ $t('main.stats.title') }}
      </v-btn>
    </div>
  </v-card>
</template>

<script setup lang="ts">
type MenuEntry = {
  title: string
  value: Array<{ title: string; value: string }>
}

const props = defineProps<{
  menu: boolean
  menuItems: MenuEntry[]
  reloadItems: string[]
}>()

const emit = defineEmits<{
  (e: 'update:menu', value: boolean): void
  (e: 'update:reloadItems', value: string[]): void
  (e: 'backup'): void
  (e: 'log'): void
  (e: 'stats'): void
}>()

const updateReloadItems = (value: string[] | null) => {
  emit('update:reloadItems', value ?? props.reloadItems)
}
</script>

<style scoped lang="scss">
.main-toolbar,
.main-menu {
  border: 1px solid var(--np-border);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.2), transparent 28%),
    var(--np-surface);
  backdrop-filter: blur(30px) saturate(1.15);
  box-shadow: var(--np-shadow);
}

.main-toolbar,
.main-menu {
  position: relative;
  overflow: hidden;
}

.main-toolbar::before,
.main-menu::before {
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

.main-toolbar > *,
.main-menu > * {
  position: relative;
  z-index: 1;
}

.main-toolbar {
  margin-top: 16px;
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  justify-content: flex-start;
  gap: 12px;
  border-radius: 28px;
}

.main-toolbar__label {
  font-size: 0.88rem;
  font-weight: 700;
  color: var(--np-text-muted);
}

.main-toolbar__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  width: 100%;
  justify-content: flex-start;
}

.main-toolbar__button {
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.5);
  border: 1px solid rgba(148, 163, 184, 0.15);
  color: var(--np-text-main);
  text-transform: none;
  letter-spacing: 0;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.5);
}

.main-toolbar__button:hover {
  transform: translateY(-1px);
}

.main-menu {
  overflow: hidden;
  border-radius: 30px;
  background: rgba(255, 255, 255, 0.42);
}

.main-menu__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 20px;
}

.main-menu__group {
  margin: 0;
}

.main-menu__section {
  background: rgba(255, 255, 255, 0.28);
}

:global(.v-theme--dark) .main-toolbar,
:global(.v-theme--dark) .main-menu {
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

:global(.v-theme--dark) .main-toolbar__button,
:global(.v-theme--dark) .main-menu__section {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(148, 163, 184, 0.18);
}

:global(.v-theme--dark) .main-toolbar__label {
  color: rgba(186, 202, 224, 0.78);
}

@media (max-width: 960px) {
  .main-toolbar {
    padding: 14px;
    align-items: flex-start;
    flex-direction: column;
  }
}

@media (max-width: 600px) {
  .main-toolbar {
    margin-top: 12px;
    padding: 12px;
    gap: 10px;
    align-items: stretch;
  }

  .main-toolbar__label {
    margin-bottom: 2px;
    align-self: flex-start;
  }

  .main-toolbar__actions {
    display: grid;
    grid-template-columns: 1fr;
    gap: 8px;
    width: 100%;
    align-self: stretch;
  }

  .main-toolbar__button {
    width: 100%;
    min-height: 42px;
    padding-inline: 14px;
    justify-content: flex-start;
    border-radius: 16px;
    background:
      linear-gradient(180deg, rgba(255, 255, 255, 0.82), rgba(255, 255, 255, 0.58)),
      rgba(255, 255, 255, 0.4);
    border: 1px solid rgba(148, 163, 184, 0.12);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.62),
      0 10px 18px rgba(15, 23, 42, 0.06);
  }
}
</style>
