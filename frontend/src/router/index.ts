// Composables
import { createRouter, createWebHistory } from 'vue-router'
import Login from '@/views/Login.vue'
import Data from '@/store/modules/data'
import api from '@/plugins/api'
import { isPageVisible, onPageVisibilityChange } from '@/utils/pageVisibility'

const routes = [
  {
    path: '/login',
    name: 'pages.login',
    component: Login,
  },
  {
    path: '/',
    component: () => import('@/layouts/default/Default.vue'),
    meta: { requiresAuth: true },
    children: [
      {
        path: '/',
        name: 'pages.home',
        component: () => import('@/views/Home.vue'),
        meta: { dataRefreshInterval: 15000 },
      },
      {
        path: '/ports',
        name: 'pages.ports',
        component: () => import('@/views/Ports.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
      {
        path: '/fleet',
        name: 'pages.fleet',
        component: () => import('@/views/Fleet.vue'),
        meta: { dataRefreshInterval: 0 },
      },
      {
        path: '/inbounds',
        name: 'pages.inbounds',
        component: () => import('@/views/Inbounds.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
      {
        path: '/clients',
        name: 'pages.clients',
        component: () => import('@/views/Clients.vue'),
        meta: { dataRefreshInterval: 15000 },
      },  
      {
        path: '/outbounds',
        name: 'pages.outbounds',
        component: () => import('@/views/Outbounds.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
      {
        path: '/services',
        name: 'pages.services',
        component: () => import('@/views/Services.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
      {
        path: '/endpoints',
        name: 'pages.endpoints',
        component: () => import('@/views/Endpoints.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
      {
        path: '/rules',
        name: 'pages.rules',
        component: () => import('@/views/Rules.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
      {
        path: '/tls',
        name: 'pages.tls',
        component: () => import('@/views/Tls.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
      {
        path: '/basics',
        name: 'pages.basics',
        component: () => import('@/views/Basics.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
      {
        path: '/dns',
        name: 'pages.dns',
        component: () => import('@/views/Dns.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
      {
        path: '/admins',
        name: 'pages.admins',
        component: () => import('@/views/Admins.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
      {
        path: '/settings',
        name: 'pages.settings',
        component: () => import('@/views/Settings.vue'),
        meta: { dataRefreshInterval: 30000 },
      },
    ],
  },
]

const router = createRouter({
  history: createWebHistory((window as any).BASE_URL),
  routes,
})

const DEFAULT_TITLE = 'NovaPanel'
let intervalId: ReturnType<typeof setInterval> | undefined
let authCheckPromise: Promise<boolean> | null = null
let currentIntervalMs: number | null = null
let dataLoadInFlight = false
let stopVisibilityListener: (() => void) | null = null

const checkAuth = async () => {
  if (!authCheckPromise) {
    authCheckPromise = api.get('api/checkLogin')
      .then((resp) => resp.data?.success === true)
      .catch(() => false)
      .finally(() => {
        authCheckPromise = null
      })
  }
  return authCheckPromise
}

const stopLoadDataInterval = () => {
  if (intervalId) {
    clearInterval(intervalId)
    intervalId = undefined
  }
  currentIntervalMs = null
}

const loadDataTick = async (force = false) => {
  if (!force && !isPageVisible()) {
    return
  }
  if (dataLoadInFlight) {
    return
  }

  dataLoadInFlight = true
  try {
    await Data().loadData()
  } finally {
    dataLoadInFlight = false
  }
}

const ensureVisibilityListener = () => {
  if (stopVisibilityListener) return
  stopVisibilityListener = onPageVisibilityChange((visible) => {
    if (visible && currentIntervalMs !== null) {
      void loadDataTick(true)
    }
  })
}

const getRouteRefreshInterval = (to: any) => {
  if (!to?.meta?.requiresAuth || to.path === '/login') {
    return null
  }
  const routeInterval = Number(to.meta?.dataRefreshInterval)
  if (Number.isFinite(routeInterval) && routeInterval > 0) {
    return routeInterval
  }
  return 30000
}

const syncLoadDataInterval = async (to: any) => {
  const nextInterval = getRouteRefreshInterval(to)
  if (nextInterval === null) {
    stopLoadDataInterval()
    return
  }

  ensureVisibilityListener()
  await loadDataTick(true)

  if (intervalId && currentIntervalMs === nextInterval) {
    return
  }

  stopLoadDataInterval()
  currentIntervalMs = nextInterval
  intervalId = setInterval(() => {
    void loadDataTick()
  }, nextInterval)
}

// Navigation guard to check authentication state
router.beforeEach(async (to) => {
  const isAuthenticated = await checkAuth()

  // If the route requires authentication and the user is not authenticated, redirect to /login
  if (to.meta.requiresAuth && !isAuthenticated) {
    return '/login'
  }
  if (to.path === '/login' && isAuthenticated) {
    // If already authenticated and visiting /login, redirect to '/'
    return '/'
  }

  if (!isAuthenticated || to.path === '/login') {
    stopLoadDataInterval()
    return
  }

  void syncLoadDataInterval(to)
})

export default router
