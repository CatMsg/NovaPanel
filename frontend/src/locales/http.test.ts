import { describe, expect, it } from 'vitest'
import en from './en'
import fa from './fa'
import ru from './ru'
import vi from './vi'
import zhcn from './zhcn'
import zhtw from './zhtw'

const httpLocales = { en, fa, ru, vi, zhHans: zhcn, zhHant: zhtw }

describe('HTTP inbound translations', () => {
  it('provides version validation and client compatibility guidance in every locale', () => {
    for (const [locale, messages] of Object.entries(httpLocales)) {
      const http = (messages as any).types.http
      expect(http.errors.singleVersionRequired, `${locale}.types.http.errors.singleVersionRequired`).toBeTruthy()
      expect(http.clientVersionInherited, `${locale}.types.http.clientVersionInherited`).toBeTruthy()
      expect(http.clientCompatibilityHint, `${locale}.types.http.clientCompatibilityHint`).toContain('HTTP/1.1')
      expect(http.clientCompatibilityHint, `${locale}.types.http.clientCompatibilityHint`).toContain('sing-box 1.15')
      expect(http.clientCompatibilityHint, `${locale}.types.http.clientCompatibilityHint`).not.toContain('legacyURI')
    }
  })
})
