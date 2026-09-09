<template>
  <template v-if="available">
    <v-btn icon variant="text" class="app-bar__icon-btn" aria-label="安装 NovaPanel" title="安装 NovaPanel" @click="install">
      <v-icon icon="mdi-monitor-arrow-down-variant" />
    </v-btn>
    <v-dialog v-model="showIOSHelp" max-width="420">
      <v-card rounded="xl" class="pwa-help">
        <v-card-title>添加到主屏幕</v-card-title>
        <v-card-text>
          在 Safari 底部点击“共享”，选择“添加到主屏幕”，确认后 NovaPanel 会以独立应用打开。
        </v-card-text>
        <v-card-actions><v-spacer /><v-btn color="primary" variant="tonal" @click="showIOSHelp = false">知道了</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
  </template>
</template>

<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const promptReady = ref(Boolean((window as any).__NOVAPANEL_INSTALL_PROMPT__))
const showIOSHelp = ref(false)
const standalone = window.matchMedia('(display-mode: standalone)').matches || (navigator as any).standalone === true
const isIOS = /iphone|ipad|ipod/i.test(navigator.userAgent)
const available = computed(() => !standalone && (promptReady.value || isIOS))

const onReady = () => { promptReady.value = true }
const onInstalled = () => { promptReady.value = false }

onMounted(() => {
  window.addEventListener('novapanel-install-ready', onReady)
  window.addEventListener('novapanel-installed', onInstalled)
})

onBeforeUnmount(() => {
  window.removeEventListener('novapanel-install-ready', onReady)
  window.removeEventListener('novapanel-installed', onInstalled)
})

async function install() {
  const prompt = (window as any).__NOVAPANEL_INSTALL_PROMPT__
  if (!prompt) {
    showIOSHelp.value = true
    return
  }
  await prompt.prompt()
  await prompt.userChoice
  ;(window as any).__NOVAPANEL_INSTALL_PROMPT__ = null
  promptReady.value = false
}
</script>

<style scoped>
.pwa-help { background: var(--np-surface); }
</style>
