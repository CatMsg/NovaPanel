<template>
  <v-dialog :model-value="visible" max-width="720" @update:model-value="onDialogUpdate">
    <v-card class="chain-dialog" rounded="xl">
      <v-card-title class="chain-dialog__header">
        <div><span>PROXY CHAIN</span><h2>代理链</h2><p>让一个出站的连接经由另一个出站建立。</p></div>
        <v-btn icon="mdi-close" variant="text" aria-label="关闭" @click="$emit('close')" />
      </v-card-title>
      <v-divider />
      <v-card-text class="chain-dialog__body">
        <v-row dense>
          <v-col cols="12" md="6"><v-select v-model="target" :items="editableTags" label="要配置的出站" hide-details /></v-col>
          <v-col cols="12" md="6"><v-select v-model="detour" :items="availableDetours" label="连接经由（可清空）" clearable hide-details /></v-col>
        </v-row>
        <div class="chain-preview">
          <span>流量进入</span><v-icon icon="mdi-arrow-right" /><strong>{{ target || '选择出站' }}</strong><v-icon icon="mdi-arrow-right" /><strong>{{ detour || '直接连接' }}</strong>
        </div>
        <v-alert type="warning" variant="tonal">
          修改 detour 会影响所有引用该出站的路由；循环链会被后端预检拒绝。
        </v-alert>
      </v-card-text>
      <v-card-actions class="chain-dialog__actions">
        <v-btn variant="text" @click="$emit('close')">取消</v-btn>
        <v-btn color="primary" :loading="loading" :disabled="!target" @click="$emit('save', { target, detour })">保存链路</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue'

const props = defineProps<{ visible: boolean; outbounds: any[]; allTags: string[]; loading?: boolean }>()
const emit = defineEmits(['close', 'save'])
const target = ref('')
const detour = ref('')

const editableTags = computed(() => props.outbounds.filter((item) => !['selector', 'urltest'].includes(item.type)).map((item) => item.tag))
const availableDetours = computed(() => props.allTags.filter((tag) => tag !== target.value))

watch(target, (tag) => {
  detour.value = props.outbounds.find((item) => item.tag === tag)?.detour ?? ''
})

function onDialogUpdate(value: boolean) {
  if (!value) emit('close')
}
</script>

<style scoped>
.chain-dialog { background: var(--np-surface); }
.chain-dialog__header { display: flex; justify-content: space-between; gap: 18px; padding: 24px; }
.chain-dialog__header span { color: var(--np-accent); font-size: 11px; font-weight: 700; letter-spacing: .11em; }
.chain-dialog__header h2 { margin: 3px 0 0; font-size: 24px; }
.chain-dialog__header p { margin: 6px 0 0; color: var(--np-text-muted); font-size: 13px; }
.chain-dialog__body { padding: 22px 24px; }
.chain-preview { display: flex; align-items: center; gap: 10px; margin: 18px 0; padding: 16px; overflow-x: auto; border: 1px solid var(--np-border); border-radius: 18px; background: var(--np-surface-muted); white-space: nowrap; }
.chain-preview span { color: var(--np-text-muted); }
.chain-dialog__actions { justify-content: flex-end; padding: 14px 24px 22px; }
@media (max-width: 600px) { .chain-dialog__header, .chain-dialog__body { padding-inline: 16px; } }
</style>
