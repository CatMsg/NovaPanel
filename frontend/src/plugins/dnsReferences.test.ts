import { describe, expect, it, vi } from 'vitest'
import { push } from 'notivue'
import { findDnsTagReferences, renameDnsTagReferences } from './dnsReferences'

vi.mock('notivue', () => ({ push: { error: vi.fn() } }))
vi.mock('@/locales', () => ({
  i18n: {
    global: {
      t: (key: string, values?: Record<string, string>) => `${key} ${JSON.stringify(values ?? {})}`,
    },
  },
}))

describe('findDnsTagReferences', () => {
  it('finds the default DNS and recursively nested DNS and route resolve rules', () => {
    const config = {
      dns: {
        final: 'dns-main',
        servers: [{ tag: 'dns-main', server: 'dns-main' }],
        rules: [{
          type: 'logical',
          action: 'evaluate',
          rules: [
            { action: 'route', server: 'dns-main' },
            {
              type: 'logical',
              action: 'evaluate',
              rules: [{ action: 'route', server: 'dns-main' }],
            },
          ],
        }, { action: 'reject', server: 'dns-main' }],
      },
      route: {
        rules: [{
          type: 'logical',
          action: 'route',
          server: 'dns-main',
          rules: [{ action: 'resolve', server: 'dns-main' }],
        }],
      },
    }

    expect(findDnsTagReferences(config, {}, 'dns-main')).toEqual([
      { kind: 'default-dns' },
      { kind: 'dns-rule', indexPath: [1, 1] },
      { kind: 'dns-rule', indexPath: [1, 2, 1] },
      { kind: 'route-resolve-rule', indexPath: [1, 1] },
    ])
  })

  it('recognizes only named resolver fields and their object server values', () => {
    const config = {
      route: {
        default_domain_resolver: { server: 'dns-main', strategy: 'prefer_ipv4', disable_cache: true },
        rules: [{ action: 'route', server: 'dns-main' }],
      },
      dns: {
        servers: [{ tag: 'dns-main', server: 'dns-main' }],
      },
      ntp: { domain_resolver: 'dns-main' },
      http_clients: [{
        tag: 'client',
        domain_resolver: 'dns-main',
        server: 'dns-main',
      }],
      certificate_providers: [{ stun_domain_resolver: 'dns-main' }],
      arbitrary: { server: 'dns-main' },
    }

    expect(findDnsTagReferences(config, {}, 'dns-main')).toEqual([
      { kind: 'config-resolver', field: 'default_domain_resolver', path: 'route.default_domain_resolver.server' },
      { kind: 'config-resolver', field: 'domain_resolver', path: 'ntp.domain_resolver' },
      { kind: 'config-resolver', field: 'domain_resolver', path: 'http_clients[0].domain_resolver' },
      { kind: 'config-resolver', field: 'stun_domain_resolver', path: 'certificate_providers[0].stun_domain_resolver' },
    ])
  })

  it('finds resolver dependencies in each separately persisted resource store', () => {
    const stores = {
      inbounds: [{ tag: 'in-main', domain_resolver: 'dns-main' }],
      outbounds: [{ tag: 'out-main', domain_resolver: { server: 'dns-main', strategy: 'prefer_ipv6' } }],
      endpoints: [{ tag: 'endpoint-main', stun_domain_resolver: 'dns-main' }],
      services: [{ tag: 'service-main', default_domain_resolver: 'dns-main' }],
    }

    expect(findDnsTagReferences(null, stores, 'dns-main')).toEqual([
      { kind: 'external-resolver', store: 'inbounds', tag: 'in-main', field: 'domain_resolver', path: 'domain_resolver' },
      { kind: 'external-resolver', store: 'outbounds', tag: 'out-main', field: 'domain_resolver', path: 'domain_resolver.server' },
      { kind: 'external-resolver', store: 'endpoints', tag: 'endpoint-main', field: 'stun_domain_resolver', path: 'stun_domain_resolver' },
      { kind: 'external-resolver', store: 'services', tag: 'service-main', field: 'default_domain_resolver', path: 'default_domain_resolver' },
    ])
  })

  it('does not treat remote server addresses or arbitrary server fields as DNS references', () => {
    const config = {
      dns: { servers: [{ tag: 'dns-main', server: 'dns-main' }] },
      route: { rules: [{ action: 'route', server: 'dns-main' }] },
      unrelated: { server: 'dns-main' },
    }
    const stores = {
      outbounds: [{ tag: 'remote', server: 'dns-main' }],
    }

    expect(findDnsTagReferences(config, stores, 'dns-main')).toEqual([])
  })

  it('visits named handshakes as options, not as domain-map field names', () => {
    const config = {
      inbounds: [{
        tag: 'shadow',
        handshake_for_server_name: {
          'edge.example.com': { server: 'edge.example.com', domain_resolver: { server: 'dns-main', strategy: 'ipv4_only' } },
          domain_resolver: { server: 'dns-main' },
        },
      }],
    }
    expect(findDnsTagReferences(config, {}, 'dns-main')).toEqual([{
      kind: 'config-resolver', field: 'domain_resolver',
      path: 'inbounds[0].handshake_for_server_name["edge.example.com"].domain_resolver.server',
    }])
    expect(renameDnsTagReferences(config, 'dns-main', 'dns-new')).toBe(1)
    expect(config.inbounds[0].handshake_for_server_name['edge.example.com'].domain_resolver).toEqual({ server: 'dns-new', strategy: 'ipv4_only' })
    expect(config.inbounds[0].handshake_for_server_name.domain_resolver.server).toBe('dns-main')
  })

  it('finds DNS tag-map references in DNS and route rules, including unknown imported tags', () => {
    const config = {
      dns: {
        rules: [{
          action: 'reject',
          dns_server_address: { 'dns-missing': '192.0.2.0/24' },
          dns_search_domain: { 'dns-other': ['corp.example'] },
        }],
      },
      route: {
        rules: [{
          action: 'route',
          dns_search_domain: { 'dns-missing': ['corp.example', 'internal.example'] },
          dns_server_address: { 'dns-other': '2001:db8::/32' },
        }],
      },
    }

    expect(findDnsTagReferences(config, {}, 'dns-missing')).toEqual([
      { kind: 'dns-rule', indexPath: [1] },
      { kind: 'route-resolve-rule', indexPath: [1] },
    ])
  })
})

describe('renameDnsTagReferences', () => {
  it('migrates supported references without changing unrelated servers or resolver options', () => {
    const config = {
      dns: {
        final: 'dns-old',
        rules: [{ type: 'logical', rules: [{ action: 'route', server: 'dns-old' }] }],
        servers: [{ tag: 'dns-old', server: 'dns-old' }],
      },
      route: {
        default_domain_resolver: { server: 'dns-old', strategy: 'prefer_ipv4', disable_cache: true },
        rules: [{
          type: 'logical',
          rules: [
            { action: 'resolve', server: 'dns-old' },
            { action: 'route', server: 'dns-old' },
          ],
        }],
      },
      http_clients: [{ tag: 'client', domain_resolver: 'dns-old', server: 'dns-old' }],
    }

    expect(renameDnsTagReferences(config, 'dns-old', 'dns-new')).toBe(5)
    expect(config).toEqual({
      dns: {
        final: 'dns-new',
        rules: [{ type: 'logical', rules: [{ action: 'route', server: 'dns-new' }] }],
        servers: [{ tag: 'dns-old', server: 'dns-old' }],
      },
      route: {
        default_domain_resolver: { server: 'dns-new', strategy: 'prefer_ipv4', disable_cache: true },
        rules: [{
          type: 'logical',
          rules: [
            { action: 'resolve', server: 'dns-new' },
            { action: 'route', server: 'dns-old' },
          ],
        }],
      },
      http_clients: [{ tag: 'client', domain_resolver: 'dns-new', server: 'dns-old' }],
    })
  })

  it('ignores resolver-looking keys inside headers and hosts predefined maps', () => {
    const config = {
      dns: {
        servers: [{
          tag: 'dns-main',
          type: 'hosts',
          predefined: { domain_resolver: 'dns-main', nested: { stun_domain_resolver: 'dns-main' } },
        }],
      },
      http_clients: [{
        tag: 'client',
        headers: { default_domain_resolver: 'dns-main' },
      }],
    }

    expect(findDnsTagReferences(config, {}, 'dns-main')).toEqual([])
    expect(renameDnsTagReferences(config, 'dns-main', 'dns-new')).toBe(0)
    expect(config.dns.servers[0].predefined).toEqual({
      domain_resolver: 'dns-main',
      nested: { stun_domain_resolver: 'dns-main' },
    })
    expect(config.http_clients[0].headers.default_domain_resolver).toBe('dns-main')
  })

  it('renames only matching DNS tag-map keys and preserves scalar, array and unrelated values', () => {
    const config = {
      dns: {
        rules: [{
          action: 'reject',
          dns_server_address: { 'dns-old': '192.0.2.1', 'dns-other': ['192.0.2.2'] },
          dns_search_domain: { 'dns-other': 'corp.example' },
        }],
      },
      route: {
        rules: [{
          action: 'route',
          dns_search_domain: { 'dns-old': ['corp.example', 'internal.example'] },
          dns_server_address: { 'dns-other': '2001:db8::1' },
        }],
      },
    }

    expect(renameDnsTagReferences(config, 'dns-old', 'dns-new')).toBe(2)
    expect(config.dns.rules[0].dns_server_address).toEqual({
      'dns-new': '192.0.2.1',
      'dns-other': ['192.0.2.2'],
    })
    expect(config.dns.rules[0].dns_search_domain).toEqual({ 'dns-other': 'corp.example' })
    expect(config.route.rules[0].dns_search_domain).toEqual({
      'dns-new': ['corp.example', 'internal.example'],
    })
    expect(config.route.rules[0].dns_server_address).toEqual({ 'dns-other': '2001:db8::1' })
  })

  it('blocks a tag-map key collision without merging values or partially renaming references', () => {
    const config = {
      dns: {
        final: 'dns-old',
        rules: [
          { action: 'route', server: 'dns-old' },
          {
            action: 'reject',
            dns_search_domain: { 'dns-old': ['corp.example'], 'dns-new': ['internal.example'] },
          },
        ],
      },
      route: {
        rules: [{
          action: 'route',
          dns_server_address: { 'dns-old': '192.0.2.0/24', 'dns-new': ['2001:db8::/32'] },
        }],
      },
    }
    const before = structuredClone(config)

    expect(() => renameDnsTagReferences(config, 'dns-old', 'dns-new')).toThrow(/dns-old.*dns-new/)
    expect(config).toEqual(before)
    expect(push.error).toHaveBeenCalledWith(expect.objectContaining({
      title: expect.stringContaining('dns.references.renameBlockedTitle'),
      message: expect.stringContaining('dns.references.renameCollisionText'),
    }))
  })
})
