<template>
  <v-dialog transition="dialog-bottom-transition" width="min(94vw, 680px)">
    <v-card class="share-card" rounded="xl" id="qrcode-modal" :loading="loading">
      <v-card-title class="share-heading">
        <span>{{ $t('ui.share.title') }}</span>
        <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="$emit('close')" />
      </v-card-title>
      <v-divider />
      <div class="share-body">
        <v-skeleton-loader v-if="loading" type="article, image" />
        <v-alert v-else-if="loadError" type="error" variant="tonal" class="ma-4">
          {{ $t('ui.share.loadFailed') }}
          <v-btn variant="text" @click="load">{{ $t('ui.common.refresh') }}</v-btn>
        </v-alert>
        <template v-else-if="client">
          <div class="share-summary">
            <div class="share-summary__identity">
              <div class="share-summary__avatar">{{ client.name?.slice(0, 1)?.toUpperCase() || '?' }}</div>
              <div><strong>{{ client.name }}</strong><span>{{ client.enable ? $t('ui.share.active') : $t('ui.share.paused') }}</span></div>
            </div>
            <div class="share-summary__metrics">
              <div><span>{{ $t('stats.volume') }}</span><strong>{{ client.volume ? formatBytes(client.volume) : $t('unlimited') }}</strong></div>
              <div><span>{{ $t('date.expiry') }}</span><strong>{{ expiryLabel }}</strong></div>
              <div><span>{{ $t('pages.inbounds') }}</span><strong>{{ client.inbounds?.length ?? 0 }}</strong></div>
              <div><span>{{ $t('client.rateLimit') }}</span><strong>↑ {{ formatRate(client.uploadLimit) }} · ↓ {{ formatRate(client.downloadLimit) }}</strong></div>
            </div>
            <div class="share-summary__actions">
              <v-btn :color="client.enable ? 'warning' : 'success'" variant="tonal" :disabled="actionLoading" @click="toggleEnabled">
                <v-icon :icon="client.enable ? 'mdi-pause-circle-outline' : 'mdi-play-circle-outline'" start />
                {{ client.enable ? $t('ui.share.pause') : $t('ui.share.resume') }}
              </v-btn>
              <v-btn color="error" variant="tonal" :disabled="actionLoading" @click="revokeCredentials">
                <v-icon icon="mdi-key-change" start />{{ $t('ui.share.revoke') }}
              </v-btn>
            </div>
          </div>
          <v-tabs v-model="tab" density="compact" fixed-tabs>
            <v-tab value="sub">{{ $t('setting.sub') }}</v-tab>
            <v-tab value="link">{{ $t('client.links') }}</v-tab>
          </v-tabs>
          <div class="share-content">
            <template v-if="tab === 'sub'">
              <p class="share-hint">{{ $t('ui.share.formatHint') }}</p>
              <v-btn-toggle v-model="format" mandatory divided class="share-formats" color="primary" :aria-label="$t('ui.share.format')">
                <v-btn value="clash">Clash / Mihomo</v-btn>
                <v-btn value="auto">{{ $t('ui.share.autoFormat') }}</v-btn>
                <v-btn value="json">JSON</v-btn>
                <v-btn value="singbox">sing-box</v-btn>
              </v-btn-toggle>
            </template>
            <v-select v-else-if="clientLinks.length" v-model="linkIndex" :items="linkOptions" :label="$t('ui.share.chooseNode')" hide-details />
            <v-alert v-if="!selectedValue" type="info" variant="tonal">
              {{ tab === 'sub' ? $t('ui.share.noSubscription') : $t('ui.share.noLinks') }}
            </v-alert>
            <template v-else>
              <p class="share-hint" data-testid="share-format-hint">{{ selectedHint }}</p>
              <div v-if="canRenderQr" class="share-qr" role="img" :aria-label="$t('client.qrCode')" data-testid="share-qr">
                <QrcodeVue :value="selectedValue" :size="240" level="M" :margin="4" render-as="svg" />
              </div>
              <v-alert v-else type="info" variant="tonal">{{ $t('ui.share.qrTooLong') }}</v-alert>
              <v-textarea :model-value="selectedValue" :label="$t('client.links')" readonly auto-grow rows="2" max-rows="4" hide-details data-testid="share-url" />
            </template>
          </div>
        </template>
      </div>
      <v-divider />
      <v-card-actions class="share-footer">
        <v-btn variant="text" @click="$emit('close')">{{ $t('actions.close') }}</v-btn>
        <v-spacer />
        <v-btn color="primary" variant="tonal" :disabled="loading || !selectedValue" @click="copyToClipboard(selectedValue)">
          <v-icon icon="mdi-content-copy" start />{{ $t('ui.share.copySelected') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { defineComponent } from 'vue'
import QrcodeVue from 'qrcode.vue'
import Data from '@/store/modules/data'
import Clipboard from 'clipboard'
import { i18n } from '@/locales'
import { push } from 'notivue'
import { shuffleConfigs, type Client } from '@/types/clients'

export default defineComponent({
  props: { id: { type: Number, required: true }, visible: Boolean },
  emits: ['close'],
  components: { QrcodeVue },
  data() {
    return {
      tab: 'sub', format: 'clash', linkIndex: 0,
      client: null as Client | null,
      loading: false, loadError: false, actionLoading: false, loadSequence: 0,
    }
  },
  computed: {
    clientSub(): string {
      return Data().subURI && this.client?.name ? Data().subURI + encodeURIComponent(this.client.name) : ''
    },
    clientLinks() { return this.client?.links ?? [] },
    linkOptions() {
      return this.clientLinks.map((link, index) => ({ title: link.remark || i18n.global.t('client.' + link.type), value: index }))
    },
    selectedValue(): string {
      if (this.tab === 'link') return this.clientLinks[this.linkIndex]?.uri ?? ''
      if (!this.clientSub) return ''
      if (this.format === 'singbox') {
        return 'sing-box://import-remote-profile?url=' + encodeURIComponent(this.clientSub + '?format=json') + '#' + encodeURIComponent(this.client?.name ?? '')
      }
      if (this.format === 'auto') return this.clientSub
      return this.clientSub + '?format=' + this.format
    },
    selectedHint(): string {
      return i18n.global.t('ui.share.' + (this.tab === 'link' ? 'nodeHint' : this.format === 'singbox' ? 'singboxHint' : 'subscriptionHint'))
    },
    canRenderQr(): boolean {
      // Leave room below the QR byte-mode capacity at error-correction level M.
      return new TextEncoder().encode(this.selectedValue).length <= 2000
    },
    expiryLabel(): string {
      return this.client?.expiry ? new Date(this.client.expiry * 1000).toLocaleDateString() : i18n.global.t('unlimited')
    },
  },
  watch: {
    visible: {
      immediate: true,
      handler(visible: boolean) {
        if (visible) {
          this.tab = 'sub'
          this.format = 'clash'
          this.linkIndex = 0
          this.load()
        } else {
          this.loadSequence++
          this.client = null
          this.loading = false
        }
      },
    },
    id() { if (this.visible) { this.linkIndex = 0; this.load() } },
  },
  methods: {
    async load() {
      const sequence = ++this.loadSequence
      const id = this.id
      this.client = null
      this.loadError = false
      this.loading = true
      try {
        const client = await Data().loadClients(id)
        if (sequence !== this.loadSequence || !this.visible || id !== this.id) return
        if (client.id !== id) { this.loadError = true; return }
        this.client = client
      } catch {
        if (sequence === this.loadSequence) this.loadError = true
      } finally {
        if (sequence === this.loadSequence) this.loading = false
      }
    },
    copyToClipboard(text: string) {
      if (!text) return
      const button = document.createElement('button')
      const container = document.getElementById('qrcode-modal') ?? document.body
      container.appendChild(button)
      const clipboard = new Clipboard(button, { text: () => text, container })
      clipboard.on('success', () => push.success({ message: i18n.global.t('copyToClipboard') }))
      clipboard.on('error', () => push.error({ message: i18n.global.t('failed') + ': ' + i18n.global.t('copyToClipboard') }))
      button.click()
      clipboard.destroy()
      button.remove()
    },
    async saveClient(client: Client) {
      if (this.actionLoading) return
      this.actionLoading = true
      try {
        if (await Data().save('clients', 'edit', client) && this.visible && this.id === client.id) await this.load()
      } finally { this.actionLoading = false }
    },
    async toggleEnabled() {
      if (this.client) await this.saveClient({ ...this.client, enable: !this.client.enable })
    },
    async revokeCredentials() {
      if (!this.client || !window.confirm(i18n.global.t('ui.share.revokeConfirm'))) return
      const config = JSON.parse(JSON.stringify(this.client.config || {}))
      shuffleConfigs(config)
      await this.saveClient({ ...this.client, config })
    },
    formatBytes(value: number) {
      if (!value) return '0 B'
      const units = ['B', 'KB', 'MB', 'GB', 'TB']
      let size = value
      let index = 0
      while (size >= 1024 && index < units.length - 1) { size /= 1024; index++ }
      return `${size.toFixed(size >= 100 || index === 0 ? 0 : 1)} ${units[index]}`
    },
    formatRate(value: number) {
      return value ? `${Number((value * 8 / 1_000_000).toFixed(2))} Mbps` : i18n.global.t('unlimited')
    },
  },
})
</script>

<style scoped>
.share-card { display: flex; flex-direction: column; overflow: hidden; max-height: 90dvh; }
.share-heading { display: flex; align-items: center; justify-content: space-between; flex-shrink: 0; }
.share-body { overflow-y: auto; min-height: 0; }
.share-summary { display: grid; gap: 14px; padding: 18px 20px; background: var(--np-surface-muted); }
.share-summary__identity { display: flex; align-items: center; gap: 12px; min-width: 0; }
.share-summary__avatar { display: grid; width: 46px; height: 46px; flex-shrink: 0; place-items: center; border-radius: 15px; color: var(--np-accent); background: rgba(10,132,255,.12); font-size: 1.1rem; font-weight: 800; }
.share-summary__identity > div:last-child { display: grid; gap: 3px; min-width: 0; overflow-wrap: anywhere; }
.share-summary__identity span, .share-summary__metrics span, .share-hint { color: var(--np-text-muted); font-size: .76rem; }
.share-summary__metrics { display: grid; grid-template-columns: repeat(4,minmax(0,1fr)); gap: 8px; }
.share-summary__metrics > div { display: grid; gap: 4px; min-width: 0; padding: 10px; border: 1px solid var(--np-border); border-radius: 12px; background: var(--np-surface); }
.share-summary__metrics strong { overflow: hidden; font-size: .8rem; text-overflow: ellipsis; white-space: nowrap; }
.share-summary__actions { display: flex; flex-wrap: wrap; gap: 8px; }
.share-content { display: grid; gap: 14px; padding: 16px 20px 20px; }
.share-formats { display: grid; grid-template-columns: repeat(4,minmax(0,1fr)); height: auto; }
.share-formats .v-btn { min-width: 0; padding-inline: 6px; font-size: .75rem; }
.share-qr { display: grid; place-items: center; }
.share-qr :deep(svg) { max-width: 100%; height: auto; border-radius: 12px; }
.share-footer { flex-shrink: 0; padding: 12px 16px; }
@media (max-width:600px) {
  .share-summary, .share-content { padding: 14px; }
  .share-summary__metrics { grid-template-columns: repeat(2,minmax(0,1fr)); }
  .share-summary__actions .v-btn { flex: 1 1 calc(50% - 4px); }
  .share-formats { grid-template-columns: repeat(2,minmax(0,1fr)); }
}
</style>
