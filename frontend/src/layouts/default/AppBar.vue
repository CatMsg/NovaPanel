<template>
  <v-app-bar class="app-bar" flat height="72">
    <div class="app-bar__frame">
      <v-btn v-if="isMobile" icon variant="text" class="app-bar__menu" :aria-label="$t('menu.open')" :title="$t('menu.open')" @click="emit('toggleDrawer')">
        <v-icon icon="mdi-menu" />
      </v-btn>
      <div v-else class="app-bar__spacer"></div>
      <div class="app-bar__title">
        <div class="app-bar__eyebrow">{{ $t('main.hero.badge') }}</div>
        <v-app-bar-title :text="$t(<string>route.name)" class="app-bar__headline" />
      </div>
      <v-spacer />
      <div class="app-bar__actions">
        <PwaInstallButton />
        <v-btn
          variant="text"
          class="app-bar__search-btn"
          :aria-label="$t('ui.nav.commandTitle')"
          :title="$t('ui.nav.commandHint')"
          @click="openCommandPalette"
        >
          <v-icon icon="mdi-magnify" />
          <span v-if="!isMobile">{{ $t('ui.nav.commandTitle') }}</span>
          <kbd v-if="!isMobile">⌘K</kbd>
        </v-btn>
        <v-menu>
          <template v-slot:activator="{ props }">
            <v-btn icon v-bind="props" variant="text" class="app-bar__icon-btn" :aria-label="$t('menu.language')" :title="$t('menu.language')">
              <v-icon>mdi-translate</v-icon>
            </v-btn>
          </template>
          <v-list>
            <v-list-item
              v-for="lang in languages"
              :key="lang.value"
              @click="changeLocale(lang.value)"
              :active="isActiveLocale(lang.value)"
            >
              <v-list-item-title>{{ lang.title }}</v-list-item-title>
            </v-list-item>
          </v-list>
        </v-menu>
        <v-menu>
          <template v-slot:activator="{ props }">
            <v-btn icon v-bind="props" variant="text" class="app-bar__icon-btn" :aria-label="$t('menu.theme')" :title="$t('menu.theme')">
              <v-icon>mdi-theme-light-dark</v-icon>
            </v-btn>
          </template>
          <v-list>
            <v-list-item
              v-for="th in themes"
              :key="th.value"
              @click="changeTheme(th.value)"
              :prepend-icon="th.icon"
              :active="isActiveTheme(th.value)"
            >
              <v-list-item-title>{{ $t(`theme.${th.value}`) }}</v-list-item-title>
            </v-list-item>
          </v-list>
        </v-menu>
      </div>
    </div>
  </v-app-bar>
  <v-dialog v-model="commandOpen" max-width="560" @after-leave="finishCommandLeave">
    <v-card class="command-palette" rounded="xl">
      <v-text-field
        ref="commandInput"
        v-model="commandQuery"
        :placeholder="$t('ui.nav.commandHint')"
        prepend-inner-icon="mdi-magnify"
        variant="solo"
        density="comfortable"
        hide-details
        autofocus
        @keydown.enter.prevent="openFirstCommand"
      />
      <v-divider />
      <v-list v-if="filteredCommands.length" density="comfortable" nav>
        <v-list-item
          v-for="item in filteredCommands"
          :key="item.path"
          :to="item.path"
          :prepend-icon="item.icon"
          :title="$t(item.title)"
          :subtitle="$t(item.group)"
          rounded="lg"
          @click="commandOpen = false"
        />
      </v-list>
      <v-card-text v-else class="command-palette__empty">{{ $t('ui.nav.noResults') }}</v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { useLocale, useTheme } from 'vuetify'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { languages } from '@/locales'
import PwaInstallButton from '@/components/PwaInstallButton.vue'
import { navigationGroups } from '@/navigation'

defineProps(['isMobile'])
const emit = defineEmits(['toggleDrawer'])

const route = useRoute()
const router = useRouter()
const { locale: i18nLocale, t } = useI18n()
const vuetifyLocale = useLocale()
const theme = useTheme()
const commandOpen = ref(false)
const commandClosing = ref(false)
const reopenCommandAfterClose = ref(false)
const commandQuery = ref('')
const commandItems = navigationGroups.flatMap(group => group.items.map(item => ({ ...item, group: group.title })))
const filteredCommands = computed(() => {
  const query = commandQuery.value.trim().toLocaleLowerCase()
  return commandItems.filter(item => !query || `${t(item.title)} ${t(item.group)}`.toLocaleLowerCase().includes(query))
})

const openFirstCommand = () => {
  const first = filteredCommands.value[0]
  if (!first) return
  commandOpen.value = false
  commandQuery.value = ''
  void router.push(first.path)
}

const openCommandPalette = () => {
  if (commandClosing.value) {
    reopenCommandAfterClose.value = true
    return
  }
  commandOpen.value = true
}

const finishCommandLeave = () => {
  commandClosing.value = false
  if (!reopenCommandAfterClose.value) return
  reopenCommandAfterClose.value = false
  commandOpen.value = true
}

watch(commandOpen, (open, wasOpen) => {
  if (!open && wasOpen) commandClosing.value = true
})

const handleShortcut = (event: KeyboardEvent) => {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault()
    if (commandClosing.value) {
      reopenCommandAfterClose.value = !reopenCommandAfterClose.value
      return
    }
    commandOpen.value = !commandOpen.value
  }
}

const stopRouteWatch = watch(() => route.fullPath, () => {
  reopenCommandAfterClose.value = false
  commandOpen.value = false
  commandQuery.value = ''
})

onMounted(() => window.addEventListener('keydown', handleShortcut))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleShortcut)
  stopRouteWatch()
})

const changeLocale = (l: string) => {
  i18nLocale.value = l
  vuetifyLocale.current.value = l
  localStorage.setItem('locale', l)
  window.location.reload()
}
const isActiveLocale = (l: string) => i18nLocale.value === l
const themes = [
  { value: 'light', icon: 'mdi-white-balance-sunny' },
  { value: 'dark', icon: 'mdi-moon-waning-crescent' },
  { value: 'system', icon: 'mdi-laptop' },
]

const changeTheme = (th: string) => {
  theme.change(th)
  localStorage.setItem('theme', th)
}
const isActiveTheme = (th: string) => {
  const current = localStorage.getItem('theme') ?? 'system'
  return current == th
}
</script>

<style scoped>
.app-bar__search-btn { min-width: 0; gap: 7px; border-radius: 12px; }
.app-bar__search-btn span { font-size: .78rem; }
.app-bar__search-btn kbd { padding: 2px 5px; border: 1px solid var(--np-border); border-radius: 5px; color: var(--np-text-muted); font-family: inherit; font-size: .65rem; font-weight: 600; line-height: 1.2; }
.command-palette { overflow: hidden; }
.command-palette :deep(.v-list) { max-height: min(62vh, 480px); overflow: auto; }
.command-palette__empty { color: var(--np-text-muted); }
@media (max-width: 600px) {
  .app-bar__search-btn { padding-inline: 8px; }
}
</style>
