export type ListableString = string | string[]
export type DnsTagConditionMap = Record<string, ListableString>

export interface DnsConditionServer {
  tag?: unknown
  type?: unknown
}

export const dnsConditionServerTypes = new Set([
  'local',
  'dhcp',
  'resolved',
  'tailscale',
  'openvpn',
  'openconnect',
])

export function dnsConditionServerOptions(servers: unknown, finalTag: unknown): unknown[] {
  const hasFinal = typeof finalTag === 'string' && finalTag.length > 0
  if (Array.isArray(servers)) {
    return servers.length === 0 && !hasFinal ? [{ tag: 'local', type: 'local' }] : servers
  }
  return servers == null && !hasFinal ? [{ tag: 'local', type: 'local' }] : []
}

export type DnsConditionTagStatus = 'supported' | 'unknown' | 'unsupported' | 'duplicate'

export function selectableDnsConditionServers(servers: unknown): Array<{ tag: string; type: string }> {
  if (!Array.isArray(servers)) return []
  const records = servers.filter((server): server is DnsConditionServer =>
    typeof server === 'object' && server !== null && !Array.isArray(server)
  )
  const tagCounts = new Map<string, number>()
  for (const server of records) {
    if (typeof server.tag === 'string' && server.tag.length > 0) {
      tagCounts.set(server.tag, (tagCounts.get(server.tag) ?? 0) + 1)
    }
  }
  return records.flatMap(server => {
    if (typeof server.tag !== 'string' || typeof server.type !== 'string' ||
      !dnsConditionServerTypes.has(server.type) || tagCounts.get(server.tag) !== 1) return []
    return [{ tag: server.tag, type: server.type }]
  })
}

export function dnsConditionTagStatus(
  tag: string,
  servers: unknown,
): DnsConditionTagStatus {
  if (!Array.isArray(servers)) return 'unknown'
  const matches = servers.filter((server): server is DnsConditionServer =>
    typeof server === 'object' && server !== null && !Array.isArray(server) && server.tag === tag
  )
  if (matches.length === 0) return 'unknown'
  if (matches.length > 1) return 'duplicate'
  return typeof matches[0].type === 'string' && dnsConditionServerTypes.has(matches[0].type)
    ? 'supported'
    : 'unsupported'
}

export function listableText(value: unknown): string {
  if (typeof value === 'string') return value
  if (Array.isArray(value)) return value.map(item => String(item)).join('\n')
  return value === undefined ? '' : JSON.stringify(value)
}

export function listableFromText(text: string, previous: unknown): ListableString {
  const values = text.split(/[\r\n,]/).map(value => value.trim()).filter(Boolean)
  if (Array.isArray(previous) || values.length > 1) return values
  return values[0] ?? ''
}
