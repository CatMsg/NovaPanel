import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import api, { readApi } from './api'

const originalAdapter = api.defaults.adapter

function installDeferredAdapter() {
  const requests: Array<{
    config: InternalAxiosRequestConfig
    resolve: (data?: unknown) => void
    reject: (error: Error) => void
  }> = []
  api.defaults.adapter = config => new Promise<AxiosResponse>((resolve, reject) => {
    requests.push({
      config,
      resolve: (data = {}) => resolve({ data, config, status: 200, statusText: 'OK', headers: {} }),
      reject,
    })
  })
  return requests
}

afterEach(() => { api.defaults.adapter = originalAdapter })

describe('API request lifetimes', () => {
  it('shares simultaneous data reads without cancelling either caller, then fetches fresh data', async () => {
    const requests = installDeferredAdapter()
    const first = readApi('api/clients', { params: { id: 17 } })
    const second = readApi('api/clients', { params: { id: 17 } })
    await vi.waitFor(() => expect(requests).toHaveLength(1))
    requests[0].resolve({ name: 'alice' })
    expect((await first).data).toEqual({ name: 'alice' })
    expect((await second).data).toEqual({ name: 'alice' })

    const refresh = readApi('api/clients', { params: { id: 17 } })
    await vi.waitFor(() => expect(requests).toHaveLength(2))
    requests[1].resolve({ name: 'updated' })
    expect((await refresh).data).toEqual({ name: 'updated' })
  })

  it('does not combine different users, caller-specific reads or action-like GETs', async () => {
    const requests = installDeferredAdapter()
    const reads = [
      readApi('api/clients', { params: { id: 17 } }),
      readApi('api/clients', { params: { id: 18 } }),
      readApi('api/clients', { params: { id: 17 }, signal: new AbortController().signal }),
      readApi('api/logout'), readApi('api/logout'),
    ]
    await vi.waitFor(() => expect(requests).toHaveLength(5))
    requests.forEach(request => request.resolve())
    await Promise.all(reads)
  })

  it('does not cancel concurrent POST writes, including distinct multipart uploads', async () => {
    const requests = installDeferredAdapter()
    const uploadA = new FormData()
    uploadA.append('file', 'backup-a')
    const uploadB = new FormData()
    uploadB.append('file', 'backup-b')
    const writes = [
      api.post('api/save', { object: 'clients', data: 'same' }),
      api.post('api/save', { object: 'clients', data: 'same' }),
      api.post('api/restore', uploadA), api.post('api/restore', uploadB),
    ]
    await vi.waitFor(() => expect(requests).toHaveLength(4))
    expect(requests.every(request => !request.config.cancelToken)).toBe(true)
    requests.forEach(request => request.resolve())
    const results = await Promise.allSettled(writes)
    expect(results.every(result => result.status === 'fulfilled')).toBe(true)
  })

  it('drops failed reads so a retry can recover', async () => {
    const requests = installDeferredAdapter()
    const first = readApi('api/inbounds')
    const second = readApi('api/inbounds')
    const outcomes = Promise.allSettled([first, second])
    await vi.waitFor(() => expect(requests).toHaveLength(1))
    requests[0].reject(new Error('offline'))
    expect((await outcomes).every(result => result.status === 'rejected')).toBe(true)
    const retry = readApi('api/inbounds')
    await vi.waitFor(() => expect(requests).toHaveLength(2))
    requests[1].resolve()
    await retry
  })

  it('a write invalidates sharing of older reads without aborting their consumers', async () => {
    const requests = installDeferredAdapter()
    const oldRead = readApi('api/load')
    await vi.waitFor(() => expect(requests).toHaveLength(1))
    const write = api.post('api/save', { object: 'clients' })
    const freshRead = readApi('api/load')
    await vi.waitFor(() => expect(requests).toHaveLength(3))
    requests[1].resolve()
    await write
    requests[0].resolve({ revision: 1 })
    await oldRead
    const secondFreshRead = readApi('api/load')
    expect(requests).toHaveLength(3)
    requests[2].resolve({ revision: 2 })
    expect((await freshRead).data).toEqual({ revision: 2 })
    expect((await secondFreshRead).data).toEqual({ revision: 2 })
  })
})
