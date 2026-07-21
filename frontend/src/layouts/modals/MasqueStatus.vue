<template>
  <v-dialog transition="dialog-bottom-transition" width="860">
    <v-card class="rounded-lg" :loading="loading">
      <v-card-title>
        <v-row>
          <v-col cols="auto">MASQUE 核心状态</v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto"><v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="$emit('close')" /></v-col>
        </v-row>
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text class="masque-status">
        <div class="text-medium-emphasis mb-4">
          {{ status.tag ? `节点：${status.tag}` : 'MASQUE 节点状态与证书解析信息' }}
        </div>
        <v-alert v-if="alert" type="warning" variant="outlined" class="mb-4" :text="alert"></v-alert>
        <template v-else>
          <v-row dense class="mb-4">
            <v-col cols="12" md="3">
              <div class="masque-status__label">运行状态</div>
              <v-chip :color="status.running ? 'success' : 'warning'" variant="flat" size="small">
                {{ status.running ? '运行中' : '未运行' }}
              </v-chip>
            </v-col>
            <v-col cols="12" md="3">
              <div class="masque-status__label">监听地址</div>
              <div class="masque-status__value">{{ status.bind_addr ?? '-' }}</div>
            </v-col>
            <v-col cols="12" md="3">
              <div class="masque-status__label">协议网络</div>
              <div class="masque-status__value">{{ status.network ?? '-' }}</div>
            </v-col>
            <v-col cols="12" md="3">
              <div class="masque-status__label">远端主机</div>
              <div class="masque-status__value">{{ status.host ?? '-' }}</div>
            </v-col>
          </v-row>

          <v-row dense class="mb-4">
            <v-col cols="12" md="4">
              <v-sheet class="masque-status__sheet" rounded="lg">
                <div class="masque-status__label">端口</div>
                <div class="masque-status__value">{{ status.port ?? '-' }}</div>
              </v-sheet>
            </v-col>
            <v-col cols="12" md="4">
              <v-sheet class="masque-status__sheet" rounded="lg">
                <div class="masque-status__label">证书文件</div>
                <div class="masque-status__value masque-status__value--path">{{ status.cert_file ?? '-' }}</div>
              </v-sheet>
            </v-col>
            <v-col cols="12" md="4">
              <v-sheet class="masque-status__sheet" rounded="lg">
                <div class="masque-status__label">密钥文件</div>
                <div class="masque-status__value masque-status__value--path">{{ status.key_file ?? '-' }}</div>
              </v-sheet>
            </v-col>
          </v-row>

          <v-sheet class="masque-status__sheet masque-status__sheet--wide" rounded="lg">
            <div class="masque-status__label">MASQUE URI 模板</div>
            <div class="masque-status__value masque-status__value--path">{{ status.template ?? '-' }}</div>
          </v-sheet>

          <v-alert
            v-if="status.cert_error"
            type="error"
            variant="outlined"
            class="mt-4"
            :text="status.cert_error"
          ></v-alert>
        </template>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import HttpUtils from '@/plugins/httputil'

type MasqueStatus = {
  tag?: string
  host?: string
  port?: number
  network?: string
  running?: boolean
  bind_addr?: string
  template?: string
  cert_file?: string
  key_file?: string
  cert_error?: string
}

export default {
  props: ['data', 'visible'],
  data() {
    return {
      loading: false,
      alert: '',
      status: <MasqueStatus>{},
    }
  },
  methods: {
    async load() {
      const tag = String(this.$props.data?.tag ?? '').trim()
      if (!tag) {
        this.alert = '缺少 MASQUE 节点标识'
        this.status = {}
        return
      }
      this.loading = true
      this.alert = ''
      const resp = await HttpUtils.get('api/masque-status', { tag })
      if (resp.success && resp.obj) {
        this.status = resp.obj as MasqueStatus
      } else {
        this.status = {}
        this.alert = resp.msg || '加载 MASQUE 状态失败'
      }
      this.loading = false
    },
    reset() {
      this.status = {}
      this.alert = ''
      this.loading = false
    },
  },
  watch: {
    visible(v) {
      if (v) {
        this.load()
      } else {
        this.reset()
      }
    },
  },
}
</script>

<style scoped>
.masque-status {
  color: rgb(var(--v-theme-on-surface));
}

.masque-status__label {
  font-size: 12px;
  color: rgba(var(--v-theme-on-surface), 0.55);
  margin-bottom: 6px;
}

.masque-status__value {
  font-size: 15px;
  font-weight: 600;
  word-break: break-all;
  color: rgba(var(--v-theme-on-surface), 0.92);
}

.masque-status__value--path {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  font-size: 13px;
  line-height: 1.5;
}

.masque-status__sheet {
  padding: 16px;
  background: rgba(var(--v-theme-surface-variant), 0.24);
  border: 1px solid rgba(var(--v-theme-outline), 0.15);
}

.masque-status__sheet--wide {
  min-height: 88px;
}
</style>
