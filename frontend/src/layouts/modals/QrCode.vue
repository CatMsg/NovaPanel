<template>
  <v-dialog transition="dialog-bottom-transition" width="min(94vw, 680px)">
    <v-card class="share-card" rounded="xl" id="qrcode-modal" :loading="loading">
      <v-card-title>
        <v-row>
          <v-col>{{ $t('ui.share.title') }}</v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto"><v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="$emit('close')" /></v-col>
        </v-row>
      </v-card-title>
      <v-divider></v-divider>
      <div v-if="!loading" class="share-summary">
        <div class="share-summary__identity">
          <div class="share-summary__avatar">{{ client.name?.slice(0, 1)?.toUpperCase() || '?' }}</div>
          <div><strong>{{ client.name }}</strong><span>{{ client.enable ? $t('ui.share.active') : $t('ui.share.paused') }}</span></div>
        </div>
        <div class="share-summary__metrics">
          <div><span>{{ $t('stats.volume') }}</span><strong>{{ client.volume ? formatBytes(client.volume) : $t('unlimited') }}</strong></div>
          <div><span>{{ $t('date.expiry') }}</span><strong>{{ expiryLabel }}</strong></div>
          <div><span>{{ $t('pages.inbounds') }}</span><strong>{{ inboundCount }}</strong></div>
          <div><span>{{ $t('client.rateLimit') }}</span><strong>↑ {{ formatRate(client.uploadLimit) }} · ↓ {{ formatRate(client.downloadLimit) }}</strong></div>
        </div>
        <div class="share-summary__actions">
          <v-btn :color="client.enable ? 'warning' : 'success'" variant="tonal" :loading="actionLoading" @click="toggleEnabled">
            <v-icon :icon="client.enable ? 'mdi-pause-circle-outline' : 'mdi-play-circle-outline'" start />
            {{ client.enable ? $t('ui.share.pause') : $t('ui.share.resume') }}
          </v-btn>
          <v-btn color="error" variant="tonal" :loading="actionLoading" @click="revokeCredentials">
            <v-icon icon="mdi-key-change" start />{{ $t('ui.share.revoke') }}
          </v-btn>
        </div>
      </div>
      <v-skeleton-loader
          class="mx-auto border"
          width="80%"
          type="text, image, divider, text, image"
          v-if="loading"
        ></v-skeleton-loader>
      <v-card-text style="overflow-y: auto; padding: 0" :hidden="loading">
        <v-tabs
          v-model="tab"
          density="compact"
          fixed-tabs
          align-tabs="center"
        >
          <v-tab value="sub">{{ $t('setting.sub') }}</v-tab>
          <v-tab value="link">{{ $t('client.links') }}</v-tab>
        </v-tabs>
        <v-window v-model="tab" style="margin-top: 10px;">
          <v-window-item value="sub">
            <div class="share-copy-grid">
              <v-btn variant="outlined" @click="copyToClipboard(clientSub)"><v-icon icon="mdi-link-variant" start />{{ $t('setting.sub') }}</v-btn>
              <v-btn variant="outlined" @click="copyToClipboard(clientSub + '?format=json')"><v-icon icon="mdi-code-json" start />{{ $t('setting.jsonSub') }}</v-btn>
              <v-btn color="primary" variant="tonal" @click="copyToClipboard(clientSub + '?format=clash')"><v-icon icon="mdi-content-copy" start />{{ $t('setting.clashSub') }}</v-btn>
            </div>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.sub') }}</v-chip><br />
                <QrcodeVue :value="clientSub" :size="size" @click="copyToClipboard(clientSub)" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.jsonSub') }}</v-chip><br />
                <QrcodeVue :value="clientSub + '?format=json'" :size="size" @click="copyToClipboard(clientSub + '?format=json')" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.clashSub') }}</v-chip><br />
                <QrcodeVue :value="clientSub + '?format=clash'" :size="size" @click="copyToClipboard(clientSub + '?format=clash')" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>SING-BOX (scan only)</v-chip><br />
                <QrcodeVue :value="singbox" :size="size" :margin="1" style="border-radius: .8rem; cursor: not-allowed;" />
              </v-col>
            </v-row>
          </v-window-item>
          <v-window-item value="link">
            <v-row v-for="l in clientLinks" :key="`${l.type}-${l.uri}`">
              <v-col style="text-align: center;">
                <v-chip>{{ l.remark?? $t('client.' + l.type) }}</v-chip><br />
                <QrcodeVue :value="l.uri" :size="size" @click="copyToClipboard(l.uri)" :margin="1" style="border-radius: .5rem; cursor: copy;" />
              </v-col>
            </v-row>
          </v-window-item>
        </v-window>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import QrcodeVue from 'qrcode.vue'
import Data from '@/store/modules/data'
import Clipboard from 'clipboard'
import { i18n } from '@/locales'
import { push } from 'notivue'
import { shuffleConfigs } from '@/types/clients'

export default {
  props: ['id', 'visible'],
  data() {
    return {
      tab: "sub",
      client: <any>{},
      loading: false,
      actionLoading: false,
    }
  },
  methods: {
    async load() {
      this.loading = true
      const newData = await Data().loadClients(this.$props.id)
      this.client = newData
      this.loading = false
    },
    copyToClipboard(txt:string) {
      const hiddenButton = document.createElement('button')
      hiddenButton.className = 'clipboard-btn'
      document.body.appendChild(hiddenButton)

      const clipboard = new Clipboard('.clipboard-btn', {
        text: () => txt,
        container: document.getElementById('qrcode-modal')?? undefined
      });

      clipboard.on('success', () => {
        clipboard.destroy()
        push.success({
          message: i18n.global.t('success') + ": " + i18n.global.t('copyToClipboard'),
          duration: 5000,
        })
      })

      clipboard.on('error', () => {
        clipboard.destroy()
        push.error({
          message: i18n.global.t('failed') + ": " + i18n.global.t('copyToClipboard'),
          duration: 5000,
        })
      })

      // Perform click on hidden button to trigger copy
      hiddenButton.click()
      document.body.removeChild(hiddenButton)
    },
    async toggleEnabled() {
      this.actionLoading = true
      const next = { ...this.client, enable: !this.client.enable }
      if (await Data().save('clients', 'edit', next)) await this.load()
      this.actionLoading = false
    },
    async revokeCredentials() {
      if (!window.confirm(i18n.global.t('ui.share.revokeConfirm'))) return
      this.actionLoading = true
      const next = { ...this.client, config: shuffleConfigs(structuredClone(this.client.config || {})) }
      if (await Data().save('clients', 'edit', next)) await this.load()
      this.actionLoading = false
    },
    formatBytes(value:number) {
      if (!value) return '0 B'
      const units = ['B', 'KB', 'MB', 'GB', 'TB']
      let size = value
      let index = 0
      while (size >= 1024 && index < units.length - 1) { size /= 1024; index += 1 }
      return `${size.toFixed(size >= 100 || index === 0 ? 0 : 1)} ${units[index]}`
    },
    formatRate(value:number) {
      if (!value) return i18n.global.t('unlimited')
      return `${Number((value * 8 / 1_000_000).toFixed(2))} Mbps`
    }
  },
  computed: {
    clientSub() {
      return Data().subURI + this.client.name
    },
    singbox() {
      const url = Data().subURI + this.client.name + "?format=json"
      return "sing-box://import-remote-profile?url=" +  encodeURIComponent(url) + "#" + this.client.name
    },
    clientLinks() {
      return this.client.links?? []
    },
    inboundCount() {
      return Array.isArray(this.client.inbounds) ? this.client.inbounds.length : 0
    },
    expiryLabel() {
      if (!this.client.expiry) return i18n.global.t('unlimited')
      return new Date(this.client.expiry * 1000).toLocaleDateString()
    },
    size() {
      if (window.innerWidth > 380) return 300
      if (window.innerWidth > 330) return 280
      return 250
    }
  },
  watch: {
    visible(v) {
      if (v) {
        this.tab = "sub"
        this.load()
      }
    },
  },
  components: { QrcodeVue }
}
</script>

<style scoped>
.share-card { overflow: hidden; }
.share-summary { display: grid; gap: 14px; padding: 18px 20px; background: var(--np-surface-muted); }
.share-summary__identity { display: flex; align-items: center; gap: 12px; }
.share-summary__avatar { display: grid; width: 46px; height: 46px; place-items: center; border-radius: 15px; color: var(--np-accent); background: rgba(10,132,255,.12); font-size: 1.1rem; font-weight: 800; }
.share-summary__identity > div:last-child { display: grid; gap: 3px; }
.share-summary__identity span, .share-summary__metrics span { color: var(--np-text-muted); font-size: .76rem; }
.share-summary__metrics { display: grid; grid-template-columns: repeat(4,minmax(0,1fr)); gap: 8px; }
.share-summary__metrics > div { display: grid; gap: 4px; min-width: 0; padding: 10px; border: 1px solid var(--np-border); border-radius: 12px; background: var(--np-surface); }
.share-summary__metrics strong { overflow: hidden; font-size: .8rem; text-overflow: ellipsis; white-space: nowrap; }
.share-summary__actions, .share-copy-grid { display: flex; flex-wrap: wrap; gap: 8px; }
.share-copy-grid { padding: 4px 16px 10px; }
.share-copy-grid .v-btn { flex: 1 1 150px; }
@media (max-width:600px) {
  .share-summary { padding: 14px; }
  .share-summary__metrics { grid-template-columns: repeat(2,minmax(0,1fr)); }
  .share-summary__actions .v-btn { flex: 1 1 calc(50% - 4px); }
}
</style>
