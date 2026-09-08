<template>
  <Notivue v-slot="item">
    <NotivueSwipe :item="item">
      <Notification
        :item="item"
        :theme="theme"
        :dir="direction"
        :icons="outlinedIcons"
        :hideClose="true"
        @click="item.clear"
      />
    </NotivueSwipe>
  </Notivue>
</template>

<script lang="ts" setup>
import { Notivue, Notification, NotivueSwipe, outlinedIcons, pastelTheme, darkTheme } from 'notivue'
import { computed } from 'vue'
import { useTheme } from 'vuetify'
import vuetify from '@/plugins/vuetify'

const Theme = useTheme()

const theme = computed(() =>{
  let currenTheme = Theme.global.name.value == "light" ? pastelTheme : darkTheme
  currenTheme = {
    ...currenTheme,
    '--nv-width': 'auto',
    '--nv-radius': '18px',
    '--nv-shadow': '0 18px 48px rgba(15, 23, 42, 0.16)',
  }
  return currenTheme
})

const direction = computed(() => {
  return vuetify.locale.isRtl ? 'rtl' : 'ltr'
})
</script>

<style>
:root {
  --nv-z: 10020;
}

.Notivue__notification {
  max-width: min(420px, calc(100vw - 24px));
  border-color: var(--np-border);
}

.Notivue__transition-enter-active {
  transition: opacity var(--np-duration-fast) var(--np-ease-out), transform var(--np-duration-fast) var(--np-ease-out);
}

.Notivue__transition-enter-from {
  opacity: 0;
  transform: scale(0.96);
}

.Notivue__transition-leave-active {
  transition: opacity var(--np-duration-press) var(--np-ease-out);
}

@media (max-width: 600px) {
  :root {
    --nv-root-left: 8px;
    --nv-root-right: 8px;
    --nv-root-bottom: calc(8px + env(safe-area-inset-bottom));
  }
}

</style>
