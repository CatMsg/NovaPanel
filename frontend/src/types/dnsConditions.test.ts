import { describe, expect, it } from 'vitest'
import {
  dnsConditionTagStatus,
  dnsConditionServerOptions,
  listableFromText,
  listableText,
  selectableDnsConditionServers,
} from './dnsConditions'

describe('DNS tag condition types', () => {
  const servers = [
    { type: 'local', tag: 'system' },
    { type: 'dhcp', tag: 'dhcp' },
    { type: 'udp', tag: 'static-udp' },
    { type: 'local', tag: 'duplicate' },
    { type: 'tls', tag: 'duplicate' },
  ]

  it('offers only uniquely tagged server types supported by alpha12', () => {
    expect(selectableDnsConditionServers(servers)).toEqual([
      { tag: 'system', type: 'local' },
      { tag: 'dhcp', type: 'dhcp' },
    ])
    expect(dnsConditionTagStatus('static-udp', servers)).toBe('unsupported')
    expect(dnsConditionTagStatus('missing', servers)).toBe('unknown')
    expect(dnsConditionTagStatus('duplicate', servers)).toBe('duplicate')
  })

  it('offers the implicit local transport only when no DNS servers or explicit final exist', () => {
    expect(dnsConditionServerOptions([], undefined)).toEqual([{ tag: 'local', type: 'local' }])
    expect(dnsConditionServerOptions(undefined, '')).toEqual([{ tag: 'local', type: 'local' }])
    expect(dnsConditionServerOptions([], 'dns-explicit')).toEqual([])
    expect(dnsConditionServerOptions([{}], undefined)).toEqual([{}])
  })

  it('formats and parses listable values while retaining scalar or array form when edited', () => {
    expect(listableText('192.0.2.1')).toBe('192.0.2.1')
    expect(listableText(['192.0.2.1', '192.0.2.2'])).toBe('192.0.2.1\n192.0.2.2')
    expect(listableFromText('192.0.2.1', '192.0.2.1')).toBe('192.0.2.1')
    expect(listableFromText('192.0.2.1\n192.0.2.2', '192.0.2.1')).toEqual(['192.0.2.1', '192.0.2.2'])
    expect(listableFromText('corp.test\nexample.test', ['old.test'])).toEqual(['corp.test', 'example.test'])
  })
})
