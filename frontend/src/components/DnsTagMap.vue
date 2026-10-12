<template>
  <section class="dns-tag-map">
    <h3>{{ $t(titleKey) }}</h3>
    <p class="dns-tag-map__hint">{{ $t(hintKey) }}</p>
    <template v-for="entry in entries" :key="`warning-${entry.tag}`">
      <v-alert v-if="entry.status !== 'supported'" class="mb-3" type="warning" variant="tonal">
        {{ tagStatusMessage(entry) }}
      </v-alert>
    </template>
    <v-card v-for="entry in entries" :key="`row-${entry.tag}`" class="dns-tag-map__row" variant="tonal">
      <v-row align="center">
        <v-col cols="12" sm="4">
          <div class="dns-tag-map__tag-label">{{ $t('dns.condition.serverTag') }}</div>
          <code>{{ entry.tag }}</code>
          <div v-if="entry.status === 'supported'" class="dns-tag-map__type">{{ entry.type }}</div>
        </v-col>
        <v-col cols="12" sm="7">
          <v-textarea
            :model-value="listableText(entry.value)"
            :label="$t(valueLabelKey)"
            rows="2"
            auto-grow
            hide-details
            @update:model-value="updateValue(entry.tag, $event)"
          />
        </v-col>
        <v-col cols="12" sm="1" class="dns-tag-map__remove">
          <v-btn
            icon="mdi-delete"
            color="error"
            variant="text"
            :aria-label="$t('dns.condition.remove') + ' ' + entry.tag"
            @click="removeEntry(entry.tag)"
          />
        </v-col>
      </v-row>
    </v-card>
    <v-alert v-if="selectableServers.length === 0" class="mt-3" type="info" variant="tonal">
      {{ $t('dns.condition.noSupportedServers') }}
    </v-alert>
    <v-row v-else class="dns-tag-map__add" align="center">
      <v-col cols="12" sm="4">
        <v-select
          v-model="draftTag"
          :items="availableServers"
          item-title="tag"
          item-value="tag"
          :label="$t('dns.condition.selectServer')"
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="7">
        <v-textarea
          v-model="draftValue"
          :label="$t(valueLabelKey)"
          rows="2"
          auto-grow
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="1" class="dns-tag-map__remove">
        <v-btn
          icon="mdi-plus"
          color="primary"
          variant="tonal"
          :aria-label="$t('dns.condition.add')"
          :disabled="!canAdd"
          @click="addEntry"
        />
      </v-col>
    </v-row>
  </section>
</template>

<script lang="ts">
import {
  dnsConditionTagStatus,
  listableFromText,
  listableText,
  selectableDnsConditionServers,
  type DnsTagConditionMap,
} from '@/types/dnsConditions'

export default {
  props: ['rule', 'servers', 'field'],
  data() {
    return { draftTag: '', draftValue: '' }
  },
  computed: {
    entries(): Array<{ tag: string; value: unknown; status: string; type: string }> {
      const map = this.conditionMap
      return Object.entries(map).map(([tag, value]) => {
        const server = this.selectableServers.find(item => item.tag === tag)
        return {
          tag,
          value,
          status: dnsConditionTagStatus(tag, this.$props.servers),
          type: server?.type ?? this.serverType(tag),
        }
      })
    },
    conditionMap(): Record<string, unknown> {
      const map = this.$props.rule[this.$props.field]
      return typeof map === 'object' && map !== null && !Array.isArray(map) ? map : {}
    },
    selectableServers(): Array<{ tag: string; type: string }> {
      return selectableDnsConditionServers(this.$props.servers)
    },
    availableServers(): Array<{ tag: string; type: string }> {
      const used = new Set(Object.keys(this.conditionMap))
      return this.selectableServers.filter(server => !used.has(server.tag))
    },
    canAdd(): boolean {
      return Boolean(this.draftTag && this.draftValue.trim()) &&
        this.availableServers.some(server => server.tag === this.draftTag)
    },
    titleKey(): string {
      return this.$props.field === 'dns_server_address'
        ? 'dns.condition.serverAddress'
        : 'dns.condition.searchDomain'
    },
    hintKey(): string {
      return this.$props.field === 'dns_server_address'
        ? 'dns.condition.addressHint'
        : 'dns.condition.searchHint'
    },
    valueLabelKey(): string {
      return this.$props.field === 'dns_server_address'
        ? 'dns.condition.values'
        : 'dns.condition.searchValues'
    },
  },
  methods: {
    listableText,
    serverType(tag: string): string {
      const server = Array.isArray(this.$props.servers)
        ? this.$props.servers.find((item: any) => item?.tag === tag)
        : undefined
      return typeof server?.type === 'string' ? server.type : ''
    },
    tagStatusMessage(entry: { tag: string; status: string; type: string }): string {
      if (entry.status === 'duplicate') return this.$t('dns.condition.duplicateTag', { tag: entry.tag })
      if (entry.status === 'unsupported') {
        return this.$t('dns.condition.unsupportedTag', { tag: entry.tag, type: entry.type || 'unknown' })
      }
      return this.$t('dns.condition.unknownTag', { tag: entry.tag })
    },
    updateValue(tag: string, text: string) {
      const map = { ...this.conditionMap }
      map[tag] = listableFromText(text, map[tag])
      this.$props.rule[this.$props.field] = map as DnsTagConditionMap
    },
    removeEntry(tag: string) {
      const map = { ...this.conditionMap }
      delete map[tag]
      if (Object.keys(map).length > 0) this.$props.rule[this.$props.field] = map
      else delete this.$props.rule[this.$props.field]
    },
    addEntry() {
      if (!this.canAdd) return
      const map = { ...this.conditionMap }
      map[this.draftTag] = listableFromText(this.draftValue, undefined)
      this.$props.rule[this.$props.field] = map as DnsTagConditionMap
      this.draftTag = ''
      this.draftValue = ''
    },
  },
}
</script>

<style scoped>
.dns-tag-map { margin-top: 16px; }
.dns-tag-map h3 { margin: 0; font-size: 1rem; font-weight: 600; }
.dns-tag-map__hint { margin: 4px 0 12px; color: rgba(var(--v-theme-on-surface), .68); font-size: .82rem; }
.dns-tag-map__row { margin-bottom: 10px; padding: 8px 12px; }
.dns-tag-map__tag-label { color: rgba(var(--v-theme-on-surface), .62); font-size: .75rem; }
.dns-tag-map__type { color: rgba(var(--v-theme-on-surface), .58); font-size: .75rem; }
.dns-tag-map__remove { display: flex; justify-content: center; }
.dns-tag-map__add { margin-top: 4px; }
</style>
