<template>
  <v-card class="settings-hero" rounded="xl" variant="flat">
    <div class="settings-hero__topline">
      <span class="settings-hero__badge">{{ $t('pages.settings') }}</span>
      <span class="settings-hero__badge settings-hero__badge--soft">{{ stateChange ? '未保存' : '已同步' }}</span>
    </div>
    <v-row class="settings-hero__content" align="center">
      <v-col cols="12" lg="8">
        <div class="settings-hero__title-row">
          <div class="settings-hero__icon">
            <v-icon icon="mdi-cog-outline" size="32" />
          </div>
          <div>
            <h1 class="settings-hero__title">{{ $t('pages.settings') }}</h1>
            <p class="settings-hero__subtitle">
              管理面板界面、订阅输出和路径设置，所有配置集中在一页里，便于检查和回退。
            </p>
          </div>
        </div>
        <div class="settings-hero__meta">
          <span>当前标签 {{ tab }}</span>
          <span>•</span>
          <span>{{ stateChange ? '未保存更改' : '配置已同步' }}</span>
        </div>
      </v-col>
      <v-col cols="12" lg="4" class="settings-hero__actions">
        <v-btn color="primary" size="large" @click="save" :loading="loading" :disabled="!stateChange">
          <v-icon icon="mdi-content-save-outline" start />
          {{ $t('actions.save') }}
        </v-btn>
        <v-btn variant="outlined" color="warning" @click="restartApp" :loading="loading" :disabled="stateChange">
          <v-icon icon="mdi-restart" start />
          {{ $t('actions.restartApp') }}
        </v-btn>
      </v-col>
    </v-row>
  </v-card>

  <v-card class="settings-panel" rounded="xl" variant="flat" :loading="loading">
    <v-tabs
      v-model="tab"
      color="primary"
      align-tabs="center"
      show-arrows
    >
      <v-tab value="t1">{{ $t('setting.interface') }}</v-tab>
      <v-tab value="t2">{{ $t('setting.sub') }}</v-tab>
      <v-tab value="t3">{{ $t('setting.jsonSub') }}</v-tab>
      <v-tab value="t4">{{ $t('setting.clashSub') }}</v-tab>
    </v-tabs>
    <v-card-text>
      <v-window v-model="tab">
        <v-window-item value="t1">
          <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.webListen" :label="$t('setting.addr')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model.number="webPort" min="1" type="number" :label="$t('setting.port')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.webPath" :label="$t('setting.webPath')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.webDomain" :label="$t('setting.domain')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.webKeyFile" :label="$t('setting.sslKey')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.webCertFile" :label="$t('setting.sslCert')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.webURI" :label="$t('setting.webUri')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field
              type="number"
              v-model.number="sessionMaxAge"
              min="0"
              :label="$t('setting.sessionAge')"
              :suffix="$t('date.m')"
              hide-details
              ></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field
              type="number"
              v-model.number="trafficAge"
              min="0"
              :label="$t('setting.trafficAge')"
              :suffix="$t('date.d')"
              hide-details
              ></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.timeLocation" :label="$t('setting.timeLoc')" hide-details></v-text-field>
          </v-col>
        </v-row>
        </v-window-item>

        <v-window-item value="t2">
          <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-switch color="primary" v-model="subEncode" :label="$t('setting.subEncode')" hide-details />
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-switch color="primary" v-model="subShowInfo" :label="$t('setting.subInfo')" hide-details />
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.subListen" :label="$t('setting.addr')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field
              type="number"
              v-model.number="subPort"
              min="1"
              :label="$t('setting.port')"
              hide-details></v-text-field>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.subKeyFile" :label="$t('setting.sslKey')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.subCertFile" :label="$t('setting.sslCert')" hide-details></v-text-field>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.subDomain" :label="$t('setting.domain')" hide-details></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.subPath" :label="$t('setting.path')" hide-details></v-text-field>
          </v-col>
        </v-row>
      <v-row>
        <v-col cols="12" sm="6" md="4">
          <v-text-field
              type="number"
              v-model.number="subUpdates"
              min="0"
              :label="$t('setting.update')"
              hide-details
              ></v-text-field>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="settings.subURI" :label="$t('setting.subUri')" hide-details></v-text-field>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-select
              v-model="settings.subMode"
              :items="subModes"
              :label="$t('setting.subMode')"
              hide-details
            ></v-select>
          </v-col>
        </v-row>
        <v-row v-if="settings.subMode == 'master'">
          <v-col cols="12" md="8">
            <v-textarea
              v-model="settings.subMasterSources"
              :label="$t('setting.subMasterSources')"
              :hint="$t('setting.subMasterSourcesHint')"
              persistent-hint
              rows="6"
              auto-grow
              hide-details="auto"
            ></v-textarea>
          </v-col>
        </v-row>
        <v-row v-if="settings.subMode == 'master'">
          <v-col cols="12" md="8">
            <v-text-field
              :model-value="aggregateSubURI"
              :label="$t('setting.subAggregateUri')"
              readonly
              hide-details
            ></v-text-field>
          </v-col>
        </v-row>
        </v-window-item>

        <v-window-item value="t3">
          <SubJsonExtVue :settings="settings" />
        </v-window-item>

        <v-window-item value="t4">
          <SubClashExtVue :settings="settings" />
        </v-window-item>
      </v-window>
    </v-card-text>
  </v-card>
</template>

<script lang="ts" setup>
import { i18n } from '@/locales'
import { Ref, computed, defineAsyncComponent, inject, onMounted, ref } from 'vue'
import HttpUtils from '@/plugins/httputil'
import { FindDiff } from '@/plugins/utils'
import { push } from 'notivue'

const SubJsonExtVue = defineAsyncComponent(() => import('@/components/SubJsonExt.vue'))
const SubClashExtVue = defineAsyncComponent(() => import('@/components/SubClashExt.vue'))
const tab = ref("t1")
const loading:Ref = inject('loading')?? ref(false)
const oldSettings = ref({})

const settings = ref({
	webListen: "",
	webDomain: "",
	webPort: "2095",
	webCertFile: "",
	webKeyFile: "",
  webPath: "/app/",
  webURI: "",
	sessionMaxAge: "0",
  trafficAge: "30",
	timeLocation: "Asia/Shanghai",
  subListen: "",
	subPort: "2096",
	subPath: "/sub/",
	subDomain: "",
	subCertFile: "",
	subKeyFile: "",
	subUpdates: "12",
	subEncode: "true",
	subShowInfo: "true",
  subURI: "",
  subMode: "slave",
  subMasterSources: "",
  subJsonExt: "",
  subClashExt: "",
})

const aggregateSubURI = computed(() => {
  const base = buildSubBaseURI()
  if (!base) return ''
  return base.endsWith('/') ? base + 'aggregate' : base + '/aggregate'
})

const subModes = [
  { title: i18n.global.t('setting.subModeMaster'), value: 'master' },
  { title: i18n.global.t('setting.subModeSlave'), value: 'slave' },
]

onMounted(async () => {
  loading.value = true
  await loadData()
  loading.value = false
})

const loadData = async () => {
  loading.value = true
  const msg = await HttpUtils.get('api/settings')
  loading.value = false
  if (msg.success) {
    setData(msg.obj)
    autoFillSubDomain()
  }
}

const setData = (data: any) => {
  settings.value = data
  oldSettings.value = { ...data }
}

const save = async () => {
  loading.value = true
  autoFillSubDomain()
  const msg = await HttpUtils.post('api/save', { object: 'settings', action: 'set', data: JSON.stringify(settings.value) })
  if (msg.success) {
    push.success({
      title: i18n.global.t('success'),
      duration: 5000,
      message: i18n.global.t('actions.set') + " " + i18n.global.t('pages.settings')
    })
    setData(msg.obj.settings)
  }
  loading.value = false
}

const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))

const restartApp = async () => {
  loading.value = true
  const msg = await HttpUtils.post('api/restartApp',{})
  if (msg.success) {
    let url = settings.value.webURI
    if (url !== "") {
      const isTLS = settings.value.webCertFile !== "" || settings.value.webKeyFile !== ""
      url = buildURL(settings.value.webDomain,settings.value.webPort.toString(),isTLS, settings.value.webPath)
    }
    await sleep(3000)
    window.location.replace(url)
  }
  loading.value = false
}

const buildURL = (host: string, port: string, isTLS: boolean, path: string) => {
  if (!host || host.length == 0) host = window.location.hostname
  if (!port || port.length == 0) port = window.location.port

  const protocol = isTLS ? "https:" : "http:"

  if (port === "" || (isTLS && port === "443") || (!isTLS && port === "80")) {
      port = ""
  } else {
      port = `:${port}`
  }

  return `${protocol}//${host}${port}${path}settings`
}

const buildSubBaseURI = () => {
  if (settings.value.subURI.length > 0) {
    return settings.value.subURI
  }

  let host = settings.value.subDomain
  if (!host || host.length == 0) host = window.location.hostname

  let port = settings.value.subPort
  if (!port || port.length == 0) port = "2096"

  const isTLS = settings.value.subCertFile !== "" && settings.value.subKeyFile !== ""
  const protocol = isTLS ? "https:" : "http:"

  if (port === "" || (isTLS && port === "443") || (!isTLS && port === "80")) {
    port = ""
  } else {
    port = `:${port}`
  }

  let path = settings.value.subPath
  if (!path || path.length == 0) path = "/sub/"
  if (!path.startsWith("/")) path = "/" + path
  if (!path.endsWith("/")) path += "/"

  return `${protocol}//${host}${port}${path}`
}

const isIpLiteral = (host: string) => {
  if (!host) return false
  return /^\d{1,3}(?:\.\d{1,3}){3}$/.test(host) || host.startsWith('[')
}

const autoFillSubDomain = () => {
  if (settings.value.subMode !== 'master') return
  if (settings.value.subDomain) return
  if (isIpLiteral(window.location.hostname)) return
  settings.value.subDomain = window.location.hostname
}

const subEncode = computed({
  get: () => { return settings.value.subEncode == "true" },
  set: (v:boolean) => { settings.value.subEncode = v ? "true" : "false" }
})

const subShowInfo = computed({
  get: () => { return settings.value.subShowInfo == "true" },
  set: (v:boolean) => { settings.value.subShowInfo = v ? "true" : "false" }
})

const webPort = computed({
  get: () => { return settings.value.webPort.length>0 ? parseInt(settings.value.webPort) : 2095 },
  set: (v:number) => { settings.value.webPort = v>0 ? v.toString() : "2095" }
})

const sessionMaxAge = computed({
  get: () => { return settings.value.sessionMaxAge.length>0 ? parseInt(settings.value.sessionMaxAge) : 0 },
  set: (v:number) => { settings.value.sessionMaxAge = v>0 ? v.toString() : "0" }
})

const trafficAge = computed({
  get: () => { return settings.value.trafficAge.length>0 ? parseInt(settings.value.trafficAge) : 0 },
  set: (v:number) => { settings.value.trafficAge = v>0 ? v.toString() : "0" }
})

const subPort = computed({
  get: () => { return settings.value.subPort.length>0 ? parseInt(settings.value.subPort) : 2096 },
  set: (v:number) => { settings.value.subPort = v>0 ? v.toString() : "2096" }
})

const subUpdates = computed({
  get: () => { return settings.value.subUpdates.length>0 ? parseInt(settings.value.subUpdates) : 12 },
  set: (v:number) => { settings.value.subUpdates = v>0 ? v.toString() : "12" }
})

const stateChange = computed(() => {
  return !FindDiff.deepCompare(settings.value,oldSettings.value)
})
</script>

<style scoped lang="scss">
.settings-hero,
.settings-panel {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.22), transparent 28%),
    var(--np-surface);
  border: 1px solid var(--np-border);
  box-shadow: var(--np-shadow);
  backdrop-filter: blur(28px) saturate(1.12);
}

.settings-hero {
  padding: 20px;
  margin-bottom: 18px;
  overflow: hidden;
}

.settings-hero__topline {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 16px;
}

.settings-hero__badge {
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 12px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--np-accent);
  background: rgba(10, 132, 255, 0.08);
}

.settings-hero__badge--soft {
  text-transform: none;
  letter-spacing: 0;
  color: var(--np-text-muted);
  background: rgba(148, 163, 184, 0.12);
}

.settings-hero__content {
  min-height: 120px;
}

.settings-hero__title-row {
  display: flex;
  gap: 14px;
  align-items: flex-start;
}

.settings-hero__icon {
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  border-radius: 16px;
  color: var(--np-accent);
  background: linear-gradient(145deg, rgba(59, 130, 246, 0.16), rgba(14, 165, 233, 0.08));
}

.settings-hero__title {
  margin: 0;
  font-size: clamp(28px, 3vw, 40px);
  line-height: 1.1;
  font-weight: 800;
  letter-spacing: -0.03em;
}

.settings-hero__subtitle {
  margin: 12px 0 0;
  color: var(--np-text-muted);
  line-height: 1.7;
}

.settings-hero__meta {
  margin-top: 14px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  color: var(--np-text-muted);
  font-size: 13px;
}

.settings-hero__actions {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.settings-panel {
  overflow: hidden;
}

.v-theme--dark .settings-hero,
.v-theme--dark .settings-panel {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.08), transparent 24%),
    rgba(11, 18, 31, 0.78);
  border-color: rgba(148, 163, 184, 0.16);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.3);
}

@media (max-width: 960px) {
  .settings-hero {
    padding: 16px;
  }

  .settings-hero__actions {
    justify-content: flex-start;
  }
}

@media (max-width: 600px) {
  .settings-hero__icon {
    width: 46px;
    height: 46px;
  }

  .settings-hero__title {
    font-size: 24px;
  }
}
</style>
