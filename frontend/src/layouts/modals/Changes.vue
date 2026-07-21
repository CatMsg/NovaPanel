<template>
  <v-dialog transition="dialog-bottom-transition" width="90%" max-width="800" :loading="loading">
    <v-card class="rounded-lg changes-dialog">
      <v-card-title class="changes-dialog__title">
        <span>{{ $t('admin.changes') }}</span>
        <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="$emit('close')" />
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text class="changes-dialog__body">
        <v-row class="changes-dialog__filters">
          <v-col cols="12" sm="4" md="3">
            <v-select
            hide-details
            :label="$t('admin.actor')"
            :items="['', 'DepleteJob', ...admins]"
            v-model="user"
            @update:model-value="loadData">
            </v-select>
          </v-col>
          <v-col cols="12" sm="4" md="3">
            <v-select
            hide-details
            :label="$t('admin.key')"
            :items="['', 'inbounds', 'outbounds', 'clients', 'route', 'tls', 'experimental']"
            v-model="key"
            @update:model-value="loadData">
            </v-select>
          </v-col>
          <v-col cols="6" sm="4" md="3">
            <v-select
            hide-details
            :label="$t('count')"
            :items="[10,20,30,50,100]"
            v-model.number="chngCount"
            @update:model-value="loadData">
            </v-select>
          </v-col>
          <v-col cols="auto" align="center" justify="center">
            <v-btn
              icon="mdi-refresh"
              variant="tonal"
              :loading="loading"
              @click="loadData">
              <v-icon />
            </v-btn>
          </v-col>
        </v-row>
        <div v-if="smAndDown" class="changes-dialog__mobile-list">
          <article v-for="item in changes" :key="item.id" class="changes-dialog__mobile-item">
            <div class="changes-dialog__mobile-head">
              <strong>#{{ item.id }}</strong>
              <span dir="ltr">{{ dateFormatted(item.dateTime) }}</span>
            </div>
            <div class="changes-dialog__mobile-meta">
              <div>
                <span>{{ $t('admin.actor') }}</span>
                <strong>{{ item.Actor || '-' }}</strong>
              </div>
              <div>
                <span>{{ $t('admin.key') }}</span>
                <strong>{{ item.key || '-' }}</strong>
              </div>
              <div>
                <span>{{ $t('admin.action') }}</span>
                <strong>{{ $t('actions.' + item.action) }}</strong>
              </div>
            </div>
            <pre v-if="item.obj" class="changes-dialog__mobile-object" dir="ltr">{{ item.obj }}</pre>
          </article>
          <v-alert v-if="!loading && changes.length === 0" type="info" variant="tonal" :text="$t('noData')" />
        </div>
        <v-data-table
          v-else
          :headers="changesHeaders"
          :items="changes"
          item-value="id"
          density="compact"
          show-expand
          items-per-page="10"
        >
          <template v-slot:item.dateTime="{ value }">
            <v-chip variant="text" dir="ltr" density="compact">
              {{ dateFormatted(value) }}
            </v-chip>
          </template>
          <template v-slot:item.action="{ value }">
            <v-chip density="compact">
              {{ $t('actions.' + value) }}
            </v-chip>
          </template>
          <template v-slot:expanded-row="{ columns, item }">
            <tr>
              <td :colspan="columns.length">
                <v-card dir="ltr" v-if="item.index>0">Index: {{ item.index }}</v-card>
                <v-card style="background-color: background" dir="ltr"><pre>{{ item.obj }}</pre></v-card>
              </td>
            </tr>
          </template>
        </v-data-table>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { useDisplay } from 'vuetify'

export default {
  props: ['admins', 'actor', 'visible'],
  setup() {
    const { smAndDown } = useDisplay()
    return { smAndDown }
  },
  data() {
    return {
      loading: false,
      changes: <any[]>[],
      user: '',
      key: '',
      chngCount: 10,
      expanded: [],
      changesHeaders: [
        { title: 'ID', key: 'id' },
        { title: i18n.global.t('admin.date') + '-' + i18n.global.t('admin.time'), key: 'dateTime' },
        { title: i18n.global.t('admin.actor'), key: 'Actor' },
        { title: i18n.global.t('admin.key'), key: 'key' },
        { title: i18n.global.t('admin.action'), key: 'action' },
      ],
    }
  },
  methods: {
    async loadData() {
      this.loading = true
      const data = await HttpUtils.get('api/changes',{ a: this.user, k: this.key, c: this.chngCount })
      if (data.success) {
        this.changes = data.obj?? []
        this.loading = false
      }
    },
    dateFormatted(dt: number): string {
      const date = new Date(dt*1000)
      return date.toLocaleString(this.locale)
    },
  },
  computed: {
    locale() {
      const l = i18n.global.locale.value
      switch (l) {
        case "zhHans":
          return "zh-cn"
        case "zhHant":
          return "zh-tw"
        default:
          return l
      }
    },
  },
  watch: {
    visible(newValue) {
      this.changes = []
      this.user = this.$props.actor
      this.key = ''
      this.chngCount = 10
      if (newValue) {
        this.loadData()
      }
    },
  },
}
</script>

<style scoped lang="scss">
.changes-dialog {
  display: flex;
  flex-direction: column;
  max-height: min(90vh, 900px);
}

.changes-dialog__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.changes-dialog__body {
  min-height: 0;
  overflow-y: auto;
}

.changes-dialog__mobile-list {
  display: grid;
  gap: 10px;
  margin-top: 12px;
}

.changes-dialog__mobile-item {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--np-border);
  border-radius: 18px;
  background: rgba(var(--v-theme-surface), 0.46);
}

.changes-dialog__mobile-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--np-text-main);
}

.changes-dialog__mobile-head span {
  color: var(--np-text-muted);
  font-size: 0.78rem;
}

.changes-dialog__mobile-meta {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.changes-dialog__mobile-meta > div {
  min-width: 0;
  padding: 9px;
  border-radius: 12px;
  background: rgba(148, 163, 184, 0.1);
}

.changes-dialog__mobile-meta span,
.changes-dialog__mobile-meta strong {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.changes-dialog__mobile-meta span {
  color: var(--np-text-muted);
  font-size: 0.7rem;
}

.changes-dialog__mobile-meta strong {
  margin-top: 4px;
  color: var(--np-text-main);
  font-size: 0.8rem;
}

.changes-dialog__mobile-object {
  max-height: 112px;
  margin: 0;
  padding: 10px;
  overflow: auto;
  border-radius: 12px;
  color: var(--np-text-main);
  background: rgba(15, 23, 42, 0.08);
  font-size: 0.72rem;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

@media (max-width: 600px) {
  .changes-dialog {
    max-height: calc(100dvh - 16px);
  }

  .changes-dialog__body {
    padding: 12px 14px 16px;
  }
}
</style>
