import { afterEach, describe, expect, it, vi } from 'vitest'
import RandomUtil from './randomUtil'

describe('Reality short ID generation', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('creates four unique, full-length cryptographic IDs without an empty ID', () => {
    let call = 0
    vi.stubGlobal('window', {
      crypto: {
        getRandomValues(bytes: Uint8Array) {
          bytes.fill(++call)
          return bytes
        },
      },
    })

    const ids = RandomUtil.randomShortId()

    expect(ids).toHaveLength(4)
    expect(new Set(ids).size).toBe(4)
    expect(ids.every(id => /^[0-9a-f]{16}$/.test(id))).toBe(true)
    expect(ids).not.toContain('')
  })
})
