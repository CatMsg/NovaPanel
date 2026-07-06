<template>
  <v-dialog transition="dialog-bottom-transition" width="800">
    <v-card class="rounded-lg modal-shell">
      <v-card-title class="modal-shell__title">
        <div class="modal-shell__topline">
          <v-chip size="small" variant="tonal" color="primary" class="modal-shell__badge">
            {{ $t('objects.endpoint') }}
          </v-chip>
          <v-chip size="small" variant="tonal" color="secondary" class="modal-shell__badge modal-shell__badge--soft">
            {{ $t('actions.' + title) }}
          </v-chip>
        </div>
        {{ $t('actions.' + title) + " " + $t('objects.endpoint') }}
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text style="padding: 0 16px; overflow-y: scroll;">
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-select
            hide-details
            :disabled="endpoint.id > 0"
            :label="$t('type')"
            :items="Object.keys(epTypes).map((key,index) => ({title: key, value: Object.values(epTypes)[index]}))"
            v-model="endpoint.type"
            @update:modelValue="changeType">
            </v-select>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="endpoint.tag" :label="$t('objects.tag')" hide-details></v-text-field>
          </v-col>
        </v-row>
        <Wireguard v-if="endpoint.type == epTypes.Wireguard"
          :data="endpoint"
          @getWgPubKey="getWgPubKey"
          @newWgKey="newWgKey"
          @addPeer="addWgPeer"
          @delPeer="delWgPeer"
          @refreshPeerKey="refreshWgPeerKey" />
        <Warp v-if="endpoint.type == epTypes.Warp" :data="endpoint" />
        <TailscaleVue v-if="endpoint.type == epTypes.Tailscale" :data="endpoint" />
        <Masque v-if="endpoint.type == epTypes.Masque" :data="endpoint" />
        <Dial v-if="endpoint.type != epTypes.Masque" :dial="endpoint" />
      </v-card-text>
      <v-card-actions class="modal-shell__actions">
        <v-spacer></v-spacer>
        <v-btn
          color="primary"
          variant="outlined"
          @click="closeModal"
        >
          {{ $t('actions.close') }}
        </v-btn>
        <v-btn
          color="primary"
          variant="tonal"
          :loading="loading"
          @click="saveChanges"
        >
          {{ $t('actions.save') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { EpTypes, createEndpoint } from '@/types/endpoints'
import RandomUtil from '@/plugins/randomUtil'
import Dial from '@/components/Dial.vue'
import Wireguard from '@/components/protocols/Wireguard.vue'
import Warp from '@/components/protocols/Warp.vue'
import TailscaleVue from '@/components/protocols/Tailscale.vue'
import Masque from '@/components/protocols/Masque.vue'
import HttpUtils from '@/plugins/httputil'
import { push } from 'notivue'
import { i18n } from '@/locales'
import Data from '@/store/modules/data'
export default {
  props: ['visible', 'data', 'id', 'tags'],
  emits: ['close'],
  data() {
    return {
      endpoint: createEndpoint("wireguard",{ "tag": "" }),
      title: "add",
      tab: "t1",
      loading: false,
      epTypes: EpTypes,
    }
  },
  methods: {
    async updateData(id: number) {
      if (id > 0) {
        const newData = JSON.parse(this.$props.data)
        this.endpoint = createEndpoint(newData.type, newData)
        this.title = "edit"
      }
      else {
      this.endpoint.type = "wireguard"
      this.endpoint.listen_port = RandomUtil.randomIntRange(10000, 60000)
      this.changeType()
      this.title = "add"
      }
      this.tab = "t1"
    },
    async changeType() {
      // Tag change only in add endpoint
      const tag = this.endpoint.type + "-" + RandomUtil.randomSeq(3)
      
      // Use previous data
      let prevConfig = {}
      switch (this.endpoint.type) {
        case EpTypes.Wireguard:
          const wgKeys = (await this.genWgKey())
          const randomIPoctet = RandomUtil.randomIntRange(1, 255)
          prevConfig = {
            tag: tag,
            listen_port: this.endpoint.listen_port ?? RandomUtil.randomIntRange(10000, 60000),
            address: ['10.0.0.'+ randomIPoctet.toString() +'/32','fe80::'+ randomIPoctet.toString(16) +'/128'],
            private_key: wgKeys.private_key,
            peers: [],
            ext: {
              public_key: wgKeys.public_key,
              keys: []
            }
          }
          break
        case EpTypes.Warp:
          prevConfig = {
            tag: tag,
          }
          break
        case EpTypes.Tailscale:
          prevConfig = { tag: tag }
          break
        case EpTypes.Masque: {
          const masqueKeys = await this.genMasqueKey()
          const server = await this.getMasqueServer()
          prevConfig = {
            tag: tag,
            server,
            port: RandomUtil.randomIntRange(10000, 60000),
            network: 'quic',
            private_key: masqueKeys.private_key,
            public_key: masqueKeys.public_key,
            ip: '172.16.0.' + RandomUtil.randomIntRange(2, 254).toString() + '/32',
            mtu: 1280,
            udp: true,
          }
          break
        }
      }
      this.endpoint = createEndpoint(this.endpoint.type, prevConfig)
    },
    closeModal() {
      this.updateData(0) // reset
      this.$emit('close')
    },
    async saveChanges() {
      if (!this.$props.visible) return
      
      // check duplicate tag
      const isDuplicatedTag = Data().checkTag("endpoint",this.endpoint.id, this.endpoint.tag)
      if (isDuplicatedTag) return

      if (this.endpoint.type == EpTypes.Masque) {
        const preferredHost = await this.getMasqueServer()
        const currentHost = String(this.endpoint.server ?? '').trim()
        if (!currentHost || this.isIpLiteral(currentHost)) {
          if (!preferredHost) {
            push.error({
              message: 'MASQUE 需要先在 设置-界面 里配置 subDomain 或 webDomain',
              duration: 5000,
            })
            this.loading = false
            return
          }
          this.endpoint.server = preferredHost
        }
        if (!String(this.endpoint.ip ?? '').trim()) {
          push.error({
            message: 'MASQUE 需要填写客户端 IPv4，例如 172.16.0.2/32',
            duration: 5000,
          })
          this.loading = false
          return
        }
        delete (this.endpoint as any).ipv6
      }

      // save data
      this.loading = true
      const success = await Data().save("endpoints", this.$props.id == 0 ? "new" : "edit", this.endpoint)
      if (success) this.closeModal()
      this.loading = false
    },
    async genWgKey(){
      this.loading = true
      const msg = await HttpUtils.get('api/keypairs', { k: "wireguard" })
      this.loading = false
      let result = { private_key: "", public_key: "" }
      if (msg.success) {
        msg.obj.forEach((line:string) => {
          if (line.startsWith("PrivateKey")){
            result.private_key = line.substring(12)
          }
          if (line.startsWith("PublicKey")){
            result.public_key = line.substring(11)
          }
        })
      } else {
        push.error({
          message: i18n.global.t('error') + ": " + msg.obj
        })
      }
      return result
    },
    async genMasqueKey() {
      this.loading = true
      const msg = await HttpUtils.get('api/keypairs', { k: "masque" })
      this.loading = false
      let result = { private_key: "", public_key: "" }
      if (msg.success) {
        msg.obj.forEach((line:string) => {
          if (line.startsWith("PrivateKey")){
            result.private_key = line.substring(12)
          }
          if (line.startsWith("PublicKey")){
            result.public_key = line.substring(11)
          }
        })
      } else {
        push.error({
          message: i18n.global.t('error') + ": " + msg.obj
        })
      }
      return result
    },
    async getMasqueServer() {
      try {
        const msg = await HttpUtils.get('api/settings')
        if (!msg.success || !msg.obj) {
          return ''
        }
        const subDomain = String(msg.obj.subDomain ?? '').trim()
        if (subDomain && !this.isIpLiteral(subDomain)) {
          return subDomain
        }
        const webDomain = String(msg.obj.webDomain ?? '').trim()
        if (webDomain && !this.isIpLiteral(webDomain)) {
          return webDomain
        }
      } catch {
        // ignore and use fallback
      }
      return ''
    },
    isIpLiteral(host: string) {
      const normalized = String(host ?? '').trim().replace(/^\[|\]$/g, '')
      return /^(\d{1,3}\.){3}\d{1,3}$/.test(normalized) || /^[0-9a-fA-F:]+$/.test(normalized)
    },
    async newWgKey(){
      this.loading = true
      const newKeys = await this.genWgKey()
      this.endpoint.private_key = newKeys.private_key
      if (!this.endpoint.ext) this.endpoint.ext = {keys: []}
      this.endpoint.ext.public_key = newKeys.public_key
      this.loading = false
    },
    async getWgPubKey(private_key: string) {
      if (!this.endpoint.ext) this.endpoint.ext = {keys: []}
      this.loading = true
      const msg = await HttpUtils.get('api/keypairs', { k: "wireguard", o: private_key })
      if (msg.success) {
        this.endpoint.ext.public_key = msg.obj[0]
      }
      this.loading = false
    },
    async addWgPeer(){
      if (this.endpoint.type != EpTypes.Wireguard) return
      this.loading = true
      const newKeys = await this.genWgKey()
      if (!this.endpoint.ext) this.endpoint.ext = {keys: []}
      this.endpoint.ext.keys.push(newKeys)
      this.endpoint.peers.push({
        public_key: newKeys.public_key,
        allowed_ips: [this.findFreeIP()]
      })
      this.loading = false
    },
    findFreeIP(): string{
      const peerAllowedIPs = this.endpoint.peers.map((peer: any) => peer.allowed_ips).flat()
      const localIPv4 = this.endpoint.address?.find((address: string) => address.includes('.'))?.split('/')[0]
      const octets = localIPv4?.split('.').map((part: string) => Number(part)) ?? []
      const hasValidIPv4 = octets.length === 4 && octets.every((part: number) => Number.isInteger(part) && part >= 0 && part <= 255)
      const subnet = hasValidIPv4 ? octets.slice(0, 3).join('.') : '10.0.1'

      for (let i = 2; i < 255; i++) {
        const newIP = subnet + '.' + i.toString() + '/32'
        if (i !== octets[3] && !peerAllowedIPs.includes(newIP)) return newIP
      }
      return '0.0.0.0/0'
    },
    delWgPeer(index: number){
      if (this.endpoint.type != EpTypes.Wireguard) return
      this.endpoint.ext.keys = this.endpoint.ext.keys.filter((key: any) => key.public_key != this.endpoint.peers[index].public_key)
      this.endpoint.peers.splice(index, 1)
    },
    async refreshWgPeerKey(index: number) {
      this.loading = true
      const newKeys = await this.genWgKey()
      if (!this.endpoint.ext) this.endpoint.ext = {keys: []}
      const indexKeys = this.endpoint.ext.keys.findIndex((key: any) => key.public_key == this.endpoint.peers[index].public_key)
      this.endpoint.ext.keys[indexKeys == -1 ? this.endpoint.ext.keys.length : indexKeys] = newKeys
      this.endpoint.peers[index].public_key = newKeys.public_key
      this.loading = false
    },
  },
  watch: {
    visible(v) {
      if (v) {
        this.updateData(this.$props.id)
      }
    },
  },
  components: { Dial, Wireguard, Warp, TailscaleVue, Masque }
}
</script>

<style scoped lang="scss">
.modal-shell {
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.98), rgba(243, 248, 255, 0.96));
  backdrop-filter: blur(24px) saturate(150%);
  border: 1px solid rgba(102, 153, 255, 0.18);
  box-shadow: 0 18px 48px rgba(51, 87, 168, 0.16);
}

.modal-shell__topline {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}

.modal-shell__badge {
  letter-spacing: 0.04em;
  font-weight: 700;
}

.modal-shell__badge--soft {
  opacity: 0.88;
}

.modal-shell__title {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  padding-bottom: 14px;
}

.modal-shell__actions {
  padding: 16px 20px 20px;
}

:global(.v-theme--dark) .modal-shell {
  background: linear-gradient(180deg, rgba(18, 23, 37, 0.98), rgba(14, 17, 28, 0.96));
  border-color: rgba(120, 146, 255, 0.16);
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.36);
}
</style>
