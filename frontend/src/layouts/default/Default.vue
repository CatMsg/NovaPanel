<template>
  <v-app class="app-root">
    <drawer
      v-if="!isMobile || displayDrawer"
      :isMobile="isMobile"
      :displayDrawer="displayDrawer"
      @update:displayDrawer="displayDrawer = $event"
    />
    <default-bar :isMobile="isMobile" @toggleDrawer="toggleDrawer" />
    <default-view />
  </v-app>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import DefaultBar from './AppBar.vue'
import Drawer from './Drawer.vue'
import DefaultView from './View.vue'
import { useDisplay } from 'vuetify'

const { smAndDown } = useDisplay()
const route = useRoute()
const displayDrawer = ref(false)

const toggleDrawer = () => {
  displayDrawer.value = !displayDrawer.value
}

const isMobile = computed((): boolean => smAndDown.value)

watch(smAndDown, (mobile) => {
  displayDrawer.value = !mobile
}, { immediate: true })

watch(() => route.fullPath, () => {
  if (isMobile.value) displayDrawer.value = false
})
</script>
