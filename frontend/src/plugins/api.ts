import axios from 'axios'

const api = axios.create()
const pendingRequests = new Map()

const stringifyRequestPart = (value: unknown) => {
    if (value instanceof FormData) {
        return '[form-data]'
    }
    try {
        return JSON.stringify(value ?? {})
    } catch {
        return String(value ?? '')
    }
}

const getRequestKey = (config: any) => {
    return [
        config?.method ?? 'get',
        config?.url ?? '',
        stringifyRequestPart(config?.params),
        stringifyRequestPart(config?.data),
    ].join(':')
}

api.defaults.headers.post['Content-Type'] = 'application/x-www-form-urlencoded; charset=UTF-8'
api.defaults.headers.common['X-Requested-With'] = 'XMLHttpRequest'
api.defaults.baseURL = './'

api.interceptors.request.use(
    (config) => {
        const requestKey = getRequestKey(config)

        if (pendingRequests.has(requestKey)) {
            const cancelSource = pendingRequests.get(requestKey)
            cancelSource.cancel('Duplicate request cancelled')
        }

        const cancelSource = axios.CancelToken.source()
        config.cancelToken = cancelSource.token

        pendingRequests.set(requestKey, cancelSource)

        if (config.data instanceof FormData) {
            config.headers['Content-Type'] = 'multipart/form-data'
        }
        return config
    },
    (error) => Promise.reject(error),
)

api.interceptors.response.use(
    (response) => {
        const requestKey = getRequestKey(response.config)
        pendingRequests.delete(requestKey)
        return response
    },
    (error) => {
        if (axios.isCancel(error)) {
            console.warn(error.message)
        } else if (error?.config) {
            const requestKey = getRequestKey(error.config)
            pendingRequests.delete(requestKey)
        }
        return Promise.reject(error)
    }
)

export default api
