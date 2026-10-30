<template>
  <section class="offhost-backup" aria-label="自动异地备份" :aria-busy="!!working">
    <h3>自动异地备份</h3>
    <v-alert type="warning" variant="tonal" density="compact">
      默认关闭。仅支持严格验证证书的 HTTPS 公网 WebDAV；若目标解析到内网或保留地址会拒绝连接。不执行命令。
      .db 仍包含面板及代理凭据，TLS 不等于静态加密：请使用私有、可信的存储。
      恢复演练不会覆盖当前数据库；下载后可使用上方的手动恢复。
    </v-alert>
    <v-alert v-if="error" type="error" variant="tonal" density="compact" role="alert">{{ error }}</v-alert>
    <div v-if="status" class="offhost-backup__status" role="status">
      <span>{{ status.busy ? '有备份操作进行中' : '空闲' }} · 调度器{{ status.schedulerRunning ? '运行中' : '未运行' }}</span>
      <span>最近成功：{{ date(status.state.lastSuccessAt) }}</span>
      <span>最近操作：{{ status.state.lastOperation || '无' }} · {{ date(status.state.lastFinishedAt) }}</span>
      <span>下次自动备份：{{ form.enabled ? date(status.state.nextDueAt) : '已关闭' }}</span>
      <span v-if="status.namespace">专属目录：{{ status.namespace }}</span>
      <span v-if="status.state.lastError">最近错误：{{ status.state.lastError }}</span>
      <span v-if="status.state.retentionWarning">备份已成功，清理警告：{{ status.state.retentionWarning }}</span>
    </div>
    <v-form v-if="status" @submit.prevent="save">
      <v-switch v-model="form.enabled" label="启用自动备份（保存后生效）" color="primary" :disabled="unavailable" hide-details />
      <v-text-field v-model="form.destination" label="WebDAV 目标目录 HTTPS URL" placeholder="https://storage.example/dav/backups/" :disabled="unavailable" autocomplete="off" hide-details />
      <p class="offhost-backup__hint">目标目录需已存在。应用只创建和清理自己的专属子目录；不会清理其他文件。</p>
      <v-text-field v-model="form.username" label="WebDAV 用户名" :disabled="unavailable" autocomplete="off" hide-details />
      <v-text-field v-model="password" type="password" :label="form.hasPassword ? '密码（已保存，留空保持不变）' : '密码（只写，不回显）'" :disabled="unavailable || clearPassword" autocomplete="new-password" hide-details />
      <v-checkbox v-model="clearPassword" label="清除已保存密码" :disabled="unavailable" hide-details density="compact" />
      <div class="offhost-backup__columns">
        <v-text-field v-model.number="form.intervalMinutes" type="number" min="15" max="43200" label="间隔（分钟，15–43200）" :disabled="unavailable" hide-details />
        <v-text-field v-model.number="form.retention" type="number" min="1" max="100" label="保留份数（1–100）" :disabled="unavailable" hide-details />
      </div>
      <p class="offhost-backup__hint">首次自动上传在保存后一个完整间隔执行。失败后等待下一间隔。密码随目标或用户名变更时必须重新输入。</p>
      <v-btn type="submit" color="primary" variant="tonal" :disabled="unavailable" :loading="working === 'save'">保存设置</v-btn>
    </v-form>
    <div class="offhost-backup__buttons">
      <v-btn variant="outlined" :disabled="unavailable || dirty || !configured" :loading="working === 'test'" @click="run('test')">测试连接</v-btn>
      <v-btn variant="outlined" :disabled="unavailable || dirty || !configured" :loading="working === 'backup'" @click="run('backup')">立即异地备份</v-btn>
      <v-btn variant="text" :disabled="!!working" @click="refresh">刷新状态</v-btn>
      <v-btn variant="text" :disabled="unavailable || dirty || !configured" :loading="working === 'list'" @click="run('list')">读取备份列表</v-btn>
    </div>
    <p v-if="dirty" class="offhost-backup__hint">请先保存设置，再执行远端操作。连接测试只上传并删除无敏感数据的探针文件。</p>
    <v-select v-model="selected" :items="artifacts" item-title="artifact" item-value="artifact" label="选择本实例的已提交备份" :disabled="unavailable" no-data-text="点击读取备份列表" hide-details />
    <div class="offhost-backup__buttons">
      <v-btn variant="outlined" :disabled="unavailable || dirty || !selected" :loading="working === 'rehearse'" @click="run('rehearse')">非破坏性恢复演练</v-btn>
      <v-btn variant="outlined" :disabled="unavailable || dirty || !selected" :loading="working === 'download'" @click="download">校验并下载恢复文件</v-btn>
    </div>
    <v-alert v-if="result" type="success" variant="tonal" density="compact">
      {{ result.message }}
      <div v-if="result.summary">{{ Object.entries(result.summary).map(([table, count]) => `${table}: ${count}`).join(' · ') }}</div>
    </v-alert>
    <p class="offhost-backup__hint">演练检查文件大小、SHA-256、必要数据表/字段，并在只读副本上执行完整 SQLite integrity_check 和与实际恢复共享的 fleet JSON 结构校验；不启动备份内的服务，也不证明凭据、证书或外部网络可用。备份认证及实例归属信息不写入 .db；恢复后的新实例不会清理原实例文件。连接失败时可直接从 WebDAV 专属目录取回 .db 和 .json，再走手动恢复。</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { backupSettingsPayload, backupSettingsDirty, type BackupForm } from './remoteBackupForm'

type BackupState = {
  lastSuccessAt: string; lastOperation: string; lastFinishedAt: string
  nextDueAt: string; lastError: string; retentionWarning: string
}
type BackupStatus = { settings: BackupForm; state: BackupState; busy: boolean; schedulerRunning: boolean; namespace: string }
type Manifest = { artifact: string; size: number; sha256: string }
type Result = { message: string; summary?: Record<string, number>; artifacts?: Manifest[] }
const props = defineProps<{ active: boolean; blocked?: boolean }>()
const emit = defineEmits<{ busy: [value: boolean] }>()
const status = ref<BackupStatus | null>(null)
const form = ref<BackupForm>({ enabled: false, intervalMinutes: 1440, retention: 7, destination: '', username: '', hasPassword: false })
const password = ref('')
const clearPassword = ref(false)
const working = ref('')
const error = ref('')
const result = ref<Result | null>(null)
const artifacts = ref<Manifest[]>([])
const selected = ref('')
const dirty = computed(() => !status.value || backupSettingsDirty(form.value, status.value.settings, password.value, clearPassword.value))
const configured = computed(() => !!form.value.destination && !!form.value.username && form.value.hasPassword)
const unavailable = computed(() => !!working.value || !!status.value?.busy || props.blocked)
let poll: ReturnType<typeof setInterval> | undefined
const controllers = new Set<AbortController>()

function date(value: string) { return value && !value.startsWith('0001-') ? new Date(value).toLocaleString() : '无' }

async function request(path: string, payload?: object): Promise<Response> {
  const controller = new AbortController()
  controllers.add(controller)
  const timer = setTimeout(() => controller.abort(), payload ? 195000 : 15000)
  try {
    const response = await fetch(`api/${path}`, {
      method: payload ? 'POST' : 'GET', credentials: 'same-origin', cache: 'no-store', signal: controller.signal,
      headers: payload ? { 'Content-Type': 'application/json' } : undefined,
      body: payload ? JSON.stringify(payload) : undefined,
    })
    if (!response.ok) throw new Error(`请求失败 HTTP ${response.status}`)
    // Read the body while the deadline remains active, including downloads.
    const blob = await response.blob()
    return new Response(blob, { status: response.status, headers: response.headers })
  } finally { clearTimeout(timer); controllers.delete(controller) }
}

async function json(path: string, payload?: object) {
  const response = await request(path, payload)
  const msg = await response.json()
  if (!msg.success) throw new Error(msg.msg || '备份操作失败')
  return msg.obj
}

async function refresh() {
  try {
    const fresh: BackupStatus = await json('remote-backup-status')
    const canReplace = !status.value || !dirty.value
    status.value = fresh
    if (canReplace) form.value = { ...fresh.settings }
  } catch { error.value = '无法读取备份状态，请检查登录或连接后刷新。' }
}

async function operate(action: string, fn: () => Promise<void>) {
  if (unavailable.value) return
  working.value = action
  emit('busy', true)
  error.value = ''
  result.value = null
  try { await fn() } catch (err) {
    error.value = err instanceof Error && err.name !== 'AbortError' ? err.message : '请求已取消或超时，请刷新状态确认结果，不要重复上传。'
  } finally { working.value = ''; emit('busy', false); await refresh() }
}

async function save() {
  await operate('save', async () => {
    const fresh: BackupStatus = await json('remoteBackupSave', backupSettingsPayload(form.value, password.value, clearPassword.value))
    password.value = ''
    clearPassword.value = false
    status.value = fresh
    form.value = { ...fresh.settings }
    artifacts.value = []
    selected.value = ''
  })
}

async function run(action: string) {
  await operate(action, async () => {
    const fresh: Result = await json('remoteBackupRun', { action, artifact: selected.value })
    result.value = fresh
    if (action === 'list') {
      artifacts.value = fresh.artifacts || []
      if (!artifacts.value.some(item => item.artifact === selected.value)) selected.value = artifacts.value[0]?.artifact || ''
    }
  })
}

async function download() {
  await operate('download', async () => {
    const response = await request('remoteBackupRun', { action: 'download', artifact: selected.value })
    if (response.headers.get('content-type')?.includes('application/json')) {
      const msg = await response.json()
      throw new Error(msg.msg || '恢复文件下载失败')
    }
    const url = URL.createObjectURL(await response.blob())
    const link = document.createElement('a')
    link.href = url
    link.download = selected.value
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    result.value = { message: '恢复文件已通过校验并下载；未修改当前数据库。请使用手动恢复并确认预览。' }
  })
}

watch(() => props.active, active => {
  if (poll) clearInterval(poll)
  if (active) {
    void refresh()
    poll = setInterval(() => { if (!working.value) void refresh() }, 5000)
  } else { password.value = ''; clearPassword.value = false }
}, { immediate: true })
onBeforeUnmount(() => { if (poll) clearInterval(poll); controllers.forEach(controller => controller.abort()); emit('busy', false) })
</script>

<style scoped>
.offhost-backup { display: grid; gap: 14px; border-top: 1px solid var(--np-border); padding-top: 18px; }
.offhost-backup form, .offhost-backup__status { display: grid; gap: 12px; }
.offhost-backup__status { font-size: 0.85rem; overflow-wrap: anywhere; }
.offhost-backup__hint { font-size: 0.8rem; opacity: 0.8; margin: 0; }
.offhost-backup__columns, .offhost-backup__buttons { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.offhost-backup__buttons .v-btn { min-height: 44px; }
@media (max-width: 480px) { .offhost-backup__columns, .offhost-backup__buttons { grid-template-columns: 1fr; } }
</style>
