import { createI18n } from 'vue-i18n'
import en from './en'
import fa from './fa'
import vi from './vi'
import zhcn from './zhcn'
import zhtw from './zhtw'
import ru from './ru'
import { uiMessages } from './ui'

export const i18n = createI18n({
  legacy: false,
  locale: localStorage.getItem("locale") ?? 'zhHans',
  fallbackLocale: 'zhHans',
  messages: {
    en: { ...en, ui: uiMessages.en },
    fa: { ...fa, ui: uiMessages.fa },
    vi: { ...vi, ui: uiMessages.vi },
    zhHans: { ...zhcn, ui: uiMessages.zhHans },
    zhHant: { ...zhtw, ui: uiMessages.zhHant },
    ru: { ...ru, ui: uiMessages.ru }
  },
})

export const locale = (() => {
  const l = i18n.global.locale.value
  switch (l) {
    case "zhHans":
      return "zh-cn"
    case "zhHant":
      return "zh-tw"
    default:
      return l
  }
})()

export const languages = [
  { title: 'English', value: 'en' },
  { title: 'فارسی', value: 'fa' },
  { title: 'Tiếng Việt', value: 'vi' },
  { title: '简体中文', value: 'zhHans' },
  { title: '繁體中文', value: 'zhHant' },
  { title: 'Русский', value: 'ru' },
]
