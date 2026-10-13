import { describe, expect, it } from 'vitest'
import { createInbound } from './inbounds'

describe('inbound subscription visibility', () => {
  it('defaults new inbounds to visible', () => {
    expect(createInbound('socks').exclude_from_sub).toBe(false)
  })

  it('preserves an explicit hide setting', () => {
    expect(createInbound('socks', { exclude_from_sub: true }).exclude_from_sub).toBe(true)
  })
})
