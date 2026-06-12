<template>
  <v-dialog
    v-model="control.visible"
    transition="dialog-bottom-transition"
    width="90%"
    max-width="1200"
    :loading="loading"
  >
    <v-card class="rounded-lg">
      <v-card-title>
        <v-row>
          <v-col>
            {{ $t('basic.log.title') }}
            <span v-if="searchText"> - {{ searchText }}</span>
          </v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto">
            <v-icon icon="mdi-close" @click="control.visible = false" />
          </v-col>
        </v-row>
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-select
            hide-details
            :label="$t('basic.log.level')"
            :items="logLevels"
            v-model="logLevel"
            @update:model-value="loadData">
            </v-select>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-select
            hide-details
            :label="$t('count')"
            :items="[10,20,30,50,100,200,500,1000]"
            v-model.number="logCount"
            @update:model-value="loadData">
            </v-select>
          </v-col>
          <v-col cols="auto" align="center" justify="center">
            <v-btn
              icon="mdi-refresh"
              variant="tonal"
              :loading="loading"
              @click="loadData">
              <v-icon  />
            </v-btn>
          </v-col>
        </v-row>
        <v-row v-if="searchText" class="mb-2">
          <v-col cols="auto">
            <v-chip density="compact" color="primary" variant="tonal">
              {{ $t('client.history') }}
            </v-chip>
          </v-col>
        </v-row>
        <v-alert v-if="!loading && filteredLines.length === 0" type="warning" variant="outlined" :text="$t('noData')" />
        <v-card v-else style="background-color: background" dir="ltr" v-html="filteredLines.join('<br />')"></v-card>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import HttpUtils from '@/plugins/httputil'

export default {
  props: {
    control: {
      type: Object,
      required: true,
    },
    visible: {
      type: Boolean,
      default: false,
    },
    filterText: {
      type: String,
      default: '',
    },
    defaultCount: {
      type: Number,
      default: 10,
    },
  },
  data() {
    return {
      loading: false,
      lines: [],
      logLevel: 'info',
      logLevels: [
        { title: 'DEBUG', value: 'debug' },
        { title: 'INFO', value: 'info' },
        { title: 'WARNING', value: 'warning' },
        { title: 'ERROR', value: 'err' },
      ],
      logCount: this.defaultCount > 0 ? this.defaultCount : 10,
      searchText: this.filterText,
    }
  },
  computed: {
    filteredLines(): string[] {
      if (!this.searchText) {
        return this.lines
      }
      const keyword = this.searchText.toLowerCase()
      return this.lines.filter((line: string) => line.toLowerCase().includes(keyword))
    },
  },
  methods: {
    async loadData() {
      this.loading = true
      const data = await HttpUtils.get('api/logs',{ c: this.logCount, l: this.logLevel })
      if (data.success) {
        this.lines = data.obj?? []
      }
      this.loading = false
    }
  },
  watch: {
    visible(v) {
      this.lines = []
      this.logLevel = 'info'
      this.logCount = this.defaultCount > 0 ? this.defaultCount : 10
      this.searchText = this.filterText
      if (v) {
        this.loadData()
      }
    },
    filterText(v) {
      this.searchText = v
    },
  },
}
</script>
