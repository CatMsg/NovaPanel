import fs from 'node:fs'
import { describe, expect, it } from 'vitest'
import { uiMessages } from './ui'

const requiredFleetKeys = [
  'trafficHistory',
  'trafficSamples',
  'trafficHistoryEmpty',
  'trafficCurrentPeriod',
  'trafficPeriodRange',
  'trafficBilled',
  'trafficHistoryUnavailable',
] as const

const technicalSame = new Set([
  'fleet.apiToken', 'fleet.canary', 'fleet.panel', 'fleet.rolloutMarker', 'fleet.runningAction',
  'health.endpoint', 'health.inbound', 'health.panel', 'health.telegramChatId', 'health.telegramToken',
])

describe('traffic UI translations', () => {
  it('provides the new traffic keys for every supported UI locale', () => {
    for (const [locale, messages] of Object.entries(uiMessages)) {
      for (const key of requiredFleetKeys) {
        expect(messages.fleet[key], `${locale}.fleet.${key}`).toBeTruthy()
      }
      expect(messages.health.telegramToken, `${locale}.health.telegramToken`).toBeTruthy()
      expect(messages.health.telegramChatId, `${locale}.health.telegramChatId`).toBeTruthy()
    }
  })

  it('does not silently fall back to English on the localized operations pages', () => {
    const targets = [
      ['fleet', 'src/views/Fleet.vue'],
      ['health', 'src/views/Health.vue'],
      ['settings', 'src/views/Settings.vue'],
      ['sessions', 'src/views/Sessions.vue'],
    ] as const

    for (const [section, file] of targets) {
      const source = fs.readFileSync(file, 'utf8')
      const pattern = new RegExp(`ui\\.${section}\\.([A-Za-z0-9_]+)`, 'g')
      const keys = [...new Set([...source.matchAll(pattern)].map(match => match[1]))]
      for (const locale of ['ru', 'vi', 'fa'] as const) {
        for (const key of keys) {
          const localized = (uiMessages[locale] as any)[section]?.[key]
          const english = (uiMessages.en as any)[section]?.[key]
          if (typeof localized !== 'string' || typeof english !== 'string') continue
          if (technicalSame.has(`${section}.${key}`)) continue
          expect(localized, `${locale}.${section}.${key}`).not.toBe(english)
        }
      }
    }
  })
})
