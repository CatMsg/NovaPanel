<template>
  <v-dialog transition="dialog-bottom-transition" width="90%" max-width="500">
    <v-card class="rounded-lg backup-dialog" :loading="busy">
      <v-card-title class="backup-dialog__title">
        <span>{{ $t('main.backup.title') }}</span>
        <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="control.visible = false" />
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text class="backup-dialog__body">
        <div class="backup-dialog__options">
          <v-checkbox density="compact" v-model="exclude" :label="$t('main.backup.exclStats')" value="stats" hide-details />
          <v-checkbox density="compact" v-model="exclude" :label="$t('main.backup.exclChanges')" value="changes" hide-details />
        </div>
        <div class="backup-dialog__actions">
          <v-btn
            color="primary"
            variant="tonal"
            prepend-icon="mdi-download"
            :loading="downloading"
            :disabled="busy"
            @click="backup()"
          >{{ $t('main.backup.backup') }}</v-btn>
          <v-btn
            color="primary"
            variant="outlined"
            prepend-icon="mdi-backup-restore"
            :loading="restoring"
            :disabled="busy"
            @click="restore()"
          >{{ $t('main.backup.restore') }}</v-btn>
          <v-btn
            class="backup-dialog__config"
            color="primary"
            variant="text"
            prepend-icon="mdi-file-code"
            :disabled="busy"
            @click="config()"
          >{{ $t('main.backup.sbConfig') }}</v-btn>
        </div>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { push } from 'notivue'

export default {
  props: ['control', 'visible'],
  data() {
    return {
      exclude: ['stats', 'changes'],
      downloading: false,
      restoring: false,
    }
  },
  computed: {
    busy() {
      return this.downloading || this.restoring
    },
  },
  methods: {
    async backup() {
      if (this.busy) return

      this.downloading = true
      try {
        const excludeOption = this.exclude.length > 0 ? `?exclude=${this.exclude.join(',')}` : ''
        const response = await fetch(`api/getdb${excludeOption}`, { credentials: 'same-origin' })
        if (!response.ok) {
          throw new Error(`${response.status} ${response.statusText}`.trim())
        }

        const blob = await response.blob()
        const disposition = response.headers.get('content-disposition') || ''
        const encodedName = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1]
        const quotedName = disposition.match(/filename="([^"]+)"/i)?.[1]
        const filename = encodedName
          ? decodeURIComponent(encodedName)
          : quotedName || `novapanel-backup-${new Date().toISOString().slice(0, 10)}.db`
        const url = URL.createObjectURL(blob)
        const link = document.createElement('a')
        link.href = url
        link.download = filename
        document.body.appendChild(link)
        link.click()
        link.remove()
        window.setTimeout(() => URL.revokeObjectURL(url), 1000)
        push.success({ message: i18n.global.t('main.backup.backup') })
      } catch (error) {
        push.error({
          title: i18n.global.t('failed'),
          message: error instanceof Error ? error.message : String(error),
        })
      } finally {
        this.downloading = false
      }
    },
    config() {
      if (!this.busy) window.location.href = 'api/singbox-config'
    },
    restore() {
      if (this.busy) return

      const fileInput = document.createElement('input')
      fileInput.type = 'file'
      fileInput.accept = '.db'

      fileInput.addEventListener('change', async (event: Event) => {
        const inputElement = event.target as HTMLInputElement
        const dbFile = inputElement.files ? inputElement.files[0] : null
        if (!dbFile) return

        this.restoring = true
        const formData = new FormData()
        formData.append('db', dbFile)

        try {
          const checkForm = new FormData()
          checkForm.append('db', dbFile)
          const checkMsg = await HttpUtils.post('api/validateBackup', checkForm, {
            headers: {
              'Content-Type': 'multipart/form-data',
            },
          })
          if (!checkMsg.success) return

          const counts = checkMsg.obj || {}
          const summary = [
            `用户：${counts.users ?? 0}`,
            `入站：${counts.inbounds ?? 0}`,
            `出站：${counts.outbounds ?? 0}`,
            `节点：${counts.endpoints ?? 0}`,
            `服务：${counts.services ?? 0}`,
            `受管端口：${counts.managed_port_entries ?? 0}`,
            `服务器集合：${counts.fleet_servers ?? 0}`,
          ].join('\n')
          if (!window.confirm(`备份校验通过，将恢复以下数据：\n\n${summary}\n\n继续恢复吗？`)) return

          this.control.visible = false
          const uploadMsg = await HttpUtils.post('api/importdb', formData, {
            headers: {
              'Content-Type': 'multipart/form-data',
            },
          })

          if (uploadMsg.success) {
            await new Promise(resolve => setTimeout(resolve, 1000))
            location.reload()
          }
        } finally {
          this.restoring = false
        }
      })

      fileInput.click()
    },
  },
  watch: {
    visible(v) {
      if (v) {
        this.exclude = ['stats', 'changes']
      }
    },
  },
}
</script>

<style scoped lang="scss">
.backup-dialog__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.backup-dialog__body {
  display: grid;
  gap: 18px;
  padding: 18px;
}

.backup-dialog__options {
  display: grid;
  gap: 4px;
  padding: 10px 12px;
  border: 1px solid var(--np-border);
  border-radius: 18px;
  background: rgba(var(--v-theme-surface), 0.5);
}

.backup-dialog__actions {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.backup-dialog__actions .v-btn {
  min-height: 44px;
}

.backup-dialog__config {
  grid-column: 1 / -1;
}

@media (max-width: 420px) {
  .backup-dialog__body {
    padding: 14px;
  }

  .backup-dialog__actions {
    grid-template-columns: 1fr;
  }

  .backup-dialog__config {
    grid-column: auto;
  }
}
</style>
