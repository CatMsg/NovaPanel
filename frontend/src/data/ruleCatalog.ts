export interface RuleCatalogAsset {
  tag: string
  url: string
}

export interface RuleCatalogItem {
  id: string
  name: string
  description: string
  icon: string
  assets: RuleCatalogAsset[]
  directRule?: Record<string, unknown>
  suggestedAction?: 'route' | 'reject'
}

const geosite = (name: string): RuleCatalogAsset => ({
  tag: `geosite-${name}`,
  url: `https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-${name}.srs`,
})

export const ruleCatalog: RuleCatalogItem[] = [
  { id: 'netflix', name: 'Netflix', description: 'Netflix 站点与流媒体域名', icon: 'mdi-netflix', assets: [geosite('netflix')] },
  { id: 'disney', name: 'Disney+', description: 'Disney+ 服务域名', icon: 'mdi-movie-open-star-outline', assets: [geosite('disney')] },
  { id: 'youtube', name: 'YouTube', description: 'YouTube 与 Google Video 域名', icon: 'mdi-youtube', assets: [geosite('youtube')] },
  { id: 'openai', name: 'OpenAI', description: 'OpenAI、ChatGPT 相关域名', icon: 'mdi-creation-outline', assets: [geosite('openai')] },
  { id: 'google', name: 'Google', description: 'Google 服务域名', icon: 'mdi-google', assets: [geosite('google')] },
  { id: 'telegram', name: 'Telegram', description: 'Telegram 服务域名', icon: 'mdi-send', assets: [geosite('telegram')] },
  {
    id: 'china',
    name: '中国大陆',
    description: '中国大陆域名与 IPv4/IPv6 地址段',
    icon: 'mdi-map-marker-radius-outline',
    assets: [
      geosite('cn'),
      { tag: 'geoip-cn', url: 'https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs' },
    ],
  },
  {
    id: 'private',
    name: '私有网络',
    description: 'RFC 私有地址，不依赖远程规则集',
    icon: 'mdi-lan',
    assets: [],
    directRule: { ip_is_private: true },
    suggestedAction: 'route',
  },
  {
    id: 'ads',
    name: '广告域名',
    description: '常见广告与跟踪域名，建议拒绝',
    icon: 'mdi-shield-off-outline',
    assets: [geosite('category-ads-all')],
    suggestedAction: 'reject',
  },
]
