import { describe, expect, it } from 'vitest'
import {
  trafficBudgetCap,
  trafficBudgetPercent,
  trafficBudgetReadable,
  trafficHistorySeries,
  type FleetTrafficBudget,
} from './trafficBudget'

const budget = (overrides: Partial<FleetTrafficBudget> = {}): FleetTrafficBudget => ({
  enabled: true,
  supported: true,
  limitBytes: 500,
  reserveBytes: 50,
  clientPoolBytes: 450,
  usedBytes: 225,
  meteredRxBytes: 100,
  meteredTxBytes: 225,
  offsetBytes: 0,
  accountingMode: 'tx',
  level: 'normal',
  blocked: false,
  ...overrides,
})

describe('traffic budget helpers', () => {
  it('uses the client pool as the effective cap and falls back for legacy servers', () => {
    expect(trafficBudgetCap(budget())).toBe(450)
    expect(trafficBudgetCap(budget({ clientPoolBytes: undefined }))).toBe(500)
  })

  it('calculates a clamped percentage and rejects unreadable states', () => {
    expect(trafficBudgetPercent(budget())).toBe(50)
    expect(trafficBudgetPercent(budget({ usedBytes: 900 }))).toBe(100)
    expect(trafficBudgetReadable(budget())).toBe(true)
    expect(trafficBudgetReadable(budget({ level: 'error', error: 'counter unavailable' }))).toBe(false)
  })

  it('sorts history samples before building chart series', () => {
    const series = trafficHistorySeries({
      samples: [
        { dateTime: 20, periodStart: 1, meteredRxBytes: 4, meteredTxBytes: 8, usedBytes: 8, clientPoolBytes: 100, level: 'normal' },
        { dateTime: 10, periodStart: 1, meteredRxBytes: 2, meteredTxBytes: 3, usedBytes: 3, clientPoolBytes: 100, level: 'normal' },
      ],
    })
    expect(series.labels).toEqual([10000, 20000])
    expect(series.used).toEqual([3, 8])
    expect(series.rx).toEqual([2, 4])
    expect(series.tx).toEqual([3, 8])
  })
})
