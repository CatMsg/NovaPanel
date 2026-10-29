import { describe, expect, it } from 'vitest'
import { iTls, normalizeLegacyAcme } from './tls'

describe('normalizeLegacyAcme', () => {
  it('moves legacy ACME fields into the inline certificate provider without changing options', () => {
    const source: iTls = {
      enabled: true,
      server_name: 'edge.example',
      acme: {
        domain: ['edge.example', 'alt.example'],
        data_directory: '/var/lib/novas/acme',
        email: 'ops@example.com',
        provider: 'https://acme.example/directory',
        external_account: { key_id: 'account-id', mac_key: 'account-key' },
      },
    }

    const normalized = normalizeLegacyAcme(source)

    expect(normalized.acme).toBeUndefined()
    expect(normalized.certificate_provider).toEqual({
      type: 'acme',
      domain: ['edge.example', 'alt.example'],
      data_directory: '/var/lib/novas/acme',
      email: 'ops@example.com',
      provider: 'https://acme.example/directory',
      external_account: { key_id: 'account-id', mac_key: 'account-key' },
    })
    expect(source.acme?.domain).toEqual(['edge.example', 'alt.example'])
  })

  it('normalizes the sing-box listable single-domain form', () => {
    const normalized = normalizeLegacyAcme({ acme: { domain: 'edge.example' } })
    expect(normalized.certificate_provider).toEqual({ type: 'acme', domain: ['edge.example'] })
  })

  it('keeps an incomplete legacy draft editable without throwing', () => {
    const normalized = normalizeLegacyAcme({ acme: {} as iTls['acme'] })
    expect(normalized.certificate_provider).toEqual({ type: 'acme', domain: [] })
  })

  it('does not overwrite a modern provider or silently resolve a conflict', () => {
    const provider = { type: 'acme', domain: ['edge.example'] }
    expect(normalizeLegacyAcme({ certificate_provider: provider }).certificate_provider).toEqual(provider)

    const conflict = normalizeLegacyAcme({
      acme: { domain: ['legacy.example'] },
      certificate_provider: provider,
    })
    expect(conflict.acme?.domain).toEqual(['legacy.example'])
    expect(conflict.certificate_provider).toEqual(provider)
  })
})
