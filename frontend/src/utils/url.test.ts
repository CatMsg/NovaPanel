import { describe, expect, it } from 'vitest'
import { appendUrlPathSegment } from './url'

describe('appendUrlPathSegment', () => {
  it.each([
    ['trailing slash', 'https://panel.example/sub/', 'https://panel.example/sub/aggregate'],
    ['no trailing slash', 'https://panel.example/sub', 'https://panel.example/sub/aggregate'],
    ['query', 'https://panel.example/sub?key=value', 'https://panel.example/sub/aggregate?key=value'],
    ['fragment', 'https://panel.example/sub#settings', 'https://panel.example/sub/aggregate#settings'],
    ['root path', '/', '/aggregate'],
    ['blank base', '  ', ''],
  ])('%s', (_name, base, expected) => {
    expect(appendUrlPathSegment(base, 'aggregate')).toBe(expected)
  })
})
