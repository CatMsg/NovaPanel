import { describe, expect, it, vi } from 'vitest'
import { formatSystemStartupTime, formatSystemUptime, formatSystemValue } from './systemInfo'

vi.mock('@/plugins/utils', () => ({
  HumanReadable: { formatSecond: (seconds: number) => `${seconds}s` },
}))

describe('formatSystemUptime', () => {
  it('shows a placeholder when the boot time is absent or invalid', () => {
    expect(formatSystemUptime(undefined, 1000)).toBe('-')
    expect(formatSystemUptime(null, 1000)).toBe('-')
    expect(formatSystemUptime(0, 1000)).toBe('-')
    expect(formatSystemUptime(Number.NaN, 1000)).toBe('-')
  })

  it('shows a placeholder when the boot time is in the future', () => {
    expect(formatSystemUptime(1001, 1000)).toBe('-')
  })

  it('formats uptime from a valid boot time', () => {
    expect(formatSystemUptime(940, 1000)).toBe('60s')
  })

  it('does not invent a startup date when boot time is unavailable', () => {
    expect(formatSystemStartupTime(undefined)).toBe('-')
    expect(formatSystemStartupTime(0)).toBe('-')
  })

  it('formats a valid startup date', () => {
    expect(formatSystemStartupTime(1_700_000_000, 'en-US')).toBe(
      new Date(1_700_000_000_000).toLocaleString('en-US'),
    )
  })
})

describe('formatSystemValue', () => {
  it('replaces absent and unknown values with a placeholder', () => {
    expect(formatSystemValue(undefined)).toBe('-')
    expect(formatSystemValue(' unknown ')).toBe('-')
    expect(formatSystemValue('')).toBe('-')
  })

  it('preserves known values and custom fallbacks', () => {
    expect(formatSystemValue('UFW')).toBe('UFW')
    expect(formatSystemValue(8)).toBe('8')
    expect(formatSystemValue('unknown', 'None')).toBe('None')
  })
})
