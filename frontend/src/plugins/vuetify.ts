/**
 * plugins/vuetify.ts
 *
 * Framework documentation: https://vuetifyjs.com`
 */

// Styles
import '@/styles/materialdesignicons.css'
import 'vuetify/styles/main.css'

import { fa, en, vi, zhHans, zhHant, ru } from 'vuetify/locale'

// Composables
import { createVuetify } from 'vuetify'

// https://vuetifyjs.com/en/introduction/why-vuetify/#feature-guides
export default createVuetify({
  defaults: {
    VRow: { density: 'compact' },
    VTextField: {
      variant: 'solo-filled',
    },
    VSelect: {
      variant: 'solo-filled',
    },
    VCombobox: {
      variant: 'solo-filled',
    },
    VTextarea: {
      variant: 'solo-filled',
    },
  },
  theme: {
    defaultTheme: localStorage.getItem('theme') ?? 'system',
    themes: {
      light: {
        colors: {
          primary: '#1d4ed8',
          secondary: '#0f766e',
          accent: '#0891b2',
          info: '#0284c7',
          success: '#16a34a',
          warning: '#ca8a04',
          error: '#dc2626',
          background: '#eef5ff',
          surface: '#f8fbff',
        },
      },
      dark: {
        colors: {
          primary: '#7dd3fc',
          secondary: '#5eead4',
          accent: '#38bdf8',
          info: '#60a5fa',
          success: '#4ade80',
          warning: '#fbbf24',
          error: '#fb7185',
          background: '#050b15',
          surface: '#0b1324',
        },
      },
    },
  },
  locale: {
    locale: localStorage.getItem("locale") ?? 'zhHans',
    fallback: 'zhHans',
    messages: { en, fa, vi, zhHans, zhHant, ru },
  },
})
