<template>
  <v-dialog transition="dialog-bottom-transition" width="90%" max-width="500">
    <v-card class="rounded-lg backup-dialog">
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
          <v-btn color="primary" variant="tonal" prepend-icon="mdi-download" @click="backup()">{{ $t('main.backup.backup') }}</v-btn>
          <v-btn color="primary" variant="outlined" prepend-icon="mdi-backup-restore" @click="restore()">{{ $t('main.backup.restore') }}</v-btn>
          <v-btn class="backup-dialog__config" color="primary" variant="text" prepend-icon="mdi-file-code" @click="config()">{{ $t('main.backup.sbConfig') }}</v-btn>
        </div>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import HttpUtils from '@/plugins/httputil'
export default {
  props: ['control', 'visible'],
  data() {
    return {
      exclude: ["stats", "changes"],
    }
  },
  methods: {
    backup() {
      const excludeOption = this.exclude.length>0 ? '?exclude=' +this.exclude.join(',') : ''
      window.location.href = 'api/getdb' + excludeOption
    },
    config() {
      window.location.href = 'api/singbox-config'
    },
      async restore() {
      const fileInput = document.createElement('input')
      fileInput.type = 'file'
      fileInput.accept = '.db'

      fileInput.addEventListener('change', async (event: Event) => {
        const inputElement = event.target as HTMLInputElement
        const dbFile = inputElement.files ? inputElement.files[0] : null

        if (dbFile) {
          const formData = new FormData()
          formData.append('db', dbFile)

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
        }
    })

    fileInput.click()
    }
  },
  watch: {
    visible(v) {
      if (v) {
        this.exclude = ["stats", "changes"]
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
