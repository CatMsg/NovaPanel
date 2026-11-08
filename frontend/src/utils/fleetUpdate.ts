type UpdateStatus = {
  state?: string
  message?: string
  updatedAt?: string
  runId?: string
  running?: boolean
}

type StatusResponse = { success: boolean; obj?: UpdateStatus }

type WaitOptions = {
  runId?: string
  baselineUpdatedAt?: string
  alreadyRunning?: boolean
  timeoutMs?: number
  intervalMs?: number
  sleep?: (ms: number) => Promise<void>
}

const sleep = (ms: number) => new Promise<void>((resolve) => window.setTimeout(resolve, ms))

export async function waitForFleetUpdate(
  fetchStatus: () => Promise<StatusResponse>,
  options: WaitOptions = {},
): Promise<{ status?: UpdateStatus; timedOut: boolean }> {
  const timeoutMs = options.timeoutMs ?? 10 * 60 * 1000
  const intervalMs = options.intervalMs ?? 2500
  const pause = options.sleep ?? sleep
  const deadline = Date.now() + timeoutMs
  let observedRun = Boolean(options.alreadyRunning)
  let latest: UpdateStatus | undefined

  while (Date.now() < deadline) {
    try {
      const response = await fetchStatus()
      if (response.success && response.obj) {
        latest = response.obj
        if (options.runId && latest.runId === options.runId) observedRun = true
        if (!options.runId) {
          if (latest.state === 'running' || latest.running) observedRun = true
          if (latest.updatedAt && latest.updatedAt !== options.baselineUpdatedAt) observedRun = true
        }
        if (observedRun && (latest.state === 'success' || latest.state === 'failed')) {
          return { status: latest, timedOut: false }
        }
      }
    } catch {
      // A core restart can briefly interrupt the status endpoint; keep polling.
    }
    await pause(Math.min(intervalMs, Math.max(0, deadline - Date.now())))
  }

  return { status: latest, timedOut: true }
}
