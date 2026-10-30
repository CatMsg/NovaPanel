<template>
  <v-container fluid class="login-page">
    <v-card class="login-card" rounded="xl" variant="flat">
      <header class="login-brand">
        <v-avatar size="48" class="login-brand__avatar">
          <v-img :src="logoUrl" alt="NovaPanel logo"></v-img>
        </v-avatar>
        <span>NovaPanel</span>
      </header>

      <v-form @submit.prevent="login" ref="form" class="login-form">
        <v-text-field
          v-model="username"
          :label="$t('login.username')"
          :rules="usernameRules"
          autocomplete="username"
          required
        />
        <v-text-field
          v-model="password"
          :label="$t('login.password')"
          :rules="passwordRules"
          type="password"
          autocomplete="current-password"
          required
        />
        <v-btn
          :loading="loading"
          type="submit"
          color="primary"
          block
          class="login-submit"
          v-text="$t('actions.submit')"
        />
      </v-form>

      <footer class="login-options">
        <v-select
          class="login-language"
          density="compact"
          hide-details
          variant="outlined"
          :items="languages"
          v-model="$i18n.locale"
          @update:modelValue="changeLocale"
        />
        <v-menu>
          <template #activator="{ props }">
            <v-btn
              icon
              v-bind="props"
              variant="text"
              :aria-label="$t('menu.theme')"
            >
              <v-icon>mdi-theme-light-dark</v-icon>
            </v-btn>
          </template>
          <v-list>
            <v-list-item
              v-for="th in themes"
              :key="th.value"
              :prepend-icon="th.icon"
              :active="isActiveTheme(th.value)"
              @click="changeTheme(th.value)"
            >
              <v-list-item-title>{{ $t(`theme.${th.value}`) }}</v-list-item-title>
            </v-list-item>
          </v-list>
        </v-menu>
      </footer>
    </v-card>
  </v-container>
</template>

<script lang="ts" setup>
import { ref } from 'vue'
import { useLocale, useTheme } from 'vuetify'
import { i18n, languages } from '@/locales'
import { useRouter } from 'vue-router'
import HttpUtil from '@/plugins/httputil'
import logoUrl from '@/assets/logo.png'

const theme = useTheme()
const locale = useLocale()
const router = useRouter()

const themes = [
  { value: 'light', icon: 'mdi-white-balance-sunny' },
  { value: 'dark', icon: 'mdi-moon-waning-crescent' },
  { value: 'system', icon: 'mdi-laptop' },
]

const username = ref('')
const usernameRules = [
  (value: string) => value?.length > 0 || i18n.global.t('login.unRules'),
]

const password = ref('')
const passwordRules = [
  (value: string) => value?.length > 0 || i18n.global.t('login.pwRules'),
]

const loading = ref(false)

const login = async () => {
  if (username.value === '' || password.value === '') return
  loading.value = true
  const response = await HttpUtil.post('api/login', { user: username.value, pass: password.value })
  if (response.success) {
    setTimeout(() => {
      loading.value = false
      router.push('/')
    }, 500)
  } else {
    loading.value = false
  }
}

const changeLocale = (value: string | null) => {
  locale.current.value = value ?? 'zhHans'
  localStorage.setItem('locale', locale.current.value)
}

const changeTheme = (value: string) => {
  theme.change(value)
  localStorage.setItem('theme', value)
}

const isActiveTheme = (value: string) => (localStorage.getItem('theme') ?? 'system') === value
</script>

<style scoped lang="scss">
.login-page {
  display: grid;
  min-height: calc(100vh - 72px);
  place-items: center;
  padding: 24px 16px;
}

.login-card {
  width: min(100%, 400px);
  padding: 28px;
  border: 1px solid rgba(125, 211, 252, 0.14);
  background: var(--np-surface-strong);
  box-shadow: 0 20px 60px rgba(15, 23, 42, 0.1);
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 1.1rem;
  font-weight: 750;
}

.login-brand__avatar {
  border: 1px solid rgba(125, 211, 252, 0.18);
}

.login-form {
  margin-top: 24px;
  display: grid;
  gap: 8px;
}

.login-submit {
  min-height: 46px;
  margin-top: 4px;
  border-radius: 12px;
  text-transform: none;
}

.login-options {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 20px;
}

.login-language {
  width: 148px;
}

@media (max-width: 480px) {
  .login-card {
    padding: 22px;
  }
}
</style>
