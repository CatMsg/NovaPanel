import { afterEach, describe, expect, it, vi } from 'vitest'
import { randomConfigs, shuffleConfigs } from './clients'

describe('MASQUE credential revocation', () => {
  afterEach(() => vi.unstubAllGlobals())
  it('rotates native password and removes all old Mihomo keys', () => {
    vi.stubGlobal('window', { crypto: globalThis.crypto })
    const configs = {
      masque: { username: 'alice', password: 'old-test-password', mihomo: { '1': { private_key: 'old-test-key', ip: '172.16.0.2/32' } } },
      socks: { username: 'other', password: 'untouched' },
    }
    shuffleConfigs(configs, 'masque')
    expect(configs.masque.username).toBe('alice')
    expect(configs.masque.password).not.toBe('old-test-password')
    expect(configs.masque.password.length).toBeGreaterThanOrEqual(40)
    expect(configs.masque).not.toHaveProperty('mihomo')
    expect(configs.socks.password).toBe('untouched')
  })
})

describe('Snell shared-PSK compatibility', () => {
  afterEach(() => vi.unstubAllGlobals())
  it('does not generate per-user authentication credentials', () => {
    vi.stubGlobal('window', { crypto: globalThis.crypto })
    const configs = randomConfigs('alice')
    expect(configs).not.toHaveProperty('snell')
  })
})
