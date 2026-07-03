import HttpUtils from '@/plugins/httputil'
import { defineStore } from 'pinia'
import { push } from 'notivue'
import { i18n } from '@/locales'
import { Inbound } from '@/types/inbounds'
import { Client } from '@/types/clients'
import { Config } from '@/types/config'
import { Outbound } from '@/types/outbounds'
import { Endpoint } from '@/types/endpoints'
import { Srv } from '@/types/services'
import { tls } from '@/types/tls'

export interface OnlineState {
  inbound: string[]
  outbound: string[]
  user: string[]
}

interface LoadDataPayload {
  onlines?: OnlineState
  lastLog?: string
  config?: Config
  clients?: Client[]
  tls?: tls[]
  inbounds?: Inbound[]
  outbounds?: Outbound[]
  endpoints?: Endpoint[]
  services?: Srv[]
  subURI?: string
  subMode?: string
  subAggregateURI?: string
  enableTraffic?: boolean
}

export const defaultReloadItems = [
  'g-cpu',
  'g-mem',
  'g-dsk',
  'g-swp',
  'h-cpu',
  'h-mem',
  'h-net',
  'hp-net',
  'h-dio',
  'i-sys',
  'i-sbd',
]
export const reloadItemsStorageKey = 'reloadItems:v2'
const legacyReloadItemsStorageKey = 'reloadItems'

const parseReloadItems = (value: string | null) => {
  if (!value) return []
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

const normalizeReloadItems = (items: string[]) => Array.from(new Set([
  ...defaultReloadItems,
  ...items,
]))

const storedReloadItems = parseReloadItems(
  localStorage.getItem(reloadItemsStorageKey) ?? localStorage.getItem(legacyReloadItemsStorageKey)
)
const initialReloadItems = normalizeReloadItems(storedReloadItems)

localStorage.setItem(reloadItemsStorageKey, initialReloadItems.join(','))

const Data = defineStore('Data', {
  state: () => ({ 
    lastLoad: 0,
    reloadItems: initialReloadItems,
    subURI: "",
    subMode: "slave",
    subAggregateURI: "",
    enableTraffic: false,
    onlines: {inbound: <string[]>[], outbound: <string[]>[], user: <string[]>[]} as OnlineState,
    config: {} as Config,
    inbounds: [] as Inbound[],
    outbounds: [] as Outbound[],
    services: [] as Srv[],
    endpoints: [] as Endpoint[],
    clients: [] as Client[],
    tlsConfigs: [] as tls[],
  }),
  actions: {
    async loadData() {
      const msg = await HttpUtils.get('api/load', this.lastLoad >0 ? {lu: this.lastLoad} : {} )
      if(msg.success) {
        this.onlines = msg.obj.onlines
        if (msg.obj.lastLog) {
          push.error({
            title: i18n.global.t('error.core'),
            duration: 5000,
            message: msg.obj.lastLog
          })
        }
        
        if (msg.obj.config) {
          this.setNewData(msg.obj)
        }
      }
    },
    setNewData(data: LoadDataPayload) {
      this.lastLoad = Math.floor((new Date()).getTime()/1000)
      if (data.subURI) this.subURI = data.subURI
      if (data.subMode) this.subMode = data.subMode
      if (Object.hasOwn(data, 'subAggregateURI')) this.subAggregateURI = data.subAggregateURI ?? ''
      else if (data.subMode == 'slave') this.subAggregateURI = ''
      if (Object.hasOwn(data, 'enableTraffic')) this.enableTraffic = data.enableTraffic ?? false
      if (data.config) this.config = data.config
      if (data.onlines) this.onlines = data.onlines
      if (Object.hasOwn(data, 'clients')) this.clients = data.clients ?? []
      if (Object.hasOwn(data, 'inbounds')) this.inbounds = data.inbounds ?? []
      if (Object.hasOwn(data, 'outbounds')) this.outbounds = data.outbounds ?? []
      if (Object.hasOwn(data, 'services')) this.services = data.services ?? []
      if (Object.hasOwn(data, 'endpoints')) this.endpoints = data.endpoints ?? []
      if (Object.hasOwn(data, 'tls')) this.tlsConfigs = data.tls ?? []
    },
    async loadInbounds(ids: number[]): Promise<Inbound[]> {
      const options = ids.length > 0 ? {id: ids.join(",")} : {}
      const msg = await HttpUtils.get('api/inbounds', options)
      if(msg.success) {
        return msg.obj.inbounds
      }
      return <Inbound[]>[]
    },
    async loadClients(id: number): Promise<Client> {
      const options = id > 0 ? {id: id} : {}
      const msg = await HttpUtils.get('api/clients', options)
      if(msg.success) {
        return <Client>msg.obj.clients[0]??{}
      }
      return <Client>{}
    },
    async save (object: string, action: string, data: any, initUsers?: number[]): Promise<boolean> {
      let postData = {
        object: object,
        action: action,
        data: JSON.stringify(data, null, 2),
        initUsers: initUsers?.join(',') ?? undefined
      }
      const msg = await HttpUtils.post('api/save', postData)
      if (msg.success) {
        const objectName = ['tls', 'config'].includes(object) ? object : object.substring(0, object.length - 1)
        push.success({
          title: i18n.global.t('success'),
          duration: 5000,
          message: i18n.global.t('actions.' + action) + " " + i18n.global.t('objects.' + objectName)
        })
        this.setNewData(msg.obj)
      }
      return msg.success
    },
    // Check duplicate client name
    checkClientName (id: number, newName: string): boolean {
      const oldName = id > 0 ? this.clients.findLast((i: any) => i.id == id)?.name : null
      if (newName != oldName && this.clients.findIndex((c: any) => c.name == newName) != -1) {
        push.error({
          message: i18n.global.t('error.dplData') + ": " + i18n.global.t('client.name')
        })
        return true
      }
      return false
    },
    // Check bulk client names
    checkBulkClientNames (names: string[]): boolean {
      const newNames = new Set(names)
      const oldNames = new Set(this.clients.map((c: any) => c.name))
      const allNames = new Set([...oldNames, ...newNames])
      if (newNames.size != names.length || oldNames.size + newNames.size != allNames.size) {
        push.error({
          message: i18n.global.t('error.dplData') + ": " + i18n.global.t('client.name')
        })
        return true
      }
      return false
    },
    // check duplicate tag
    checkTag (object: string, id: number, tag: string): boolean {
      let objects = <any[]>[]
      switch (object) {
        case 'inbound':
          objects = this.inbounds
          break
        case 'outbound':
          objects = this.outbounds
          break
        case 'service':
          objects = this.services
          break
        case 'endpoint':
          objects = this.endpoints
          break
        default:
          return false
      }
      const oldObject = id > 0 ? objects.findLast((i: any) => i.id == id) : null
      if (tag != oldObject?.tag && objects.findIndex((i: any) => i.tag == tag) != -1) {
        push.error({
          message: i18n.global.t('error.dplData') + ": " + i18n.global.t('objects.tag')
        })
        return true
      }
      return false
    },
  }
})

export default Data
