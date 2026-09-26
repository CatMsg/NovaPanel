<template>
  <v-navigation-drawer
    class="app-drawer"
    v-model="showDrawer"
    :temporary="isMobile"
    :permanent="!isMobile"
    :width="isMobile ? 296 : 280"
  >
    <v-list-item class="app-drawer__brand" :prepend-avatar="logoUrl" title="NovaPanel" :subtitle="$t('main.hero.badge')">
      <template v-slot:append v-if="isMobile">
        <v-btn icon variant="text" :aria-label="$t('menu.close')" :title="$t('menu.close')" @click.stop="emit('update:displayDrawer', false)">
          <v-icon icon="mdi-close" />
        </v-btn>
      </template>
    </v-list-item>

    <v-divider></v-divider>

    <div class="app-drawer__search">
      <v-text-field
        v-model="search"
        :placeholder="$t('ui.nav.search')"
        :aria-label="$t('ui.nav.search')"
        prepend-inner-icon="mdi-magnify"
        density="compact"
        variant="solo-filled"
        rounded="lg"
        hide-details
        clearable
        @keydown.enter.prevent="openFirstMatch"
      />
    </div>

    <v-list density="compact" nav class="app-drawer__nav">
      <template v-for="group in filteredGroups" :key="group.title">
        <v-list-subheader class="app-drawer__group">{{ $t(group.title) }}</v-list-subheader>
        <v-list-item
          v-for="item in group.items"
          :key="item.path"
          link
          class="app-drawer__item"
          :to="item.path"
          :active="router.currentRoute.value.path == item.path"
        >
          <template #prepend>
            <v-icon :icon="item.icon" />
          </template>
          <v-list-item-title>{{ $t(item.title) }}</v-list-item-title>
        </v-list-item>
      </template>
      <v-list-item v-if="filteredGroups.length === 0" class="app-drawer__no-results" disabled>
        <v-list-item-title>{{ $t('ui.nav.noResults') }}</v-list-item-title>
      </v-list-item>
    </v-list>
    <template v-slot:append>
      <v-list-item prepend-icon="mdi-logout" :title="$t('menu.logout')" @click="Logout"></v-list-item>
    </template>
  </v-navigation-drawer>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'
import router from '@/router'
import { logout } from '@/plugins/httputil'
import logoUrl from '@/assets/logo.png'
import { navigationGroups } from '@/navigation'
import { useI18n } from 'vue-i18n'

const props = defineProps(['isMobile','displayDrawer'])
const emit = defineEmits(['update:displayDrawer'])
const { t } = useI18n()
const search = ref('')

const filteredGroups = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  return navigationGroups.map(group => ({
    ...group,
    items: group.items.filter(item => !query || t(item.title).toLocaleLowerCase().includes(query)),
  })).filter(group => group.items.length > 0)
})

const openFirstMatch = () => {
  const first = filteredGroups.value[0]?.items[0]
  if (!first) return
  void router.push(first.path)
  search.value = ''
}

const showDrawer = computed({
  get: () => props.displayDrawer,
  set: (value: boolean) => {
    emit('update:displayDrawer', value)
  },
})

const Logout = async () => {
  logout()
}
</script>

<style scoped>
.app-drawer__search { padding: 12px 14px 4px; }
.app-drawer__nav { padding-top: 2px; }
.app-drawer__group { min-height: 30px; padding-inline: 16px; color: var(--np-text-muted); font-size: .66rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
.app-drawer__no-results { color: var(--np-text-muted); }
</style>
