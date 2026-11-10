import { describe, expect, it } from 'vitest'
import en from './en'
import fa from './fa'
import ru from './ru'
import vi from './vi'
import zhcn from './zhcn'
import zhtw from './zhtw'

const locales = { en, fa, ru, vi, zhHans: zhcn, zhHant: zhtw }
const requiredSnellKeys = [
  'modeHint',
  'modeDefault',
  'modeDefaultDescription',
  'modeUnshaped',
  'modeUnshapedDescription',
  'modeUnsafeRaw',
  'modeUnsafeRawDescription',
  'modeUnsafeWarning',
] as const

describe('Snell v6 mode translations', () => {
  it('describes all modes and the unsafe-raw warning in every locale', () => {
    for (const [locale, messages] of Object.entries(locales)) {
      const snell = (messages as any).types.snell
      for (const key of requiredSnellKeys) {
        expect(snell[key], `${locale}.types.snell.${key}`).toBeTruthy()
      }
    }
  })
})
