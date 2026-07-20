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

    <v-list density="compact" nav>
      <v-list-item link
        v-for="item in menu"
        :key="item.title"
        class="app-drawer__item"
        :to="item.path"
        :active="router.currentRoute.value.path == item.path">
        <template v-slot:prepend>
          <v-icon :icon="item.icon"></v-icon>
        </template>
        <v-list-item-title v-text="$t(item.title)"></v-list-item-title>
      </v-list-item>
    </v-list>
    <template v-slot:append>
      <v-list-item prepend-icon="mdi-logout" :title="$t('menu.logout')" @click="Logout"></v-list-item>
    </template>
  </v-navigation-drawer>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import router from '@/router'
import { logout } from '@/plugins/httputil'
import logoUrl from '@/assets/logo.png'

const props = defineProps(['isMobile','displayDrawer'])
const emit = defineEmits(['update:displayDrawer'])

const showDrawer = computed({
  get: () => props.displayDrawer,
  set: (value: boolean) => {
    emit('update:displayDrawer', value)
  },
})

const menu = [
  { title: 'pages.home', icon: 'mdi-home',  path: '/' },
  { title: 'pages.ports', icon: 'mdi-lan',  path: '/ports' },
  { title: 'pages.fleet', icon: 'mdi-server-network',  path: '/fleet' },
  { title: 'pages.inbounds', icon: 'mdi-cloud-download',  path: '/inbounds' },
  { title: 'pages.clients', icon: 'mdi-account-multiple',  path: '/clients' },
  { title: 'pages.outbounds', icon: 'mdi-cloud-upload',  path: '/outbounds' },
  { title: 'pages.endpoints', icon: 'mdi-cloud-tags',  path: '/endpoints' },
  { title: 'pages.services', icon: 'mdi-server',  path: '/services' },
  { title: 'pages.tls', icon: 'mdi-certificate',  path: '/tls' },
  { title: 'pages.basics', icon: 'mdi-application-cog',  path: '/basics' },
  { title: 'pages.rules', icon: 'mdi-routes',  path: '/rules' },
  { title: 'pages.dns', icon: 'mdi-dns',  path: '/dns' },
  { title: 'pages.admins', icon: 'mdi-account-tie',  path: '/admins' },
  { title: 'pages.settings', icon: 'mdi-cog',  path: '/settings' },
]

const Logout = async () => {
  logout()
}
</script>
