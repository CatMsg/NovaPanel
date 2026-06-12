<template>
  <v-app-bar class="app-bar" flat height="72">
    <div class="app-bar__frame">
      <v-btn v-if="isMobile" icon variant="text" class="app-bar__menu" @click="$emit('toggleDrawer')">
        <v-icon icon="mdi-menu" />
      </v-btn>
      <div v-else class="app-bar__spacer"></div>
      <div class="app-bar__title">
        <div class="app-bar__eyebrow">{{ $t('main.hero.badge') }}</div>
        <v-app-bar-title :text="$t(<string>route.name)" class="app-bar__headline" />
      </div>
      <v-spacer />
      <div class="app-bar__actions">
        <v-menu>
          <template v-slot:activator="{ props }">
            <v-btn icon v-bind="props" variant="text" class="app-bar__icon-btn">
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
            <v-btn icon v-bind="props" variant="text" class="app-bar__icon-btn">
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
</template>

<script lang="ts" setup>
import { useLocale, useTheme } from 'vuetify'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { languages } from '@/locales'

defineProps(['isMobile'])

const route = useRoute()
const { locale: i18nLocale } = useI18n()
const vuetifyLocale = useLocale()
const theme = useTheme()

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
