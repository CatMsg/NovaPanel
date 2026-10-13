import { describe, expect, it } from 'vitest'
import { createInbound, type Masque } from './inbounds'

describe('inbound subscription visibility', () => {
  it('defaults new inbounds to visible', () => {
    expect(createInbound('socks').exclude_from_sub).toBe(false)
  })

  it('preserves an explicit hide setting', () => {
    expect(createInbound('socks', { exclude_from_sub: true }).exclude_from_sub).toBe(true)
  })

  it('defaults new MASQUE inbounds to sing-box and Mihomo compatibility', () => {
    expect((createInbound('masque') as Masque).mihomo_compatible).toBe(true)
  })

  it('preserves an explicit native-only MASQUE setting', () => {
    expect((createInbound('masque', { mihomo_compatible: false }) as Masque).mihomo_compatible).toBe(false)
  })
})
