import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import HttpUtils from '@/plugins/httputil'
import type { Msg } from '@/plugins/httputil'
import type DataStore from './data'

vi.mock('@/plugins/httputil', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

vi.mock('notivue', () => ({
  push: {
    error: vi.fn(),
    info: vi.fn(),
    warning: vi.fn(),
    success: vi.fn(),
  },
}))

vi.mock('@/locales', () => ({
  i18n: { global: { t: (key: string) => key } },
}))

const response = (obj: unknown): Msg => ({ success: true, msg: '', obj })

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => { resolve = resolvePromise })
  return { promise, resolve }
}

describe('data store load lifecycle', () => {
  let store: ReturnType<typeof DataStore>

  beforeEach(async () => {
    vi.clearAllMocks()
    vi.stubGlobal('localStorage', {
      getItem: vi.fn().mockReturnValue(null),
      setItem: vi.fn(),
    })
    setActivePinia(createPinia())
    const { default: useDataStore } = await import('./data')
    store = useDataStore()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('ignores a load that resolves after a successful partial save, then refreshes fully', async () => {
    store.lastLoad = 100
    const oldRead = deferred<Msg>()
    vi.mocked(HttpUtils.get).mockReturnValueOnce(oldRead.promise)
    vi.mocked(HttpUtils.post)
      .mockResolvedValueOnce(response({ changed: true }))
      .mockResolvedValueOnce(response({
        config: { source: 'save' },
        subURI: 'saved-link',
        lastUpdate: 200,
      }))

    const pendingLoad = store.loadData()
    await expect(store.save('clients', 'update', { id: 1 })).resolves.toBe(true)

    expect(store.config).toEqual({ source: 'save' })
    expect(store.subURI).toBe('saved-link')
    expect(store.lastLoad).toBe(100)

    oldRead.resolve(response({
      config: { source: 'stale-load' },
      subURI: 'stale-link',
      lastUpdate: 150,
    }))
    await pendingLoad

    expect(store.config).toEqual({ source: 'save' })
    expect(store.subURI).toBe('saved-link')
    expect(store.lastLoad).toBe(100)

    const fullRefresh = deferred<Msg>()
    vi.mocked(HttpUtils.get).mockReturnValueOnce(fullRefresh.promise)
    const refresh = store.loadData()
    expect(HttpUtils.get).toHaveBeenNthCalledWith(1, 'api/load', { lu: 100 })
    expect(HttpUtils.get).toHaveBeenNthCalledWith(2, 'api/load', {})
    fullRefresh.resolve(response({
      config: { source: 'full-refresh' },
      subURI: '',
      lastUpdate: 300,
    }))
    await refresh

    expect(store.config).toEqual({ source: 'full-refresh' })
    expect(store.subURI).toBe('')
    expect(store.lastLoad).toBe(300)
    expect(HttpUtils.post).toHaveBeenCalledTimes(2)
  })

  it('lets a newer read win when reads resolve out of order', async () => {
    const olderRead = deferred<Msg>()
    const newerRead = deferred<Msg>()
    vi.mocked(HttpUtils.get)
      .mockReturnValueOnce(olderRead.promise)
      .mockReturnValueOnce(newerRead.promise)

    const olderLoad = store.loadData()
    const newerLoad = store.loadData()
    newerRead.resolve(response({ config: { source: 'newer' }, lastUpdate: 200 }))
    await newerLoad
    olderRead.resolve(response({ config: { source: 'older' }, lastUpdate: 100 }))
    await olderLoad

    expect(store.config).toEqual({ source: 'newer' })
    expect(store.lastLoad).toBe(200)
  })

  it('accepts the server cursor from the current full snapshot after a version reset', async () => {
    store.lastLoad = 200
    vi.mocked(HttpUtils.get).mockResolvedValueOnce(response({
      onlines: { inbound: [], outbound: [], user: [] },
      lastUpdate: 300,
    }))
    await store.loadData()
    expect(store.lastLoad).toBe(200)

    vi.mocked(HttpUtils.get).mockResolvedValueOnce(response({
      config: { source: 'snapshot' },
      lastUpdate: 150,
    }))
    await store.loadData()
    expect(store.lastLoad).toBe(150)
  })

  it('ignores a late full snapshot from the previous server-version epoch', async () => {
    store.lastLoad = 200
    const previousEpoch = deferred<Msg>()
    const currentEpoch = deferred<Msg>()
    vi.mocked(HttpUtils.get)
      .mockReturnValueOnce(previousEpoch.promise)
      .mockReturnValueOnce(currentEpoch.promise)

    const olderLoad = store.loadData()
    const currentLoad = store.loadData()
    currentEpoch.resolve(response({ config: { snapshot: 'current' }, lastUpdate: 100 }))
    await currentLoad
    expect(store.lastLoad).toBe(100)

    previousEpoch.resolve(response({ config: { snapshot: 'previous' }, lastUpdate: 200 }))
    await olderLoad

    expect(store.config).toEqual({ snapshot: 'current' })
    expect(store.lastLoad).toBe(100)
  })

  it('treats a successful null payload as an empty response', async () => {
    const currentOnlines = { inbound: ['in-1'], outbound: ['out-1'], user: ['user-1'] }
    store.onlines = currentOnlines
    store.lastLoad = 200
    vi.mocked(HttpUtils.get).mockResolvedValueOnce(response(null))

    await expect(store.loadData()).resolves.toBeUndefined()

    expect(store.onlines).toEqual(currentOnlines)
    expect(store.lastLoad).toBe(200)
  })

  it('keeps full refresh required for successful payloads without a full snapshot', async () => {
    const currentOnlines = { inbound: ['in-1'], outbound: ['out-1'], user: ['user-1'] }
    store.onlines = currentOnlines
    store.lastLoad = 200
    store.setNewData({})
    vi.mocked(HttpUtils.get)
      .mockResolvedValueOnce(response({}))
      .mockResolvedValueOnce(response({ lastUpdate: 300 }))

    await store.loadData()
    await store.loadData()

    expect(HttpUtils.get).toHaveBeenNthCalledWith(1, 'api/load', {})
    expect(HttpUtils.get).toHaveBeenNthCalledWith(2, 'api/load', {})
    expect(store.onlines).toEqual(currentOnlines)
    expect(store.lastLoad).toBe(200)
  })

  it('distinguishes an omitted subscription URI from an explicit empty URI', () => {
    store.setNewData({ subURI: 'legacy-link' })
    store.setNewData({})
    expect(store.subURI).toBe('legacy-link')

    store.setNewData({ subURI: '' })
    expect(store.subURI).toBe('')
  })

  it('does not synthesize a server cursor from the client clock for partial data', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-08T00:00:00.000Z'))
    store.lastLoad = 1_791_417_600_000

    store.setNewData({})

    expect(store.lastLoad).toBe(1_791_417_600_000)
  })
})
