export type NavigationItem = {
  title: string
  icon: string
  path: string
}

export type NavigationGroup = {
  title: string
  items: NavigationItem[]
}

export const navigationGroups: NavigationGroup[] = [
  {
    title: 'ui.nav.overview',
    items: [
      { title: 'pages.home', icon: 'mdi-home', path: '/' },
      { title: 'pages.fleet', icon: 'mdi-server-network', path: '/fleet' },
      { title: 'pages.sessions', icon: 'mdi-lan', path: '/sessions' },
      { title: 'pages.health', icon: 'mdi-heart-pulse', path: '/health' },
    ],
  },
  {
    title: 'ui.nav.proxy',
    items: [
      { title: 'pages.inbounds', icon: 'mdi-cloud-download', path: '/inbounds' },
      { title: 'pages.clients', icon: 'mdi-account-multiple', path: '/clients' },
      { title: 'pages.outbounds', icon: 'mdi-cloud-upload', path: '/outbounds' },
      { title: 'pages.endpoints', icon: 'mdi-cloud-tags', path: '/endpoints' },
      { title: 'pages.services', icon: 'mdi-server', path: '/services' },
    ],
  },
  {
    title: 'ui.nav.network',
    items: [
      { title: 'pages.ports', icon: 'mdi-lan', path: '/ports' },
      { title: 'pages.rules', icon: 'mdi-routes', path: '/rules' },
      { title: 'pages.dns', icon: 'mdi-dns', path: '/dns' },
      { title: 'pages.tls', icon: 'mdi-certificate', path: '/tls' },
    ],
  },
  {
    title: 'ui.nav.system',
    items: [
      { title: 'pages.basics', icon: 'mdi-application-cog', path: '/basics' },
      { title: 'pages.admins', icon: 'mdi-account-tie', path: '/admins' },
      { title: 'pages.settings', icon: 'mdi-cog', path: '/settings' },
    ],
  },
]

export const navigationItems = navigationGroups.flatMap(group => group.items)
