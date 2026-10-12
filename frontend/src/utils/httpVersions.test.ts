import { describe, expect, it } from 'vitest'
import {
  effectiveInboundHttpVersions,
  httpInboundIssue,
  httpOutboundIssue,
  normalizeHttpVersions,
} from './httpVersions'

describe('normalizeHttpVersions', () => {
  it('accepts listable scalar and array values without mutating the input', () => {
    expect(normalizeHttpVersions(3)).toEqual([3])
    expect(normalizeHttpVersions([1, 2, 2, 3])).toEqual([1, 2, 3])
    expect(normalizeHttpVersions([1, '3', 1.5])).toEqual([1, '3', 1.5])
    expect(normalizeHttpVersions(undefined)).toEqual([1, 2])
    expect(normalizeHttpVersions([])).toEqual([1, 2])
    expect(effectiveInboundHttpVersions({ version: 1 })).toEqual([1])
  })
})

describe('HTTP version guards', () => {
  it('requires enabled outbound TLS for HTTP/3', () => {
    expect(httpOutboundIssue({ version: 3, tls: { enabled: false } })).toBe('tlsRequired')
    expect(httpOutboundIssue({ version: 3, tls: { enabled: true } })).toBeUndefined()
    expect(httpOutboundIssue({ version: 2, disable_version_fallback: true, tls: { enabled: false } })).toBe('outboundTlsRequired')
    expect(httpOutboundIssue({ version: 2, disable_version_fallback: true, tls: { enabled: true } })).toBeUndefined()
  })

  it('allows path and Host with auto or HTTP/1 but rejects explicit HTTP/2 and HTTP/3', () => {
    expect(httpOutboundIssue({ version: 0, path: '/proxy' })).toBeUndefined()
    expect(httpOutboundIssue({ version: 1, headers: { host: 'example.test' } })).toBeUndefined()
    expect(httpOutboundIssue({ version: 2, headers: { HOST: 'example.test' } })).toBe('pathHostRequiresHttp1')
    expect(httpOutboundIssue({ version: 3, path: '/proxy', tls: { enabled: true } })).toBe('pathHostRequiresHttp1')
    expect(httpOutboundIssue({ version: 1, path: '/proxy', headers: { Host: 'example.test' } })).toBe('pathAndHostTogether')
    expect(httpOutboundIssue({ version: 0, path: '/proxy', headers: { Host: 'example.test' } })).toBe('pathAndHostTogether')
  })

  it('treats only a nonempty effective first Host value as an override', () => {
    for (const value of ['', [], [''], ['', 'ignored.example']]) {
      expect(httpOutboundIssue({ version: 0, path: '/proxy', headers: { Host: value } })).toBeUndefined()
      expect(httpOutboundIssue({ version: 2, headers: { Host: value } })).toBeUndefined()
    }
    expect(httpOutboundIssue({ version: 1, path: '/proxy', headers: { Host: ['example.test', 'ignored.example'] } })).toBe('pathAndHostTogether')
    expect(httpOutboundIssue({ version: 2, headers: { Host: ['example.test', 'ignored.example'] } })).toBe('pathHostRequiresHttp1')
  })

  it('requires an active selected inbound TLS config and a client version served by the inbound', () => {
    const tlsConfigs = [
      { id: 7, server: { enabled: false } },
      { id: 8, server: { enabled: true } },
      { id: 9, server: {} },
    ]
    expect(httpInboundIssue({ version: [1, 2, 3], tls_id: 7 }, tlsConfigs)).toBe('tlsRequired')
    expect(httpInboundIssue({ version: [1, 2, 3], tls_id: 9 }, tlsConfigs)).toBe('tlsRequired')
    expect(httpInboundIssue({ version: [1, 2, 3], tls_id: 8, out_json: { version: 3 } }, tlsConfigs)).toBeUndefined()
    expect(httpInboundIssue({ version: [1, 2], tls_id: 0, out_json: { version: 3 } }, tlsConfigs)).toBe('clientVersionMismatch')
    expect(httpInboundIssue({ version: 2, tls_id: 0 }, tlsConfigs)).toBe('http2OnlyRequiresTls')
    expect(httpInboundIssue({ version: [1, 2], tls_id: 0 }, tlsConfigs)).toBeUndefined()
    expect(httpInboundIssue({ version: [1, '3'], tls_id: 8 }, tlsConfigs)).toBe('invalidVersion')
    expect(httpInboundIssue({ version: 1.5, tls_id: 8 }, tlsConfigs)).toBe('invalidVersion')
  })
})
