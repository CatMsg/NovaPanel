<template>
  <v-container fluid class="login-shell">
    <div class="login-shell__glow login-shell__glow--one"></div>
    <div class="login-shell__glow login-shell__glow--two"></div>
    <v-row class="login-shell__content" align="center" justify="center">
      <v-col cols="12" lg="5" class="login-shell__left">
        <div class="login-brand">
          <v-avatar size="72" class="login-brand__avatar">
            <v-img src="@/assets/logo.svg" alt="NovaPanel logo"></v-img>
          </v-avatar>
          <div>
            <div class="login-brand__eyebrow">NovaPanel</div>
            <h1 class="login-brand__title">{{ $t('main.hero.title') }}</h1>
          </div>
        </div>
        <p class="login-brand__subtitle">{{ $t('main.hero.subtitle') }}</p>
        <div class="login-brand__tags">
          <v-chip class="login-brand__tag" color="primary" variant="flat">{{ $t('main.hero.live') }}</v-chip>
          <v-chip class="login-brand__tag" color="teal" variant="flat">{{ $t('main.info.firewall') }}</v-chip>
          <v-chip class="login-brand__tag" color="blue" variant="flat">{{ $t('version') }} v{{ version }}</v-chip>
        </div>
      </v-col>
      <v-col cols="12" sm="10" md="8" lg="4" class="login-shell__right">
        <v-card class="login-card" rounded="xl" variant="flat">
          <v-card-title class="login-card__title" v-text="$t('login.title')"></v-card-title>
          <v-card-text>
            <v-form @submit.prevent="login" ref="form" class="login-form">
              <v-text-field v-model="username" :label="$t('login.username')" :rules="usernameRules" required></v-text-field>
              <v-text-field v-model="password" :label="$t('login.password')" :rules="passwordRules" type="password" required></v-text-field>
              <v-btn :loading="loading" type="submit" color="primary" block class="login-form__submit" v-text="$t('actions.submit')"></v-btn>
            </v-form>
            <div class="login-actions">
              <v-select
                density="compact"
                hide-details
                variant="solo-filled"
                :items="languages"
                v-model="$i18n.locale"
                @update:modelValue="changeLocale">
                <template v-slot:append>
                  <v-menu>
                    <template v-slot:activator="{ props }">
                      <v-btn icon v-bind="props" variant="text">
                        <v-icon>mdi-theme-light-dark</v-icon>
                      </v-btn>
                    </template>
                    <v-list>
                      <v-list-item
                        v-for="th in themes"
                        :key="th.value"
                        @click="changeTheme(th.value)"
                        :prepend-icon="th.icon"
                        :active="isActiveTheme(th.value)"
                      >
                        <v-list-item-title>{{ $t(`theme.${th.value}`) }}</v-list-item-title>
                      </v-list-item>
                    </v-list>
                  </v-menu>
                </template>
              </v-select>
            </div>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>
  
<script lang="ts" setup>
import { ref } from "vue"
import { useLocale,useTheme } from 'vuetify'
import { i18n, languages } from '@/locales'
import { useRouter } from 'vue-router'
import HttpUtil from '@/plugins/httputil'
import pkg from '../../package.json'


const theme = useTheme()
const locale = useLocale()

const themes = [
  { value: 'light', icon: 'mdi-white-balance-sunny' },
  { value: 'dark', icon: 'mdi-moon-waning-crescent' },
  { value: 'system', icon: 'mdi-laptop' },
]

const username = ref('')
const usernameRules = [
  (value: string) => {
    if (value?.length > 0) return true
    return i18n.global.t('login.unRules')
  },
]

const password = ref('')
const passwordRules = [
  (value: string) => {
    if (value?.length > 0) return true
    return i18n.global.t('login.pwRules')
  },
]

const loading = ref(false)
const router = useRouter()
const version = pkg.version

const login = async () => {
  if (username.value == '' || password.value == '') return
  loading.value=true
  const response = await HttpUtil.post('api/login',{user: username.value, pass: password.value})
  if(response.success){
    setTimeout(() => {
      loading.value=false
      router.push('/')
    }, 500)
  } else {
    loading.value=false
  }
}
const changeLocale = (l: any) => {
  locale.current.value = l ?? 'zhHans'
  localStorage.setItem('locale', locale.current.value)
}
const changeTheme = (th: string) => {
  theme.change(th)
  localStorage.setItem('theme', th)
}
const isActiveTheme = (th: string) => {
  const current = localStorage.getItem('theme') ?? 'system'
  return current == th
}
</script>

<style scoped lang="scss">
.login-shell {
  position: relative;
  min-height: calc(100vh - 72px);
  padding: 32px 24px 40px;
  overflow: hidden;
}

.login-shell__content {
  position: relative;
  z-index: 1;
  width: min(1280px, 100%);
  margin: 0 auto;
  row-gap: 28px;
}

.login-shell__glow {
  position: absolute;
  border-radius: 999px;
  filter: blur(18px);
  pointer-events: none;
}

.login-shell__glow--one {
  top: -80px;
  left: -40px;
  width: 260px;
  height: 260px;
  background: rgba(37, 99, 235, 0.16);
}

.login-shell__glow--two {
  right: -120px;
  bottom: 10%;
  width: 320px;
  height: 320px;
  background: rgba(14, 165, 233, 0.12);
}

.login-shell__left {
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding-inline-end: 16px;
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 16px;
}

.login-brand__avatar {
  border: 1px solid rgba(37, 99, 235, 0.18);
  background: rgba(255, 255, 255, 0.76);
  box-shadow: 0 18px 40px rgba(15, 23, 42, 0.1);
}

.login-brand__eyebrow {
  font-size: 0.82rem;
  font-weight: 800;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: rgba(37, 99, 235, 0.9);
}

.login-brand__title {
  margin: 6px 0 0;
  font-size: clamp(2rem, 4vw, 3.4rem);
  line-height: 1.05;
}

.login-brand__subtitle {
  max-width: 60ch;
  margin: 0;
  color: rgba(71, 85, 105, 0.82);
  font-size: 1rem;
  line-height: 1.85;
}

.login-brand__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.login-card {
  padding: 6px;
}

.login-card__title {
  font-size: 1.15rem;
  font-weight: 800;
}

.login-form {
  display: grid;
  gap: 14px;
}

.login-form__submit {
  min-height: 48px;
  border-radius: 16px;
  text-transform: none;
}

.login-actions {
  margin-top: 16px;
}

:global(.v-theme--dark) .login-brand__subtitle {
  color: rgba(191, 205, 224, 0.8);
}

:global(.v-theme--dark) .login-brand__avatar {
  background: rgba(10, 18, 34, 0.9);
}

@media (max-width: 960px) {
  .login-shell {
    padding: 16px 12px 24px;
  }

  .login-shell__left {
    padding-inline-end: 0;
  }
}
</style>
