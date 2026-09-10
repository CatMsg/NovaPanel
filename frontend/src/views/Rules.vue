<template>
  <RuleVue
    v-model="ruleModal.visible"
    :visible="ruleModal.visible"
    :index="ruleModal.index"
    :data="ruleModal.data"
    :clients="clients"
    :inTags="inboundTags"
    :outTags="outboundTags"
    :rsTags="rulesetTags"
    @close="closeRuleModal"
    @save="saveRuleModal"
  />
  <RulesetVue
    v-model="rulesetModal.visible"
    :visible="rulesetModal.visible"
    :index="rulesetModal.index"
    :data="rulesetModal.data"
    :outTags="outboundTags"
    @close="closeRulesetModal"
    @save="saveRulesetModal"
  />
  <RuleImport
    v-model="importRulesModal.visible"
    :visible="importRulesModal.visible"
    :existingRulesCount="rules.length"
    :existingRulesetsCount="rulesets.length"
    :existingRulesetTags="rulesetTags"
    @save="saveImportRule"
    @close="closeImportRule"
  />
  <RulesetImport
    v-model="importRulesetsModal.visible"
    :visible="importRulesetsModal.visible"
    :outTags="outboundTags"
    :rsTags="rulesetTags"
    @save="saveImportRulesets"
    @close="closeImportRulesets"
  />
  <RuleCatalog
    :visible="catalogModal"
    :outbound-tags="outboundTags"
    :inbound-tags="inboundTags"
    :clients="clients"
    :dns-server-tags="dnsServerTags"
    :loading="catalogLoading"
    @close="catalogModal = false"
    @apply="applyCatalog"
  />
  <DnsRuleVue
    v-model="dnsRuleModal.visible"
    :visible="dnsRuleModal.visible"
    :index="dnsRuleModal.index"
    :data="dnsRuleModal.data"
    :clients="clients"
    :inTags="inboundTags"
    :serverTags="dnsServerTags"
    :ruleSets="rulesetTags"
    @close="closeDnsRuleModal"
    @save="saveDnsRuleModal"
  />
  <PageHero
    :eyebrow="$t('pages.rules')"
    :title="$t('pages.rules')"
    description="集中管理规则集、DNS 分流与流量出口，常用场景通过规则目录快速配置。"
    icon="mdi-routes"
  >
    <template #meta>
      <span>规则集 {{ rulesets.length }}</span><span>•</span><span>DNS 分流 {{ dnsRules.length }}</span><span>•</span><span>流量规则 {{ rules.length }}</span>
    </template>
    <template #actions>
      <v-btn color="primary" variant="tonal" @click="catalogModal = true">
        <v-icon icon="mdi-bookshelf" start />规则目录
      </v-btn>
      <v-menu v-model="actionMenu" :close-on-content-click="false" location="bottom center">
        <template v-slot:activator="{ props }">
          <v-btn v-bind="props" hide-details variant="outlined">
            <v-icon icon="mdi-tools" start />导入
          </v-btn>
        </template>
        <v-list density="compact" nav>
          <v-list-item link @click="showImportRule">
            <template v-slot:prepend>
              <v-icon icon="mdi-routes"></v-icon>
            </template>
            <v-list-item-title v-text="$t('rule.import.rulesTitle')"></v-list-item-title>
          </v-list-item>
          <v-list-item link @click="showImportRulesets">
            <template v-slot:prepend>
              <v-icon icon="mdi-download-multiple"></v-icon>
            </template>
            <v-list-item-title v-text="$t('rule.import.title')"></v-list-item-title>
          </v-list-item>
        </v-list>
      </v-menu>
      <v-btn variant="outlined" color="warning" @click="saveConfig" :loading="loading" :disabled="stateChange">
        <v-icon icon="mdi-content-save-outline" start />{{ $t('actions.save') }}
      </v-btn>
    </template>
  </PageHero>
  <v-row class="rules-section np-section-card">
    <v-col class="rules-section__heading" cols="12"><h2>{{ $t('basic.routing.title') }}</h2><p>设置默认路由行为与系统接口识别策略。</p></v-col>
    <v-col cols="12">
      <v-row>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-select hide-details :label="$t('basic.routing.defaultOut')" clearable
            @click:clear="delete route.final" :items="outboundTags" v-model="route.final"></v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-text-field v-model="route.default_interface" hide-details clearable
            @click:clear="delete route.default_interface" :label="$t('basic.routing.defaultIf')"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-text-field v-model.number="routeMark" hide-details type="number" min="0" :label="$t('basic.routing.defaultRm')"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-switch v-model="route.auto_detect_interface" color="primary" :label="$t('basic.routing.autoBind')" hide-details></v-switch>
        </v-col>
      </v-row>
    </v-col>
  </v-row>
  <v-row class="rules-section route-explain np-section-card">
    <v-col class="rules-section__heading" cols="12">
      <div><h2>路由命中诊断</h2><p>按当前运行态规则模拟一次匹配，不产生真实连接。</p></div>
      <v-btn color="primary" variant="tonal" :loading="explainLoading" @click="runRouteExplain"><v-icon icon="mdi-radar" start />分析</v-btn>
    </v-col>
    <v-col cols="12">
      <v-row dense>
        <v-col cols="12" md="4"><v-text-field v-model="explainInput.domain" label="域名" placeholder="www.netflix.com" hide-details /></v-col>
        <v-col cols="6" md="2"><v-text-field v-model.number="explainInput.port" type="number" min="1" max="65535" label="端口" hide-details /></v-col>
        <v-col cols="6" md="2"><v-select v-model="explainInput.network" :items="['tcp', 'udp']" label="网络" hide-details /></v-col>
        <v-col cols="12" md="4"><v-text-field v-model="explainInput.destination" label="目标 IP（可选）" placeholder="1.1.1.1" clearable hide-details /></v-col>
        <v-col cols="12" md="4"><v-select v-model="explainInput.inbound" :items="inboundTags" label="入站（可选）" clearable hide-details /></v-col>
        <v-col cols="12" md="4"><v-select v-model="explainInput.user" :items="clients" label="用户（可选）" clearable hide-details /></v-col>
        <v-col cols="12" md="4"><v-text-field v-model="explainInput.protocol" label="嗅探协议（可选）" placeholder="tls / http / dns" clearable hide-details /></v-col>
      </v-row>
      <div v-if="explainResult" class="route-explain__result" :class="{ 'route-explain__result--default': explainResult.defaultUsed }">
        <v-icon :icon="explainResult.defaultUsed ? 'mdi-sign-direction' : 'mdi-check-decagram-outline'" size="22" />
        <div>
          <strong>{{ explainResult.defaultUsed ? '使用默认出口' : `命中第 ${explainResult.ruleIndex + 1} 条规则` }}</strong>
          <span>{{ explainResult.action || explainResult.note }}</span>
          <code v-if="explainResult.rule">{{ explainResult.rule }}</code>
        </div>
      </div>
    </v-col>
  </v-row>
  <v-row class="rules-section">
    <v-col class="rules-section__heading" cols="12">
      <div><h2>DNS 分流</h2><p>选择哪些域名使用指定的 DNS 服务器；从上到下优先匹配。</p></div>
      <v-btn color="primary" variant="tonal" @click="showDnsRuleModal(-1)"><v-icon icon="mdi-dns-outline" start />添加 DNS 分流</v-btn>
    </v-col>
    <v-col v-if="dnsRules.length === 0" cols="12">
      <EmptyState icon="mdi-dns-outline" title="暂无 DNS 分流" description="默认使用 DNS 页面设置的解析器；需要特殊解析时再添加规则。" />
    </v-col>
    <v-col cols="12" sm="6" md="4" lg="3" v-for="(item, index) in <any[]>dnsRules"
      :key="item.id ?? index" :draggable="true"
      @dragstart="onDnsDragStart(index)" @dragover.prevent @drop="onDnsDrop(index)">
      <v-card class="np-resource-card" rounded="xl" variant="flat">
        <v-card-title class="route-rule-card__title">
          <span class="route-rule-card__index">{{ index + 1 }}</span>
          <div>
            <strong>{{ dnsRuleActionLabel(item) }}</strong>
            <small>{{ item.type != undefined ? `逻辑规则 · ${item.mode}` : 'DNS 规则' }}</small>
          </div>
        </v-card-title>
        <v-card-text>
          <v-row><v-col>匹配</v-col><v-col>{{ dnsRuleMatchLabel(item) }}</v-col></v-row>
          <v-row><v-col>解析器</v-col><v-col>{{ dnsRuleTargetLabel(item) }}</v-col></v-row>
          <v-row v-if="dnsRuleOptionLabel(item)"><v-col>选项</v-col><v-col>{{ dnsRuleOptionLabel(item) }}</v-col></v-row>
          <v-row v-if="item.invert"><v-col>结果</v-col><v-col>反选匹配</v-col></v-row>
        </v-card-text>
        <v-divider></v-divider>
        <v-card-actions class="np-resource-card__actions">
          <v-btn class="np-card-action" variant="text" @click="showDnsRuleModal(index)">
            <v-icon icon="mdi-file-edit" /><span>{{ $t('actions.edit') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
          <v-btn class="np-card-action" variant="text" color="warning" @click="delDnsRuleOverlay[index] = true">
            <v-icon icon="mdi-file-remove" /><span>{{ $t('actions.del') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
          </v-btn>
          <v-overlay v-model="delDnsRuleOverlay[index]" contained class="align-center justify-center">
            <v-card :title="$t('actions.del')" rounded="lg">
              <v-divider></v-divider>
              <v-card-text>{{ $t('confirm') }}</v-card-text>
              <v-card-actions>
                <v-btn color="error" variant="outlined" @click="delDnsRule(index)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delDnsRuleOverlay[index] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-overlay>
        </v-card-actions>
      </v-card>
    </v-col>
  </v-row>
  <v-row class="rules-section">
    <v-col class="rules-section__heading" cols="12">
      <div><h2>{{ $t('rule.ruleset') }}</h2><p>远程或本地规则集，可被 DNS 分流和流量规则复用。</p></div>
      <div class="rules-section__actions">
        <v-btn variant="text" :loading="healthLoading" @click="loadRuleSetHealth"><v-icon icon="mdi-refresh" start />刷新状态</v-btn>
        <v-btn color="primary" variant="tonal" @click="showRulesetModal(-1)"><v-icon icon="mdi-playlist-plus" start />{{ $t('ruleset.add') }}</v-btn>
      </div>
    </v-col>
    <v-col v-if="rulesets.length === 0" cols="12">
      <EmptyState icon="mdi-file-tree-outline" title="暂无规则集" description="添加规则集后，可在路由规则中按标签引用。" />
    </v-col>
    <v-col cols="12" sm="6" md="4" lg="3" v-for="(item, index) in <any[]>rulesets" :key="item.tag">
      <v-card class="np-resource-card" rounded="xl" variant="flat">
        <v-card-title class="ruleset-card__title">
          <span>{{ item.tag }}</span>
          <v-chip size="small" :color="healthByTag[item.tag]?.loaded ? 'success' : 'warning'" variant="tonal">
            {{ healthByTag[item.tag]?.loaded ? '已加载' : '未加载' }}
          </v-chip>
        </v-card-title>
        <v-card-subtitle style="margin-top: -15px;">
          <v-row><v-col>{{ $t('ruleset.' + item.type) }}</v-col></v-row>
        </v-card-subtitle>
        <v-card-text>
          <v-row><v-col>{{ $t('ruleset.format') }}</v-col><v-col>{{ item.format }}</v-col></v-row>
          <v-row><v-col>{{ $t('objects.outbound') }}</v-col><v-col>{{ item.download_detour ?? '-' }}</v-col></v-row>
          <v-row><v-col>{{ $t('actions.update') }}</v-col><v-col>{{ item.update_interval ?? '-' }}</v-col></v-row>
        </v-card-text>
        <v-divider></v-divider>
        <v-card-actions class="np-resource-card__actions">
          <v-btn class="np-card-action" variant="text" @click="showRulesetModal(index)">
            <v-icon icon="mdi-file-edit" /><span>{{ $t('actions.edit') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
          <v-btn class="np-card-action" variant="text" color="warning" @click="delRulesetOverlay[index] = true">
            <v-icon icon="mdi-file-remove" /><span>{{ $t('actions.del') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
          </v-btn>
          <v-overlay v-model="delRulesetOverlay[index]" contained class="align-center justify-center">
            <v-card :title="$t('actions.del')" rounded="lg">
              <v-divider></v-divider>
              <v-card-text>{{ $t('confirm') }}</v-card-text>
              <v-card-actions>
                <v-btn color="error" variant="outlined" @click="delRuleset(index)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delRulesetOverlay[index] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-overlay>
        </v-card-actions>
      </v-card>
    </v-col>
  </v-row>
  <v-row class="rules-section">
    <v-col class="rules-section__heading" cols="12">
      <div><h2>{{ $t('rule.listTitle') }}</h2><p>这里显示实际生效的流量规则；常用分流优先使用上方“规则目录”。</p></div>
      <v-btn color="primary" variant="tonal" @click="showRuleModal(-1)"><v-icon icon="mdi-plus" start />手动添加</v-btn>
    </v-col>
    <v-col v-if="rules.length === 0" cols="12">
      <EmptyState icon="mdi-routes" title="暂无路由规则" description="添加第一条规则后，流量会按列表顺序执行匹配。" />
    </v-col>
    <v-col cols="12" sm="6" md="4" lg="3" v-for="(item, index) in <any[]>rules"
        :key="item.id ?? index" :draggable="true"
        @dragstart="onDragStart(index)" @dragover.prevent @drop="onDrop(index)">
      <v-card class="np-resource-card" rounded="xl" variant="flat">
        <v-card-title class="route-rule-card__title">
          <span class="route-rule-card__index">{{ index + 1 }}</span>
          <div>
            <strong>{{ ruleActionLabel(item) }}</strong>
            <small>{{ item.type != undefined ? `逻辑规则 · ${item.mode}` : ruleCategoryLabel(item) }}</small>
          </div>
        </v-card-title>
        <v-card-text>
          <v-row><v-col>匹配</v-col><v-col>{{ ruleMatchLabel(item) }}</v-col></v-row>
          <v-row v-if="ruleTargetLabel(item)"><v-col>目标</v-col><v-col>{{ ruleTargetLabel(item) }}</v-col></v-row>
          <v-row v-if="ruleScopeLabel(item)"><v-col>范围</v-col><v-col>{{ ruleScopeLabel(item) }}</v-col></v-row>
          <v-row v-if="item.invert"><v-col>结果</v-col><v-col>反选匹配</v-col></v-row>
        </v-card-text>
        <v-divider></v-divider>
        <v-card-actions class="np-resource-card__actions">
          <v-btn class="np-card-action" variant="text" @click="showRuleModal(index)">
            <v-icon icon="mdi-file-edit" /><span>{{ $t('actions.edit') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
          <v-btn class="np-card-action" variant="text" color="warning" @click="delRuleOverlay[index] = true">
            <v-icon icon="mdi-file-remove" /><span>{{ $t('actions.del') }}</span>
            <v-tooltip activator="parent" location="top" :text="$t('actions.del')"></v-tooltip>
          </v-btn>
          <v-overlay v-model="delRuleOverlay[index]" contained class="align-center justify-center">
            <v-card :title="$t('actions.del')" rounded="lg">
              <v-divider></v-divider>
              <v-card-text>{{ $t('confirm') }}</v-card-text>
              <v-card-actions>
                <v-btn color="error" variant="outlined" @click="delRule(index)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delRuleOverlay[index] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-overlay>
        </v-card-actions>
      </v-card>
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import { computed, defineAsyncComponent, ref, onBeforeMount } from 'vue'
import { Config } from '@/types/config'
import { ruleset } from '@/types/rules'
import { dnsRule } from '@/types/dns'
import { FindDiff } from '@/plugins/utils'
import PageHero from '@/components/PageHero.vue'
import EmptyState from '@/components/EmptyState.vue'
import HttpUtils from '@/plugins/httputil'
import { push } from 'notivue'

const RuleVue = defineAsyncComponent(() => import('@/layouts/modals/Rule.vue'))
const RulesetVue = defineAsyncComponent(() => import('@/layouts/modals/Ruleset.vue'))
const RulesetImport = defineAsyncComponent(() => import('@/layouts/modals/RulesetImport.vue'))
const RuleImport = defineAsyncComponent(() => import('@/layouts/modals/RuleImport.vue'))
const RuleCatalog = defineAsyncComponent(() => import('@/layouts/modals/RuleCatalog.vue'))
const DnsRuleVue = defineAsyncComponent(() => import('@/layouts/modals/DnsRule.vue'))

const oldConfig = ref({})
const loading = ref(false)
const actionMenu = ref(false)
const appConfig = computed((): Config => {
  return <Config> Data().config
})

onBeforeMount(async () => {
  loading.value = true
  while (Data().lastLoad == 0) {
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  oldConfig.value = JSON.parse(JSON.stringify(Data().config))
  await loadRuleSetHealth()
  loading.value = false
})

const routeMark = computed({
  get() { return route.value.default_mark ?? 0 },
  set(v:number) { v>0 ? route.value.default_mark = v : delete appConfig.value.route.default_mark }
})

const stateChange = computed(() => FindDiff.deepCompare(appConfig.value, oldConfig.value))

const saveConfig = async () => {
  loading.value = true
  try {
    const success = await Data().save("config", "set", appConfig.value)
    if (success) {
      oldConfig.value = JSON.parse(JSON.stringify(Data().config))
    }
  } finally {
    loading.value = false
  }
}

const clients = computed((): string[] => Data().clients.map((c:any) => c.name))
const route = computed((): any => appConfig.value.route ?? {})

const rules = computed((): any[] => {
  const data = route.value
  if (!data) return []
  if (!('rules' in data) || !Array.isArray(data.rules)) data.rules = []
  return data.rules
})

const rulesets = computed((): any[] => {
  const data = route.value
  if (!data) return []
  if (!('rule_set' in data) || !Array.isArray(data.rule_set)) data.rule_set = []
  return data.rule_set
})

const rulesetTags = computed((): string[] => rulesets.value.map((rs:any) => rs.tag))

const outboundTags = computed((): string[] => [
  ...Data().outbounds?.map((o:any) => o.tag),
  ...Data().endpoints?.filter((e:any) => e.type != "masque").map((e:any) => e.tag)
])

const inboundTags = computed((): string[] => [
  ...Data().inbounds?.map((o:any) => o.tag),
  ...Data().endpoints?.filter((e:any) => e.listen_port > 0 && e.type != "masque").map((e:any) => e.tag)
])

const dnsServerTags = computed((): string[] =>
  appConfig.value.dns?.servers?.filter((server: any) => server.tag)?.map((server: any) => server.tag) ?? []
)

const dnsRules = computed((): dnsRule[] => {
  if (!appConfig.value.dns) return []
  if (!Array.isArray(appConfig.value.dns.rules)) appConfig.value.dns.rules = []
  return appConfig.value.dns.rules
})

const catalogModal = ref(false)
const catalogLoading = ref(false)

async function applyCatalog(payload: any) {
  catalogLoading.value = true
  try {
    const draft: any = JSON.parse(JSON.stringify(appConfig.value))
    draft.route = draft.route ?? {}
    draft.route.rules = Array.isArray(draft.route.rules) ? draft.route.rules : []
    draft.route.rule_set = Array.isArray(draft.route.rule_set) ? draft.route.rule_set : []
    const knownTags = new Set(draft.route.rule_set.map((item: any) => item.tag))
    let changed = false
    for (const asset of payload.item.assets) {
      if (knownTags.has(asset.tag)) continue
      const ruleSet: any = {
        type: 'remote',
        tag: asset.tag,
        format: 'binary',
        url: asset.url,
        update_interval: '1d',
      }
      if (payload.downloadDetour) ruleSet.download_detour = payload.downloadDetour
      draft.route.rule_set.push(ruleSet)
      knownTags.add(asset.tag)
      changed = true
    }
    const selectedTags = payload.item.assets.map((asset: any) => asset.tag)
    if (payload.action !== 'none') {
      const rule: any = { ...(payload.item.directRule ?? {}) }
      if (selectedTags.length > 0) rule.rule_set = selectedTags
      if (payload.inbound) rule.inbound = [payload.inbound]
      if (payload.user) rule.auth_user = [payload.user]
      rule.action = payload.action
      if (payload.action === 'route') rule.outbound = payload.outbound

      let insertAt = 0
      const setupActions = new Set(['sniff', 'resolve', 'hijack-dns'])
      while (insertAt < draft.route.rules.length && setupActions.has(draft.route.rules[insertAt]?.action)) insertAt++
      const duplicate = draft.route.rules.some((item: any) => JSON.stringify(item) === JSON.stringify(rule))
      if (!duplicate) {
        draft.route.rules.splice(insertAt, 0, rule)
        changed = true
      }
    }

    if (payload.dnsServer && selectedTags.length > 0) {
      draft.dns = draft.dns ?? {}
      draft.dns.rules = Array.isArray(draft.dns.rules) ? draft.dns.rules : []
      const reusableRuleIndex = draft.dns.rules.findIndex((rule: any) =>
        rule?.action === 'route' &&
        rule?.server === payload.dnsServer &&
        Array.isArray(rule.rule_set) &&
        !rule.type &&
        !rule.inbound &&
        !rule.auth_user
      )
      if (reusableRuleIndex >= 0) {
        const reusableRule = draft.dns.rules[reusableRuleIndex]
        const mergedTags = [...new Set([...reusableRule.rule_set, ...selectedTags])]
        if (mergedTags.length !== reusableRule.rule_set.length || reusableRuleIndex !== 0) changed = true
        reusableRule.rule_set = mergedTags
        draft.dns.rules.splice(reusableRuleIndex, 1)
        draft.dns.rules.unshift(reusableRule)
      } else {
        draft.dns.rules.unshift({
          rule_set: selectedTags,
          action: 'route',
          server: payload.dnsServer,
        })
        changed = true
      }
    }

    if (!changed) {
      push.info({ message: '所选规则已经配置，无需重复添加。' })
      catalogModal.value = false
      return
    }

    const success = await Data().save('config', 'set', draft)
    if (success) {
      oldConfig.value = JSON.parse(JSON.stringify(Data().config))
      catalogModal.value = false
      await loadRuleSetHealth()
    }
  } finally {
    catalogLoading.value = false
  }
}

const healthLoading = ref(false)
const ruleSetHealth = ref<any[]>([])
const healthByTag = computed(() => Object.fromEntries(ruleSetHealth.value.map((item) => [item.tag, item])))

async function loadRuleSetHealth() {
  healthLoading.value = true
  try {
    const response = await HttpUtils.get('api/ruleset-health')
    if (response.success) ruleSetHealth.value = response.obj ?? []
  } finally {
    healthLoading.value = false
  }
}

const explainInput = ref({ domain: '', destination: '', port: 443, inbound: '', user: '', network: 'tcp', protocol: '' })
const explainResult = ref<any | null>(null)
const explainLoading = ref(false)

async function runRouteExplain() {
  explainLoading.value = true
  try {
    const response = await HttpUtils.post('api/routeExplain', explainInput.value, {
      headers: { 'Content-Type': 'application/json' },
    })
    if (response.success) explainResult.value = response.obj
  } finally {
    explainLoading.value = false
  }
}

function listRuleValues(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String).filter(Boolean)
  return value === undefined || value === null || value === '' ? [] : [String(value)]
}

function compactRuleValues(values: string[]): string {
  if (values.length <= 2) return values.join('、')
  return `${values.slice(0, 2).join('、')} 等 ${values.length} 项`
}

function ruleActionLabel(rule: any): string {
  switch (rule?.action) {
    case 'route': return '流量分流'
    case 'reject': return '拒绝访问'
    case 'sniff': return '识别目标域名'
    case 'hijack-dns': return '接管 DNS 请求'
    case 'resolve': return '解析目标域名'
    default: return rule?.action || '未设置动作'
  }
}

function ruleCategoryLabel(rule: any): string {
  if (['sniff', 'hijack-dns', 'resolve'].includes(rule?.action)) return '系统规则'
  return '自定义规则'
}

function ruleMatchLabel(rule: any): string {
  if (Array.isArray(rule?.rules)) return `${rule.rules.length} 条子规则`
  const candidates = [
    rule?.rule_set,
    rule?.domain,
    rule?.domain_suffix,
    rule?.domain_keyword,
    rule?.ip_cidr,
    rule?.protocol,
    rule?.network,
    rule?.port,
    rule?.port_range,
  ]
  for (const candidate of candidates) {
    const values = listRuleValues(candidate)
    if (values.length > 0) return compactRuleValues(values)
  }
  if (['sniff', 'hijack-dns', 'resolve'].includes(rule?.action)) return '符合该系统动作的连接'
  if (rule?.ip_is_private) return '私有网络地址'
  return '全部流量'
}

function ruleTargetLabel(rule: any): string {
  if (rule?.action === 'route') return rule.outbound || route.value.final || '默认出口'
  return ''
}

function ruleScopeLabel(rule: any): string {
  const inbound = listRuleValues(rule?.inbound)
  const users = listRuleValues(rule?.auth_user)
  const parts = []
  if (inbound.length > 0) parts.push(`入站 ${compactRuleValues(inbound)}`)
  if (users.length > 0) parts.push(`用户 ${compactRuleValues(users)}`)
  return parts.join(' · ')
}

function dnsRuleActionLabel(rule: any): string {
  switch (rule?.action) {
    case 'route': return 'DNS 分流'
    case 'reject': return '拒绝解析'
    case 'route-options': return '调整解析选项'
    case 'predefined': return '固定响应'
    default: return rule?.action || 'DNS 分流'
  }
}

function dnsRuleMatchLabel(rule: any): string {
  if (Array.isArray(rule?.rules)) return `${rule.rules.length} 条子规则`
  const candidates = [
    rule?.rule_set,
    rule?.domain,
    rule?.domain_suffix,
    rule?.domain_keyword,
    rule?.query_type,
    rule?.inbound,
    rule?.auth_user,
    rule?.source_ip_cidr,
  ]
  for (const candidate of candidates) {
    const values = listRuleValues(candidate)
    if (values.length > 0) return compactRuleValues(values)
  }
  return '全部 DNS 查询'
}

function dnsRuleTargetLabel(rule: any): string {
  if (rule?.action === 'reject') return '拒绝'
  if (rule?.action === 'predefined') return '预定义响应'
  return rule?.server || appConfig.value.dns?.final || '默认 DNS'
}

function dnsRuleOptionLabel(rule: any): string {
  const options = []
  if (rule?.strategy) options.push(String(rule.strategy))
  if (rule?.disable_cache) options.push('禁用缓存')
  if (rule?.rewrite_ttl !== undefined) options.push(`TTL ${rule.rewrite_ttl}`)
  return options.join(' · ')
}

let delRuleOverlay = ref(new Array<boolean>)
let delRulesetOverlay = ref(new Array<boolean>)
let delDnsRuleOverlay = ref(new Array<boolean>)

const ruleModal = ref({ visible: false, index: -1, data: "" })
const showRuleModal = (index: number) => {
  ruleModal.value.index = index
  ruleModal.value.data = index == -1 ? '' : JSON.stringify(rules.value[index])
  ruleModal.value.visible = true
}
const closeRuleModal = () => { ruleModal.value.visible = false }
const saveRuleModal = (data:any) => {
  if (ruleModal.value.index == -1) rules.value.push(data)
  else rules.value[ruleModal.value.index] = data
  ruleModal.value.visible = false
}
const delRule = (index: number) => { rules.value.splice(index, 1); delRuleOverlay.value[index] = false }

const dnsRuleModal = ref({ visible: false, index: -1, data: '' })
const showDnsRuleModal = (index: number) => {
  dnsRuleModal.value.index = index
  dnsRuleModal.value.data = index == -1 ? '' : JSON.stringify(dnsRules.value[index])
  dnsRuleModal.value.visible = true
}
const closeDnsRuleModal = () => { dnsRuleModal.value.visible = false }
const saveDnsRuleModal = (data: dnsRule) => {
  if (dnsRuleModal.value.index == -1) dnsRules.value.push(data)
  else dnsRules.value[dnsRuleModal.value.index] = data
  dnsRuleModal.value.visible = false
}
const delDnsRule = (index: number) => {
  dnsRules.value.splice(index, 1)
  delDnsRuleOverlay.value[index] = false
}

const rulesetModal = ref({ visible: false, index: -1, data: "" })
const showRulesetModal = (index: number) => {
  rulesetModal.value.index = index
  rulesetModal.value.data = index == -1 ? '' : JSON.stringify(rulesets.value[index])
  rulesetModal.value.visible = true
}
const closeRulesetModal = () => { rulesetModal.value.visible = false }
const saveRulesetModal = (data:ruleset) => {
  if (rulesetModal.value.index == -1) {
    rulesets.value.push(data)
  } else {
    const previousTag = rulesets.value[rulesetModal.value.index]?.tag
    rulesets.value[rulesetModal.value.index] = data
    if (previousTag && previousTag !== data.tag) {
      replaceRuleSetReferences(route.value.rules, previousTag, data.tag)
      replaceRuleSetReferences(appConfig.value.dns?.rules, previousTag, data.tag)
    }
  }
  rulesetModal.value.visible = false
}
const delRuleset = (index: number) => {
  const tag = rulesets.value[index]?.tag
  const references = findRuleSetReferences(tag)
  delRulesetOverlay.value[index] = false
  if (references.length > 0) {
    const visibleReferences = references.slice(0, 3).join('、')
    const remainder = references.length > 3 ? `，另有 ${references.length - 3} 处` : ''
    push.error({
      title: '无法删除规则集',
      message: `${tag} 仍被 ${visibleReferences}${remainder} 引用，请先删除或修改对应规则。`,
    })
    return
  }
  rulesets.value.splice(index, 1)
}

function findRuleSetReferences(tag: string): string[] {
  if (!tag) return []
  return [
    ...collectRuleSetReferencePaths(route.value.rules, tag, 'route.rules'),
    ...collectRuleSetReferencePaths(appConfig.value.dns?.rules, tag, 'dns.rules'),
  ]
}

function collectRuleSetReferencePaths(value: unknown, tag: string, path: string): string[] {
  if (Array.isArray(value)) {
    return value.flatMap((item, index) => collectRuleSetReferencePaths(item, tag, `${path}[${index}]`))
  }
  if (!value || typeof value !== 'object') return []

  const references: string[] = []
  for (const [key, item] of Object.entries(value)) {
    const itemPath = `${path}.${key}`
    if (key === 'rule_set') {
      const tags = Array.isArray(item) ? item : [item]
      if (tags.includes(tag)) references.push(itemPath)
      continue
    }
    references.push(...collectRuleSetReferencePaths(item, tag, itemPath))
  }
  return references
}

function replaceRuleSetReferences(value: unknown, previousTag: string, nextTag: string): void {
  if (Array.isArray(value)) {
    value.forEach((item) => replaceRuleSetReferences(item, previousTag, nextTag))
    return
  }
  if (!value || typeof value !== 'object') return

  for (const [key, item] of Object.entries(value)) {
    if (key === 'rule_set') {
      const target = value as Record<string, unknown>
      if (Array.isArray(item)) {
        target[key] = item.map((tag) => tag === previousTag ? nextTag : tag)
      } else if (item === previousTag) {
        target[key] = nextTag
      }
      continue
    }
    replaceRuleSetReferences(item, previousTag, nextTag)
  }
}

const draggedItemIndex = ref<number | null>(null)
const onDragStart = (index: number) => { draggedItemIndex.value = index }
const onDrop = (index: any) => {
  if (draggedItemIndex.value !== null) {
    const draggedItem = rules.value[draggedItemIndex.value]
    rules.value.splice(draggedItemIndex.value, 1)
    rules.value.splice(index, 0, draggedItem)
    draggedItemIndex.value = null
  }
}

const draggedDnsRuleIndex = ref<number | null>(null)
const onDnsDragStart = (index: number) => { draggedDnsRuleIndex.value = index }
const onDnsDrop = (index: number) => {
  if (draggedDnsRuleIndex.value === null) return
  const draggedItem = dnsRules.value[draggedDnsRuleIndex.value]
  dnsRules.value.splice(draggedDnsRuleIndex.value, 1)
  dnsRules.value.splice(index, 0, draggedItem)
  draggedDnsRuleIndex.value = null
}

const importRulesModal = ref({ visible: false })

function showImportRule() {
  importRulesModal.value.visible = true
}

function closeImportRule() {
  importRulesModal.value.visible = false
}

function saveImportRule(block: any, mode: 'merge' | 'replace', applyFinal: boolean) {
  if (mode === 'replace') {
    route.value.rules = block.rules ?? []
    route.value.rule_set = block.rule_set ?? []
  } else {
    const existingTags = new Set(rulesetTags.value)
    if (block.rules) rules.value.push(...block.rules)
    if (block.rule_set) {
      for (const rs of block.rule_set) {
        if (!existingTags.has(rs.tag)) rulesets.value.push(rs)
      }
    }
  }
  if (applyFinal && block.final) route.value.final = block.final
  importRulesModal.value.visible = false
}

const importRulesetsModal = ref({ visible: false })

function showImportRulesets() {
  importRulesetsModal.value.visible = true
}

function closeImportRulesets() {
  importRulesetsModal.value.visible = false
}

function saveImportRulesets(items: any[]) {
  rulesets.value.push(...items)
  importRulesetsModal.value.visible = false
}
</script>

<style scoped>
.rules-section {
  margin: 0 0 18px;
  border: 1px solid var(--np-border);
  border-radius: 26px;
  background: var(--np-surface-muted);
}

.rules-section.np-section-card {
  padding: 12px;
}

.rules-section__heading h2 {
  margin: 0;
  font-size: 18px;
  letter-spacing: -0.02em;
}

.rules-section__heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.rules-section__heading p {
  margin: 5px 0 0;
  color: var(--np-text-muted);
  font-size: 13px;
}

.rules-section__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ruleset-card__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.route-rule-card__title {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 72px;
}

.route-rule-card__title > div {
  display: grid;
  min-width: 0;
}

.route-rule-card__title strong {
  overflow: hidden;
  font-size: 16px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.route-rule-card__title small {
  margin-top: 2px;
  color: var(--np-text-muted);
  font-size: 12px;
}

.route-rule-card__index {
  display: grid;
  flex: 0 0 34px;
  width: 34px;
  height: 34px;
  place-items: center;
  border-radius: 12px;
  color: var(--np-accent);
  background: rgba(10, 132, 255, .1);
  font-size: 13px;
  font-weight: 700;
}

.route-explain__result {
  display: flex;
  gap: 12px;
  margin-top: 18px;
  padding: 16px;
  border: 1px solid rgba(52, 199, 89, .28);
  border-radius: 18px;
  background: rgba(52, 199, 89, .08);
}

.route-explain__result--default {
  border-color: var(--np-border);
  background: var(--np-surface-muted);
}

.route-explain__result div {
  min-width: 0;
  display: grid;
  gap: 4px;
}

.route-explain__result span {
  color: var(--np-text-muted);
}

.route-explain__result code {
  overflow: auto;
  padding-top: 5px;
  color: var(--np-text-muted);
  font-size: 12px;
}

@media (max-width: 599px) {
  .rules-section { border-radius: 22px; }

  .rules-section__heading {
    align-items: flex-start;
  }

  .rules-section__heading .v-btn {
    min-width: 42px;
    padding-inline: 10px;
  }

  .rules-section__heading .v-btn :deep(.v-btn__content) {
    font-size: 0;
  }

  .rules-section__heading .v-btn :deep(.v-icon) {
    margin: 0;
    font-size: 20px;
  }

  .rules-section__actions {
    gap: 2px;
  }
}
</style>
