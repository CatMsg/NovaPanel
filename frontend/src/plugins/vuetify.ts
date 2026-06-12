/**
 * plugins/vuetify.ts
 *
 * Framework documentation: https://vuetifyjs.com`
 */

// Styles
import '@mdi/font/css/materialdesignicons.css'
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
          primary: '#2563eb',
          secondary: '#0f766e',
          accent: '#0891b2',
          info: '#0ea5e9',
          success: '#22c55e',
          warning: '#f59e0b',
          error: '#ef4444',
          background: '#edf4ff',
          surface: '#ffffff',
        },
      },
      dark: {
        colors: {
          primary: '#60a5fa',
          secondary: '#34d399',
          accent: '#22d3ee',
          info: '#38bdf8',
          success: '#4ade80',
          warning: '#fbbf24',
          error: '#f87171',
          background: '#07111f',
          surface: '#0f172a',
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
