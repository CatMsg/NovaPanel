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
      <v-card class="np-resource-card admin-card" rounded="xl" variant="flat">
        <div class="admin-card__head">
          <div class="admin-card__avatar"><v-icon icon="mdi-shield-account-outline" /></div>
          <div class="admin-card__identity">
            <strong>{{ item.username }}</strong>
            <span>{{ $t('pages.admins') }}</span>
          </div>
          <v-chip size="x-small" color="success" variant="tonal">{{ $t('enable') }}</v-chip>
        </div>
        <div class="admin-card__login">
          <span>{{ $t('admin.lastLogin') }}</span>
          <strong>{{ item.loginDate }} <small>{{ item.loginTime }}</small></strong>
        </div>
        <div class="admin-card__meta">
          <span>IP</span>
          <strong>{{ item.ip }}</strong>
        </div>
        <v-card-actions class="admin-card__actions">
          <v-btn color="primary" variant="tonal" @click="showEditModal(item)">
            <v-icon icon="mdi-account-edit" start />{{ $t('actions.edit') }}
          </v-btn>
          <v-btn variant="outlined" @click="showChangesModal(item.username)">
            <v-icon icon="mdi-list-box-outline" start />{{ $t('admin.changes') }}
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
.admins-grid {
  margin-top: 0;
}

.admin-card {
  min-height: 100%;
  overflow: hidden;
  padding: 16px;
}

.admin-card__head {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 11px;
}

.admin-card__avatar {
  display: grid;
  width: 42px;
  height: 42px;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid rgba(10, 132, 255, 0.16);
  border-radius: 14px;
  color: var(--np-accent);
  background: linear-gradient(145deg, rgba(125, 211, 252, 0.18), rgba(59, 130, 246, 0.08));
}

.admin-card__identity {
  display: flex;
  min-width: 0;
  flex: 1 1 auto;
  flex-direction: column;
}

.admin-card__identity strong {
  overflow: hidden;
  color: var(--np-text-main);
  font-size: 1rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.admin-card__identity span,
.admin-card__login > span,
.admin-card__meta > span {
  color: var(--np-text-muted);
  font-size: 0.72rem;
}

.admin-card__login {
  display: grid;
  gap: 5px;
  margin-top: 16px;
  padding: 13px 14px;
  border: 1px solid var(--np-border);
  border-radius: 15px;
  background: var(--np-surface-muted);
}

.admin-card__login strong {
  color: var(--np-text-main);
  font-size: 0.88rem;
}

.admin-card__login small {
  margin-left: 6px;
  color: var(--np-text-muted);
  font-weight: 600;
}

.admin-card__meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 3px 4px;
}

.admin-card__meta strong {
  min-width: 0;
  overflow: hidden;
  color: var(--np-text-main);
  font-size: 0.8rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.admin-card__actions {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  padding: 14px 0 0 !important;
}

.admin-card__actions .v-btn {
  min-width: 0;
}

@media (max-width: 599px) {
  .admin-card {
    padding: 14px;
  }
}
</style>
