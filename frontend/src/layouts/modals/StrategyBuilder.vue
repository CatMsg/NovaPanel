<template>
  <v-dialog :model-value="visible" max-width="760" scrollable @update:model-value="onDialogUpdate">
    <v-card class="strategy-dialog" rounded="xl">
      <v-card-title class="strategy-dialog__header">
        <div><span>OUTBOUND ORCHESTRATION</span><h2>创建策略组</h2><p>手动选择、自动测速或按顺序故障回退。</p></div>
        <v-btn icon="mdi-close" variant="text" aria-label="关闭" @click="$emit('close')" />
      </v-card-title>
      <v-divider />
      <v-card-text class="strategy-dialog__body">
        <v-btn-toggle v-model="mode" color="primary" mandatory divided class="strategy-dialog__modes">
          <v-btn value="manual">手动选择</v-btn>
          <v-btn value="urltest">自动测速</v-btn>
          <v-btn value="failover">有序回退</v-btn>
        </v-btn-toggle>
        <v-row dense class="mt-4">
          <v-col cols="12" md="5"><v-text-field v-model="tag" label="策略组标签" placeholder="streaming-auto" hide-details /></v-col>
          <v-col cols="12" md="7"><v-select v-model="members" :items="memberTags" label="成员出口（选择顺序即优先级）" multiple chips closable-chips hide-details /></v-col>
          <v-col v-if="mode === 'manual'" cols="12"><v-select v-model="defaultMember" :items="members" label="默认成员" clearable hide-details /></v-col>
          <template v-if="mode !== 'manual'">
            <v-col cols="12" md="8"><v-text-field v-model="testUrl" label="探测地址" hide-details /></v-col>
            <v-col cols="12" md="4"><v-text-field v-model.number="intervalSeconds" type="number" min="10" max="3600" label="探测间隔（秒）" hide-details /></v-col>
          </template>
          <template v-if="mode === 'urltest'">
            <v-col cols="12" md="6"><v-text-field v-model.number="tolerance" type="number" min="0" label="切换容差（毫秒）" hide-details /></v-col>
            <v-col cols="12" md="6"><v-switch v-model="interruptConnections" label="切换时中断已有连接" color="primary" hide-details /></v-col>
          </template>
          <template v-if="mode === 'failover'">
            <v-col cols="6"><v-text-field v-model.number="failureThreshold" type="number" min="1" max="10" label="失败阈值" hide-details /></v-col>
            <v-col cols="6"><v-text-field v-model.number="recoveryThreshold" type="number" min="1" max="10" label="恢复阈值" hide-details /></v-col>
          </template>
        </v-row>
        <v-alert class="mt-4" type="info" variant="tonal">
          <template v-if="mode === 'failover'">按列表顺序选择第一个可用出口；连续达到阈值后才切换，避免瞬时抖动。</template>
          <template v-else-if="mode === 'urltest'">由 sing-box 原生 URLTest 按延迟自动选择成员。</template>
          <template v-else>创建 sing-box 原生 Selector，可在运行时手动切换。</template>
        </v-alert>
      </v-card-text>
      <v-card-actions class="strategy-dialog__actions">
        <v-btn variant="text" @click="$emit('close')">取消</v-btn>
        <v-btn color="primary" :loading="loading" :disabled="!valid" @click="submit">创建</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue'

const props = defineProps<{ visible: boolean; memberTags: string[]; loading?: boolean }>()
const emit = defineEmits(['close', 'save'])

const mode = ref<'manual' | 'urltest' | 'failover'>('manual')
const tag = ref('')
const members = ref<string[]>([])
const defaultMember = ref('')
const testUrl = ref('https://www.gstatic.com/generate_204')
const intervalSeconds = ref(30)
const tolerance = ref(50)
const interruptConnections = ref(false)
const failureThreshold = ref(2)
const recoveryThreshold = ref(2)

const valid = computed(() => tag.value.trim().length > 0 && members.value.length >= 2)

watch(members, () => {
  if (defaultMember.value && !members.value.includes(defaultMember.value)) defaultMember.value = ''
})

function submit() {
  if (!valid.value) return
  const outbound: any = {
    type: mode.value === 'urltest' ? 'urltest' : 'selector',
    tag: tag.value.trim(),
    outbounds: [...members.value],
  }
  if (mode.value === 'manual') {
    if (defaultMember.value) outbound.default = defaultMember.value
    outbound.interrupt_exist_connections = interruptConnections.value
  }
  if (mode.value === 'urltest') {
    outbound.url = testUrl.value.trim()
    outbound.interval = `${Math.max(10, intervalSeconds.value)}s`
    outbound.tolerance = Math.max(0, tolerance.value)
    outbound.interrupt_exist_connections = interruptConnections.value
  }
  emit('save', {
    mode: mode.value,
    outbound,
    policy: mode.value === 'failover' ? {
      tag: outbound.tag,
      members: outbound.outbounds,
      testUrl: testUrl.value.trim(),
      intervalSeconds: intervalSeconds.value,
      failureThreshold: failureThreshold.value,
      recoveryThreshold: recoveryThreshold.value,
      enabled: true,
    } : null,
  })
}

function onDialogUpdate(value: boolean) {
  if (!value) emit('close')
}
</script>

<style scoped>
.strategy-dialog { background: var(--np-surface); }
.strategy-dialog__header { display: flex; justify-content: space-between; gap: 18px; padding: 24px; }
.strategy-dialog__header span { color: var(--np-accent); font-size: 11px; font-weight: 700; letter-spacing: .11em; }
.strategy-dialog__header h2 { margin: 3px 0 0; font-size: 24px; }
.strategy-dialog__header p { margin: 6px 0 0; color: var(--np-text-muted); font-size: 13px; }
.strategy-dialog__body { padding: 22px 24px; }
.strategy-dialog__modes { width: 100%; }
.strategy-dialog__modes :deep(.v-btn) { flex: 1; }
.strategy-dialog__actions { justify-content: flex-end; padding: 14px 24px 22px; }
@media (max-width: 600px) {
  .strategy-dialog__header, .strategy-dialog__body { padding-inline: 16px; }
  .strategy-dialog__modes :deep(.v-btn) { padding-inline: 8px; font-size: 12px; }
}
</style>
