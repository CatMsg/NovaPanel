import { describe, expect, it } from 'vitest'
import { waitForFleetUpdate } from './fleetUpdate'

describe('fleet update polling', () => {
  it('ignores a stale terminal result until the requested run appears', async () => {
    const results = [
      { success: true, obj: { state: 'success', updatedAt: '2026-11-01T00:00:00Z' } },
      { success: true, obj: { state: 'running', runId: 'new-run' } },
      { success: true, obj: { state: 'success', runId: 'new-run' } },
    ]
    const result = await waitForFleetUpdate(async () => results.shift() ?? { success: false }, {
      runId: 'new-run',
      intervalMs: 0,
      sleep: async () => {},
    })

    expect(result).toEqual({ status: { state: 'success', runId: 'new-run' }, timedOut: false })
  })

  it('recognizes failures from older servers by a new status timestamp', async () => {
    const result = await waitForFleetUpdate(async () => ({
      success: true,
      obj: { state: 'failed', updatedAt: '2026-11-08T10:00:01Z', message: 'asset download failed' },
    }), {
      baselineUpdatedAt: '2026-11-08T10:00:00Z',
      intervalMs: 0,
      sleep: async () => {},
    })

    expect(result.status?.state).toBe('failed')
    expect(result.timedOut).toBe(false)
  })

  it('recognizes a first-run failure when there was no prior status timestamp', async () => {
    const result = await waitForFleetUpdate(async () => ({
      success: true,
      obj: { state: 'failed', updatedAt: '2026-11-08T10:00:01Z', message: 'asset download failed' },
    }), {
      intervalMs: 0,
      sleep: async () => {},
    })

    expect(result.status?.state).toBe('failed')
    expect(result.timedOut).toBe(false)
  })

  it('waits for a task already running when the user starts the batch', async () => {
    const results = [
      { success: true, obj: { state: 'running' } },
      { success: true, obj: { state: 'success' } },
    ]
    const result = await waitForFleetUpdate(async () => results.shift() ?? { success: false }, {
      alreadyRunning: true,
      intervalMs: 0,
      sleep: async () => {},
    })

    expect(result.status?.state).toBe('success')
    expect(result.timedOut).toBe(false)
  })
})
