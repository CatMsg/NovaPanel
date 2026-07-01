export function buildMasqueConfig(endpoint: any): string {
  const yamlStr = (value: any) => JSON.stringify(String(value ?? ''))
  const network = String(endpoint.network ?? 'quic')
  const fields = [
    `name: ${yamlStr(endpoint.tag ?? 'masque')}`,
    `type: masque`,
    `server: ${yamlStr(endpoint.server ?? '')}`,
    `port: ${endpoint.port ?? 443}`,
    `network: ${yamlStr(network)}`,
    `private-key: ${yamlStr(endpoint.private_key ?? '')}`,
    `public-key: ${yamlStr(endpoint.public_key ?? '')}`,
  ]
  if (endpoint.ip) {
    fields.push(`ip: ${yamlStr(endpoint.ip)}`)
  }
  if (endpoint.mtu) {
    fields.push(`mtu: ${endpoint.mtu}`)
  }
  if (endpoint.udp !== undefined) {
    fields.push(`udp: ${endpoint.udp ? 'true' : 'false'}`)
  }
  return `- { ${fields.join(', ')} }`
}
