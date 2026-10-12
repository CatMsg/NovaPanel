import { describe, expect, it } from 'vitest'
import { createInbound, type HTTP } from '@/types/inbounds'
import {
  effectiveInboundHttpVersions,
  httpInboundIssue,
  httpOutboundIssue,
  normalizeHttpVersions,
  requiresHttpVersionSelection,
  selectedInboundHttpVersion,
  synchronizeHttpInboundClientVersion,
} from './httpVersions'

describe('HTTP inbound version selection', () => {
  it('defaults new HTTP inbounds to a scalar HTTP/1.1 version', () => {
    expect((createInbound('http') as HTTP).version).toBe(1)
  })

  it('reads valid scalars and singleton arrays without normalizing legacy data', () => {
    expect(normalizeHttpVersions(3)).toEqual([3])
    expect(normalizeHttpVersions([1, 2, 2, 3])).toEqual([1, 2, 2, 3])
    expect(normalizeHttpVersions([1, '3', 1.5])).toEqual([1, '3', 1.5])
    expect(normalizeHttpVersions(undefined)).toEqual([])
    expect(normalizeHttpVersions([])).toEqual([])
    expect(effectiveInboundHttpVersions({ version: 1 })).toEqual([1])
    expect(selectedInboundHttpVersion(1)).toBe(1)
    expect(selectedInboundHttpVersion([2])).toBe(2)
    expect(selectedInboundHttpVersion(undefined)).toBeUndefined()
    expect(selectedInboundHttpVersion([1, 2])).toBeUndefined()
    expect(selectedInboundHttpVersion('3')).toBeUndefined()
    expect(requiresHttpVersionSelection(undefined)).toBe(true)
    expect(requiresHttpVersionSelection([1, 2])).toBe(true)
    expect(requiresHttpVersionSelection([1])).toBe(false)
  })

  it('synchronizes the client only for a valid single version and manages fallback flags', () => {
    const http1 = { version: 1, out_json: { version: 3, disable_version_fallback: true, path: '/proxy' } }
    expect(synchronizeHttpInboundClientVersion(http1)).toBe(true)
    expect(http1.out_json).toEqual({ version: 1, path: '/proxy' })

    const http2 = { version: [2], out_json: { version: 1 } }
    expect(synchronizeHttpInboundClientVersion(http2)).toBe(true)
    expect(http2.out_json).toEqual({ version: 2, disable_version_fallback: true })

    const http3 = { version: 3, out_json: { disable_version_fallback: true } }
    expect(synchronizeHttpInboundClientVersion(http3)).toBe(true)
    expect(http3.out_json).toEqual({ version: 3 })

    const legacy = { version: [1, 2], out_json: { version: 3, disable_version_fallback: true } }
    const original = structuredClone(legacy)
    expect(synchronizeHttpInboundClientVersion(legacy)).toBe(false)
    expect(legacy).toEqual(original)
  })
})

describe('HTTP version guards', () => {
  it('keeps standalone outbound TLS and path/Host validation independent', () => {
    expect(httpOutboundIssue({ version: 3, tls: { enabled: false } })).toBe('tlsRequired')
    expect(httpOutboundIssue({ version: 3, tls: { enabled: true } })).toBeUndefined()
    expect(httpOutboundIssue({ version: 2, disable_version_fallback: true, tls: { enabled: false } })).toBe('outboundTlsRequired')
    expect(httpOutboundIssue({ version: 2, disable_version_fallback: true, tls: { enabled: true } })).toBeUndefined()
    expect(httpOutboundIssue({ version: 0, path: '/proxy' })).toBeUndefined()
    expect(httpOutboundIssue({ version: 1, headers: { host: 'example.test' } })).toBeUndefined()
    expect(httpOutboundIssue({ version: 2, headers: { HOST: 'example.test' } })).toBe('pathHostRequiresHttp1')
    expect(httpOutboundIssue({ version: 3, path: '/proxy', tls: { enabled: true } })).toBe('pathHostRequiresHttp1')
    expect(httpOutboundIssue({ version: 1, path: '/proxy', headers: { Host: 'example.test' } })).toBe('pathAndHostTogether')
    expect(httpOutboundIssue({ version: 0, path: '/proxy', headers: { Host: 'example.test' } })).toBe('pathAndHostTogether')
  })

  it('uses the inherited inbound version for Path/Host checks and ignores stale client versions', () => {
    expect(httpInboundIssue({
      version: 1,
      out_json: { version: 3, path: '/proxy' },
    }, [])).toBeUndefined()
    expect(httpInboundIssue({
      version: 1,
      out_json: { version: 3, path: '/proxy', headers: { Host: 'example.test' } },
    }, [])).toBe('pathAndHostTogether')
    expect(httpInboundIssue({
      version: 2,
      tls_id: 8,
      out_json: { version: 1, path: '/proxy' },
    }, [{ id: 8, server: { enabled: true } }])).toBe('pathHostRequiresHttp1')
    expect(httpInboundIssue({
      version: 3,
      tls_id: 8,
      out_json: { version: 1, headers: { Host: 'example.test' } },
    }, [{ id: 8, server: { enabled: true } }])).toBe('pathHostRequiresHttp1')
  })

  it('requires one valid inbound version and active TLS for HTTP/2 and HTTP/3', () => {
    const tlsConfigs = [
      { id: 7, server: { enabled: false } },
      { id: 8, server: { enabled: true } },
      { id: 9, server: {} },
    ]
    expect(httpInboundIssue({ version: 1, tls_id: 0 }, tlsConfigs)).toBeUndefined()
    expect(httpInboundIssue({ version: 2, tls_id: 0 }, tlsConfigs)).toBe('http2OnlyRequiresTls')
    expect(httpInboundIssue({ version: 2, tls_id: 8 }, tlsConfigs)).toBeUndefined()
    expect(httpInboundIssue({ version: 3, tls_id: 0 }, tlsConfigs)).toBe('tlsRequired')
    expect(httpInboundIssue({ version: 3, tls_id: 8 }, tlsConfigs)).toBeUndefined()
    expect(httpInboundIssue({ version: undefined, tls_id: 0 }, tlsConfigs)).toBe('singleVersionRequired')
    expect(httpInboundIssue({ version: [1, 2], tls_id: 8 }, tlsConfigs)).toBe('singleVersionRequired')
    expect(httpInboundIssue({ version: [1, '3'], tls_id: 8 }, tlsConfigs)).toBe('invalidVersion')
    expect(httpInboundIssue({ version: 1.5, tls_id: 8 }, tlsConfigs)).toBe('invalidVersion')
  })
})
