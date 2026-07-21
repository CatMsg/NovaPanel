<template>
  <AdminModal 
    v-model="editModal.visible"
    :visible="editModal.visible"
    :user="editModal.user"
    @close="closeEditModal"
    @save="saveEditModal"
  />
  <ChangeModal 
    v-model="changesModal.visible"
    :visible="changesModal.visible"
    :admins="users.map((u:any) => u.username)"
    :actor="changesModal.actor"
    @close="closeChangesModal"
  />
  <TokenModal 
    v-model="tokenModal.visible"
    :visible="tokenModal.visible"
    @close="closeTokenModal"
  />
  <PageHero
    :eyebrow="$t('pages.admins')"
    :title="$t('pages.admins')"
    description="管理后台账号、查看操作记录并维护远程 API 访问令牌。"
    icon="mdi-shield-account-outline"
    :status="`${users.length} 个管理员`"
  >
    <template #meta>
      <span>管理员 {{ users.length }}</span><span>•</span><span>操作审计</span><span>•</span><span>API 令牌</span>
    </template>
    <template #actions>
      <v-btn color="primary" variant="tonal" @click="showChangesModal('')"><v-icon icon="mdi-history" start />{{ $t('admin.changes') }}</v-btn>
      <v-btn color="primary" variant="outlined" @click="showTokenModal()"><v-icon icon="mdi-key-outline" start />{{ $t('admin.api.token') }}</v-btn>
    </template>
  </PageHero>
  <v-row class="admins-grid">
    <v-col v-if="users.length === 0" cols="12">
      <EmptyState icon="mdi-account-alert-outline" title="暂无管理员数据" description="未读取到管理员账号，请刷新页面或检查数据库状态。" />
    </v-col>
    <v-col cols="12" sm="6" lg="4" v-for="item in <any[]>users" :key="item.id">
      <v-card class="np-resource-card admin-card" rounded="xl" variant="flat" :title="item.username">
        <v-card-subtitle style="margin-top: -15px;">
          {{ $t('admin.lastLogin') }}
        </v-card-subtitle>
        <v-card-text>
          <v-row>
            <v-col>{{ $t('admin.date') }}</v-col>
            <v-col>
              {{ item.loginDate }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>{{ $t('admin.time') }}</v-col>
            <v-col>
              {{ item.loginTime }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>IP</v-col>
            <v-col>
              {{ item.ip }}
            </v-col>
          </v-row>
        </v-card-text>
        <v-divider></v-divider>
        <v-card-actions style="padding: 0;">
          <v-btn icon="mdi-account-edit" @click="showEditModal(item)">
            <v-icon />
            <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
          <v-btn icon="mdi-list-box-outline" @click="showChangesModal(item.username)">
            <v-icon />
            <v-tooltip activator="parent" location="top" :text="$t('admin.changes')"></v-tooltip>
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { Ref, defineAsyncComponent, ref, inject, onMounted } from 'vue'
import PageHero from '@/components/PageHero.vue'
import EmptyState from '@/components/EmptyState.vue'

const AdminModal = defineAsyncComponent(() => import('@/layouts/modals/Admin.vue'))
const ChangeModal = defineAsyncComponent(() => import('@/layouts/modals/Changes.vue'))
const TokenModal = defineAsyncComponent(() => import('@/layouts/modals/Token.vue'))

const loading:Ref = inject('loading')?? ref(false)

const users = ref(<any[]>[])

onMounted(async () => {
  loading.value = true
  await loadData()
  loading.value = false
})

const loadData = async () => {
  loading.value = true
  const msg = await HttpUtils.get('api/users')
  loading.value = false
  if (msg.success) {
    msg.obj.forEach((u:any) => {
      const lastLogin = u.lastLogin.split(" ")
      const localLastLogin = lastLogin.length > 2 ? dateFormatted(Date.parse(lastLogin[0] + " " + lastLogin[1])) : "- -"
      const loginDateTime = localLastLogin.split(" ")
      users.value.push({
        id: u.id,
        username: u.username,
        loginDate: loginDateTime[0],
        loginTime: loginDateTime[1],
        ip: lastLogin[2]?? "-",
      })
    })
  }
}

const dateFormatted = (dt: number): string => {
  const locale = i18n.global.locale.value.replace('zh', 'zh-')
  const date = new Date(dt)
  return date.toLocaleString(locale)
}

const editModal = ref({
  visible: false,
  user: {},
})

const showEditModal = (user: any) => {
  editModal.value.user = user
  editModal.value.visible = true
}
const closeEditModal = () => {
  editModal.value.visible = false
  editModal.value.user = {}
}
const saveEditModal = async (data:any) => {
  loading.value=true
  const response = await HttpUtils.post('api/changePass',data)
  if(response.success){
    setTimeout(() => {
      loading.value=false
      editModal.value.visible = false
    }, 500)
  } else {
    loading.value=false
  }
}

const changesModal = ref({
  visible: false,
  actor: '',
})
const showChangesModal = (actor: string) => {
  changesModal.value.actor = actor
  changesModal.value.visible = true
}
const closeChangesModal = () => {
  changesModal.value.visible = false
  changesModal.value.actor = ''
}

const tokenModal = ref({
  visible: false,
})
const showTokenModal = () => {
  tokenModal.value.visible = true
}
const closeTokenModal = () => {
  tokenModal.value.visible = false
}
</script>

<style scoped>
.admins-grid { margin-top: 0; }
.admin-card { min-height: 100%; }
.admin-card :deep(.v-card-actions) { padding: 10px 12px; gap: 6px; }
</style>
