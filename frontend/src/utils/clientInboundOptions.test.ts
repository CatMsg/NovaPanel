import { describe, expect, it } from 'vitest'
import { getClientInboundOptions } from './clientInboundOptions'

describe('getClientInboundOptions', () => {
  it('keeps supported inbounds when users are empty, null, or supplied by an older API', () => {
    const options = getClientInboundOptions([
      { id: 1, type: 'vless', tag: 'empty-vless', users: [] },
      { id: 2, type: 'socks', tag: 'null-socks', users: null },
      { id: 3, type: 'shadowtls', tag: 'shadowtls-v3', version: 3, users: [] },
      { id: 4, type: 'shadowsocks', tag: 'regular-shadowsocks', managed: false, users: [] },
    ])

    expect(options).toEqual([
      { title: 'empty-vless', value: 1 },
      { title: 'null-socks', value: 2 },
      { title: 'shadowtls-v3', value: 3 },
      { title: 'regular-shadowsocks', value: 4 },
    ])
  })

  it('excludes unsupported protocols, managed shadowsocks, and ShadowTLS below v3', () => {
    expect(getClientInboundOptions([
      { id: 1, type: 'dns', tag: 'dns', users: [] },
      { id: 2, type: 'shadowsocks', tag: 'managed', managed: true, users: [] },
      { id: 3, type: 'shadowtls', tag: 'legacy-shadowtls', version: 2, users: [] },
      { id: 4, type: 'socks', tag: 'no-membership' },
    ])).toEqual([])
  })

  it('ignores malformed catalog entries and invalid metadata', () => {
    expect(getClientInboundOptions([
      null,
      'not-an-inbound',
      { id: '5', type: 'vless', tag: 'string-id', users: [] },
      { id: 6, type: 'vless', tag: '  ', users: [] },
      { id: 7, type: 'vless', tag: 'bad-users', users: {} },
      { id: 8, type: 'shadowsocks', tag: 'bad-managed', managed: 'false', users: [] },
      { id: 9, type: 'shadowtls', tag: 'bad-version', version: '3', users: [] },
    ])).toEqual([])
    expect(getClientInboundOptions({ id: 1, type: 'vless', tag: 'not-an-array', users: [] })).toEqual([])
  })
})
