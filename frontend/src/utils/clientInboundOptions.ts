export type ClientInboundOption = {
  title: string
  value: number
}

const clientInboundTypes = new Set([
  'mixed',
  'socks',
  'http',
  'shadowsocks',
  'vmess',
  'trojan',
  'naive',
  'hysteria',
  'shadowtls',
  'tuic',
  'hysteria2',
  'vless',
  'anytls',
  'mieru',
  'masque',
])

const hasOwn = (value: Record<string, unknown>, key: string): boolean =>
  Object.prototype.hasOwnProperty.call(value, key)

export function getClientInboundOptions(inbounds: unknown): ClientInboundOption[] {
  if (!Array.isArray(inbounds)) return []

  const options: ClientInboundOption[] = []
  for (const candidate of inbounds) {
    if (!candidate || typeof candidate !== 'object' || Array.isArray(candidate)) continue

    const inbound = candidate as Record<string, unknown>
    if (!hasOwn(inbound, 'users')) continue

    const { id, tag, type, users } = inbound
    if (typeof id !== 'number' || !Number.isSafeInteger(id) || id <= 0) continue
    if (typeof tag !== 'string' || tag.trim() === '') continue
    if (typeof type !== 'string' || !clientInboundTypes.has(type)) continue
    if (users !== null && (!Array.isArray(users) || !users.every(user => typeof user === 'string'))) continue

    if (type === 'shadowsocks' && hasOwn(inbound, 'managed')) {
      if (typeof inbound.managed !== 'boolean' || inbound.managed) continue
    }
    if (type === 'shadowtls' && hasOwn(inbound, 'version')) {
      if (typeof inbound.version !== 'number' || !Number.isInteger(inbound.version) || inbound.version < 3) continue
    }

    options.push({ title: tag, value: id })
  }

  return options
}
