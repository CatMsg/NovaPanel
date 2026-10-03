import axios, { type AxiosRequestConfig, type AxiosResponse } from 'axios'

const api = axios.create({ baseURL: './' })
const pendingReads = new Map<string, Promise<AxiosResponse>>()
const sharedReadPaths = new Set(['api/load', 'api/clients', 'api/inbounds'])

api.defaults.headers.post['Content-Type'] = 'application/x-www-form-urlencoded; charset=UTF-8'
api.defaults.headers.common['X-Requested-With'] = 'XMLHttpRequest'

api.interceptors.request.use(config => {
    // A write must neither cancel another write nor reuse a read started before it.
    if (!['get', 'head'].includes(config.method ?? 'get')) pendingReads.clear()
    if (config.data instanceof FormData) config.headers['Content-Type'] = 'multipart/form-data'
    return config
}, undefined, { synchronous: true })

export function readApi(url: string, config: AxiosRequestConfig = {}): Promise<AxiosResponse> {
    // Only ordinary data reads share an in-flight response. Caller-specific cancellation,
    // headers, adapters and action-like GET endpoints keep independent request lifetimes.
    if (!sharedReadPaths.has(url) || Object.keys(config).some(key => key !== 'params')) {
        return api.get(url, config)
    }
    const key = api.getUri({ ...config, url })
    const existing = pendingReads.get(key)
    if (existing) return existing

    const request = api.get(url, config).finally(() => {
        if (pendingReads.get(key) === request) pendingReads.delete(key)
    })
    pendingReads.set(key, request)
    return request
}

export default api
